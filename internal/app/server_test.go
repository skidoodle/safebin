package app

import (
	"bytes"
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
