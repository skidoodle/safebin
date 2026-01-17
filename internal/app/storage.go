package app

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"time"
)

const (
	cleanupInterval = 1 * time.Hour
	tempExpiry      = 4 * time.Hour
	minRetention    = 24 * time.Hour
	maxRetention    = 365 * 24 * time.Hour
	bytesInMB       = 1 << 20
)

func (app *App) StartCleanupTask(ctx context.Context) {
	ticker := time.NewTicker(cleanupInterval)

	for {
		select {
		case <-ctx.Done():
			ticker.Stop()
			return
		case <-ticker.C:
			app.CleanStorage(app.Conf.StorageDir)
			app.CleanTemp(filepath.Join(app.Conf.StorageDir, "tmp"))
		}
	}
}

func (app *App) CleanStorage(path string) {
	entries, err := os.ReadDir(path)
	if err != nil {
		app.Logger.Error("Failed to read storage dir", "err", err)
		return
	}

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		expiry := CalculateRetention(info.Size(), app.Conf.MaxMB)

		if time.Since(info.ModTime()) > expiry {
			if err := os.RemoveAll(filepath.Join(path, entry.Name())); err != nil {
				app.Logger.Error("Failed to remove expired file", "path", entry.Name(), "err", err)
			}
		}
	}
}

func (app *App) CleanTemp(path string) {
	entries, err := os.ReadDir(path)
	if err != nil {
		app.Logger.Error("Failed to read temp dir", "err", err)
		return
	}

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		if time.Since(info.ModTime()) > tempExpiry {
			if err := os.RemoveAll(filepath.Join(path, entry.Name())); err != nil {
				app.Logger.Error("Failed to remove expired temp file", "path", entry.Name(), "err", err)
			}
		}
	}
}

func CalculateRetention(fileSize, maxMB int64) time.Duration {
	ratio := math.Max(0, math.Min(1, float64(fileSize)/float64(maxMB*bytesInMB)))

	invRatio := 1.0 - ratio
	retention := float64(maxRetention) * (invRatio * invRatio * invRatio)

	if retention < float64(minRetention) {
		return minRetention
	}

	return time.Duration(retention)
}
