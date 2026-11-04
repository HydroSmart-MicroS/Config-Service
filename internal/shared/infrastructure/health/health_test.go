package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadinessNamesFailuresWithoutSecretsAndLivenessStaysUp(t *testing.T) {
	checks := 0
	c := Controller{Service: "test", Checks: map[string]Probe{
		"database": func(context.Context) (map[string]any, error) {
			checks++
			return nil, errors.New("mongodb://private:secret@host")
		},
		"rabbitmq": func(context.Context) (map[string]any, error) {
			checks++
			return map[string]any{"connection": "UP"}, nil
		}}}
	response := httptest.NewRecorder()
	c.ServeHTTP(response, httptest.NewRequest("GET", "/health/ready", nil))
	if response.Code != 503 || checks != 2 || strings.Contains(response.Body.String(), "secret") {
		t.Fatal("failure was hidden or exposed credentials", response.Code)
	}
	var body struct {
		Components map[string]struct{ Status string }
	}
	if json.Unmarshal(response.Body.Bytes(), &body) != nil || body.Components["database"].Status != "DOWN" || body.Components["rabbitmq"].Status != "UP" {
		t.Fatal("wrong dependency statuses")
	}
	live := httptest.NewRecorder()
	c.ServeHTTP(live, httptest.NewRequest("GET", "/health/live", nil))
	if live.Code != 200 || checks != 2 {
		t.Fatal("liveness touched dependencies")
	}
}
