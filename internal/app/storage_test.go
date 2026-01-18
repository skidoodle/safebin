package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

func TestCleanup_AbandonedMerge(t *testing.T) {
	tmpDir := t.TempDir()
	tmpStorage := filepath.Join(tmpDir, TempDirName)
	os.MkdirAll(tmpStorage, 0700)

	db, _ := InitDB(tmpDir)
	defer db.Close()

	app := &App{
		Conf:   Config{StorageDir: tmpDir},
		Logger: discardLogger(),
		DB:     db,
	}

	abandonedFile := filepath.Join(tmpStorage, "m_abandoned_upload_id")
	if err := os.WriteFile(abandonedFile, []byte("partial data"), 0600); err != nil {
		t.Fatal(err)
	}

	oldTime := time.Now().Add(-TempExpiry - time.Hour)
	if err := os.Chtimes(abandonedFile, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	app.CleanTemp(tmpStorage)

	if _, err := os.Stat(abandonedFile); !os.IsNotExist(err) {
		t.Error("Cleanup failed to remove abandoned merge file from crashed session")
	}
}

func TestCleanup_AbandonedChunks(t *testing.T) {
	tmpDir := t.TempDir()
	tmpStorage := filepath.Join(tmpDir, TempDirName)
	os.MkdirAll(tmpStorage, 0700)

	db, _ := InitDB(tmpDir)
	defer db.Close()

	app := &App{
		Conf:   Config{StorageDir: tmpDir},
		Logger: discardLogger(),
		DB:     db,
	}

	chunkDir := filepath.Join(tmpStorage, "some_upload_id")
	os.MkdirAll(chunkDir, 0700)
	os.WriteFile(filepath.Join(chunkDir, "0"), []byte("chunk data"), 0600)

	oldTime := time.Now().Add(-TempExpiry - time.Hour)
	os.Chtimes(chunkDir, oldTime, oldTime)

	app.CleanTemp(tmpStorage)

	if _, err := os.Stat(chunkDir); !os.IsNotExist(err) {
		t.Error("Cleanup failed to remove abandoned chunk directory")
	}
}

func TestCleanup_ExpiredStorage(t *testing.T) {
	storageDir := t.TempDir()
	db, _ := InitDB(storageDir)
	defer db.Close()

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
	f, _ := os.Create(path)
	f.Truncate(100 * MegaByte)
	f.Close()

	expiredMeta := FileMeta{
		ID:        filename,
		Size:      100 * MegaByte,
		CreatedAt: time.Now().Add(-MinRetention - 2*time.Hour),
		ExpiresAt: time.Now().Add(-time.Hour),
	}

	app.DB.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(DBBucketName))
		data, _ := json.Marshal(expiredMeta)
		return b.Put([]byte(filename), data)
	})

	app.CleanStorage()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Cleanup failed to remove expired large file")
	}

	app.DB.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(DBBucketName))
		if v := b.Get([]byte(filename)); v != nil {
			t.Error("Cleanup failed to remove metadata")
		}
		return nil
	})
}
