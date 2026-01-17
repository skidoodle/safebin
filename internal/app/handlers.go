package app

import (
	"encoding/base64"
	"errors"
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

const (
	uploadChunkSize    = 8 << 20
	maxRequestOverhead = 10 << 20
	permUserRWX        = 0o700
	slugLength         = 22
	keyLength          = 16
	megaByte           = 1 << 20
	chunkSafetyMargin  = 2
)

var reUploadID = regexp.MustCompile(`^[a-zA-Z0-9]{10,50}$`)

func (app *App) HandleHome(writer http.ResponseWriter, request *http.Request) {
	err := app.Tmpl.ExecuteTemplate(writer, "base", map[string]any{
		"MaxMB": app.Conf.MaxMB,
		"Host":  request.Host,
	})

	if err != nil {
		app.Logger.Error("Template error", "err", err)
	}
}

func (app *App) HandleUpload(writer http.ResponseWriter, request *http.Request) {
	limit := (app.Conf.MaxMB * megaByte) + megaByte
	request.Body = http.MaxBytesReader(writer, request.Body, limit)

	file, header, err := request.FormFile("file")

	if err != nil {
		if err.Error() == "http: request body too large" {
			app.SendError(writer, request, http.StatusRequestEntityTooLarge)
			return
		}

		app.SendError(writer, request, http.StatusBadRequest)

		return
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			app.Logger.Error("Failed to close upload file", "err", closeErr)
		}
	}()

	tmp, err := os.CreateTemp(filepath.Join(app.Conf.StorageDir, "tmp"), "up_*")

	if err != nil {
		app.Logger.Error("Failed to create temp file", "err", err)
		app.SendError(writer, request, http.StatusInternalServerError)

		return
	}

	tmpPath := tmp.Name()

	defer func() {
		if removeErr := os.Remove(tmpPath); removeErr != nil && !os.IsNotExist(removeErr) {
			app.Logger.Error("Failed to remove temp file", "err", removeErr)
		}
	}()

	defer func() {
		if closeErr := tmp.Close(); closeErr != nil {
			app.Logger.Error("Failed to close temp file", "err", closeErr)
		}
	}()

	if _, err := io.Copy(tmp, file); err != nil {
		app.Logger.Error("Failed to write temp file", "err", err)
		app.SendError(writer, request, http.StatusRequestEntityTooLarge)

		return
	}

	app.FinalizeFile(writer, request, tmp, header.Filename)
}

func (app *App) HandleChunk(writer http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestOverhead)

	uid := request.FormValue("upload_id")

	idx, err := strconv.Atoi(request.FormValue("index"))
	if err != nil {
		app.SendError(writer, request, http.StatusBadRequest)
		return
	}

	maxChunks := int((app.Conf.MaxMB*megaByte)/uploadChunkSize) + chunkSafetyMargin

	if !reUploadID.MatchString(uid) || idx > maxChunks || idx < 0 {
		app.SendError(writer, request, http.StatusBadRequest)
		return
	}

	file, _, err := request.FormFile("chunk")

	if err != nil {
		if err.Error() == "http: request body too large" {
			app.SendError(writer, request, http.StatusRequestEntityTooLarge)
			return
		}

		app.SendError(writer, request, http.StatusBadRequest)

		return
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			app.Logger.Error("Failed to close chunk file", "err", closeErr)
		}
	}()

	if err := app.saveChunk(uid, idx, file); err != nil {
		app.Logger.Error("Failed to save chunk", "err", err)
		app.SendError(writer, request, http.StatusInternalServerError)
	}
}

