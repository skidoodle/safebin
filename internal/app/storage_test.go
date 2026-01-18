package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

func TestCleanup_AbandonedChunks(t *testing.T) {
	tmpDir := t.TempDir()
	tmpStorage := filepath.Join(tmpDir, TempDirName)
	if err := os.MkdirAll(tmpStorage, 0700); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	db, err := InitDB(tmpDir)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("Failed to close DB: %v", err)
		}
	}()

	app := &App{
		Conf:   Config{StorageDir: tmpDir},
		Logger: discardLogger(),
		DB:     db,
	}

	chunkDir := filepath.Join(tmpStorage, "some_upload_id")
	if err := os.MkdirAll(chunkDir, 0700); err != nil {
		t.Fatalf("MkdirAll chunkDir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chunkDir, "0"), []byte("chunk data"), 0600); err != nil {
		t.Fatalf("WriteFile chunk failed: %v", err)
	}

	oldTime := time.Now().Add(-TempExpiry - time.Hour)
	if err := os.Chtimes(chunkDir, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes failed: %v", err)
	}

	app.CleanTemp(tmpStorage)

	if _, err := os.Stat(chunkDir); !os.IsNotExist(err) {
		t.Error("Cleanup failed to remove abandoned chunk directory")
	}
}

func TestCleanup_ExpiredStorage(t *testing.T) {
	storageDir := t.TempDir()
	db, err := InitDB(storageDir)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("Failed to close DB: %v", err)
		}
	}()

	app := &App{
		Conf: Config{
			StorageDir: storageDir,
			MaxMB:      100,
		},
		Logger: discardLogger(),
		DB:     db,
	}

	filename := "large_file_id"
	path := filepath.Join(storageDir, filename)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create file failed: %v", err)
	}
	if err := f.Truncate(100 * MegaByte); err != nil {
		t.Fatalf("Truncate failed: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close file failed: %v", err)
	}

	expiredMeta := FileMeta{
		ID:        filename,
		Size:      100 * MegaByte,
		CreatedAt: time.Now().Add(-MinRetention - 2*time.Hour),
		ExpiresAt: time.Now().Add(-time.Hour),
	}

	if err := app.DB.Update(func(tx *bbolt.Tx) error {
		bFiles := tx.Bucket([]byte(DBBucketName))
		bIndex := tx.Bucket([]byte(DBBucketIndexName))

		data, _ := json.Marshal(expiredMeta)
		if err := bFiles.Put([]byte(filename), data); err != nil {
			return err
		}

		indexKey := []byte(expiredMeta.ExpiresAt.Format(time.RFC3339) + "_" + filename)
		return bIndex.Put(indexKey, []byte(filename))
	}); err != nil {
		t.Fatalf("DB Update failed: %v", err)
	}

	app.CleanStorage()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Cleanup failed to remove expired large file")
	}

	if err := app.DB.View(func(tx *bbolt.Tx) error {
		bFiles := tx.Bucket([]byte(DBBucketName))
		if v := bFiles.Get([]byte(filename)); v != nil {
			t.Error("Cleanup failed to remove metadata")
		}

		bIndex := tx.Bucket([]byte(DBBucketIndexName))
		indexKey := []byte(expiredMeta.ExpiresAt.Format(time.RFC3339) + "_" + filename)
		if v := bIndex.Get(indexKey); v != nil {
			t.Error("Cleanup failed to remove index entry")
		}
		return nil
	}); err != nil {
		t.Fatalf("DB View failed: %v", err)
	}
}
