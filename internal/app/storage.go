package app

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/skidoodle/safebin/internal/crypto"
	"go.etcd.io/bbolt"
)

func (app *App) StartCleanupTask(ctx context.Context) {
	ticker := time.NewTicker(CleanupInterval)

	for {
		select {
		case <-ctx.Done():
			ticker.Stop()
			return
		case <-ticker.C:
			app.CleanStorage()
			app.CleanTemp(filepath.Join(app.Conf.StorageDir, TempDirName))
		}
	}
}

func (app *App) saveChunk(uid string, idx int, src io.Reader) error {
	dir := filepath.Join(app.Conf.StorageDir, TempDirName, uid)

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

	key := make([]byte, crypto.KeySize)
	if _, err := rand.Read(key); err != nil {
		return fmt.Errorf("generate chunk key: %w", err)
	}

	if _, err := dest.Write(key); err != nil {
		return fmt.Errorf("write chunk key: %w", err)
	}

	streamer, err := crypto.NewGCMStreamer(key)
	if err != nil {
		return fmt.Errorf("create streamer: %w", err)
	}

	if err := streamer.EncryptStream(dest, src); err != nil {
		return fmt.Errorf("encrypt chunk: %w", err)
	}

	return nil
}

func (app *App) getChunkDecryptors(uid string, total int) ([]io.ReadSeeker, func(), error) {
	files := make([]*os.File, 0, total)
	decryptors := make([]io.ReadSeeker, 0, total)

	closeAll := func() {
		for _, f := range files {
			_ = f.Close()
		}
	}

	for i := range total {
		partPath := filepath.Join(app.Conf.StorageDir, TempDirName, uid, strconv.Itoa(i))
		f, err := os.Open(partPath)
		if err != nil {
			closeAll()
			return nil, nil, fmt.Errorf("open chunk %d: %w", i, err)
		}
		files = append(files, f)

		key := make([]byte, crypto.KeySize)
		if _, err := io.ReadFull(f, key); err != nil {
			closeAll()
			return nil, nil, fmt.Errorf("read chunk key %d: %w", i, err)
		}

		info, err := f.Stat()
		if err != nil {
			closeAll()
			return nil, nil, fmt.Errorf("stat chunk %d: %w", i, err)
		}

		bodySize := info.Size() - int64(crypto.KeySize)
		if bodySize < 0 {
			closeAll()
			return nil, nil, fmt.Errorf("invalid chunk size %d", i)
		}

		bodyReader := io.NewSectionReader(f, int64(crypto.KeySize), bodySize)

		streamer, err := crypto.NewGCMStreamer(key)
		if err != nil {
			closeAll()
			return nil, nil, fmt.Errorf("create streamer %d: %w", i, err)
		}

		decryptor := crypto.NewDecryptor(bodyReader, streamer.AEAD, bodySize)
		decryptors = append(decryptors, decryptor)
	}

	return decryptors, closeAll, nil
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

func (app *App) RegisterFile(id string, size int64) error {
	retention := CalculateRetention(size, app.Conf.MaxMB)
	meta := FileMeta{
		ID:        id,
		Size:      size,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(retention),
	}

	return app.DB.Update(func(tx *bbolt.Tx) error {
		bFiles := tx.Bucket([]byte(DBBucketName))
		bIndex := tx.Bucket([]byte(DBBucketIndexName))

		data, err := json.Marshal(meta)
		if err != nil {
			return err
		}

		if err := bFiles.Put([]byte(id), data); err != nil {
			return err
		}

		indexKey := []byte(meta.ExpiresAt.Format(time.RFC3339) + "_" + id)
		return bIndex.Put(indexKey, []byte(id))
	})
}

func (app *App) CleanStorage() {
	now := time.Now().Format(time.RFC3339)
	var toDeleteIDs []string
	var toDeleteKeys []string

	err := app.DB.View(func(tx *bbolt.Tx) error {
		bIndex := tx.Bucket([]byte(DBBucketIndexName))
		if bIndex == nil {
			return nil
		}
		c := bIndex.Cursor()

		for k, v := c.First(); k != nil; k, v = c.Next() {
			if string(k) > now {
				break
			}

			toDeleteKeys = append(toDeleteKeys, string(k))
			toDeleteIDs = append(toDeleteIDs, string(v))
		}
		return nil
	})

	if err != nil {
		app.Logger.Error("Failed to view DB for cleanup", "err", err)
		return
	}

	if len(toDeleteIDs) == 0 {
		return
	}

	err = app.DB.Update(func(tx *bbolt.Tx) error {
		bFiles := tx.Bucket([]byte(DBBucketName))
		bIndex := tx.Bucket([]byte(DBBucketIndexName))

		for i, id := range toDeleteIDs {
			path := filepath.Join(app.Conf.StorageDir, id)
			if err := os.RemoveAll(path); err != nil {
				app.Logger.Error("Failed to remove expired file", "path", id, "err", err)
			}

			if err := bFiles.Delete([]byte(id)); err != nil {
				app.Logger.Error("Failed to delete metadata", "id", id, "err", err)
			}

			if err := bIndex.Delete([]byte(toDeleteKeys[i])); err != nil {
				app.Logger.Error("Failed to delete index", "key", toDeleteKeys[i], "err", err)
			}
		}
		return nil
	})

	if err != nil {
		app.Logger.Error("Failed to update DB during cleanup", "err", err)
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
