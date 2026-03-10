package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

func TestSiteFailureTrackerPrunesAfterGracePeriod(t *testing.T) {
	var tracker siteFailureTracker
	site := "https://dead.example.com/"
	start := time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC)

	if tracker.mark(site, false, start) {
		t.Fatalf("first failure must not prune immediately")
	}
	if tracker.mark(site, false, start.Add(siteHealthPruneAfter-time.Second)) {
		t.Fatalf("site must survive until the full grace period elapses")
	}
	if !tracker.mark(site, false, start.Add(siteHealthPruneAfter)) {
		t.Fatalf("site should prune once the grace period elapses")
	}
	if tracker.mark(site, true, start.Add(siteHealthPruneAfter+time.Second)) {
		t.Fatalf("healthy site must clear failure state")
	}
	if tracker.mark(site, false, start.Add(siteHealthPruneAfter+2*time.Second)) {
		t.Fatalf("failure timer must restart after a healthy check")
	}
}

func TestSiteRegistryMergeAndRemove(t *testing.T) {
	var registry siteRegistry
	registry.Init([]string{"https://portal.gosuda.org/"})

	got := registry.Merge([]string{
		"https://foo.example.com/path",
		"foo.example.com",
		"https://bar.example.com/",
	})
	want := []string{
		"https://portal.gosuda.org/",
		"https://foo.example.com/",
		"https://bar.example.com/",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected merged sites: got %v want %v", got, want)
	}

	removed := registry.Remove([]string{"https://foo.example.com/other"})
	if !slices.Equal(removed, []string{"https://foo.example.com/"}) {
		t.Fatalf("unexpected removed sites: %v", removed)
	}

	remaining := registry.List()
	want = []string{
		"https://portal.gosuda.org/",
		"https://bar.example.com/",
	}
	if !slices.Equal(remaining, want) {
		t.Fatalf("unexpected remaining sites: got %v want %v", remaining, want)
	}
}

func TestDeriveBootstrapSitesDedupes(t *testing.T) {
	got := deriveBootstrapSites([]string{
		"https://portal.gosuda.org/",
		"https://portal.gosuda.org/",
		"https://relay.example.com/",
	})
	want := []string{
		"https://portal.gosuda.org/",
		"https://relay.example.com/",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected bootstrap sites: got %v want %v", got, want)
	}
}

func TestRegistrationStatusItemsUsesLeaseMap(t *testing.T) {
	gSites = siteRegistry{}
	gPortalMgr = portalManager{}
	gSites.Init([]string{
		"https://online.example.com/",
		"https://offline.example.com/",
	})
	gPortalMgr.leases = map[string]*portalLease{
		canonicalRelay("https://online.example.com"): {
			relay: "https://online.example.com",
		},
	}

	items := registrationStatusItems()
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	status := map[string]bool{}
	for _, it := range items {
		status[it.Link] = it.Healthy
	}

	if !status["https://online.example.com/"] {
		t.Fatalf("online site must be healthy by lease-map status")
	}
	if status["https://offline.example.com/"] {
		t.Fatalf("offline site must not be healthy without lease-map entry")
	}
}

func TestHandleSitesKeepsOfflineSiteWhenRegistrationFails(t *testing.T) {
	gSites = siteRegistry{}
	gPortalMgr = portalManager{} // handler intentionally nil -> registration fails
	h := http.HandlerFunc(handleSites)

	req := httptest.NewRequest(http.MethodPost, "/api/sites", bytes.NewBufferString(`{"url":"https://retry.example.com/"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var got []string
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(got) != 1 || got[0] != "https://retry.example.com/" {
		t.Fatalf("unexpected sites after post: %v", got)
	}

	list := gSites.List()
	if len(list) != 1 || list[0] != "https://retry.example.com/" {
		t.Fatalf("site should remain tracked for retry, got %v", list)
	}
}
