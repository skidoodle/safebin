package app

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skidoodle/safebin/internal/crypto"
)

func setupTestApp(t *testing.T) (*App, string) {
	storageDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(storageDir, TempDirName), 0700); err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	webDir := filepath.Join(storageDir, "web")
	if err := os.MkdirAll(webDir, 0700); err != nil {
		t.Fatalf("Failed to create web dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(webDir, "layout.html"), []byte(`{{define "layout"}}{{template "content" .}}{{end}}`), 0600); err != nil {
		t.Fatalf("Failed to write layout.html: %v", err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "home.html"), []byte(`{{define "content"}}OK{{end}}`), 0600); err != nil {
		t.Fatalf("Failed to write home.html: %v", err)
	}

	testFS := os.DirFS(webDir)
	tmpl := ParseTemplates(testFS)

	db, err := InitDB(storageDir)
	if err != nil {
		t.Fatalf("Failed to init db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Failed to close DB: %v", err)
		}
	})

	app := &App{
		Conf: Config{
			StorageDir: storageDir,
			MaxMB:      10,
		},
		Logger: discardLogger(),
		Tmpl:   tmpl,
		Assets: testFS,
		DB:     db,
	}

	return app, storageDir
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestIntegration_StandardUploadAndDownload(t *testing.T) {
	app, _ := setupTestApp(t)
	server := httptest.NewServer(app.Routes())
	defer server.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "test.txt")
	if err != nil {
		t.Fatalf("CreateFormFile failed: %v", err)
	}
	content := []byte("Hello Safebin")
	if _, err := part.Write(content); err != nil {
		t.Fatalf("Write part failed: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Writer close failed: %v", err)
	}

	req, _ := http.NewRequest("POST", server.URL+"/", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Upload request failed: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("Failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Upload failed status: %d", resp.StatusCode)
	}

	respBytes, _ := io.ReadAll(resp.Body)
	respStr := string(respBytes)
	parts := strings.Split(strings.TrimSpace(respStr), "/")
	slugWithExt := parts[len(parts)-1]

	downloadURL := fmt.Sprintf("%s/%s", server.URL, slugWithExt)
	resp, err = http.Get(downloadURL)
	if err != nil {
		t.Fatalf("Download request failed: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("Failed to close download response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Download failed status: %d", resp.StatusCode)
	}

	downloadedContent, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(content, downloadedContent) {
		t.Errorf("Content mismatch. Want %s, got %s", content, downloadedContent)
	}
}

func TestIntegration_ChunkedUpload(t *testing.T) {
	app, _ := setupTestApp(t)
	server := httptest.NewServer(app.Routes())
	defer server.Close()

	uploadID := "testchunkid123"
	content := []byte("Chunk1Content-Chunk2Content")
	chunk1 := content[:13]
	chunk2 := content[13:]

	uploadChunk(t, server.URL, uploadID, 0, chunk1)
	uploadChunk(t, server.URL, uploadID, 1, chunk2)

	finishURL := fmt.Sprintf("%s/upload/finish", server.URL)
	form := map[string]string{
		"upload_id": uploadID,
		"total":     "2",
		"filename":  "chunked.txt",
	}

	resp := postForm(t, finishURL, form)
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("Failed to close finish response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Finish failed: %d", resp.StatusCode)
	}

	respBytes, _ := io.ReadAll(resp.Body)
	respStr := string(respBytes)
	parts := strings.Split(strings.TrimSpace(respStr), "/")
	slugWithExt := parts[len(parts)-1]

	downloadURL := fmt.Sprintf("%s/%s", server.URL, slugWithExt)
	dlResp, err := http.Get(downloadURL)
	if err != nil {
		t.Fatalf("Download request failed: %v", err)
	}
	dlBytes, _ := io.ReadAll(dlResp.Body)
	if err := dlResp.Body.Close(); err != nil {
		t.Errorf("Failed to close download response body: %v", err)
	}

	if !bytes.Equal(content, dlBytes) {
		t.Errorf("Chunked reassembly failed. Want %s, got %s", content, dlBytes)
	}
}

