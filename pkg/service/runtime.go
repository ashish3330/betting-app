package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	//nolint:gosec // G108: pprof is only exposed via the separate admin listener bound to 127.0.0.1 and gated behind ENVIRONMENT/PPROF_ENABLED — never on the main service mux.
	_ "net/http/pprof"
	"os"
	"strings"
	"time"

	"github.com/lotus-exchange/lotus-exchange/pkg/telemetry"
)

// Config describes the runtime parameters for an HTTP service managed by Run.
// Additional knobs can be added without breaking callers thanks to the struct
// form.
type Config struct {
	// ServiceName is used for log messages and startup/shutdown traces.
	ServiceName string
	// Port is the TCP port to bind (":<port>" is added automatically).
	Port string
	// Logger receives startup, shutdown and error messages. Required.
	Logger *slog.Logger
}

// Run starts an HTTP server with sensible production defaults and blocks
// until the context is cancelled (typically by SIGINT/SIGTERM via
// signal.NotifyContext) or the server itself errors.
//
// On shutdown it gives in-flight requests up to 30 seconds to finish via
// http.Server.Shutdown. Any server-start or shutdown error is returned.
func Run(ctx context.Context, cfg Config, handler http.Handler) error {
	if cfg.Logger == nil {
		return fmt.Errorf("service.Run: Logger is required")
	}
	if cfg.Port == "" {
		return fmt.Errorf("service.Run: Port is required")
	}

	addr := cfg.Port
	if !strings.HasPrefix(addr, ":") {
		addr = ":" + addr
	}

	// Admin HTTP listener bound to localhost that serves pprof and any other
	// debug endpoints registered on the default mux. Enabled by default
	// outside production; opt-in in production via PPROF_ENABLED=true.
	if os.Getenv("ENVIRONMENT") != "production" || os.Getenv("PPROF_ENABLED") == "true" {
		adminPort := os.Getenv("ADMIN_PORT")
		if adminPort == "" {
			adminPort = "6060"
		}
		go func() {
			adminMux := http.NewServeMux()
			// pprof registers its handlers on http.DefaultServeMux via its
			// package init. Forward /debug/pprof/ to the default mux so the
			// admin listener exposes them without touching the main app mux.
			adminMux.Handle("/debug/pprof/", http.DefaultServeMux)
			adminSrv := &http.Server{
				Addr:              "127.0.0.1:" + adminPort,
				Handler:           adminMux,
				ReadHeaderTimeout: 5 * time.Second,
			}
			cfg.Logger.Info("admin server starting", "addr", adminSrv.Addr)
			if err := adminSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				cfg.Logger.Warn("admin server failed", "error", err)
			}
		}()
	}

	// Initialize distributed tracing. When OTEL_EXPORTER_OTLP_ENDPOINT is
	// unset, SetupTracer returns a no-op shutdown and leaves the global
	// tracer provider as the default noop provider.
	shutdownTracer, err := telemetry.SetupTracer(ctx, cfg.ServiceName, "v1.0.0", cfg.Logger)
	if err != nil {
		cfg.Logger.Warn("failed to setup tracer", "error", err)
	}
	defer func() {
		if shutdownTracer == nil {
			return
		}
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracer(sctx); err != nil {
			cfg.Logger.Warn("tracer shutdown error", "error", err)
		}
	}()

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	srvErr := make(chan error, 1)
	go func() {
		cfg.Logger.Info("http server starting", "service", cfg.ServiceName, "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			srvErr <- err
		}
		close(srvErr)
	}()

	select {
	case err := <-srvErr:
		if err != nil {
			return fmt.Errorf("service.Run: listen: %w", err)
		}
		return nil
	case <-ctx.Done():
		cfg.Logger.Info("shutdown signal received", "service", cfg.ServiceName)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("service.Run: shutdown: %w", err)
	}

	cfg.Logger.Info("http server stopped", "service", cfg.ServiceName)
	return nil
}
