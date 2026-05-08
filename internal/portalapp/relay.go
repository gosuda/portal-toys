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

	"github.com/gosuda/portal-tunnel/v2/sdk"
	"github.com/gosuda/portal-tunnel/v2/types"
	"github.com/gosuda/portal-tunnel/v2/utils"
)

const relayCompatibilityTimeout = 5 * time.Second

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
func ResolveRelayURLs(ctx context.Context, relayURLs []string, discovery bool, _ []byte) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	resolved, err := utils.ResolvePortalRelayURLs(relayURLs, discovery)
	if err != nil {
		return nil, err
	}
	if len(relayURLs) == 0 || len(resolved) == 0 {
		return resolved, nil
	}

	return filterCompatibleRelayURLs(ctx, resolved)
}

func Expose(ctx context.Context, cfg sdk.ExposeConfig) (*sdk.Exposure, error) {
	return sdk.Expose(ctx, cfg)
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
