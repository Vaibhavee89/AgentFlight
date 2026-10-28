// Package webui serves the embedded single-page UI under /ui/. Requests for
// existing files get them; requests for other paths without a file extension
// get index.html, so client-side routes survive a reload. Missing asset paths
// return 404.
package webui

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/Vaibhavee89/agentflight/web"
)

// Prefix is the URL prefix the UI is served under.
const Prefix = "/ui"

// Handler returns the UI handler, to be mounted at Prefix + "/".
func Handler() http.Handler {
	dist, err := fs.Sub(web.FS, "dist")
	if err != nil {
		// The embed path is a compile-time constant, so this is unreachable;
		// the handler reports it rather than panicking.
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "ui unavailable: "+err.Error(), http.StatusInternalServerError)
		})
	}
	return &handler{dist: dist}
}

type handler struct {
	dist fs.FS
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rel := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, Prefix), "/")
	if rel == "" {
		rel = "index.html"
	}
	if info, err := fs.Stat(h.dist, rel); err == nil && !info.IsDir() {
		http.ServeFileFS(w, r, h.dist, rel)
		return
	}
	if path.Ext(rel) != "" {
		http.NotFound(w, r)
		return
	}
	http.ServeFileFS(w, r, h.dist, "index.html")
}
