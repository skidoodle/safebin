package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/skidoodle/safebin/internal/app"
)

func main() {
	cfg := app.LoadConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
	}))

	logger.Info("Initializing Safebin Server",
		"storage_dir", cfg.StorageDir,
		"max_file_size", fmt.Sprintf("%dMB", cfg.MaxMB),
	)

	tmpDir := filepath.Join(cfg.StorageDir, app.TempDirName)
	if err := os.MkdirAll(tmpDir, app.PermUserRWX); err != nil {
		logger.Error("Failed to initialize storage directory", "err", err)
		os.Exit(1)
	}

	db, err := app.InitDB(cfg.StorageDir)
	if err != nil {
		logger.Error("Failed to initialize database", "err", err)
		os.Exit(1)
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("Failed to close database", "err", err)
		}
	}()

	application := &app.App{
		Conf:   cfg,
		Logger: logger,
		Tmpl:   app.ParseTemplates(),
		DB:     db,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go application.StartCleanupTask(ctx)

	srv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      application.Routes(),
		ReadTimeout:  app.ServerTimeout,
		WriteTimeout: app.ServerTimeout,
		IdleTimeout:  app.ServerTimeout,
	}

	go func() {
		application.Logger.Info("Server is ready and listening", "addr", cfg.Addr)

		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			application.Logger.Error("Server failed to start", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	application.Logger.Info("Shutting down gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), app.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		application.Logger.Error("Forced shutdown", "err", err)
	}

	application.Logger.Info("Server stopped")
}
