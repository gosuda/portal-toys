package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gosuda/portal/v2/sdk"
	"github.com/gosuda/portal/v2/types"
	"github.com/gosuda/portal/v2/utils"
	"github.com/rs/zerolog/log"
)

const (
	siteHealthSweepInterval = 15 * time.Second
	siteHealthPruneAfter    = 2 * time.Minute
	relayRetryInterval      = 10 * time.Second
)

// portalManager keeps active portal client/listeners per relay URL.
type portalManager struct {
	handler http.Handler
	ctx     context.Context
	mu      sync.Mutex
	leases  map[string]*portalLease
}

type portalLease struct {
	relay    string
	exposure *sdk.Exposure
}

type siteFailureTracker struct {
	mu          sync.Mutex
	failedSince map[string]time.Time
}

type siteRegistry struct {
	mu    sync.RWMutex
	sites []string
}

var gPortalMgr portalManager

var gSiteFailures siteFailureTracker

var gSites siteRegistry

func (m *portalManager) Init(ctx context.Context, handler http.Handler) {
	if ctx == nil {
		ctx = context.Background()
	}
	m.ctx = ctx
	m.handler = handler
	if m.leases == nil {
		m.leases = make(map[string]*portalLease)
	}
}

func (m *portalManager) context() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

func (m *portalManager) ConnectRelay(relayURL string, name, description string, hide bool, owner string, tags []string) error {
	if m.handler == nil {
		return fmt.Errorf("portal manager not initialized")
	}
	ctx := m.context()
	normalizedRelay, err := utils.NormalizeRelayURL(relayURL)
	if err != nil {
		return fmt.Errorf("normalize relay: %w", err)
	}
	key := canonicalRelay(normalizedRelay)
	m.mu.Lock()
	if _, ok := m.leases[key]; ok {
		m.mu.Unlock()
		return nil
	}
	exposure, err := sdk.Expose(ctx, sdk.ExposeConfig{
		RelayURLs:    []string{normalizedRelay},
		BanMITM:      flagBanMITM,
		Name:         name,
		IdentityPath: flagIdentityPath,
		Metadata: types.LeaseMetadata{
			Description: description,
			Owner:       owner,
			Thumbnail:   flagThumbnail,
			Tags:        tags,
			Hide:        hide,
		},
	})
	if err != nil {
		m.mu.Unlock()
		return fmt.Errorf("expose: %w", err)
	}
	go func() {
		if err := http.Serve(exposure, m.handler); err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
			log.Error().Err(err).Msgf("[portal-list] relay http serve error (%s)", normalizedRelay)
		}
	}()
	m.leases[key] = &portalLease{relay: normalizedRelay, exposure: exposure}
	m.mu.Unlock()
	log.Info().Msgf("[portal-list] registered on %s", normalizedRelay)
	return nil
}

func (m *portalManager) ConnectFromSite(siteURL string, name, description string, hide bool, owner string, tags []string) (string, error) {
	if err := m.ConnectRelay(siteURL, name, description, hide, owner, tags); err != nil {
		return "", err
	}
	return siteURL, nil
}

func (m *portalManager) DisconnectRelay(relayURL string) bool {
	key := canonicalRelay(relayURL)

	m.mu.Lock()
	lease := m.leases[key]
	if lease != nil {
		delete(m.leases, key)
	}
	m.mu.Unlock()

	if lease == nil {
		return false
	}
	if err := lease.exposure.Close(); err != nil {
		log.Warn().Err(err).Msgf("[portal-list] close exposure failed (%s)", lease.relay)
	}
	log.Info().Msgf("[portal-list] unregistered from %s", lease.relay)
	return true
}

func (m *portalManager) DisconnectSite(siteURL string) bool {
	relay := deriveRelayFromSite(siteURL)
	if relay == "" {
		relay = siteURL
	}
	return m.DisconnectRelay(relay)
}

func (m *portalManager) HasRelay(relayURL string) bool {
	key := canonicalRelay(relayURL)
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.leases[key]
	return ok
}

func (m *portalManager) ActiveRelays() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.leases))
	for _, lease := range m.leases {
		out = append(out, lease.relay)
	}
	sort.Strings(out)
	return out
}

func (m *portalManager) Shutdown() {
	m.mu.Lock()
	leases := make([]*portalLease, 0, len(m.leases))
	for k, l := range m.leases {
		leases = append(leases, l)
		delete(m.leases, k)
	}
	m.mu.Unlock()

	for _, l := range leases {
		_ = l.exposure.Close()
	}
}

func (r *siteRegistry) Init(initial []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sites = mergeSiteLists(nil, initial)
}

func (r *siteRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, len(r.sites))
	copy(out, r.sites)
	return out
}

func (r *siteRegistry) Merge(additions []string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sites = mergeSiteLists(r.sites, additions)
	out := make([]string, len(r.sites))
	copy(out, r.sites)
	return out
}

func (r *siteRegistry) Remove(doomed []string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	next, removed := removeSiteList(r.sites, doomed)
	r.sites = next
	out := make([]string, len(removed))
	copy(out, removed)
	return out
}