func TestIntegration_ChunkedUpload_VerifyEncryption(t *testing.T) {
	app, storageDir := setupTestApp(t)
	server := httptest.NewServer(app.Routes())
	defer server.Close()

	uploadID := "securechunk123"
	plaintext := []byte("This is a secret message that should be encrypted")

	uploadChunk(t, server.URL, uploadID, 0, plaintext)

	chunkPath := filepath.Join(storageDir, TempDirName, uploadID, "0")
	encryptedData, err := os.ReadFile(chunkPath)
	if err != nil {
		t.Fatalf("Failed to read chunk file: %v", err)
	}

	if bytes.Contains(encryptedData, plaintext) {
		t.Fatal("Chunk file contains plaintext data!")
	}

	if len(encryptedData) <= crypto.KeySize {
		t.Fatalf("Chunk file too small: %d bytes", len(encryptedData))
	}

	key := encryptedData[:crypto.KeySize]
	ciphertext := encryptedData[crypto.KeySize:]

	streamer, err := crypto.NewGCMStreamer(key)
	if err != nil {
		t.Fatalf("Failed to create streamer: %v", err)
	}

	r := bytes.NewReader(ciphertext)
	d := crypto.NewDecryptor(r, streamer.AEAD, int64(len(ciphertext)))

	decrypted, err := io.ReadAll(d)
	if err != nil {
		t.Fatalf("Failed to decrypt chunk: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("Decrypted data mismatch.\nWant: %s\nGot:  %s", plaintext, decrypted)
	}
}

func TestIntegration_Upload_VerifyEncryption(t *testing.T) {
	app, storageDir := setupTestApp(t)
	server := httptest.NewServer(app.Routes())
	defer server.Close()

	plaintext := []byte("Sensitive Data For Full Upload")

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "secret.txt")
	if err != nil {
		t.Fatalf("CreateFormFile failed: %v", err)
	}
	if _, err := part.Write(plaintext); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Writer close failed: %v", err)
	}

	req, _ := http.NewRequest("POST", server.URL+"/", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("Failed to close response body: %v", err)
		}
	}()

	respBytes, _ := io.ReadAll(resp.Body)
	slug := filepath.Base(strings.TrimSpace(string(respBytes)))

	if len(slug) < SlugLength {
		t.Fatalf("Invalid slug: %s", slug)
	}
	keyBase64 := slug[:SlugLength]
	key, _ := base64.RawURLEncoding.DecodeString(keyBase64)
	ext := filepath.Ext("secret.txt")
	id := crypto.GetID(key, ext)

	finalPath := filepath.Join(storageDir, id)
	finalData, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("Failed to read final file: %v", err)
	}

	if bytes.Contains(finalData, plaintext) {
		t.Fatal("Final file contains plaintext!")
	}

	streamer, _ := crypto.NewGCMStreamer(key)
	d := crypto.NewDecryptor(bytes.NewReader(finalData), streamer.AEAD, int64(len(finalData)))
	decrypted, _ := io.ReadAll(d)

	if !bytes.Equal(decrypted, plaintext) {
		t.Error("Final file decryption failed")
	}
}

func uploadChunk(t *testing.T, baseURL, uid string, idx int, data []byte) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("upload_id", uid); err != nil {
		t.Fatalf("WriteField upload_id failed: %v", err)
	}
	if err := writer.WriteField("index", fmt.Sprintf("%d", idx)); err != nil {
		t.Fatalf("WriteField index failed: %v", err)
	}
	part, err := writer.CreateFormFile("chunk", "blob")
	if err != nil {
		t.Fatalf("CreateFormFile failed: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("Write part failed: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Writer close failed: %v", err)
	}

	req, _ := http.NewRequest("POST", baseURL+"/upload/chunk", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("Chunk %d upload failed: %v", idx, err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("Failed to close chunk response body: %v", err)
	}
}

func postForm(t *testing.T, url string, fields map[string]string) *http.Response {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			t.Fatalf("WriteField %s failed: %v", k, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Writer close failed: %v", err)
	}

	req, _ := http.NewRequest("POST", url, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Post form failed: %v", err)
	}
	return resp
}
