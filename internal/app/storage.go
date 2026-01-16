package app

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"time"
)

func (app *App) StartCleanupTask(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			app.CleanDir(app.Conf.StorageDir, false)
			app.CleanDir(filepath.Join(app.Conf.StorageDir, "tmp"), true)
		}
	}
}

func (app *App) CleanDir(path string, isTmp bool) {
	entries, _ := os.ReadDir(path)
	for _, entry := range entries {
		info, _ := entry.Info()
		expiry := 4 * time.Hour
		if !isTmp {
			expiry = CalculateRetention(info.Size(), app.Conf.MaxMB)
		}

		if time.Since(info.ModTime()) > expiry {
			os.RemoveAll(filepath.Join(path, entry.Name()))
		}
	}
}

func CalculateRetention(fileSize int64, maxMB int64) time.Duration {
	const (
		minAge = 24 * time.Hour
		maxAge = 365 * 24 * time.Hour
	)
	ratio := math.Max(0, math.Min(1, float64(fileSize)/float64(maxMB<<20)))
	retention := float64(maxAge) * math.Pow(1.0-ratio, 3)
	if retention < float64(minAge) {
		return minAge
	}
	return time.Duration(retention)
}
