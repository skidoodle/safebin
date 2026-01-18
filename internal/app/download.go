package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"github.com/skidoodle/safebin/internal/crypto"
	"go.etcd.io/bbolt"
)

func (app *App) HandleGetFile(writer http.ResponseWriter, request *http.Request) {
	slug := request.PathValue("slug")
	if len(slug) < SlugLength {
		app.SendError(writer, request, http.StatusBadRequest)
		return
	}

	keyBase64 := slug[:SlugLength]
	ext := slug[SlugLength:]

	key, err := base64.RawURLEncoding.DecodeString(keyBase64)
	if err != nil || len(key) != KeyLength {
		app.SendError(writer, request, http.StatusUnauthorized)
		return
	}

	id := crypto.GetID(key, ext)

	var meta FileMeta
	err = app.DB.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(DBBucketName))
		if b == nil {
			return fmt.Errorf("bucket not found")
		}
		data := b.Get([]byte(id))
		if data == nil {
			return fmt.Errorf("file not found")
		}
		return json.Unmarshal(data, &meta)
	})

	if err != nil {
		app.SendError(writer, request, http.StatusNotFound)
		return
	}

	path := filepath.Join(app.Conf.StorageDir, id)
	info, err := os.Stat(path)
	if err != nil {
		app.SendError(writer, request, http.StatusNotFound)
		return
	}

	if info.Size() != meta.Size {
		app.Logger.Error("Integrity check failed: disk size mismatch",
			"id", id,
			"disk_bytes", info.Size(),
			"expected_bytes", meta.Size,
		)
		app.SendError(writer, request, http.StatusInternalServerError)
		return
	}

	file, err := os.Open(path)

	if err != nil {
		app.Logger.Error("Failed to open file", "path", path, "err", err)
		app.SendError(writer, request, http.StatusInternalServerError)
		return
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			app.Logger.Error("Failed to close file", "err", closeErr)
		}
	}()

	streamer, err := crypto.NewGCMStreamer(key)

	if err != nil {
		app.Logger.Error("Failed to create crypto streamer", "err", err)
		app.SendError(writer, request, http.StatusInternalServerError)
		return
	}

	decryptor := crypto.NewDecryptor(file, streamer.AEAD, info.Size())

	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	csp := "default-src 'none'; img-src 'self' data:; media-src 'self' data:; " +
		"style-src 'unsafe-inline'; sandbox allow-forms allow-scripts allow-downloads allow-same-origin"

	writer.Header().Set("Content-Type", contentType)
	writer.Header().Set("Content-Security-Policy", csp)
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", slug))

	http.ServeContent(writer, request, slug, info.ModTime(), decryptor)
}
