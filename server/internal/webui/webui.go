// Package webui serves the dashboard that web/ builds into dist/. The built files are embedded in the server binary,
// so one file carries the server and its page. The folder is not committed (only a .gitkeep is), so a server built
// without building web/ first still compiles and says so instead of showing a blank page.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var built embed.FS

// Handler serves the embedded dashboard.
func Handler() http.Handler {
	sub, err := fs.Sub(built, "dist")
	if err != nil {
		return notBuilt()
	}
	return handlerFor(sub)
}

func handlerFor(fsys fs.FS) http.Handler {
	if _, err := fs.Stat(fsys, "index.html"); err != nil {
		return notBuilt()
	}
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The page is checked again on every load so an updated server shows its new dashboard at once; the
		// hashed files under assets/ never change, so they can be kept.
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}

func notBuilt() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "The dashboard is not built into this server. Run `npm run build` in web/, then rebuild the server.", http.StatusNotFound)
	})
}
