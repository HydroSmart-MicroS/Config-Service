package repositories

import (
	"encoding/json"
	"fmt"
	"hydrosmart/config/internal/configuration/domain/model/valueobjects"
	"os"
	"path/filepath"
	"strings"
)

var allowed = map[string][]string{
	"identity":      {"BETTER_AUTH_URL", "JWT_ISSUER", "JWT_AUDIENCE", "TRUSTED_ORIGINS"},
	"properties":    {"JWT_ISSUER", "JWKS_URL", "JWT_AUDIENCE"},
	"reports":       {"JWT_ISSUER", "JWKS_URL", "JWT_AUDIENCE"},
	"notifications": {"JWT_ISSUER", "JWKS_URL", "JWT_AUDIENCE"},
	"billing":       {"JWT_ISSUER", "JWKS_URL", "JWT_AUDIENCE"},
	"telemetry":     {"MONGODB_DATABASE", "JWT_ISSUER", "JWKS_URL", "JWT_AUDIENCE", "PROPERTIES_INTERNAL_URL", "MQTT_BROKER_URL"},
	"analytics":     {"DATABASE_CONNECT_TIMEOUT"},
	"gateway":       {"IDENTITY_UPSTREAM", "PROPERTIES_UPSTREAM", "TELEMETRY_UPSTREAM", "ANALYTICS_UPSTREAM", "REPORTS_UPSTREAM", "NOTIFICATIONS_UPSTREAM", "BILLING_UPSTREAM"},
}

type Configs = valueobjects.Configurations

func LoadConfigs(dir string) (Configs, error) {
	configs := Configs{}
	for service, keys := range allowed {
		data, err := os.ReadFile(filepath.Join(dir, "local", service+".json"))
		if err != nil {
			return nil, fmt.Errorf("cannot read configuration for %s", service)
		}
		values := map[string]string{}
		if err := json.Unmarshal(data, &values); err != nil {
			return nil, fmt.Errorf("invalid JSON for %s", service)
		}
		if len(values) != len(keys) {
			return nil, fmt.Errorf("unexpected or missing keys for %s", service)
		}
		for _, key := range keys {
			value, ok := values[key]
			if !ok || value == "" || strings.ContainsAny(value, "\r\n\\\x00") {
				return nil, fmt.Errorf("invalid configuration key %s for %s", key, service)
			}
		}
		configs[service] = values
	}
	return configs, nil
}

type FileConfigurationRepository struct{ Values valueobjects.Configurations }

func (r FileConfigurationRepository) Get(service, profile string) (map[string]string, bool) {
	if profile != "local" {
		return nil, false
	}
	values, ok := r.Values[service]
	return values, ok
}
