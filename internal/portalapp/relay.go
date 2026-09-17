package portalapp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gosuda/portal-tunnel/v2/portal/discovery"
	"github.com/gosuda/portal-tunnel/v2/portal/identity"
	"github.com/gosuda/portal-tunnel/v2/sdk"
	"github.com/gosuda/portal-tunnel/v2/types"
	"github.com/gosuda/portal-tunnel/v2/utils"
)

const relayCompatibilityTimeout = 5 * time.Second

// ExposeConfig carries the toy-facing exposure settings. Identity file
// resolution and relay URL resolution happen here because sdk.Expose
// expects an already-resolved identity (it never creates or persists
// keys) and concrete relay URLs.
type ExposeConfig struct {
	RelayURLs    []string
	Discovery    bool
	BanMITM      bool
	Identity     types.Identity
	IdentityPath string
	Metadata     types.LeaseMetadata
}

func ResolveBoolEnv(fallback bool, envNames ...string) bool {
	for _, envName := range envNames {
		raw := strings.TrimSpace(os.Getenv(envName))
		if raw == "" {
			continue
		}
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return fallback
		}
		return parsed
	}
	return fallback
}

// ResolveRelayURLs expands the configured relay inputs using discovery.
func ResolveRelayURLs(ctx context.Context, relayURLs []string, useDiscovery bool, _ []byte) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	resolved, err := discovery.ResolveRelayURLs(relayURLs, useDiscovery)
	if err != nil {
		return nil, err
	}
	if len(relayURLs) == 0 || len(resolved) == 0 {
		return resolved, nil
	}

	return filterCompatibleRelayURLs(ctx, resolved)
}

// Expose resolves the toy identity and relay membership, then hands them
// to the portal SDK.
func Expose(ctx context.Context, cfg ExposeConfig) (*sdk.Exposure, error) {
	listenerIdentity, err := identity.LoadOrCreate(cfg.Identity.Name, "", cfg.IdentityPath, "")
	if err != nil {
		return nil, fmt.Errorf("resolve identity: %w", err)
	}

	relayURLs, err := discovery.ResolveRelayURLs(cfg.RelayURLs, cfg.Discovery)
	if err != nil {
		return nil, err
	}

	opts := []sdk.Option{sdk.WithMetadata(cfg.Metadata)}
	if cfg.BanMITM {
		opts = append(opts, sdk.WithMITMProtection(true))
	}
	return sdk.Expose(ctx, listenerIdentity, relayURLs, opts...)
}

func filterCompatibleRelayURLs(ctx context.Context, relayURLs []string) ([]string, error) {
	compatible := make([]string, 0, len(relayURLs))
	var checkErr error

	for _, relayURL := range relayURLs {
		if err := checkRelayCompatibility(ctx, relayURL); err != nil {
			checkErr = errors.Join(checkErr, fmt.Errorf("%s: %w", relayURL, err))
			continue
		}
		compatible = append(compatible, relayURL)
	}

	if len(compatible) == 0 && len(relayURLs) > 0 {
		return nil, fmt.Errorf("no compatible portal relays found: %w", checkErr)
	}
	return compatible, nil
}

func checkRelayCompatibility(ctx context.Context, rawRelayURL string) error {
	relayURL, err := url.Parse(rawRelayURL)
	if err != nil {
		return fmt.Errorf("parse relay url: %w", err)
	}
	if relayURL.Host == "" {
		return fmt.Errorf("relay url host is empty")
	}

	checkCtx, cancel := context.WithTimeout(ctx, relayCompatibilityTimeout)
	defer cancel()

	_, client, transport, err := utils.NewHTTPTLSClient(checkCtx, relayURL, relayCompatibilityTimeout)
	if err != nil {
		return err
	}
	defer transport.CloseIdleConnections()

	var domain types.DomainResponse
	if err := utils.HTTPDoAPIPath(checkCtx, client, relayURL, http.MethodGet, types.PathSDKDomain, nil, nil, &domain); err != nil {
		return err
	}
	if strings.TrimSpace(domain.ProtocolVersion) != types.SDKVersion {
		return fmt.Errorf("relay sdk protocol version mismatch: relay=%q client=%q", domain.ProtocolVersion, types.SDKVersion)
	}
	return nil
}
