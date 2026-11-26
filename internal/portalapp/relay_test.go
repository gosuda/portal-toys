package portalapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gosuda/portal-tunnel/v2/portal/identity"
	"github.com/gosuda/portal-tunnel/v2/types"
	portalutils "github.com/gosuda/portal-tunnel/v2/utils"
)

func TestExposeLeavesBootstrapSelectionToDiscovery(t *testing.T) {
	// An unverified bootstrap must remain a discovery candidate, not an
	// explicit listener that bypasses MaxActiveRelays.
	bootstrap := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(bootstrap.Close)
	previous := types.BootstrapRelays
	types.BootstrapRelays = []string{bootstrap.URL}
	t.Cleanup(func() { types.BootstrapRelays = previous })

	id, err := identity.Generate("discovery-test")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	exposure, err := Expose(ctx, ExposeConfig{
		Identity:        id,
		Discovery:       true,
		MaxActiveRelays: 1,
	})
	if err != nil {
		t.Fatalf("Expose() error = %v", err)
	}
	defer exposure.Close()
	if relays := exposure.Relays(); len(relays) != 0 {
		t.Fatalf("unverified bootstrap became an explicit listener: %+v", relays)
	}
}

func TestResolveRelayURLsFiltersIncompatibleRelays(t *testing.T) {
	t.Parallel()

	validRelay := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != types.PathSDKDomain {
			http.NotFound(w, r)
			return
		}
		portalutils.WriteAPIData(w, http.StatusOK, types.DomainResponse{
			ProtocolVersion: types.SDKVersion,
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
