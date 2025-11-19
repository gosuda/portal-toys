package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

//go:embed static
var staticFS embed.FS

// NewHandler constructs the HTTP handler (UI + APIs).
func NewHandler() http.Handler {
	mux := http.NewServeMux()
	// health
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	// info (for UI)
	mux.HandleFunc("/api/info", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		sites, _ := readSites(sitesJSONPath)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"server_urls": flagServerURLs,
			"portal_base": flagPortalBase,
			"data_path":   flagSitesPath,
			"sites":       sites,
			"name":        flagName,
		})
	})

	// APIs
	mux.HandleFunc("/api/portals", handlePortals)
	mux.HandleFunc("/api/sites", handleSites)
	mux.HandleFunc("/api/health", handleHealth)

	// Static UI
	sub, err := fs.Sub(staticFS, "static")
	if err == nil {
		mux.Handle("/", http.FileServer(http.FS(sub)))
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "static not available", http.StatusServiceUnavailable)
		})
	}
	return mux
}

// handlePortals fetches the portal site root HTML and extracts SSR JSON from script#__SSR_DATA__
func handlePortals(w http.ResponseWriter, r *http.Request) {
	// Aggregate from all sites if requested
	if r.URL.Query().Get("all") == "1" {
		sites, err := readSites(sitesJSONPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		type agg struct {
			Base string      `json:"base"`
			Data interface{} `json:"data"`
			Err  string      `json:"err,omitempty"`
		}
		out := make([]agg, 0, len(sites))
		for _, s := range sites {
			v, err := fetchSSRPortals(s)
			a := agg{Base: s}
			if err != nil {
				a.Err = err.Error()
			} else {
				a.Data = v
			}
			out = append(out, a)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
		return
	}

	// Otherwise single base
	base := flagPortalBase
	if q := r.URL.Query().Get("base"); q != "" {
		base = q
	}
	list, err := fetchSSRPortals(base)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	// If health=1, include per-portal health check results in a normalized list
	if r.URL.Query().Get("health") == "1" {
		items := extractPortalItems(list)
		checked := healthCheckItems(r.Context(), items)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(checked)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

var ssrRe = regexp.MustCompile(`(?is)<script[^>]+id=["']__SSR_DATA__[^>]*>(.*?)</script>`) // capture inner JSON

func fetchSSRPortals(base string) (any, error) {
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		return nil, fmt.Errorf("invalid portal base: %s", base)
	}
	// Ensure trailing slash
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	req, err := http.NewRequest(http.MethodGet, base, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "portal-list/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("fetch portal base: %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	m := ssrRe.FindSubmatch(body)
	if len(m) < 2 {
		return nil, errors.New("SSR data not found in portal site")
	}
	// The SSR JSON might be wrapped as an array (often is). Return raw parsed JSON.
	var v any
	if err := json.Unmarshal(m[1], &v); err != nil {
		// Some portals might embed escaped JSON string; try to unquote once.
		var s string
		if json.Unmarshal(m[1], &s) == nil {
			if err2 := json.Unmarshal([]byte(s), &v); err2 == nil {
				return v, nil
			}
		}
		return nil, fmt.Errorf("parse SSR JSON: %w", err)
	}
	return v, nil
}

// Normalized portal item and health result
type PortalCard struct {
	Name        string `json:"name"`
	Link        string `json:"link"`
	Kind        string `json:"kind,omitempty"`
	Connected   bool   `json:"connected,omitempty"`
	LastSeen    string `json:"lastSeen,omitempty"`
	LastSeenISO string `json:"lastSeenISO,omitempty"`
	Healthy     bool   `json:"healthy"`
	CheckedAt   string `json:"checkedAt"` // RFC3339
	Error       string `json:"error,omitempty"`
}

// extractPortalItems attempts to normalize SSR entries into PortalCard skeletons
func extractPortalItems(ssr any) []PortalCard {
	var out []PortalCard
	arr, ok := ssr.([]any)
	if !ok {
		// Some SSR formats wrap data; try to detect common shapes
		if m, ok := ssr.(map[string]any); ok {
			for _, k := range []string{"data", "items", "list"} {
				if v, ok2 := m[k]; ok2 {
					return extractPortalItems(v)
				}
			}
		}
		return out
	}
	for _, it := range arr {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		pc := PortalCard{}
		// String helpers
		gs := func(keys ...string) string {
			for _, k := range keys {
				if v, ok := m[k]; ok && v != nil {
					if s, ok := v.(string); ok {
						return s
					}
				}
			}
			return ""
		}
		gb := func(keys ...string) bool {
			for _, k := range keys {
				if v, ok := m[k]; ok && v != nil {
					switch x := v.(type) {
					case bool:
						return x
					case float64:
						return x != 0
					case string:
						return strings.EqualFold(x, "true") || x == "1"
					}
				}
			}
			return false
		}
		pc.Name = gs("Name", "name")
		pc.Link = gs("Link", "link")
		pc.Kind = gs("Kind", "kind")
		pc.LastSeen = gs("LastSeen", "lastSeen")
		pc.LastSeenISO = gs("LastSeenISO", "lastSeenISO")
		pc.Connected = gb("Connected", "connected")
		out = append(out, pc)
	}
	return out
}

// healthCheckItems runs a quick HTTP health check for each portal link
func healthCheckItems(ctx context.Context, items []PortalCard) []PortalCard {
	// Shallow copy
	out := make([]PortalCard, len(items))
	copy(out, items)
	// Concurrency limiter
	sem := make(chan struct{}, 8)
	done := make(chan int)
	client := &http.Client{Timeout: 5 * time.Second}
	for i := range out {
		sem <- struct{}{}
		go func(idx int) {
			defer func() { <-sem; done <- idx }()
			link := normalizeURL(out[idx].Link)
			if link == "" {
				out[idx].Healthy = false
				out[idx].CheckedAt = time.Now().UTC().Format(time.RFC3339)
				out[idx].Error = "empty link"
				return
			}
			ok, err := isHealthy(ctx, client, link)
			out[idx].Healthy = ok
			out[idx].CheckedAt = time.Now().UTC().Format(time.RFC3339)
			if err != nil {
				out[idx].Error = err.Error()
			}
			// Store back normalized link for UI
			out[idx].Link = link
		}(i)
	}
	// Wait for all
	for i := 0; i < len(out); i++ {
		<-done
	}
	return out
}

func normalizeURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "//") {
		return "https:" + s
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return s
	}
	// Otherwise, assume https scheme
	return "https://" + s
}

func isHealthy(ctx context.Context, client *http.Client, urlStr string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, urlStr, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "portal-list/1.0")
	resp, err := client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			return true, nil
		}
		// If method not allowed, try GET
		if resp.StatusCode == http.StatusMethodNotAllowed {
			// fall through to GET
		} else {
			return false, fmt.Errorf("%s", resp.Status)
		}
	}
	// Fallback GET
	req2, err2 := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err2 != nil {
		return false, err2
	}
	req2.Header.Set("User-Agent", "portal-list/1.0")
	resp2, err2 := client.Do(req2)
	if err2 != nil {
		return false, err2
	}
	defer resp2.Body.Close()
	if resp2.StatusCode >= 200 && resp2.StatusCode < 400 {
		return true, nil
	}
	return false, fmt.Errorf("%s", resp2.Status)
}

