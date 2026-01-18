package app

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/skidoodle/safebin/internal/crypto"
)

var reUploadID = regexp.MustCompile(`^[a-zA-Z0-9]{10,50}$`)

func (app *App) HandleUpload(writer http.ResponseWriter, request *http.Request) {
	limit := (app.Conf.MaxMB * MegaByte) + MegaByte
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

	tmp, err := os.CreateTemp(filepath.Join(app.Conf.StorageDir, TempDirName), "up_*")

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

	if _, err := tmp.Seek(0, 0); err != nil {
		app.Logger.Error("Seek failed", "err", err)
		app.SendError(writer, request, http.StatusInternalServerError)
		return
	}

	key, err := crypto.DeriveKey(tmp)
	if err != nil {
		app.Logger.Error("Key derivation failed", "err", err)
		app.SendError(writer, request, http.StatusInternalServerError)
		return
	}

	if _, err := tmp.Seek(0, 0); err != nil {
		app.Logger.Error("Seek failed", "err", err)
		app.SendError(writer, request, http.StatusInternalServerError)
		return
	}

	app.finalizeUpload(writer, request, tmp, key, header.Filename)
}

func (app *App) HandleChunk(writer http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(writer, request.Body, MaxRequestOverhead)

	uid := request.FormValue("upload_id")

	idx, err := strconv.Atoi(request.FormValue("index"))
	if err != nil {
		app.SendError(writer, request, http.StatusBadRequest)
		return
	}

	maxChunks := int((app.Conf.MaxMB*MegaByte)/UploadChunkSize) + ChunkSafetyMargin

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

func (app *App) HandleFinish(writer http.ResponseWriter, request *http.Request) {
	uid := request.FormValue("upload_id")

	total, err := strconv.Atoi(request.FormValue("total"))
	if err != nil {
		app.SendError(writer, request, http.StatusBadRequest)
		return
	}

	maxChunks := int((app.Conf.MaxMB*MegaByte)/UploadChunkSize) + ChunkSafetyMargin

	if !reUploadID.MatchString(uid) || total > maxChunks || total <= 0 {
		app.SendError(writer, request, http.StatusBadRequest)
		return
	}

	files, err := app.openChunkFiles(uid, total)
	if err != nil {
		app.Logger.Error("Failed to open chunks", "err", err)
		app.SendError(writer, request, http.StatusInternalServerError)
		return
	}

	defer func() {
		for _, f := range files {
			_ = f.Close()
		}
		if err := os.RemoveAll(filepath.Join(app.Conf.StorageDir, TempDirName, uid)); err != nil {
			app.Logger.Error("Failed to remove chunk dir", "err", err)
		}
	}()

	readers := make([]io.Reader, len(files))
	for i, f := range files {
		readers[i] = f
	}

	key, err := crypto.DeriveKey(io.MultiReader(readers...))
	if err != nil {
		app.Logger.Error("Key derivation failed", "err", err)
		app.SendError(writer, request, http.StatusInternalServerError)
		return
	}

	for _, f := range files {
		if _, err := f.Seek(0, 0); err != nil {
			app.Logger.Error("Failed to reset chunk", "err", err)
			app.SendError(writer, request, http.StatusInternalServerError)
			return
		}
	}

	multiSrc := io.MultiReader(readers...)

	app.finalizeUpload(writer, request, multiSrc, key, request.FormValue("filename"))
}

func (app *App) finalizeUpload(writer http.ResponseWriter, request *http.Request, src io.Reader, key []byte, filename string) {
	ext := filepath.Ext(filename)
	id := crypto.GetID(key, ext)
	finalPath := filepath.Join(app.Conf.StorageDir, id)

	if info, err := os.Stat(finalPath); err == nil {
		if err := app.RegisterFile(id, info.Size()); err != nil {
			app.Logger.Error("Failed to update metadata for existing file", "err", err)
		}
		app.RespondWithLink(writer, request, key, filename)
		return
	}

	if err := app.encryptAndSave(src, key, finalPath); err != nil {
		app.Logger.Error("Encryption failed", "err", err)
		app.SendError(writer, request, http.StatusInternalServerError)
		return
	}

	if info, err := os.Stat(finalPath); err == nil {
		if err := app.RegisterFile(id, info.Size()); err != nil {
			app.Logger.Error("Failed to save metadata", "err", err)
		}
	} else {
		app.Logger.Error("Failed to stat new file", "err", err)
	}

	app.RespondWithLink(writer, request, key, filename)
}
