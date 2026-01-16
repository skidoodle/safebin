package app

import (
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/skidoodle/safebin/internal/crypto"
)

var reUploadID = regexp.MustCompile(`^[a-zA-Z0-9]{10,50}$`)

func (app *App) HandleHome(w http.ResponseWriter, r *http.Request) {
	err := app.Tmpl.ExecuteTemplate(w, "base", map[string]any{
		"MaxMB": app.Conf.MaxMB,
		"Host":  r.Host,
	})
	if err != nil {
		app.Logger.Error("Template error", "err", err)
	}
}

func (app *App) HandleUpload(w http.ResponseWriter, r *http.Request) {
	limit := (app.Conf.MaxMB << 20) + (1 << 20)
	r.Body = http.MaxBytesReader(w, r.Body, limit)

	file, header, err := r.FormFile("file")
	if err != nil {
		if err.Error() == "http: request body too large" {
			app.SendError(w, r, http.StatusRequestEntityTooLarge)
			return
		}
		app.SendError(w, r, http.StatusBadRequest)
		return
	}
	defer file.Close()

	tmpPath := filepath.Join(app.Conf.StorageDir, "tmp", fmt.Sprintf("up_%d", os.Getpid()))
	tmp, err := os.Create(tmpPath)
	if err != nil {
		app.Logger.Error("Failed to create temp file", "err", err)
		app.SendError(w, r, http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmpPath)
	defer tmp.Close()

	if _, err := io.Copy(tmp, file); err != nil {
		app.Logger.Error("Failed to write temp file", "err", err)
		app.SendError(w, r, http.StatusRequestEntityTooLarge)
		return
	}

	app.FinalizeFile(w, r, tmp, header.Filename)
}

func (app *App) HandleChunk(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)

	uid := r.FormValue("upload_id")
	idx, err := strconv.Atoi(r.FormValue("index"))
	if err != nil {
		app.SendError(w, r, http.StatusBadRequest)
		return
	}

	const chunkSize = 8 << 20
	maxChunks := int((app.Conf.MaxMB<<20)/chunkSize) + 2

	if !reUploadID.MatchString(uid) || idx > maxChunks || idx < 0 {
		app.SendError(w, r, http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("chunk")
	if err != nil {
		if err.Error() == "http: request body too large" {
			app.SendError(w, r, http.StatusRequestEntityTooLarge)
			return
		}
		app.SendError(w, r, http.StatusBadRequest)
		return
	}
	defer file.Close()

	dir := filepath.Join(app.Conf.StorageDir, "tmp", uid)
	if err := os.MkdirAll(dir, 0700); err != nil {
		app.Logger.Error("Failed to create chunk dir", "err", err)
		app.SendError(w, r, http.StatusInternalServerError)
		return
	}

	dest, err := os.Create(filepath.Join(dir, strconv.Itoa(idx)))
	if err != nil {
		app.Logger.Error("Failed to create chunk file", "err", err)
		app.SendError(w, r, http.StatusInternalServerError)
		return
	}
	defer dest.Close()

	if _, err := io.Copy(dest, file); err != nil {
		app.Logger.Error("Failed to save chunk", "err", err)
		app.SendError(w, r, http.StatusInternalServerError)
		return
	}
}

func (app *App) HandleFinish(w http.ResponseWriter, r *http.Request) {
	uid := r.FormValue("upload_id")
	total, err := strconv.Atoi(r.FormValue("total"))
	if err != nil {
		app.SendError(w, r, http.StatusBadRequest)
		return
	}

	const chunkSize = 8 << 20
	maxChunks := int((app.Conf.MaxMB<<20)/chunkSize) + 2

	if !reUploadID.MatchString(uid) || total > maxChunks || total <= 0 {
		app.SendError(w, r, http.StatusBadRequest)
		return
	}

	tmpPath := filepath.Join(app.Conf.StorageDir, "tmp", "m_"+uid)
	merged, err := os.Create(tmpPath)
	if err != nil {
		app.Logger.Error("Failed to create merge file", "err", err)
		app.SendError(w, r, http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmpPath)
	defer merged.Close()

	limit := app.Conf.MaxMB << 20
	var written int64

	for i := range total {
		partPath := filepath.Join(app.Conf.StorageDir, "tmp", uid, strconv.Itoa(i))
		part, err := os.Open(partPath)
		if err != nil {
			app.Logger.Error("Missing chunk during merge", "uid", uid, "index", i, "err", err)
			app.SendError(w, r, http.StatusBadRequest)
			return
		}

		n, err := io.Copy(merged, part)
		part.Close()
		if err != nil {
			app.Logger.Error("Failed to append chunk", "err", err)
			app.SendError(w, r, http.StatusInternalServerError)
			return
		}

		written += n
		if written > limit {
			app.SendError(w, r, http.StatusRequestEntityTooLarge)
			return
		}
	}

	if err := merged.Close(); err != nil {
		app.Logger.Error("Failed to close merged file", "err", err)
		app.SendError(w, r, http.StatusInternalServerError)
		return
	}

	mergedRead, err := os.Open(tmpPath)
	if err != nil {
		app.Logger.Error("Failed to open merged file", "err", err)
		app.SendError(w, r, http.StatusInternalServerError)
		return
	}
	defer mergedRead.Close()

	app.FinalizeFile(w, r, mergedRead, r.FormValue("filename"))
	os.RemoveAll(filepath.Join(app.Conf.StorageDir, "tmp", uid))
}

func (app *App) HandleGetFile(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if len(slug) < 22 {
		app.SendError(w, r, http.StatusBadRequest)
		return
	}

	keyBase64 := slug[:22]
	ext := slug[22:]

	key, err := base64.RawURLEncoding.DecodeString(keyBase64)
	if err != nil || len(key) != 16 {
		app.SendError(w, r, http.StatusUnauthorized)
		return
	}

	id := crypto.GetID(key, ext)
	path := filepath.Join(app.Conf.StorageDir, id)

	info, err := os.Stat(path)
	if err != nil {
		app.SendError(w, r, http.StatusNotFound)
		return
	}

	f, err := os.Open(path)
	if err != nil {
		app.Logger.Error("Failed to open file", "path", path, "err", err)
		app.SendError(w, r, http.StatusInternalServerError)
		return
	}
	defer f.Close()

	streamer, err := crypto.NewGCMStreamer(key)
	if err != nil {
		app.Logger.Error("Failed to create crypto streamer", "err", err)
		app.SendError(w, r, http.StatusInternalServerError)
		return
	}

	decryptor := crypto.NewDecryptor(f, streamer.AEAD, info.Size())

	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; media-src 'self' data:; style-src 'unsafe-inline'; sandbox allow-forms allow-scripts allow-downloads allow-same-origin")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", slug))

	http.ServeContent(w, r, slug, info.ModTime(), decryptor)
}

func (app *App) FinalizeFile(w http.ResponseWriter, r *http.Request, src *os.File, filename string) {
	if _, err := src.Seek(0, 0); err != nil {
		app.Logger.Error("Seek failed", "err", err)
		app.SendError(w, r, http.StatusInternalServerError)
		return
	}

	key, err := crypto.DeriveKey(src)
	if err != nil {
		app.Logger.Error("Key derivation failed", "err", err)
		app.SendError(w, r, http.StatusInternalServerError)
		return
	}

	ext := filepath.Ext(filename)
	id := crypto.GetID(key, ext)

	if _, err := src.Seek(0, 0); err != nil {
		app.Logger.Error("Seek failed", "err", err)
		app.SendError(w, r, http.StatusInternalServerError)
		return
	}

	finalPath := filepath.Join(app.Conf.StorageDir, id)

	if _, err := os.Stat(finalPath); err == nil {
		app.RespondWithLink(w, r, key, filename)
		return
	}

	out, err := os.Create(finalPath + ".tmp")
	if err != nil {
		app.Logger.Error("Failed to create final file", "err", err)
		app.SendError(w, r, http.StatusInternalServerError)
		return
	}
	defer func() {
		out.Close()
		os.Remove(finalPath + ".tmp")
	}()

	streamer, err := crypto.NewGCMStreamer(key)
	if err != nil {
		app.Logger.Error("Failed to create streamer", "err", err)
		app.SendError(w, r, http.StatusInternalServerError)
		return
	}

	if err := streamer.EncryptStream(out, src); err != nil {
		app.Logger.Error("Encryption failed", "err", err)
		app.SendError(w, r, http.StatusInternalServerError)
		return
	}

	if err := out.Close(); err != nil {
		app.Logger.Error("Failed to close final file", "err", err)
		app.SendError(w, r, http.StatusInternalServerError)
		return
	}

	if err := os.Rename(finalPath+".tmp", finalPath); err != nil {
		app.Logger.Error("Failed to rename final file", "err", err)
		app.SendError(w, r, http.StatusInternalServerError)
		return
	}

	app.RespondWithLink(w, r, key, filename)
}
