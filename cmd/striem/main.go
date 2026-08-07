package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kawijayaa/striem/internal/api"
	"github.com/kawijayaa/striem/internal/database"
	"github.com/kawijayaa/striem/internal/deployment"
)

func main() {
	logger := newConsoleLogger(os.Stdout)
	dataDir := envOrDefault("STRIEM_DATA_DIR", "./data")
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		logger.Error("Could not create data directory", "error", err)
		os.Exit(1)
	}

	store, err := database.Open(filepath.Join(dataDir, "striem.db"))
	if err != nil {
		logger.Error("Could not open database", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	if manifestPath := os.Getenv("STRIEM_CONFIG"); manifestPath != "" {
		datasets, err := deployment.Load(context.Background(), store, manifestPath)
		if err != nil {
			logger.Error("Could not load deployment", "error", err)
			os.Exit(1)
		}
		var eventCount int64
		for _, dataset := range datasets {
			eventCount += dataset.EventCount
		}
		logger.Info("Deployment loaded", "datasets", len(datasets), "events", eventCount)
	} else {
		logger.Warn("STRIEM_CONFIG is not set; starting without events")
	}
	if _, err := store.DB().ExecContext(context.Background(), "PRAGMA analysis_limit = 400; PRAGMA optimize;"); err != nil {
		logger.Error("Could not optimize database", "error", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              envOrDefault("STRIEM_ADDR", ":8080"),
		Handler:           api.New(store, logger).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("Server listening", "address", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("Server stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("Could not shut down server cleanly", "error", err)
	}
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
