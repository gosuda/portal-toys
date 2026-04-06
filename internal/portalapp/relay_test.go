package portalapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gosuda/portal-tunnel/v2/types"
	portalutils "github.com/gosuda/portal-tunnel/v2/utils"
)

func TestResolveRelayURLsFiltersIncompatibleRelays(t *testing.T) {
	t.Parallel()

	validRelay := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != types.PathSDKDomain {
			http.NotFound(w, r)
			return
		}
		portalutils.WriteAPIData(w, http.StatusOK, types.DomainResponse{
			ProtocolVersion: types.ProtocolVersion,
			ReleaseVersion:  "test",
		})
	}))
	defer validRelay.Close()

	invalidRelay := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Client sent an HTTP request to an HTTPS server.", http.StatusBadRequest)
	}))
	defer invalidRelay.Close()

	relayURLs, err := ResolveRelayURLs(context.Background(), []string{invalidRelay.URL, validRelay.URL}, false, nil)
	if err != nil {
		t.Fatalf("ResolveRelayURLs() error = %v", err)
	}
	if len(relayURLs) != 1 {
		t.Fatalf("len(ResolveRelayURLs()) = %d, want 1", len(relayURLs))
	}
	if relayURLs[0] != validRelay.URL {
		t.Fatalf("ResolveRelayURLs()[0] = %q, want %q", relayURLs[0], validRelay.URL)
	}
}

func TestResolveRelayURLsReturnsErrorWhenAllRelaysAreIncompatible(t *testing.T) {
	t.Parallel()

	invalidRelay := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Client sent an HTTP request to an HTTPS server.", http.StatusBadRequest)
	}))
	defer invalidRelay.Close()

	relayURLs, err := ResolveRelayURLs(context.Background(), []string{invalidRelay.URL}, false, nil)
	if err == nil {
		t.Fatal("ResolveRelayURLs() error = nil, want error")
	}
	if len(relayURLs) != 0 {
		t.Fatalf("len(ResolveRelayURLs()) = %d, want 0", len(relayURLs))
	}
}
