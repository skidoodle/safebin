package app

import (
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
	path := filepath.Join(storageDir, DBFileName)
	db, err := bbolt.Open(path, 0600, &bbolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, err
	}

	err = db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(DBBucketName))
		return err
	})

	if err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}
