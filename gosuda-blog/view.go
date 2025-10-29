package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rs/zerolog/log"
)

// fileServerWithSPA returns a handler that serves static files from dir.
// If a path is not found and looks like an SPA route, it falls back to index.html.
var (
	reHref = regexp.MustCompile(`href="(/[^\"]*)"`)
	reSrc  = regexp.MustCompile(`src="(/[^\"]*)"`)
)

func rewriteHTMLToRelative(b []byte, relPrefix string) []byte {
	// helper closure to process matches safely
	process := func(re *regexp.Regexp, attr string, in []byte) []byte {
		return re.ReplaceAllFunc(in, func(m []byte) []byte {
			// m looks like attr="/something"
			// Extract inside quotes
			parts := re.FindSubmatch(m)
			if len(parts) < 2 {
				return m
			}
			path := string(parts[1]) // begins with '/'
			if strings.HasPrefix(path, "//") {
				// protocol-relative URL, leave as-is
				return m
			}
			rel := strings.TrimPrefix(path, "/")
			if relPrefix != "" {
				rel = relPrefix + rel
			}
			return fmt.Appendf(nil, "%s=\"%s\"", attr, rel)
		})
	}
	// Also rewrite absolute gosuda.org URLs to relative so they stay under /peer/<id>/
	reAbsHref := regexp.MustCompile(`href="https?://gosuda\.org/([^"]*)"`)
	reAbsSrc := regexp.MustCompile(`src="https?://gosuda\.org/([^"]*)"`)
	b = process(reAbsHref, "href", b)
	b = process(reAbsSrc, "src", b)
	out := process(reHref, "href", b)
	out = process(reSrc, "src", out)
	// Inject a base tag script like paint to ensure proper relative resolution under /peer/{token}
	// Do this only if there's no existing <base ...>
	if !strings.Contains(strings.ToLower(string(out)), "<base ") {
		inject := `<script>(function(){var b=document.createElement('base');var p=window.location.pathname;b.href=p.endsWith('/')?p:(p+'/');document.head.appendChild(b);})();</script>`
		// Insert after first <head>
		outStr := string(out)
		if strings.Contains(outStr, "<head>") {
			outStr = strings.Replace(outStr, "<head>", "<head>"+inject, 1)
			return []byte(outStr)
		}
	}
	return out
}

// rewriteCSSToRelative adjusts url(...) references so absolute-root paths and gosuda.org URLs
// become relative with the appropriate relPrefix so assets load under subpaths like /peer/<id>/lang/...
func rewriteCSSToRelative(b []byte, relPrefix string) []byte {
	// Use a simple matcher for url(...) then parse inside without backrefs
	reURL := regexp.MustCompile(`url\(([^)]*)\)`)
	return reURL.ReplaceAllFunc(b, func(m []byte) []byte {
		sub := reURL.FindSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		inner := strings.TrimSpace(string(sub[1]))
		quote := ""
		if len(inner) >= 2 {
			if (inner[0] == '\'' && inner[len(inner)-1] == '\'') || (inner[0] == '"' && inner[len(inner)-1] == '"') {
				quote = string(inner[0])
				inner = inner[1 : len(inner)-1]
			}
		}
		p := inner
		// Normalize absolute host gosuda.org to root-absolute
		if after, ok := strings.CutPrefix(p, "http://gosuda.org/"); ok {
			p = "/" + after
		} else if after0, ok0 := strings.CutPrefix(p, "https://gosuda.org/"); ok0 {
			p = "/" + after0
		}
		if after, ok := strings.CutPrefix(p, "/"); ok {
			p = after
			if relPrefix != "" {
				p = relPrefix + p
			}
		}
		return []byte("url(" + quote + p + quote + ")")
	})
}

// serveFileWithOptionalRewrite reads file p, optionally rewrites bytes with rw using relPrefix,
// sets content type, and writes the response.
func serveFileWithOptionalRewrite(w http.ResponseWriter, r *http.Request, p string, contentType string, relPrefix string, rw func([]byte, string) []byte) {
	f, err := os.Open(p)
	if err != nil {
		http.Error(w, "failed to open file", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, "failed to read file", http.StatusInternalServerError)
		return
	}
	if rw != nil {
		b = rw(b, relPrefix)
	}
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(b)
}

