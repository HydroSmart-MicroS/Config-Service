package rest

import (
	"encoding/json"
	"fmt"
	"hydrosmart/config/internal/configuration/domain/model/queries"
	"hydrosmart/config/internal/configuration/domain/services"
	"hydrosmart/config/internal/configuration/interfaces/rest/transform"
	"net/http"
	"sort"
)

func NewHandler(service services.ConfigurationQueryService) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/config/{service}/{profile}", func(w http.ResponseWriter, r *http.Request) {
		values, ok := service.Handle(queries.GetConfigurationQuery{Service: r.PathValue("service"), Profile: r.PathValue("profile")})
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		switch r.URL.Query().Get("format") {
		case "", "json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(transform.ToResource(values))
		case "properties":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			keys := make([]string, 0, len(values))
			for k := range values {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(w, "%s=%s\n", k, values[k])
			}
		default:
			http.Error(w, "unsupported format", http.StatusBadRequest)
		}
	})
	return mux
}
