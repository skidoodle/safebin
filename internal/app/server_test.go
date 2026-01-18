package app

import (
	"bytes"
	"fmt"
	"html/template"
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
	os.MkdirAll(filepath.Join(storageDir, "tmp"), 0700)

	tmplDir := filepath.Join(storageDir, "templates")
	os.MkdirAll(tmplDir, 0700)
	os.WriteFile(filepath.Join(tmplDir, "base.html"), []byte(`{{define "base"}}{{template "content" .}}{{end}}`), 0600)
	os.WriteFile(filepath.Join(tmplDir, "index.html"), []byte(`{{define "content"}}OK{{end}}`), 0600)

	tmpl := template.Must(template.New("base").Parse(`{{define "base"}}OK{{end}}`))

	app := &App{
		Conf: Config{
			StorageDir: storageDir,
			MaxMB:      10,
		},
		Logger: discardLogger(),
		Tmpl:   tmpl,
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
	part, _ := writer.CreateFormFile("file", "test.txt")
	content := []byte("Hello Safebin")
	part.Write(content)
	writer.Close()

	req, _ := http.NewRequest("POST", server.URL+"/", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Upload request failed: %v", err)
	}
	defer resp.Body.Close()

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
	defer resp.Body.Close()

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
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Finish failed: %d", resp.StatusCode)
	}

	respBytes, _ := io.ReadAll(resp.Body)
	respStr := string(respBytes)
	parts := strings.Split(strings.TrimSpace(respStr), "/")
	slugWithExt := parts[len(parts)-1]

	downloadURL := fmt.Sprintf("%s/%s", server.URL, slugWithExt)
	dlResp, _ := http.Get(downloadURL)
	dlBytes, _ := io.ReadAll(dlResp.Body)
	dlResp.Body.Close()

	if !bytes.Equal(content, dlBytes) {
		t.Errorf("Chunked reassembly failed. Want %s, got %s", content, dlBytes)
	}
}

func uploadChunk(t *testing.T, baseURL, uid string, idx int, data []byte) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("upload_id", uid)
	writer.WriteField("index", fmt.Sprintf("%d", idx))
	part, _ := writer.CreateFormFile("chunk", "blob")
	part.Write(data)
	writer.Close()

	req, _ := http.NewRequest("POST", baseURL+"/upload/chunk", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("Chunk %d upload failed: %v", idx, err)
	}
	resp.Body.Close()
}

func postForm(t *testing.T, url string, fields map[string]string) *http.Response {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for k, v := range fields {
		writer.WriteField(k, v)
	}
	writer.Close()

	req, _ := http.NewRequest("POST", url, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Post form failed: %v", err)
	}
	return resp
}
