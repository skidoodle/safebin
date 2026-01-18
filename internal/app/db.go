package app

import (
	"os"
	"path/filepath"
	"time"

	"go.etcd.io/bbolt"
)

type FileMeta struct {
	ID        string    `json:"id"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

func InitDB(storageDir string) (*bbolt.DB, error) {
	dbDir := filepath.Join(storageDir, DBDirName)
	if err := os.MkdirAll(dbDir, PermUserRWX); err != nil {
		return nil, err
	}

	path := filepath.Join(dbDir, DBFileName)
	db, err := bbolt.Open(path, 0600, &bbolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, err
	}

	err = db.Update(func(tx *bbolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte(DBBucketName)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(DBBucketIndexName)); err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}
