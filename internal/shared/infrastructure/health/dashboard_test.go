package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDashboardDistinguishesDependencyFailureFromStoppedService(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ready" {
			w.WriteHeader(503)
			_, _ = w.Write([]byte(`{"status":"DOWN","components":{"database":{"status":"UP","details":{"type":"PostgreSQL","password":"private-secret"}},"rabbitmq":{"status":"DOWN","details":{"message":"amqp://private-secret@host","durationMs":4}}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"UP"}`))
	}))
	defer server.Close()
	stopped := httptest.NewServer(http.NotFoundHandler())
	stopped.Close()
	dashboard := &Dashboard{client: &http.Client{Timeout: time.Second}, targets: []dashboardTarget{
		{"degraded", "Degraded", server.URL, "/ready", "/live", false},
		{"stopped", "Stopped", stopped.URL, "/ready", "/live", false},
	}}
	result := dashboard.snapshot(context.Background())
	if result.Status != "DOWN" || result.CheckedAt.IsZero() || len(result.Services) != 2 {
		t.Fatal("missing aggregate failure or timestamp", result)
	}
	degraded, stoppedService := result.Services[0], result.Services[1]
	if degraded.Ready.Status != "DOWN" || degraded.Ready.HTTP != 503 || degraded.Live.Status != "UP" || degraded.Ready.Components["database"].Status != "UP" || degraded.Ready.Components["rabbitmq"].Status != "DOWN" {
		t.Fatal("dependency failure was not distinguished from process failure", degraded)
	}
	if stoppedService.Ready.Status != "UNREACHABLE" || stoppedService.Live.Status != "UNREACHABLE" {
		t.Fatal("stopped service was not reported as unreachable", stoppedService)
	}
	body, _ := json.Marshal(result)
	if strings.Contains(string(body), "private-secret") || strings.Contains(string(body), "amqp://") {
		t.Fatal("dashboard forwarded raw upstream diagnostics")
	}
}

func TestDashboardRejectsInvalidHealthAndBoundsSlowProbes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bad":
			_, _ = w.Write([]byte(`<html>not health</html>`))
		case "/inconsistent":
			_, _ = w.Write([]byte(`{"status":"UP","components":{"database":{"status":"DOWN"}}}`))
		case "/slow":
			<-r.Context().Done()
		default:
			_, _ = w.Write([]byte(`{"status":"UP"}`))
		}
	}))
	defer server.Close()
	dashboard := &Dashboard{client: &http.Client{Timeout: 50 * time.Millisecond}, targets: []dashboardTarget{
		{"invalid", "Invalid", server.URL, "/bad", "/live", false},
		{"inconsistent", "Inconsistent", server.URL, "/inconsistent", "/live", false},
		{"slow", "Slow", server.URL, "/slow", "/slow", false},
		{"good", "Good", server.URL, "/ready", "/live", false},
	}}
	result := dashboard.snapshot(context.Background())
	for i, want := range []string{"UNKNOWN", "DOWN", "UNREACHABLE", "UP"} {
		if result.Services[i].Ready.Status != want {
			t.Fatalf("service %d: want %s, got %s", i, want, result.Services[i].Ready.Status)
		}
	}
}

func TestDashboardRoutesDoNotReplaceConfigurationOrFollowRedirects(t *testing.T) {
	dashboard := NewDashboard()
	mux := http.NewServeMux()
	dashboard.Register(mux)
	mux.HandleFunc("GET /v1/config/identity/local", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/", 200}, {"GET", "/status", 200}, {"GET", "/status/", 200},
		{"GET", "/status/dashboard.js", 200}, {"GET", "/status/dashboard.css", 200},
		{"POST", "/status/services", 405}, {"GET", "/status/unknown", 404},
		{"GET", "/v1/config/identity/local", 204},
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
		if response.Code != tc.status {
			t.Fatalf("%s %s: want %d, got %d", tc.method, tc.path, tc.status, response.Code)
		}
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/private-secret", http.StatusFound)
	}))
	defer redirect.Close()
	probe := dashboard.probe(context.Background(), redirect.URL, false)
	if probe.Status != "DOWN" || probe.HTTP != 302 || strings.Contains(probe.Message, "private-secret") {
		t.Fatal("redirect was followed or leaked", probe)
	}
}
