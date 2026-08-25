package ui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed static
var staticFS embed.FS

func Handler() http.Handler {
	sub, _ := fs.Sub(staticFS, "static")
	fileServer := http.StripPrefix("/assets", http.FileServer(http.FS(sub)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case path == "/" || path == "":
			data, err := fs.ReadFile(sub, "index.html")
			if err != nil {
				http.Error(w, "ui missing", 500)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(data)
		case strings.HasPrefix(path, "/assets/"):
			fileServer.ServeHTTP(w, r)
		case !strings.HasPrefix(path, "/v1/") && !strings.HasPrefix(path, "/admin/"):
			http.Redirect(w, r, "/", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	})
}
