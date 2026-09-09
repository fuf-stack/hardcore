package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fuf-stack/hardcore/health"
	"github.com/fuf-stack/hardcore/service"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, ":8080"); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("example service stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, address string) error {
	probes, err := health.New()
	if err != nil {
		return err
	}

	server := newServer(address, probes)
	probes.SetReady(true)
	slog.Info("starting example service", "address", server.Addr)
	return service.ListenAndServe(
		ctx,
		server,
		service.WithShutdownHook(func(context.Context) error {
			probes.SetReady(false)
			return nil
		}),
	)
}

func newServer(address string, probes *health.Probes) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", probes.LivenessHandler())
	mux.Handle("GET /readyz", probes.ReadinessHandler())
	mux.HandleFunc("GET /", func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("Hardcore example service\n"))
	})

	return &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}
