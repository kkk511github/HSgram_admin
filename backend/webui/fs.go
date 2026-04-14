package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static/*
var staticFiles embed.FS

func NewHandler() http.Handler {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}

	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "" {
			w.Header().Set("Cache-Control", "no-store")
			http.ServeFileFS(w, r, sub, "index.html")
			return
		}

		path := r.URL.Path[1:]
		if _, err := fs.Stat(sub, path); err == nil {
			w.Header().Set("Cache-Control", "no-store")
			fileServer.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Cache-Control", "no-store")
		http.ServeFileFS(w, r, sub, "index.html")
	})
}