func (app *App) saveChunk(uid string, idx int, src io.Reader) error {
	dir := filepath.Join(app.Conf.StorageDir, "tmp", uid)

	if err := os.MkdirAll(dir, permUserRWX); err != nil {
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

	if _, err := io.Copy(dest, src); err != nil {
		return fmt.Errorf("copy chunk: %w", err)
	}

	return nil
}

func (app *App) HandleFinish(writer http.ResponseWriter, request *http.Request) {
	uid := request.FormValue("upload_id")

	total, err := strconv.Atoi(request.FormValue("total"))
	if err != nil {
		app.SendError(writer, request, http.StatusBadRequest)
		return
	}

	maxChunks := int((app.Conf.MaxMB*megaByte)/uploadChunkSize) + chunkSafetyMargin

	if !reUploadID.MatchString(uid) || total > maxChunks || total <= 0 {
		app.SendError(writer, request, http.StatusBadRequest)
		return
	}

	mergedPath, err := app.mergeChunks(uid, total)

	if err != nil {
		app.Logger.Error("Merge failed", "err", err)

		if errors.Is(err, io.ErrShortWrite) {
			app.SendError(writer, request, http.StatusRequestEntityTooLarge)
		} else {
			app.SendError(writer, request, http.StatusInternalServerError)
		}

		return
	}

	defer func() {
		if removeErr := os.Remove(mergedPath); removeErr != nil && !os.IsNotExist(removeErr) {
			app.Logger.Error("Failed to remove merged file", "err", removeErr)
		}
	}()

	mergedRead, err := os.Open(mergedPath)

	if err != nil {
		app.Logger.Error("Failed to open merged file", "err", err)
		app.SendError(writer, request, http.StatusInternalServerError)

		return
	}

	defer func() {
		if closeErr := mergedRead.Close(); closeErr != nil {
			app.Logger.Error("Failed to close merged reader", "err", closeErr)
		}
	}()

	app.FinalizeFile(writer, request, mergedRead, request.FormValue("filename"))

	if err := os.RemoveAll(filepath.Join(app.Conf.StorageDir, "tmp", uid)); err != nil {
		app.Logger.Error("Failed to remove chunk dir", "err", err)
	}
}

func (app *App) mergeChunks(uid string, total int) (string, error) {
	tmpPath := filepath.Join(app.Conf.StorageDir, "tmp", "m_"+uid)

	merged, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("create merge file: %w", err)
	}

	defer func() {
		if closeErr := merged.Close(); closeErr != nil {
			app.Logger.Error("Failed to close merged file", "err", closeErr)
		}
	}()

	limit := app.Conf.MaxMB * megaByte
	var written int64

	for i := range total {
		partPath := filepath.Join(app.Conf.StorageDir, "tmp", uid, strconv.Itoa(i))

		part, err := os.Open(partPath)
		if err != nil {
			return "", fmt.Errorf("open chunk %d: %w", i, err)
		}

		n, err := io.Copy(merged, part)

		if closeErr := part.Close(); closeErr != nil {
			app.Logger.Error("Failed to close chunk part", "err", closeErr)
		}

		if err != nil {
			return "", fmt.Errorf("append chunk %d: %w", i, err)
		}

		written += n
		if written > limit {
			return "", io.ErrShortWrite
		}
	}

	return tmpPath, nil
}

func (app *App) HandleGetFile(writer http.ResponseWriter, request *http.Request) {
	slug := request.PathValue("slug")
	if len(slug) < slugLength {
		app.SendError(writer, request, http.StatusBadRequest)
		return
	}

	keyBase64 := slug[:slugLength]
	ext := slug[slugLength:]

	key, err := base64.RawURLEncoding.DecodeString(keyBase64)
	if err != nil || len(key) != keyLength {
		app.SendError(writer, request, http.StatusUnauthorized)
		return
	}

	id := crypto.GetID(key, ext)
	path := filepath.Join(app.Conf.StorageDir, id)

	info, err := os.Stat(path)
	if err != nil {
		app.SendError(writer, request, http.StatusNotFound)
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

func (app *App) FinalizeFile(writer http.ResponseWriter, request *http.Request, src *os.File, filename string) {
	if _, err := src.Seek(0, 0); err != nil {
		app.Logger.Error("Seek failed", "err", err)
		app.SendError(writer, request, http.StatusInternalServerError)

		return
	}

	key, err := crypto.DeriveKey(src)

	if err != nil {
		app.Logger.Error("Key derivation failed", "err", err)
		app.SendError(writer, request, http.StatusInternalServerError)

		return
	}

	ext := filepath.Ext(filename)
	id := crypto.GetID(key, ext)
	finalPath := filepath.Join(app.Conf.StorageDir, id)

	if _, err := os.Stat(finalPath); err == nil {
		app.RespondWithLink(writer, request, key, filename)
		return
	}

	if _, err := src.Seek(0, 0); err != nil {
		app.Logger.Error("Seek failed", "err", err)
		app.SendError(writer, request, http.StatusInternalServerError)

		return
	}

	if err := app.encryptAndSave(src, key, finalPath); err != nil {
		app.Logger.Error("Encryption failed", "err", err)
		app.SendError(writer, request, http.StatusInternalServerError)

		return
	}

	app.RespondWithLink(writer, request, key, filename)
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
