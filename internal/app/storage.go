package app

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/skidoodle/safebin/internal/crypto"
)

func (app *App) StartCleanupTask(ctx context.Context) {
	ticker := time.NewTicker(CleanupInterval)

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

func (app *App) saveChunk(uid string, idx int, src io.Reader) error {
	dir := filepath.Join(app.Conf.StorageDir, "tmp", uid)

	if err := os.MkdirAll(dir, PermUserRWX); err != nil {
		return fmt.Errorf("create chunk dir: %w", err)
	}

	dest, err := os.Create(filepath.Join(dir, strconv.Itoa(idx)))
	if err != nil {
		return fmt.Errorf("create chunk file: %w", err)
	}

	defer func() {
		if closeErr := dest.Close(); closeErr != nil {
			app.Logger.Error("Failed to close chunk dest", "err", closeErr)
		}
	}()

	if _, err := io.Copy(dest, src); err != nil {
		return fmt.Errorf("copy chunk: %w", err)
	}

	return nil
}

func (app *App) mergeChunks(uid string, total int) (string, error) {
	tmpPath := filepath.Join(app.Conf.StorageDir, "tmp", "m_"+uid)

	merged, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("create merge file: %w", err)
	}

	defer func() {
		if closeErr := merged.Close(); closeErr != nil {
			app.Logger.Error("Failed to close merged file", "err", closeErr)
		}
	}()

	limit := app.Conf.MaxMB * MegaByte
	var written int64

	for i := range total {
		partPath := filepath.Join(app.Conf.StorageDir, "tmp", uid, strconv.Itoa(i))

		part, err := os.Open(partPath)
		if err != nil {
			return "", fmt.Errorf("open chunk %d: %w", i, err)
		}

		n, err := io.Copy(merged, part)

		if closeErr := part.Close(); closeErr != nil {
			app.Logger.Error("Failed to close chunk part", "err", closeErr)
		}

		if err != nil {
			return "", fmt.Errorf("append chunk %d: %w", i, err)
		}

		written += n
		if written > limit {
			return "", io.ErrShortWrite
		}
	}

	return tmpPath, nil
}

func (app *App) encryptAndSave(src io.Reader, key []byte, finalPath string) error {
	out, err := os.Create(finalPath + ".tmp")
	if err != nil {
		return fmt.Errorf("create final file: %w", err)
	}

	var closed bool

	defer func() {
		if !closed {
			if closeErr := out.Close(); closeErr != nil {
				app.Logger.Error("Failed to close final file", "err", closeErr)
			}
		}

		if removeErr := os.Remove(finalPath + ".tmp"); removeErr != nil && !os.IsNotExist(removeErr) {
			app.Logger.Error("Failed to remove temp final file", "err", removeErr)
		}
	}()

	streamer, err := crypto.NewGCMStreamer(key)
	if err != nil {
		return fmt.Errorf("create streamer: %w", err)
	}

	if err := streamer.EncryptStream(out, src); err != nil {
		return fmt.Errorf("encrypt stream: %w", err)
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("close final file: %w", err)
	}

	closed = true

	if err := os.Rename(finalPath+".tmp", finalPath); err != nil {
		return fmt.Errorf("rename final file: %w", err)
	}

	return nil
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

		if time.Since(info.ModTime()) > TempExpiry {
			if err := os.RemoveAll(filepath.Join(path, entry.Name())); err != nil {
				app.Logger.Error("Failed to remove expired temp file", "path", entry.Name(), "err", err)
			}
		}
	}
}

func CalculateRetention(fileSize, maxMB int64) time.Duration {
	ratio := math.Max(0, math.Min(1, float64(fileSize)/float64(maxMB*MegaByte)))

	invRatio := 1.0 - ratio
	retention := float64(MaxRetention) * (invRatio * invRatio * invRatio)

	if retention < float64(MinRetention) {
		return MinRetention
	}

	return time.Duration(retention)
}
