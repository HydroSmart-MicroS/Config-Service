package main

import (
	"encoding/json"
	"hydrosmart/config/internal/configuration/application"
	"hydrosmart/config/internal/configuration/domain/model/valueobjects"
	"hydrosmart/config/internal/configuration/infrastructure/persistence/filesystem/repositories"
	"hydrosmart/config/internal/configuration/interfaces/rest"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigurationContract(t *testing.T) {
	configs, err := loadConfigs("config")
	if err != nil {
		t.Fatal(err)
	}
	h := handler(configs)
	for _, tc := range []struct {
		path     string
		status   int
		contains string
	}{
		{"/v1/config/identity/local", 200, "\"JWT_ISSUER\""},
		{"/v1/config/properties/local?format=properties", 200, "JWKS_URL=http://identity:8080/api/auth/jwks\n"},
		{"/v1/config/unknown/local", 404, ""},
		{"/v1/config/identity/production", 404, ""},
		{"/v1/config/identity/local?format=xml", 400, ""},
	} {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.contains) {
				t.Fatalf("unexpected response: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
func TestRejectSecretsAndUnsafeValues(t *testing.T) {
	original, err := loadConfigs("config")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"secret", "newline", "missing", "backslash"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "local"), 0700); err != nil {
				t.Fatal(err)
			}
			for service, values := range original {
				copy := map[string]string{}
				for k, v := range values {
					copy[k] = v
				}
				if service == "identity" {
					switch mode {
					case "secret":
						copy["DATABASE_PASSWORD"] = "must-not-be-served"
					case "newline":
						copy["JWT_ISSUER"] = "bad\nINJECTED=value"
					case "missing":
						delete(copy, "JWT_ISSUER")
					case "backslash":
						copy["JWT_ISSUER"] = "bad\\value"
					}
				}
				data, _ := json.Marshal(copy)
				if err := os.WriteFile(filepath.Join(dir, "local", service+".json"), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := loadConfigs(dir); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func loadConfigs(dir string) (valueobjects.Configurations, error) {
	return repositories.LoadConfigs(dir)
}
func handler(configs valueobjects.Configurations) http.Handler {
	return rest.NewHandler(application.NewQueries(repositories.FileConfigurationRepository{Values: configs}))
}
