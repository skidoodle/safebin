package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

func TestInitDB(t *testing.T) {
	tmpDir := t.TempDir()

	db, err := InitDB(tmpDir)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("Failed to close DB: %v", err)
		}
	}()

	dbPath := filepath.Join(tmpDir, DBDirName, DBFileName)
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("Database file was not created")
	}

	err = db.View(func(tx *bbolt.Tx) error {
		if b := tx.Bucket([]byte(DBBucketName)); b == nil {
			t.Errorf("Bucket '%s' was not created", DBBucketName)
		}
		if b := tx.Bucket([]byte(DBBucketIndexName)); b == nil {
			t.Errorf("Bucket '%s' was not created", DBBucketIndexName)
		}
		return nil
	})
	if err != nil {
		t.Errorf("View failed: %v", err)
	}
}

func TestDB_MetadataLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := InitDB(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("Failed to close DB: %v", err)
		}
	}()

	app := &App{
		Conf: Config{StorageDir: tmpDir, MaxMB: 100},
		DB:   db,
	}

	fileID := "test-file-id"
	fileSize := int64(1024)

	if err := app.RegisterFile(fileID, fileSize); err != nil {
		t.Fatalf("RegisterFile failed: %v", err)
	}

	err = db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(DBBucketName))
		data := b.Get([]byte(fileID))
		if data == nil {
			t.Fatal("Metadata not found in DB")
		}

		var meta FileMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			t.Fatalf("Failed to unmarshal meta: %v", err)
		}

		if meta.ID != fileID {
			t.Errorf("Want ID %s, got %s", fileID, meta.ID)
		}
		if meta.Size != fileSize {
			t.Errorf("Want Size %d, got %d", fileSize, meta.Size)
		}
		if meta.ExpiresAt.Before(time.Now()) {
			t.Error("Expiration time is in the past")
		}

		bIndex := tx.Bucket([]byte(DBBucketIndexName))
		indexKey := []byte(meta.ExpiresAt.Format(time.RFC3339) + "_" + fileID)
		if val := bIndex.Get(indexKey); val == nil {
			t.Error("Index entry not found")
		} else if string(val) != fileID {
			t.Errorf("Index value mismatch: want %s, got %s", fileID, string(val))
		}

		return nil
	})
	if err != nil {
		t.Error(err)
	}
}
