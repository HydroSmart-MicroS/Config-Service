package health

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type Probe func(context.Context) (map[string]any, error)
type Controller struct {
	Service string
	Checks  map[string]Probe
}

func (c Controller) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	if r.URL.Path == "/health/live" {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "UP", "service": c.Service, "components": map[string]any{"application": map[string]string{"status": "UP"}}})
		return
	}
	if r.URL.Path != "/health/ready" {
		w.WriteHeader(404)
		return
	}
	components := map[string]any{}
	status := "UP"
	for name, check := range c.Checks {
		start := time.Now()
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		details, err := check(ctx)
		cancel()
		state := "UP"
		if err != nil {
			state = "DOWN"
			status = "DOWN"
		}
		if details == nil {
			details = map[string]any{}
		}
		if err != nil {
			messages := map[string]string{"database": "No se pudo consultar MongoDB; revisa su disponibilidad y acceso.", "rabbitmq": "No se pudo comprobar RabbitMQ; revisa la conexión, permisos y el receptor RPC.", "mqtt": "La conexión o suscripción MQTT no está disponible.", "configuration": "La configuración cargada no está disponible."}
			details["message"] = messages[name]
		}
		details["durationMs"] = float64(time.Since(start).Microseconds()) / 1000
		components[name] = map[string]any{"status": state, "details": details}
	}
	if status == "DOWN" {
		w.WriteHeader(503)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"status": status, "service": c.Service, "checkedAt": time.Now().UTC().Format(time.RFC3339Nano), "components": components})
}
