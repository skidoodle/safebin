package app

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
)

func (app *App) Routes() *http.ServeMux {
	mux := http.NewServeMux()

	fileServer := http.FileServer(http.FS(app.Assets))

	staticHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "" || strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}

		if strings.HasSuffix(r.URL.Path, ".html") {
			http.NotFound(w, r)
			return
		}

		fileServer.ServeHTTP(w, r)
	})

	mux.Handle("GET /static/", http.StripPrefix("/static/", staticHandler))
	mux.HandleFunc("GET /{$}", app.HandleHome)
	mux.HandleFunc("POST /{$}", app.HandleUpload)
	mux.HandleFunc("POST /upload/chunk", app.HandleChunk)
	mux.HandleFunc("POST /upload/finish", app.HandleFinish)
	mux.HandleFunc("GET /{slug}", app.HandleGetFile)

	return mux
}

func (app *App) HandleHome(writer http.ResponseWriter, request *http.Request) {
	err := app.Tmpl.ExecuteTemplate(writer, "layout", map[string]any{
		"MaxMB": app.Conf.MaxMB,
		"Host":  request.Host,
	})

	if err != nil {
		app.Logger.Error("Template error", "err", err)
	}
}

func (app *App) RespondWithLink(writer http.ResponseWriter, request *http.Request, key []byte, originalName string) {
	keySlug := base64.RawURLEncoding.EncodeToString(key)
	ext := filepath.Ext(originalName)
	link := fmt.Sprintf("%s/%s%s", request.Host, keySlug, ext)

	if request.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		html := `
			<div class="result-container">
				<div class="dim result-label">Upload Complete:</div>
				<div class="copy-box">
					<input type="text" value="%s" id="share-url" readonly onclick="this.select()">
					<button onclick="copyToClipboard(this)">Copy</button>
				</div>
				<div class="reset-wrapper">
					<button class="reset-btn" onclick="resetUI()">Upload another</button>
				</div>
			</div>`

		if _, err := fmt.Fprintf(writer, html, link); err != nil {
			app.Logger.Error("Failed to write response", "err", err)
		}
		return
	}

	scheme := "https"
	if request.TLS == nil {
		scheme = "http"
	}

	if _, err := fmt.Fprintf(writer, "%s://%s\n", scheme, link); err != nil {
		app.Logger.Error("Failed to write response", "err", err)
	}
}

func (app *App) SendError(writer http.ResponseWriter, request *http.Request, code int) {
	if request.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		writer.WriteHeader(code)

		html := `
			<div class="result-container">
				<div class="error-text">Error %d</div>
				<div class="reset-wrapper">
					<button class="reset-btn" onclick="resetUI()">Try again</button>
				</div>
			</div>`

		if _, err := fmt.Fprintf(writer, html, code); err != nil {
			app.Logger.Error("Failed to write error response", "err", err)
		}
		return
	}

	http.Error(writer, http.StatusText(code), code)
}
