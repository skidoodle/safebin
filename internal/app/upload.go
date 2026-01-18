package app

import (
	"errors"
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
