package main

import (
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
