package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/oka/striem/internal/api"
	"github.com/oka/striem/internal/database"
	"github.com/oka/striem/internal/deployment"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dataDir := envOrDefault("STRIEM_DATA_DIR", "./data")
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		logger.Error("create data directory", "error", err)
		os.Exit(1)
	}

	store, err := database.Open(filepath.Join(dataDir, "striem.db"))
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	if manifestPath := os.Getenv("STRIEM_CONFIG"); manifestPath != "" {
		datasets, err := deployment.Load(context.Background(), store, manifestPath)
		if err != nil {
			logger.Error("load deployment datasets", "error", err)
			os.Exit(1)
		}
		for _, dataset := range datasets {
			logger.Info("loaded deployment dataset", "name", dataset.Name, "events", dataset.EventCount)
		}
	} else {
		logger.Warn("STRIEM_CONFIG is not set; starting without events")
	}

	server := &http.Server{
		Addr:              envOrDefault("STRIEM_ADDR", ":8080"),
		Handler:           api.New(store, logger).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("Striem listening", "address", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("serve", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("shutdown", "error", err)
	}
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