func canonicalRelay(relay string) string {
	s := strings.ToLower(strings.TrimSpace(relay))
	s = strings.TrimRight(s, "/")
	return s
}

func canonicalSite(site string) string {
	s := sanitizeSiteInput(site)
	if s == "" {
		s = normalizeURL(site)
	}
	return canonicalRelay(s)
}

func deriveRelayFromSite(site string) string {
	u, err := url.Parse(normalizeURL(site))
	if err != nil || u.Host == "" {
		return ""
	}
	scheme := "https"
	if u.Scheme == "http" {
		scheme = "http"
	}
	return fmt.Sprintf("%s://%s", scheme, u.Host)
}

func deriveBootstrapSites(relays []string) []string {
	sites := make([]string, 0, len(relays))
	for _, relay := range relays {
		relay = strings.TrimSpace(relay)
		if relay == "" {
			continue
		}
		sites = append(sites, derivePortalBase(relay))
	}
	if len(sites) == 0 {
		sites = append(sites, "https://portal.gosuda.org/")
	}
	return mergeSiteLists(nil, sites)
}

func (t *siteFailureTracker) mark(site string, healthy bool, now time.Time) bool {
	key := canonicalSite(site)
	if key == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.failedSince == nil {
		t.failedSince = make(map[string]time.Time)
	}
	if healthy {
		delete(t.failedSince, key)
		return false
	}
	since, ok := t.failedSince[key]
	if !ok {
		t.failedSince[key] = now
		return false
	}
	return now.Sub(since) >= siteHealthPruneAfter
}

func (t *siteFailureTracker) forget(site string) {
	key := canonicalSite(site)
	if key == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.failedSince, key)
}

func (t *siteFailureTracker) syncSites(sites []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(sites) == 0 {
		clear(t.failedSince)
		return
	}
	keep := make(map[string]struct{}, len(sites))
	for _, site := range sites {
		if key := canonicalSite(site); key != "" {
			keep[key] = struct{}{}
		}
	}
	for key := range t.failedSince {
		if _, ok := keep[key]; !ok {
			delete(t.failedSince, key)
		}
	}
}

func monitorSiteHealth(ctx context.Context) {
	ticker := time.NewTicker(siteHealthSweepInterval)
	defer ticker.Stop()

	for {
		pruneFailedSitesOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func pruneFailedSitesOnce(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}

	sites := gSites.List()
	gSiteFailures.syncSites(sites)
	if len(sites) == 0 {
		return
	}

	items := make([]PortalCard, 0, len(sites))
	for _, site := range sites {
		san := sanitizeSiteInput(site)
		if san == "" {
			continue
		}
		items = append(items, PortalCard{
			Name: guessNameFromURL(san),
			Link: san,
		})
	}
	if len(items) == 0 {
		return
	}

	rounds := max((len(items)+31)/32, 1)
	checkCtx, cancel := context.WithTimeout(ctx, time.Duration(rounds*3+1)*time.Second)
	defer cancel()

	checked := healthCheckItems(checkCtx, items)
	now := time.Now().UTC()
	doomed := make([]string, 0, len(checked))
	for _, item := range checked {
		site := sanitizeSiteInput(item.Link)
		if site == "" {
			continue
		}
		if gSiteFailures.mark(site, item.Healthy, now) {
			doomed = append(doomed, site)
		}
	}
	if len(doomed) == 0 {
		return
	}

	removed := gSites.Remove(doomed)
	for _, site := range removed {
		gSiteFailures.forget(site)
		gPortalMgr.DisconnectSite(site)
		log.Warn().Msgf("[portal-list] removed unhealthy site after %s: %s", siteHealthPruneAfter, site)
	}
}

func monitorRelayRegistration(ctx context.Context) {
	ticker := time.NewTicker(relayRetryInterval)
	defer ticker.Stop()
	for {
		retryRelayRegistrationOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func retryRelayRegistrationOnce(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	tags := utils.SplitCSV(flagTags)
	for _, site := range gSites.List() {
		if ctx.Err() != nil {
			return
		}
		site = sanitizeSiteInput(site)
		if site == "" {
			continue
		}
		relay := deriveRelayFromSite(site)
		if relay == "" || gPortalMgr.HasRelay(relay) {
			continue
		}
		if _, err := gPortalMgr.ConnectFromSite(site, flagName, flagDescription, flagHide, flagOwner, tags); err != nil {
			log.Warn().Err(err).Msgf("[portal-list] relay register retry failed for %s", site)
			continue
		}
		log.Info().Msgf("[portal-list] relay registered by retry loop: %s", relay)
		time.Sleep(300 * time.Millisecond)
	}
}

func derivePortalBase(relay string) string {
	relayURLs := utils.SplitCSV(firstNonEmpty(relay, ""))
	first := ""
	if len(relayURLs) > 0 {
		first = strings.TrimSpace(relayURLs[0])
	}
	if first == "" {
		return ""
	}
	if !strings.HasSuffix(first, "/") {
		return first + "/"
	}
	return first
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
