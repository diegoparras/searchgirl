// Package web is the human face: the Searchgirl SPA, embedded in the binary
// and served over the same HTTP server as the API and the MCP endpoint. It is
// deliberately thin — every piece of data the UI shows comes from /api.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed assets
var assetsFS embed.FS

type Server struct{}

func New() *Server { return &Server{} }

// Mount registers the SPA on the given mux. The SPA is a single page: any
// path that is not a real asset falls back to index.html so /?q=... deep
// links work after a refresh.
func (s *Server) Mount(mux *http.ServeMux) {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			if _, err := fs.Stat(sub, r.URL.Path[1:]); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// index.html para / y cualquier ruta desconocida (deep links del SPA).
		http.ServeFileFS(w, r, sub, "index.html")
	})
}
