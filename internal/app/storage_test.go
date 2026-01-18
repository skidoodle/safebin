package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanup_AbandonedMerge(t *testing.T) {
	tmpDir := t.TempDir()
	tmpStorage := filepath.Join(tmpDir, "tmp")
	os.MkdirAll(tmpStorage, 0700)

	app := &App{
		Conf: Config{StorageDir: tmpDir},
		Logger: discardLogger(),
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
	tmpStorage := filepath.Join(tmpDir, "tmp")
	os.MkdirAll(tmpStorage, 0700)

	app := &App{
		Conf: Config{StorageDir: tmpDir},
		Logger: discardLogger(),
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
	app := &App{
		Conf: Config{
			StorageDir: storageDir,
			MaxMB:      100,
		},
		Logger: discardLogger(),
	}

	filename := "large_file_id"
	path := filepath.Join(storageDir, filename)
	f, _ := os.Create(path)
	f.Truncate(100 * MegaByte) // Max size
	f.Close()

	oldTime := time.Now().Add(-MinRetention - time.Hour)
	os.Chtimes(path, oldTime, oldTime)

	app.CleanStorage(storageDir)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Cleanup failed to remove expired large file")
	}
}
