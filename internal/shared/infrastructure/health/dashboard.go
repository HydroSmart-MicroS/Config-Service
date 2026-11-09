package health

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"sync"
	"time"
)

//go:embed dashboard.html dashboard-login.html dashboard.css dashboard.js
var dashboardFiles embed.FS
var dashboardPages = template.Must(template.ParseFS(dashboardFiles, "dashboard.html", "dashboard-login.html"))

func renderStatusLogin(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = dashboardPages.ExecuteTemplate(w, "dashboard-login.html", message)
}

type dashboardTarget struct {
	ID, Name, URL, Ready, Live string
	Basic                      bool
}

type dashboardComponent struct {
	Status  string `json:"status"`
	Details struct {
		Type       string  `json:"type,omitempty"`
		DurationMS float64 `json:"durationMs,omitempty"`
	} `json:"details"`
}

type dashboardProbe struct {
	Status     string                        `json:"status"`
	HTTP       int                           `json:"httpStatus,omitempty"`
	DurationMS int64                         `json:"durationMs"`
	Message    string                        `json:"message,omitempty"`
	Components map[string]dashboardComponent `json:"components,omitempty"`
}

type dashboardService struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Endpoint string         `json:"endpoint"`
	Basic    bool           `json:"basic"`
	Ready    dashboardProbe `json:"readiness"`
	Live     dashboardProbe `json:"liveness"`
}

type dashboardSnapshot struct {
	Status    string             `json:"status"`
	CheckedAt time.Time          `json:"checkedAt"`
	Services  []dashboardService `json:"services"`
}

type Dashboard struct {
	client  *http.Client
	targets []dashboardTarget
	sessions *statusSessions
}

func NewDashboard() *Dashboard {
	return &Dashboard{
		client: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		targets: []dashboardTarget{
			{"identity", "Identity", "http://identity:8080", "/health/ready", "/health/live", false},
			{"properties", "Properties", "http://properties:8080", "/actuator/health/readiness", "/actuator/health/liveness", false},
			{"telemetry", "Telemetry · IoT", "http://telemetry:8080", "/health/ready", "/health/live", false},
			{"analytics", "Analytics", "http://analytics:8080", "/health/ready", "/health/live", false},
			{"reports", "Reports", "http://reports:8080", "/actuator/health/readiness", "/actuator/health/liveness", false},
			{"notifications", "Notifications", "http://notifications:8080", "/actuator/health/readiness", "/actuator/health/liveness", false},
			{"billing", "Billing", "http://billing:8080", "/actuator/health/readiness", "/actuator/health/liveness", false},
			{"config-service", "Config Service", "http://127.0.0.1:8080", "/health/ready", "/health/live", false},
			{"gateway", "API Gateway", "http://gateway:8080", "/health", "/health", true},
		},
	}
}

func (d *Dashboard) Register(mux *http.ServeMux) {
	mux.Handle("/{$}", d)
	mux.Handle("/status", d)
	mux.Handle("/status/", d)
}

func (d *Dashboard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	if d.sessions == nil && r.Header.Get("X-Hydrosmart-Status-Session") == "1" {
		http.Error(w, "Panel no disponible", http.StatusServiceUnavailable)
		return
	}
	if d.sessions != nil && d.sessions.handle(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path == "/status/services" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(d.snapshot(r.Context()))
		return
	}
	file, contentType := "dashboard.html", "text/html; charset=utf-8"
	switch r.URL.Path {
	case "/", "/status", "/status/":
		w.Header().Set("Content-Type", contentType)
		_ = dashboardPages.ExecuteTemplate(w, "dashboard.html", d.sessions != nil)
		return
	case "/status/dashboard.css":
		file, contentType = "dashboard.css", "text/css; charset=utf-8"
	case "/status/dashboard.js":
		file, contentType = "dashboard.js", "text/javascript; charset=utf-8"
	default:
		http.NotFound(w, r)
		return
	}
	body, err := dashboardFiles.ReadFile(file)
	if err != nil {
		http.Error(w, "Panel no disponible", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(body)
}

func (d *Dashboard) snapshot(ctx context.Context) dashboardSnapshot {
	result := dashboardSnapshot{Status: "UP", Services: make([]dashboardService, len(d.targets))}
	var services sync.WaitGroup
	for i, target := range d.targets {
		services.Add(1)
		go func() {
			defer services.Done()
			item := dashboardService{ID: target.ID, Name: target.Name, Endpoint: target.Ready, Basic: target.Basic}
			var live sync.WaitGroup
			if !target.Basic {
				live.Add(1)
				go func() { defer live.Done(); item.Live = d.probe(ctx, target.URL+target.Live, false) }()
			}
			item.Ready = d.probe(ctx, target.URL+target.Ready, target.Basic)
			live.Wait()
			if target.Basic {
				item.Live = item.Ready
			}
			result.Services[i] = item
		}()
	}
	services.Wait()
	for _, service := range result.Services {
		if service.Ready.Status != "UP" || service.Live.Status != "UP" {
			result.Status = "DOWN"
		}
	}
	result.CheckedAt = time.Now().UTC()
	return result
}

func (d *Dashboard) probe(ctx context.Context, url string, basic bool) (result dashboardProbe) {
	start := time.Now()
	defer func() { result.DurationMS = time.Since(start).Milliseconds() }()
	result.Status, result.Message = "UNREACHABLE", "No se pudo conectar o se agotó el tiempo de espera."
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	// Health uses HTTP so a broker outage cannot hide the state of the other dependencies.
	response, err := d.client.Do(req)
	if err != nil {
		return
	}
	defer response.Body.Close()
	result.HTTP = response.StatusCode
	result.Status, result.Message = "DOWN", fmt.Sprintf("El endpoint respondió HTTP %d.", response.StatusCode)
	if basic {
		if response.StatusCode == http.StatusOK {
			result.Status, result.Message = "UP", "Comprueba solo el proceso del Gateway; sus dependencias se muestran en los demás servicios."
		}
		return
	}
	var body struct {
		Status     string                        `json:"status"`
		Components map[string]dashboardComponent `json:"components"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&body); err != nil {
		if response.StatusCode == http.StatusOK {
			result.Status, result.Message = "UNKNOWN", "La respuesta no contiene un estado de salud válido."
		}
		return
	}
	result.Components = make(map[string]dashboardComponent)
	for _, name := range []string{"database", "rabbitmq", "mqtt", "configuration"} {
		if component, ok := body.Components[name]; ok {
			if component.Status != "UP" && component.Status != "DOWN" {
				component.Status = "UNKNOWN"
			}
			if component.Details.Type != "PostgreSQL" && component.Details.Type != "MongoDB" {
				component.Details.Type = ""
			}
			result.Components[name] = component
		}
	}
	if response.StatusCode == http.StatusOK && body.Status == "UP" {
		result.Status, result.Message = "UP", ""
		for _, component := range result.Components {
			if component.Status != "UP" {
				result.Status, result.Message = "DOWN", "Una dependencia no está disponible."
			}
		}
	} else if body.Status == "DOWN" {
		result.Message = "El servicio responde, pero una comprobación de salud ha fallado."
	} else if response.StatusCode == http.StatusOK {
		result.Status, result.Message = "UNKNOWN", "La respuesta no contiene un estado de salud válido."
	}
	return
}
