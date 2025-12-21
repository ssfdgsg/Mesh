package controlplane

import (
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// 把单页应用（SPA）作为静态资源
func registerUISPA(mux *http.ServeMux, uiDir string) {
	// Serve the SPA at `/` to avoid redirect loops and keep the UI reachable even
	// if clients or proxies normalize `/ui/` differently.
	mux.Handle("/", spaFileServer(uiDir))

	// Also serve `/ui` (no trailing slash) to avoid ServeMux's automatic
	// redirect from `/ui` -> `/ui/`, which can loop when a client/proxy
	// rewrites the path back.
	mux.Handle("/ui", http.StripPrefix("/ui", spaFileServer(uiDir)))
	mux.Handle("/ui/", http.StripPrefix("/ui/", spaFileServer(uiDir)))
}

func spaFileServer(root string) http.Handler {
	fs := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqPath := r.URL.Path
		if reqPath == "" || reqPath == "/" {
			r2 := new(http.Request)
			*r2 = *r
			r2.URL = newCopyURL(r.URL)
			// Use "/" so FileServer serves index.html without redirecting
			// "/index.html" -> "./" (which can cause redirect loops under StripPrefix).
			r2.URL.Path = "/"
			fs.ServeHTTP(w, r2)
			return
		}

		// Vite/React SPA: for non-asset routes, fall back to index.html.
		clean := path.Clean("/" + reqPath)
		clean = strings.TrimPrefix(clean, "/")
		if strings.Contains(clean, "..") {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}

		localPath := filepath.Join(root, filepath.FromSlash(clean))
		if fi, err := os.Stat(localPath); err == nil && !fi.IsDir() {
			fs.ServeHTTP(w, r)
			return
		}

		r2 := new(http.Request)
		*r2 = *r
		r2.URL = newCopyURL(r.URL)
		r2.URL.Path = "/"
		fs.ServeHTTP(w, r2)
	})
}

func newCopyURL(u *url.URL) *url.URL {
	u2 := *u
	return &u2
}