func fileServerWithSPA(dir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If under /peer/{token} without trailing slash, redirect to add '/'
		if strings.HasPrefix(r.URL.Path, "/peer/") {
			rest := strings.TrimPrefix(r.URL.Path, "/peer/")
			if !strings.Contains(rest, "/") {
				http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
				return
			}
		}
		// Normalize path; if under /peer/{token}/..., strip the prefix so assets map to dir
		effectivePath := r.URL.Path
		if strings.HasPrefix(effectivePath, "/peer/") {
			rest := strings.TrimPrefix(effectivePath, "/peer/")
			if i := strings.IndexByte(rest, '/'); i >= 0 {
				// token := rest[:i]
				suffix := rest[i:] // includes leading '/'
				if suffix == "" {  // exact token only
					effectivePath = "/"
				} else {
					effectivePath = suffix
				}
			} else {
				// no trailing slash after token: treat as root
				effectivePath = "/"
			}
		}
		// Clean URL path using POSIX rules, then convert to OS path and strip leading '/'
		cleanURLPath := path.Clean(effectivePath)
		if after, ok := strings.CutPrefix(cleanURLPath, "/"); ok {
			cleanURLPath = after
		}
		// Map to file under dir safely
		p := filepath.Join(dir, filepath.FromSlash(cleanURLPath))
		log.Debug().Str("method", r.Method).Str("url", r.URL.Path).Str("mapped", p).Msg("incoming request")
		// Prevent path traversal outside dir
		if rel, err := filepath.Rel(dir, p); err != nil || strings.HasPrefix(rel, "..") {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		// If it's a directory, try to serve index.html in that directory
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			idx := filepath.Join(p, "index.html")
			if _, err := os.Stat(idx); err == nil {
				log.Debug().Str("serve", idx).Msg("serve directory index")
				relPrefix := calcRelPrefix(cleanURLPath, true)
				serveFileWithOptionalRewrite(w, r, idx, "text/html; charset=utf-8", relPrefix, rewriteHTMLToRelative)
				return
			}
		}
		// If file exists, serve as-is
		if _, err := os.Stat(p); err == nil {
			// Serve the exact resolved file to avoid any URL prefix ambiguity
			log.Debug().Str("serve", p).Msg("serve static file")
			lower := strings.ToLower(p)
			switch {
			case strings.HasSuffix(lower, ".html"):
				relPrefix := calcRelPrefix(cleanURLPath, false)
				serveFileWithOptionalRewrite(w, r, p, "text/html; charset=utf-8", relPrefix, rewriteHTMLToRelative)
			case strings.HasSuffix(lower, ".css"):
				relPrefix := calcRelPrefix(cleanURLPath, false)
				serveFileWithOptionalRewrite(w, r, p, "text/css; charset=utf-8", relPrefix, rewriteCSSToRelative)
			default:
				http.ServeFile(w, r, p)
			}
			return
		}
		// If no extension, try adding .html (pretty URLs)
		if !strings.Contains(filepath.Base(p), ".") {
			pHTML := p + ".html"
			if _, err := os.Stat(pHTML); err == nil {
				log.Debug().Str("serve", pHTML).Msg("serve pretty URL .html")
				relPrefix := calcRelPrefix(cleanURLPath, false)
				serveFileWithOptionalRewrite(w, r, pHTML, "text/html; charset=utf-8", relPrefix, rewriteHTMLToRelative)
				return
			}
		}
		// SPA fallback for non-asset paths
		if !strings.Contains(filepath.Base(p), ".") {
			idx := filepath.Join(dir, "index.html")
			log.Debug().Str("fallback", idx).Msg("SPA fallback to index.html")
			relPrefix := calcRelPrefix(cleanURLPath, false)
			serveFileWithOptionalRewrite(w, r, idx, "text/html; charset=utf-8", relPrefix, rewriteHTMLToRelative)
			return
		}
		log.Debug().Str("url", r.URL.Path).Msg("not found")
		http.NotFound(w, r)
	})
}

// calcRelPrefix computes how many "../" are needed from the current cleanURLPath
// to reach the web root (dist). cleanURLPath has no leading '/'.
func calcRelPrefix(cleanURLPath string, isDir bool) string {
	var depthPath string
	if isDir {
		depthPath = path.Clean(cleanURLPath)
	} else {
		depthPath = path.Dir(cleanURLPath)
	}
	if depthPath == "." || depthPath == "/" || depthPath == "" {
		return ""
	}
	segs := strings.Split(depthPath, "/")
	n := 0
	for _, s := range segs {
		if s == "" || s == "." {
			continue
		}
		n++
	}
	return strings.Repeat("../", n)
}
