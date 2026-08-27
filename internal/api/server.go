// Package api wires up the HTTP server: routes, middleware, embedded SPA.
package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rei/bandai30/internal/auth"
	"github.com/rei/bandai30/internal/scrape"
	"github.com/rei/bandai30/internal/store"
)

type Server struct {
	Store     *store.Store
	Scraper   *scrape.Client
	PhotosDir string
	WebFS     fs.FS           // embedded SPA (web/*)
	NoAuth    bool            // if true, bypass session auth; everyone is "anon"
	BaseCtx   context.Context // process lifetime; outlives any single request
	Version   string          // commit this binary was built from; reported by /api/health

	verOnce  sync.Once
	assetVer string
}

// assetVersion is a short hash of the embedded UI files. It changes on every
// rebuild that touches the UI, and is appended to app.js/styles.css URLs so
// CDN/browser caches are busted automatically.
func (s *Server) assetVersion() string {
	s.verOnce.Do(func() {
		h := sha256.New()
		for _, f := range []string{"index.html", "app.js", "styles.css"} {
			if b, err := fs.ReadFile(s.WebFS, f); err == nil {
				h.Write(b)
			}
		}
		s.assetVer = hex.EncodeToString(h.Sum(nil))[:10]
	})
	return s.assetVer
}

// Handler returns the root http.Handler with all routes registered.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Public, and deliberately so — see health().
	mux.HandleFunc("GET /api/health", s.health)

	// Public auth endpoints.
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/auth/me", s.handleMe)

	// Authenticated endpoints. In NoAuth mode the wrapper is a no-op so the
	// "anon" user injected by AnonMiddleware satisfies CurrentUser().
	authd := func(h http.HandlerFunc) http.Handler {
		if s.NoAuth {
			return http.HandlerFunc(h)
		}
		return auth.RequireAuth(http.HandlerFunc(h))
	}
	// Photos: gated behind auth (or open in NoAuth mode).
	mux.Handle("GET /photos/{name}", authd(s.servePhoto))

	mux.Handle("GET /api/items", authd(s.listItems))
	mux.Handle("POST /api/items", authd(s.createItem))
	mux.Handle("GET /api/items/{id}", authd(s.getItem))
	mux.Handle("PUT /api/items/{id}", authd(s.updateItem))
	mux.Handle("DELETE /api/items/{id}", authd(s.deleteItem))
	mux.Handle("POST /api/upload", authd(s.uploadPhoto))
	mux.Handle("GET /api/items/{id}/photos", authd(s.listUserPhotos))
	mux.Handle("POST /api/items/{id}/photos", authd(s.addUserPhoto))
	mux.Handle("DELETE /api/items/{id}/photos/{photoID}", authd(s.deleteUserPhoto))
	mux.Handle("POST /api/scrape", authd(s.startScrape))
	mux.Handle("POST /api/scrape/{slug}", authd(s.startScrape))
	mux.Handle("GET /api/scrape/status", authd(s.scrapeStatus))
	mux.Handle("GET /api/settings", authd(s.getSettings))
	mux.Handle("PUT /api/settings", authd(s.putSettings))
	mux.Handle("GET /api/backups", authd(s.listBackups))
	mux.Handle("POST /api/backups", authd(s.runBackup))
	mux.Handle("GET /api/backups/{name}", authd(s.downloadBackup))
	mux.Handle("GET /api/collections", authd(s.listCollections))
	mux.Handle("GET /api/stats", authd(s.statsHandler))
	mux.Handle("GET /api/categories", authd(s.categoriesHandler))
	mux.Handle("GET /api/export", authd(s.exportJSON))
	mux.Handle("POST /api/import", authd(s.importJSON))

	// SPA: fall back to index.html for any unknown GET so hash routes work.
	mux.HandleFunc("/", s.serveSPA)

	if s.NoAuth {
		return auth.AnonMiddleware(mux)
	}
	return auth.Middleware(s.Store, mux)
}

func (s *Server) serveSPA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	// Strip leading slash to look up in the embedded FS.
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	if _, err := fs.Stat(s.WebFS, name); err != nil {
		// Unknown path → return index.html (SPA hash routing handles the rest).
		name = "index.html"
	}
	if name == "index.html" {
		s.serveIndex(w)
		return
	}
	// Versioned assets (app.js?v=…) can be cached hard; the version query busts
	// CDN/browser caches whenever the UI changes.
	if r.URL.RawQuery != "" {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeFileFS(w, r, s.WebFS, name)
}

// serveIndex returns index.html with versioned asset URLs and a no-store header,
// so the CDN never serves a stale shell that points at old JS/CSS.
func (s *Server) serveIndex(w http.ResponseWriter) {
	b, err := fs.ReadFile(s.WebFS, "index.html")
	if err != nil {
		http.Error(w, "index missing", http.StatusInternalServerError)
		return
	}
	ver := s.assetVersion()
	html := strings.ReplaceAll(string(b), `"/app.js"`, `"/app.js?v=`+ver+`"`)
	html = strings.ReplaceAll(html, `"/styles.css"`, `"/styles.css?v=`+ver+`"`)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, html)
}

func (s *Server) servePhoto(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" || strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.PhotosDir, name)
	// A photo's URL never changes but its CONTENT does: a placeholder cover is
	// replaced in place once Bandai publishes the real shot. With no
	// Cache-Control the browser applies heuristic freshness and keeps serving
	// the old bytes without ever asking — the logo appeared to survive a
	// refresh that had in fact already fixed it. "no-cache" still allows
	// caching, it just forces revalidation, so the usual answer is a 304.
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, path)
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// ctxOf is shorthand: returns the request context. Kept so handlers stay terse.
func ctxOf(r *http.Request) context.Context { return r.Context() }
