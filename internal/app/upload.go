package app

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/skidoodle/safebin/internal/crypto"
)

var reUploadID = regexp.MustCompile(`^[a-zA-Z0-9]{10,50}$`)

func (app *App) HandleUpload(writer http.ResponseWriter, request *http.Request) {
	limit := (app.Conf.MaxMB * MegaByte) + MegaByte
	request.Body = http.MaxBytesReader(writer, request.Body, limit)

	mr, err := request.MultipartReader()
	if err != nil {
		app.SendError(writer, request, http.StatusBadRequest)
		return
	}

	var filename string
	var partReader io.Reader

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			app.SendError(writer, request, http.StatusBadRequest)
			return
		}

		if part.FormName() == "file" {
			filename = part.FileName()
			partReader = part
			break
		}
	}

	if partReader == nil {
		app.SendError(writer, request, http.StatusBadRequest)
		return
	}

	tmp, err := os.CreateTemp(filepath.Join(app.Conf.StorageDir, TempDirName), "up_*")
	if err != nil {
		app.Logger.Error("Failed to create temp file", "err", err)
		app.SendError(writer, request, http.StatusInternalServerError)
		return
	}
	tmpPath := tmp.Name()

	defer func() {
		_ = tmp.Close()
		if removeErr := os.Remove(tmpPath); removeErr != nil && !os.IsNotExist(removeErr) {
			app.Logger.Error("Failed to remove temp file", "err", removeErr)
		}
	}()

	ephemeralKey := make([]byte, crypto.KeySize)
	if _, err := rand.Read(ephemeralKey); err != nil {
		app.Logger.Error("Failed to generate ephemeral key", "err", err)
		app.SendError(writer, request, http.StatusInternalServerError)
		return
	}

	pr, pw := io.Pipe()
	hasher := sha256.New()
	errChan := make(chan error, 1)

	go func() {
		_, err := io.Copy(io.MultiWriter(hasher, pw), partReader)
		_ = pw.CloseWithError(err)
		errChan <- err
	}()

	defer func() {
		if closeErr := pr.Close(); closeErr != nil {
			app.Logger.Error("Failed to close pipe reader", "err", closeErr)
		}
	}()

	streamer, err := crypto.NewGCMStreamer(ephemeralKey)
	if err != nil {
		app.Logger.Error("Failed to create streamer", "err", err)
		app.SendError(writer, request, http.StatusInternalServerError)
		return
	}

	if err := streamer.EncryptStream(tmp, pr); err != nil {
		app.Logger.Error("Failed to encrypt stream", "err", err)
		app.SendError(writer, request, http.StatusInternalServerError)
		return
	}

	if err := <-errChan; err != nil {
		if errors.Is(err, http.ErrMissingBoundary) || strings.Contains(err.Error(), "request body too large") {
			app.SendError(writer, request, http.StatusRequestEntityTooLarge)
		} else {
			app.Logger.Error("Failed to read/hash upload", "err", err)
			app.SendError(writer, request, http.StatusInternalServerError)
		}
		return
	}

	convergentKey := hasher.Sum(nil)[:crypto.KeySize]

	if _, err := tmp.Seek(0, 0); err != nil {
		app.Logger.Error("Seek failed", "err", err)
		app.SendError(writer, request, http.StatusInternalServerError)
		return
	}

	info, _ := tmp.Stat()
	decryptor := crypto.NewDecryptor(tmp, streamer.AEAD, info.Size())

	app.finalizeUpload(writer, request, decryptor, convergentKey, filename)
}

func (app *App) HandleChunk(writer http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(writer, request.Body, MaxRequestOverhead)

	uid := request.FormValue("upload_id")
	idx, err := strconv.Atoi(request.FormValue("index"))
	if err != nil {
		app.SendError(writer, request, http.StatusBadRequest)
		return
	}

	maxChunks := int((app.Conf.MaxMB*MegaByte)/MinChunkSize) + ChunkSafetyMargin

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

	maxChunks := int((app.Conf.MaxMB*MegaByte)/MinChunkSize) + ChunkSafetyMargin

	if !reUploadID.MatchString(uid) || total > maxChunks || total <= 0 {
		app.SendError(writer, request, http.StatusBadRequest)
		return
	}

	defer func() {
		if err := os.RemoveAll(filepath.Join(app.Conf.StorageDir, TempDirName, uid)); err != nil {
			app.Logger.Error("Failed to remove chunk dir", "err", err)
		}
	}()

	hasher := sha256.New()
	for i := range total {
		rc, err := app.openChunkDecryptor(uid, i)
		if err != nil {
			app.Logger.Error("Failed to open chunk for hashing", "index", i, "err", err)
			app.SendError(writer, request, http.StatusInternalServerError)
			return
		}
		if _, err := io.Copy(hasher, rc); err != nil {
			_ = rc.Close()
			app.Logger.Error("Failed to hash chunk", "index", i, "err", err)
			app.SendError(writer, request, http.StatusInternalServerError)
			return
		}
		_ = rc.Close()
	}

	convergentKey := hasher.Sum(nil)[:crypto.KeySize]

	multiSrc := &SequentialChunkReader{
		app:   app,
		uid:   uid,
		total: total,
	}
	defer func() {
		if err := multiSrc.Close(); err != nil {
			app.Logger.Error("Failed to close sequential reader", "uid", uid, "err", err)
		}
	}()

	app.finalizeUpload(writer, request, multiSrc, convergentKey, request.FormValue("filename"))
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
