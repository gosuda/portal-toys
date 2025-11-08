package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type PageMeta struct {
	Name      string    `json:"name"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Storage struct{ Dir string }

func (s Storage) pagesDir() string { return filepath.Join(s.Dir, "pages") }
func (s Storage) metaPath() string { return filepath.Join(s.Dir, "pages.json") }

// pagePath returns a safe on-disk path for a given name.
// It broadly allows names but prevents directory traversal and invalid filename chars.
func (s Storage) pagePath(name string) string {
	base := strings.TrimSpace(name)
	if base == "" {
		base = "untitled"
	}
	// keep only the base and replace path separators and forbidden chars
	base = filepath.Base(base)
	base = strings.Map(func(r rune) rune {
		switch r {
		case '\\', '/', ':', '*', '?', '"', '<', '>', '|':
			return '-'
		}
		if r < 0x20 { // control chars
			return '-'
		}
		return r
	}, base)
	if base == "." || base == ".." || base == "" {
		base = "untitled"
	}
	// limit file name length to avoid OS issues
	if len(base) > 120 {
		base = base[:120]
	}
	return filepath.Join(s.pagesDir(), base+".html")
}
func (s Storage) ensure() error { return os.MkdirAll(s.pagesDir(), 0o755) }

func (s Storage) loadMeta() ([]PageMeta, error) {
	f, err := os.Open(s.metaPath())
	if err != nil {
		if os.IsNotExist(err) {
			return []PageMeta{}, nil
		}
		return nil, err
	}
	defer f.Close()
	var m []PageMeta
	dec := json.NewDecoder(f)
	if err := dec.Decode(&m); err == nil {
		return m, nil
	}
	// Fallback for legacy files using `slug` key
	_ = f.Close()
	f2, err2 := os.Open(s.metaPath())
	if err2 != nil {
		return []PageMeta{}, nil
	}
	defer f2.Close()
	var legacy []struct {
		Slug      string    `json:"slug"`
		Title     string    `json:"title"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	if err := json.NewDecoder(f2).Decode(&legacy); err != nil {
		return []PageMeta{}, nil
	}
	out := make([]PageMeta, 0, len(legacy))
	for _, it := range legacy {
		out = append(out, PageMeta{Name: it.Slug, Title: it.Title, UpdatedAt: it.UpdatedAt})
	}
	return out, nil
}

func (s Storage) saveMeta(m []PageMeta) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	tmp := s.metaPath() + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.metaPath())
}

//go:embed static/*
var staticFS embed.FS

// NewHandler builds the HTTP router for OpenBoard.
func NewHandler(name string, dataDir string) http.Handler {
	st := Storage{Dir: dataDir}
	_ = st.ensure()

	r := chi.NewRouter()

	// Immutable static UI (embedded into the binary)
	sub, _ := fs.Sub(staticFS, "static")
	serveStatic := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, err := fs.ReadFile(sub, name)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			// small cache for index/view shell; content changes only on deploy
			w.Header().Set("Cache-Control", "public, max-age=300")
			_, _ = w.Write(b)
		}
	}
	r.Get("/", serveStatic("index.html"))
	r.Get("/index.html", serveStatic("index.html"))

	// RAW user HTML (mutable, stored under data dir)
	r.Get("/raw/*", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/raw/")
		name = strings.TrimSuffix(name, ".html")
		p := st.pagePath(name)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Permissive CSP since user opted-in to RAW; still restrict object-src
		w.Header().Set("Content-Security-Policy", "default-src 'self' 'unsafe-inline' 'unsafe-eval' data: blob: *; object-src 'none'; frame-ancestors 'self';")
		http.ServeFile(w, r, p)
	})

	// Sandbox viewer wrapper (static HTML reads slug from URL)
	r.Get("/p/*", serveStatic("sandbox.html"))
	r.Get("/sandbox.html", serveStatic("sandbox.html"))

	// API: list pages
	r.Get("/api/pages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		pages, err := st.loadMeta()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
			return
		}
		sort.Slice(pages, func(i, j int) bool { return pages[i].UpdatedAt.After(pages[j].UpdatedAt) })
		_ = json.NewEncoder(w).Encode(map[string]any{"pages": pages})
	})

	// API: create/update page
	r.Post("/api/pages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var req struct{ Name, Title, HTML string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid json"})
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if strings.TrimSpace(req.Title) == "" {
			req.Title = req.Name
		}
		if err := st.ensure(); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
			return
		}
		if err := os.WriteFile(st.pagePath(req.Name), []byte(req.HTML), 0o644); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
			return
		}
		metas, _ := st.loadMeta()
		found := false
		now := time.Now()
		for i := range metas {
			if metas[i].Name == req.Name {
				metas[i].Title = req.Title
				metas[i].UpdatedAt = now
				found = true
				break
			}
		}
		if !found {
			metas = append(metas, PageMeta{Name: req.Name, Title: req.Title, UpdatedAt: now})
		}
		if err := st.saveMeta(metas); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})

	return r
}
