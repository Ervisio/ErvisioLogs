// Package webui serves the embedded single-page application.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
)

//go:embed all:dist
var embedded embed.FS

// Handler serves the built UI from the embedded filesystem.
func Handler() http.Handler {
	sub, _ := fs.Sub(embedded, "dist")
	return handler(sub)
}

func handler(root fs.FS) http.Handler {
	files := http.FileServerFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			httpx.WriteProblem(w, r, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(root, name); err == nil {
			if strings.HasPrefix(name, "assets/") { // content-hashed by Vite
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}
		// A missing file with an extension is a real 404; anything else is a client-side route.
		if path.Ext(name) != "" {
			httpx.WriteProblem(w, r, http.StatusNotFound, "not found")
			return
		}
		index, err := fs.ReadFile(root, "index.html")
		if err != nil {
			httpx.WriteProblem(w, r, http.StatusNotFound, "web UI is not built into this binary")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	})
}
