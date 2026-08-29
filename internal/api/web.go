package api

import (
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/meowkey-dev/analog/internal/apierr"
)

// serveWeb is the SPA fallback: the built bundle when it exists, index.html for
// anything the bundle does not have a file for, so client-side routes survive a
// reload. Production is then one origin (SPEC §5).
func (s *Server) serveWeb(w http.ResponseWriter, r *http.Request) {
	if s.Web == nil {
		apierr.NotFound("no such path").Write(w)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name != "" && name != "." {
		if f, err := s.Web.Open(name); err == nil {
			defer f.Close()
			if info, err := f.Stat(); err == nil && !info.IsDir() {
				serveFile(w, name, f)
				return
			}
		}
	}
	s.serveIndex(w)
}

func (s *Server) serveIndex(w http.ResponseWriter) {
	f, err := s.Web.Open("index.html")
	if err != nil {
		apierr.NotFound("no such path").Write(w)
		return
	}
	defer f.Close()
	// Asset filenames are content-hashed and may cache forever, but index.html
	// names them — cache it and a rebuild is invisible until a hard reload.
	w.Header().Set("Cache-Control", "no-cache")
	serveFile(w, "index.html", f)
}

func serveFile(w http.ResponseWriter, name string, f fs.File) {
	if ctype := mime.TypeByExtension(filepath.Ext(name)); ctype != "" {
		w.Header().Set("Content-Type", ctype)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}
