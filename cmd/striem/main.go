package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
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
	if value := os.Getenv("STRIEM_SQLITE_MEMORY_LIMIT"); value != "" {
		limit, err := strconv.ParseInt(value, 10, 64)
		if err == nil {
			err = store.LimitMemory(context.Background(), limit)
		}
		if err != nil {
			logger.Error("STRIEM_SQLITE_MEMORY_LIMIT must be a positive number of bytes", "error", err)
			os.Exit(1)
		}
	}

	manifestPath := os.Getenv("STRIEM_CONFIG")
	apiServer := api.New(store, logger)
	apiServer.SetClientIPHeader(os.Getenv("STRIEM_CLIENT_IP_HEADER"))
	if err := apiServer.SetAccessKey(os.Getenv("STRIEM_ACCESS_KEY")); err != nil {
		logger.Error("Invalid STRIEM_ACCESS_KEY", "error", err)
		os.Exit(1)
	}
	nativeKusto, err := nativeKustoFromEnv()
	if err == nil {
		err = apiServer.SetNativeKusto(nativeKusto)
	}
	if err != nil {
		logger.Error("Could not configure native Kusto", "error", err)
		os.Exit(1)
	}
	if manifestPath != "" {
		if challengeName, err := deployment.ReadChallengeName(manifestPath); err == nil {
			apiServer.SetChallengeName(challengeName)
		}
	}
	apiServer.SetLoading()
	server := &http.Server{
		Addr:              envOrDefault("STRIEM_ADDR", ":8080"),
		Handler:           apiServer.Handler(),
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

	startupFailed := false
	if manifestPath != "" {
		datasets, err := deployment.Load(context.Background(), store, manifestPath)
		if err != nil {
			logger.Error("Could not load deployment", "error", err)
			apiServer.SetStartupError("Deployment ingestion failed. Check the server logs for details.")
			startupFailed = true
		} else {
			var eventCount int64
			for _, dataset := range datasets {
				eventCount += dataset.EventCount
			}
			logger.Info("Deployment loaded", "datasets", len(datasets), "events", eventCount)
			if state, err := store.ChallengeState(context.Background()); err == nil && state.Total > 0 && os.Getenv("STRIEM_ACCESS_KEY") == "" {
				logger.Warn("Challenge questions are configured without STRIEM_ACCESS_KEY; anyone who can reach this deployment shares its progress and can read the flag once it is solved")
			}
		}
	} else {
		logger.Warn("STRIEM_CONFIG is not set; starting without events")
	}
	if !startupFailed {
		if _, err := store.DB().ExecContext(context.Background(), "PRAGMA analysis_limit = 400; PRAGMA optimize;"); err != nil {
			logger.Error("Could not optimize database", "error", err)
			apiServer.SetStartupError("Database optimization failed. Check the server logs for details.")
			startupFailed = true
		}
	}
	if !startupFailed {
		if err := apiServer.RefreshCatalog(context.Background()); err != nil {
			logger.Error("Could not publish ingested deployment", "error", err)
			apiServer.SetStartupError("Could not prepare the ingested deployment. Check the server logs for details.")
		} else {
			logger.Info("Web interface ready")
		}
	}

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
