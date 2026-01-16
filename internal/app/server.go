package app

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"path/filepath"
)

func (app *App) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	fs := http.FileServer(http.Dir("./web/static"))
	mux.Handle("GET /static/", http.StripPrefix("/static/", fs))
	mux.HandleFunc("GET /{$}", app.HandleHome)
	mux.HandleFunc("POST /{$}", app.HandleUpload)
	mux.HandleFunc("POST /upload/chunk", app.HandleChunk)
	mux.HandleFunc("POST /upload/finish", app.HandleFinish)
	mux.HandleFunc("GET /{slug}", app.HandleGetFile)
	return mux
}

func (app *App) RespondWithLink(w http.ResponseWriter, r *http.Request, key []byte, originalName string) {
	keySlug := base64.RawURLEncoding.EncodeToString(key)
	ext := filepath.Ext(originalName)
	link := fmt.Sprintf("%s/%s%s", r.Host, keySlug, ext)
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		fmt.Fprintf(w, `
			<div class="result-container">
				<div class="dim result-label">Upload Complete:</div>
				<div class="copy-box">
					<input type="text" value="%s" id="share-url" readonly onclick="this.select()">
					<button onclick="copyToClipboard(this)">Copy</button>
				</div>
				<div class="reset-wrapper">
					<button class="reset-btn" onclick="resetUI()">Upload another</button>
				</div>
			</div>`, link)
		return
	}
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	fmt.Fprintf(w, "%s://%s\n", scheme, link)
}

func (app *App) SendError(w http.ResponseWriter, r *http.Request, code int) {
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		w.WriteHeader(code)
		fmt.Fprintf(w, `
			<div class="result-container">
				<div class="error-text">Error %d</div>
				<div class="reset-wrapper">
					<button class="reset-btn" onclick="resetUI()">Try again</button>
				</div>
			</div>`, code)
		return
	}
	http.Error(w, http.StatusText(code), code)
}