// handleHealth: direct URL reachability from sites.json (no SSR parsing)
func handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sites, err := readSites(sitesJSONPath)
	if err != nil || !hasNonEmpty(sites) {
		// Fallback to derived portal base if sites.json is missing/empty
		sites, _ = loadSitesOrInit(sitesJSONPath, flagServerURLs)
	}
	items := make([]PortalCard, 0, len(sites))
	for _, s := range sites {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		link := normalizeURL(s)
		items = append(items, PortalCard{
			Name:      guessNameFromURL(link),
			Link:      link,
			Kind:      "http/1.1",
			Connected: false,
		})
	}
	checked := healthCheckItems(ctx, items)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(checked)
}

func guessNameFromURL(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
	}
	if u.Path != "" && u.Path != "/" {
		return fmt.Sprintf("%s%s", u.Host, u.Path)
	}
	return u.Host
}

// handleSites supports GET (list) and POST (add url) operations.
func handleSites(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sites, err := readSites(sitesJSONPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sites)
	case http.MethodPost:
		var body struct {
			URL  string   `json:"url"`
			URLs []string `json:"urls"`
		}
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		var toAdd []string
		if body.URL != "" {
			toAdd = append(toAdd, body.URL)
		}
		if len(body.URLs) > 0 {
			toAdd = append(toAdd, body.URLs...)
		}
		if len(toAdd) == 0 {
			http.Error(w, "missing url", http.StatusBadRequest)
			return
		}
		// Try to connect/register to each URL before persisting
		tags := strings.Split(flagTags, ",")
		for _, s := range toAdd {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if _, err := gPortalMgr.ConnectFromSite(r.Context(), s, flagName, flagDescription, flagHide, flagOwner, tags); err != nil {
				http.Error(w, fmt.Sprintf("failed to connect/register: %v", err), http.StatusBadRequest)
				return
			}
		}
		// Load current
		sites, _ := readSites(sitesJSONPath)
		// Build canonical set
		canon := func(s string) string { return strings.TrimRight(strings.ToLower(normalizeURL(s)), "/") }
		seen := make(map[string]struct{}, len(sites))
		for _, s := range sites {
			seen[canon(s)] = struct{}{}
		}
		// Add new
		for _, s := range toAdd {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			c := canon(s)
			if _, ok := seen[c]; ok {
				continue
			}
			sites = append(sites, normalizeURL(s))
			seen[c] = struct{}{}
		}
		if err := writeSites(sitesJSONPath, sites); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sites)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// Sites list persistence
func loadSitesOrInit(path string, bootstraps []string) ([]string, error) {
	// If file has non-empty list, return it. Else generate from bootstraps and save.
	sites, err := readSites(path)
	if err == nil && hasNonEmpty(sites) {
		return sites, nil
	}
	// derive from bootstraps
	uniq := make(map[string]struct{})
	for _, b := range bootstraps {
		for _, s := range strings.Split(b, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			base := derivePortalBase(s)
			uniq[base] = struct{}{}
		}
	}
	// Fallback default if still empty
	if len(uniq) == 0 {
		uniq["https://portal.gosuda.org/"] = struct{}{}
	}
	out := make([]string, 0, len(uniq))
	for k := range uniq {
		out = append(out, k)
	}
	if err := writeSites(path, out); err != nil {
		return out, err
	}
	return out, nil
}

func readSites(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// empty file
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil, errors.New("empty sites file")
	}
	var v []string
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return v, nil
}

func writeSites(path string, sites []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(sites, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func hasNonEmpty(ss []string) bool {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}
