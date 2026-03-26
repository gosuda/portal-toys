package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gosuda/portal/v2/utils"
	"github.com/rs/zerolog/log"
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
		relayURLs, err := utils.ResolvePortalRelayURLs(context.Background(), utils.SplitCSV(flagServerURLs), flagDiscovery)
		if err != nil {
			relayURLs = utils.SplitCSV(flagServerURLs)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"server_urls": relayURLs,
			"portal_base": flagPortalBase,
			"sites":       gSites.List(),
			"name":        flagName,
		})
	})

	// APIs
	mux.HandleFunc("/api/portals", handlePortals)
	mux.HandleFunc("/api/sites", handleSites)
	mux.HandleFunc("/api/health", handleHealth)
	mux.HandleFunc("/api/relays", handleRelays)

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
		sites := gSites.List()
		type agg struct {
			Base string `json:"base"`
			Data any    `json:"data"`
			Err  string `json:"err,omitempty"`
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
	return healthCheckItemsWithCallback(ctx, items, nil)
}

// healthCheckItemsWithCallback runs health checks and invokes cb for each result when ready.
func healthCheckItemsWithCallback(ctx context.Context, items []PortalCard, cb func(PortalCard)) []PortalCard {
	// Shallow copy
	out := make([]PortalCard, len(items))
	copy(out, items)
	// Concurrency limiter
	lim := 32
	if len(out) > 0 && len(out) < lim {
		lim = len(out)
	}
	if lim <= 0 {
		lim = 1
	}
	sem := make(chan struct{}, lim)
	done := make(chan int)
	// Fast HTTP client with aggressive timeouts to reduce page load latency
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   1 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   1 * time.Second,
		ResponseHeaderTimeout: 2 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
	}
	client := &http.Client{Transport: tr, Timeout: 3 * time.Second}
	for i := range out {
		sem <- struct{}{}
		go func(idx int) {
			defer func() { <-sem; done <- idx }()
			link := normalizeURL(out[idx].Link)
			if link == "" {
				out[idx].Healthy = false
				out[idx].CheckedAt = time.Now().UTC().Format(time.RFC3339)
				out[idx].Error = "empty link"
				if cb != nil && ctx.Err() == nil {
					cb(out[idx])
				}
				return
			}
			// Per-check timeout to avoid slow endpoints delaying the page
			perCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			ok, err := isHealthy(perCtx, client, link)
			out[idx].Healthy = ok
			out[idx].CheckedAt = time.Now().UTC().Format(time.RFC3339)
			if err != nil {
				out[idx].Error = err.Error()
			}
			// Store back normalized link for UI
			out[idx].Link = link
			if cb != nil && ctx.Err() == nil {
				cb(out[idx])
			}
		}(i)
	}
	// Wait for all
	for range out {
		<-done
	}
	return out
}

func normalizeURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	// Keep only the first whitespace-separated token to avoid trailing notes like
	// "https://host/ - something" or "https://host/ extra text".
	if fields := strings.Fields(s); len(fields) > 0 {
		s = fields[0]
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

// sanitizeSiteInput trims, extracts a clean base URL and drops any trailing
// commentary (e.g., after a dash). It returns scheme://host[:port]/ form.
func sanitizeSiteInput(raw string) string {
	// Normalize basic scheme and strip trailing notes
	s := normalizeURL(raw)
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return ""
	}
	scheme := "https"
	if strings.EqualFold(u.Scheme, "http") {
		scheme = "http"
	}
	// Lowercase host, keep port if present
	host := strings.ToLower(u.Host)
	return fmt.Sprintf("%s://%s/", scheme, host)
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

func registrationStatusItems() []PortalCard {
	now := time.Now().UTC().Format(time.RFC3339)
	sites := gSites.List()
	items := make([]PortalCard, 0, len(sites))
	for _, s := range sites {
		s = sanitizeSiteInput(s)
		if s == "" {
			continue
		}
		relay := deriveRelayFromSite(s)
		connected := relay != "" && gPortalMgr.HasRelay(relay)
		item := PortalCard{
			Name:      guessNameFromURL(s),
			Link:      s,
			Kind:      "relay-lease",
			Connected: connected,
			Healthy:   connected,
			CheckedAt: now,
		}
		if !connected {
			item.Error = "offline: relay lease not registered"
		}
		items = append(items, item)
	}
	return items
}

// handleHealth: relay registration status from the in-memory site list.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	streamMode := false
	if v := r.URL.Query().Get("stream"); v == "1" || strings.EqualFold(v, "true") {
		streamMode = true
	}
	items := registrationStatusItems()
	// Stream results as they are ready when requested
	if streamMode {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Cache-Control", "no-cache")
		enc := json.NewEncoder(w)
		for _, pc := range items {
			if ctx.Err() != nil {
				return
			}
			if err := enc.Encode(pc); err != nil {
				return
			}
			flusher.Flush()
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(items)
}

// handleRelays returns active relay base URLs backed by current leases.
func handleRelays(w http.ResponseWriter, _ *http.Request) {
	relays := gPortalMgr.ActiveRelays()
	if relays == nil {
		relays = []string{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(relays)
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
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(gSites.List())
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
		var sanitizedToAdd []string
		for _, s := range toAdd {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			san := sanitizeSiteInput(s)
			if san == "" {
				http.Error(w, fmt.Sprintf("failed to parse url: %s", s), http.StatusBadRequest)
				return
			}
			sanitizedToAdd = append(sanitizedToAdd, san)
		}
		merged := gSites.Merge(sanitizedToAdd)
		// Best-effort immediate registration; retry loop in main.go keeps trying forever.
		tags := utils.SplitCSV(flagTags)
		for _, san := range sanitizedToAdd {
			if _, err := gPortalMgr.ConnectFromSite(san, flagName, flagDescription, flagHide, flagOwner, tags); err != nil {
				log.Warn().Err(err).Msgf("[portal-list] register deferred (offline): %s", san)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(merged)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func siteHostKey(s string) string {
	u, err := url.Parse(normalizeURL(s))
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Host)
}

func mergeSiteLists(current []string, additions []string) []string {
	seen := make(map[string]struct{}, len(current)+len(additions))
	out := make([]string, 0, len(current)+len(additions))
	appendSite := func(raw string) {
		san := sanitizeSiteInput(raw)
		if san == "" {
			return
		}
		key := siteHostKey(san)
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, san)
	}
	for _, site := range current {
		appendSite(site)
	}
	for _, site := range additions {
		appendSite(site)
	}
	return out
}

func removeSiteList(current []string, doomed []string) ([]string, []string) {
	removeKeys := make(map[string]struct{}, len(doomed))
	for _, site := range doomed {
		if key := siteHostKey(site); key != "" {
			removeKeys[key] = struct{}{}
		}
	}
	if len(removeKeys) == 0 {
		out := make([]string, len(current))
		copy(out, current)
		return out, nil
	}

	seen := make(map[string]struct{}, len(current))
	next := make([]string, 0, len(current))
	removed := make([]string, 0, len(removeKeys))
	for _, site := range current {
		san := sanitizeSiteInput(site)
		if san == "" {
			continue
		}
		key := siteHostKey(san)
		if key == "" {
			continue
		}
		if _, ok := removeKeys[key]; ok {
			removed = append(removed, san)
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		next = append(next, san)
	}
	return next, removed
}
