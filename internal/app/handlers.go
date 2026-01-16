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
		app.SendError(w, r, http.StatusBadRequest)
		return
	}
	defer file.Close()

	tmpPath := filepath.Join(app.Conf.StorageDir, "tmp", fmt.Sprintf("up_%d", os.Getpid()))
	tmp, _ := os.Create(tmpPath)
	defer os.Remove(tmpPath)
	defer tmp.Close()

	if _, err := io.Copy(tmp, file); err != nil {
		app.SendError(w, r, http.StatusRequestEntityTooLarge)
		return
	}

	app.FinalizeFile(w, r, tmp, header.Filename)
}

func (app *App) HandleChunk(w http.ResponseWriter, r *http.Request) {
	uid := r.FormValue("upload_id")
	idx, _ := strconv.Atoi(r.FormValue("index"))

	if !reUploadID.MatchString(uid) || idx > 1000 {
		app.SendError(w, r, http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("chunk")
	if err != nil {
		return
	}
	defer file.Close()

	dir := filepath.Join(app.Conf.StorageDir, "tmp", uid)
	os.MkdirAll(dir, 0700)

	dest, _ := os.Create(filepath.Join(dir, strconv.Itoa(idx)))
	defer dest.Close()
	io.Copy(dest, file)
}

func (app *App) HandleFinish(w http.ResponseWriter, r *http.Request) {
	uid := r.FormValue("upload_id")
	total, _ := strconv.Atoi(r.FormValue("total"))

	if !reUploadID.MatchString(uid) || total > 1000 {
		app.SendError(w, r, http.StatusBadRequest)
		return
	}

	tmpPath := filepath.Join(app.Conf.StorageDir, "tmp", "m_"+uid)
	merged, _ := os.Create(tmpPath)
	defer os.Remove(tmpPath)
	defer merged.Close()

	for i := range total {
		partPath := filepath.Join(app.Conf.StorageDir, "tmp", uid, strconv.Itoa(i))
		part, err := os.Open(partPath)
		if err != nil {
			continue
		}
		io.Copy(merged, part)
		part.Close()
	}

	app.FinalizeFile(w, r, merged, r.FormValue("filename"))
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

	f, _ := os.Open(path)
	defer f.Close()

	streamer, _ := crypto.NewGCMStreamer(key)
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
	src.Seek(0, 0)
	key, _ := crypto.DeriveKey(src)

	ext := filepath.Ext(filename)
	id := crypto.GetID(key, ext)

	src.Seek(0, 0)
	finalPath := filepath.Join(app.Conf.StorageDir, id)

	if _, err := os.Stat(finalPath); err == nil {
		app.RespondWithLink(w, r, key, filename)
		return
	}

	out, _ := os.Create(finalPath + ".tmp")
	streamer, _ := crypto.NewGCMStreamer(key)
	if err := streamer.EncryptStream(out, src); err != nil {
		out.Close()
		os.Remove(finalPath + ".tmp")
		app.SendError(w, r, http.StatusInternalServerError)
		return
	}
	out.Close()
	os.Rename(finalPath+".tmp", finalPath)
	app.RespondWithLink(w, r, key, filename)
}
