package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/excelano/xensus/config"
	"github.com/excelano/xensus/store"
)

// Run brings the server up: opens the database, applies migrations,
// builds the router, and listens until SIGINT/SIGTERM. Shutdown is
// graceful with a 10s deadline so in-flight requests can finish.
func Run(ctx context.Context, cfg *config.Config) error {
	db, err := store.Open(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	slog.Info("xensus storage ready",
		"data_dir", cfg.DataDir,
		"db_file", "xensus.sqlite",
	)

	mux := http.NewServeMux()
	registerRoutes(mux, db)

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("xensus listening", "addr", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		slog.Info("shutdown signal received, draining requests")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	slog.Info("xensus stopped cleanly")
	return nil
}

// healthHandler returns 200 if the database is reachable, 503 otherwise.
// This is intentionally the only handler in Slice 2; everything else
// arrives once auth (Slice 3) is wired up.
func healthHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := db.PingContext(r.Context()); err != nil {
			slog.Warn("health check failed", "err", err)
			http.Error(w, "unhealthy", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	}
}
