package server

import (
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

const notBuiltPage = `<!doctype html>
<meta charset="utf-8">
<title>unconf</title>
<p>The frontend has not been built. Run <code>make build</code>, or use
<code>make dev</code> and open the Vite dev server.</p>
`

// newStaticHandler serves the single-page app from dist. Real files are
// served as-is; any other path falls back to index.html so client-side
// routes survive a reload. Vite's content-hashed files under /assets/ are
// cached forever; everything else is revalidated.
func newStaticHandler(dist fs.FS) http.Handler {
	files := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" && name != "index.html" && isFile(dist, name) {
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			files.ServeHTTP(w, r)
			return
		}
		serveIndex(w, r, dist)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, dist fs.FS) {
	w.Header().Set("Cache-Control", "no-cache")
	index, err := fs.ReadFile(dist, "index.html")
	if errors.Is(err, fs.ErrNotExist) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(notBuiltPage))
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(index)
}

func isFile(fsys fs.FS, name string) bool {
	info, err := fs.Stat(fsys, name)
	return err == nil && !info.IsDir()
}
