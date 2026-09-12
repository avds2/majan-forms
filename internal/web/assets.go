package web

import (
	"bytes"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed admin_dist report_assets
var embeddedAdmin embed.FS

func adminHandler() http.Handler {
	dist, err := fs.Sub(embeddedAdmin, "admin_dist")
	if err != nil {
		panic(err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		requested := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if requested == "." || requested == "" {
			requested = "index.html"
		}
		contents, readErr := fs.ReadFile(dist, requested)
		if readErr != nil {
			requested = "index.html"
			contents, readErr = fs.ReadFile(dist, requested)
		}
		if readErr != nil {
			http.Error(w, "رابط مدیریت در دسترس نیست", http.StatusServiceUnavailable)
			return
		}
		contentType := mime.TypeByExtension(path.Ext(requested))
		if contentType == "" {
			contentType = http.DetectContentType(contents)
		}
		w.Header().Set("Content-Type", contentType)
		if requested == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		} else if strings.HasPrefix(requested, "assets/") || strings.HasPrefix(requested, "fonts/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.ServeContent(w, r, requested, time.Time{}, bytes.NewReader(contents))
	})
}

func reportAssetHandler() http.Handler {
	assets, err := fs.Sub(embeddedAdmin, "report_assets")
	if err != nil {
		panic(err)
	}
	return http.StripPrefix("/report-assets/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		contents, readErr := fs.ReadFile(assets, requested)
		if readErr != nil {
			http.NotFound(w, r)
			return
		}
		contentType := mime.TypeByExtension(path.Ext(requested))
		if contentType == "" {
			contentType = http.DetectContentType(contents)
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeContent(w, r, requested, time.Time{}, bytes.NewReader(contents))
	}))
}
