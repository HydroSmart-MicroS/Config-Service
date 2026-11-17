package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTimelineKeepsNotificationInstancesAndSampleGaps(t *testing.T) {
	d := &Dashboard{}
	start := time.Date(2026, 11, 17, 15, 0, 0, 0, time.UTC)
	up := dashboardProbe{Status: "UP"}
	down := dashboardProbe{Status: "UNREACHABLE"}
	record := func(at time.Time, status string, primary, replica dashboardProbe) {
		summary := up
		if status == "DOWN" {
			summary = down
		}
		d.record(dashboardSnapshot{CheckedAt: at, Status: status, Services: []dashboardService{
			{ID: "notifications", Ready: summary, Live: summary, Instances: []dashboardService{
				{ID: "notifications-primary", Ready: primary, Live: primary},
				{ID: "notifications-replica", Ready: replica, Live: replica},
			}},
		}})
	}
	record(start.Add(-timelineSpan), "DOWN", down, down)
	record(start, "UP", up, up)
	record(start.Add(time.Second), "DEGRADED", down, up)
	record(start.Add(2*timelineStep), "DOWN", down, down)

	result := d.report(true, start.Add(2*timelineStep))
	expected := map[string]dashboardSeries{
		"system":                {States: "304", Available: 2, Observed: 3},
		"notifications":         {States: "304", Available: 2, Observed: 3},
		"notifications-primary": {States: "404", Available: 1, Observed: 3},
		"notifications-replica": {States: "204", Available: 2, Observed: 3},
	}
	for id, want := range expected {
		got := result.Timeline.Rows[id]
		want.States = strings.Repeat("0", timelineSize-3) + want.States
		if got != want {
			t.Errorf("%s: got %+v, want %+v", id, got, want)
		}
	}
	if len(d.samples) != 3 || d.report(false, start).Timeline != nil {
		t.Fatal("expired points kept or history included in the legacy response")
	}
}

func TestMonitorSharesProbesBetweenPanelRequests(t *testing.T) {
	var probes atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	d := &Dashboard{
		client:  upstream.Client(),
		targets: []dashboardTarget{{"gateway", "Gateway", upstream.URL, "/health", "/health", true}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Monitor(ctx)

	deadline := time.Now().Add(3 * time.Second)
	for d.report(false, time.Now()).CheckedAt.IsZero() {
		if time.Now().After(deadline) {
			t.Fatal("collector did not publish its initial sample")
		}
		time.Sleep(10 * time.Millisecond)
	}

	for i := 0; i < 8; i++ {
		response := httptest.NewRecorder()
		d.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/status/services?history=1", nil))
		var report dashboardReport
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &report) != nil || report.Timeline.Rows["gateway"].Available < 1 {
			t.Fatal("shared sample missing from status endpoint")
		}
	}
	if probes.Load() >= 8 {
		t.Fatal("a request triggered a new upstream probe")
	}
	for probes.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if probes.Load() < 2 {
		t.Fatal("collector did not sample again")
	}
}
