package health

import (
	"context"
	"time"
)

const (
	timelineStep = 30 * time.Second
	timelineSize = 30
	timelineSpan = timelineStep * timelineSize

	noSample    byte = '0'
	unconfirmed byte = '1'
	operational byte = '2'
	degraded    byte = '3'
	interrupted byte = '4'
)

type dashboardSample struct {
	at     time.Time
	states map[string]byte
}

type dashboardTimeline struct {
	Start       time.Time                  `json:"start"`
	StepSeconds int                        `json:"stepSeconds"`
	Rows        map[string]dashboardSeries `json:"rows"`
}

type dashboardSeries struct {
	States    string `json:"states"`
	Available int    `json:"available"`
	Observed  int    `json:"observed"`
}

type dashboardReport struct {
	dashboardSnapshot
	Timeline *dashboardTimeline `json:"timeline,omitempty"`
}

// Monitor shares one cycle of probes between all panel viewers.
func (d *Dashboard) Monitor(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		snapshot := d.snapshot(ctx)
		if ctx.Err() != nil {
			return
		}
		d.record(snapshot)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *Dashboard) record(snapshot dashboardSnapshot) {
	states := make(map[string]byte, len(snapshot.Services)+3)
	states["system"] = operational
	switch snapshot.Status {
	case "DEGRADED":
		states["system"] = degraded
	case "DOWN":
		states["system"] = interrupted
	}
	for _, service := range snapshot.Services {
		states[service.ID] = service.availability()
		for _, instance := range service.Instances {
			states[instance.ID] = instance.availability()
			if states[service.ID] == operational && !instance.healthy() {
				states[service.ID] = degraded
			}
		}
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.latest = snapshot
	cutoff := snapshot.CheckedAt.Add(-timelineSpan)
	expired := max(0, len(d.samples)-int(timelineSpan/time.Second))
	for expired < len(d.samples) && d.samples[expired].at.Before(cutoff) {
		expired++
	}
	if expired > 0 {
		n := copy(d.samples, d.samples[expired:])
		clear(d.samples[n:])
		d.samples = d.samples[:n]
	}
	d.samples = append(d.samples, dashboardSample{at: snapshot.CheckedAt, states: states})
}

func (service dashboardService) availability() byte {
	if service.healthy() {
		return operational
	}
	if service.Ready.Status == "UNKNOWN" || service.Live.Status == "UNKNOWN" {
		return unconfirmed
	}
	return interrupted
}

func (d *Dashboard) report(history bool, now time.Time) dashboardReport {
	d.mu.RLock()
	defer d.mu.RUnlock()

	report := dashboardReport{dashboardSnapshot: d.latest}
	if !history || d.latest.CheckedAt.IsZero() {
		return report
	}
	start := now.Truncate(timelineStep).Add(-timelineSpan + timelineStep)
	timeline := &dashboardTimeline{
		Start:       start,
		StepSeconds: int(timelineStep / time.Second),
		Rows:        make(map[string]dashboardSeries),
	}

	type row struct {
		states              [timelineSize]byte
		available, observed int
	}
	rows := map[string]*row{}
	for id := range d.samples[len(d.samples)-1].states {
		current := &row{}
		for i := range current.states {
			current.states[i] = noSample
		}
		rows[id] = current
	}
	for _, sample := range d.samples {
		if sample.at.Before(start) || sample.at.After(now) {
			continue
		}
		bucket := int(sample.at.Sub(start) / timelineStep)
		for id, state := range sample.states {
			current := rows[id]
			if current == nil {
				continue
			}
			if state > current.states[bucket] {
				current.states[bucket] = state
			}
			if state != unconfirmed {
				current.observed++
				if state == operational || state == degraded {
					current.available++
				}
			}
		}
	}
	for id, current := range rows {
		timeline.Rows[id] = dashboardSeries{
			States:    string(current.states[:]),
			Available: current.available,
			Observed:  current.observed,
		}
	}
	report.Timeline = timeline
	return report
}
