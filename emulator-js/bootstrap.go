package main

import (
	"embed"
	"net/http"
	"path"
)

//go:embed static
var emulatorAssets embed.FS

func withStaticHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set appropriate headers for static assets
		w.Header().Set("X-Content-Type-Options", "nosniff")

		ext := path.Ext(r.URL.Path)
		if ext == ".html" || r.URL.Path == "/" {
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			// Cache JS, CSS, WASM, images for 1 day
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}

		next.ServeHTTP(w, r)
	})
}
