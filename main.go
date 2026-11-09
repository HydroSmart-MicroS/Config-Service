package main

import (
	"context"
	"fmt"
	"hydrosmart/config/internal/configuration/application"
	"hydrosmart/config/internal/configuration/infrastructure/persistence/filesystem/repositories"
	"hydrosmart/config/internal/configuration/interfaces/rest"
	"hydrosmart/config/internal/shared/infrastructure/health"
	"hydrosmart/config/internal/shared/infrastructure/messaging"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		client := &http.Client{Timeout: 3 * time.Second}
		res, err := client.Get("http://127.0.0.1:8080/health/ready")
		if err != nil {
			os.Exit(1)
		}
		defer res.Body.Close()
		_, _ = io.Copy(io.Discard, res.Body)
		if res.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	dir := os.Getenv("CONFIG_DIR")
	if dir == "" {
		dir = "config"
	}
	configs, err := repositories.LoadConfigs(dir)
	if err != nil {
		log.Fatal(err)
	}
	var rpcState atomic.Pointer[messaging.Server]
	mux := http.NewServeMux()
	health.NewDashboard().Register(mux)
	mux.Handle("/health/", health.Controller{Service: "config-service", Checks: map[string]health.Probe{
		"configuration": func(context.Context) (map[string]any, error) {
			if len(configs) != 8 {
				return nil, fmt.Errorf("missing configuration")
			}
			return map[string]any{"profile": "local", "loadedServices": len(configs), "source": "memory"}, nil
		},
		"rabbitmq": func(ctx context.Context) (map[string]any, error) {
			state := rpcState.Load()
			return messaging.BrokerHealth(ctx, "config-service", state != nil && state.Ready())
		},
	}})
	mux.Handle("/", rest.NewHandler(application.NewQueries(repositories.FileConfigurationRepository{Values: configs})))
	server := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	rpcState.Store(messaging.StartServer(ctx, "config-service", server.Handler))
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Print("config-service listening on :8080; local profile, non-secret configuration")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
