package portalapp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gosuda/portal-tunnel/v2/portal/keyless"
	"github.com/gosuda/portal-tunnel/v2/sdk"
	"github.com/gosuda/portal-tunnel/v2/types"
	"github.com/gosuda/portal-tunnel/v2/utils"
	"github.com/rs/zerolog/log"
)

const relayCompatibilityTimeout = 8 * time.Second

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

// ResolveRelayURLs expands the configured relay inputs and keeps only relays
// that answer the Portal compatibility probe. Discovery is collapsed to a
// vetted list so runtime listeners do not keep retrying obviously bad hints.
func ResolveRelayURLs(ctx context.Context, relayURLs []string, discovery bool, rootCAPEM []byte) ([]string, error) {
	candidates, err := utils.ResolvePortalRelayURLs(ctx, relayURLs, discovery)
	if err != nil {
		return nil, fmt.Errorf("resolve relay urls: %w", err)
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	type relayResult struct {
		index int
		url   string
		err   error
	}

	results := make(chan relayResult, len(candidates))
	var wg sync.WaitGroup
	for i, relayURL := range candidates {
		wg.Add(1)
		go func(index int, relayURL string) {
			defer wg.Done()
			results <- relayResult{
				index: index,
				url:   relayURL,
				err:   checkRelayCompatibility(ctx, relayURL, rootCAPEM),
			}
		}(i, relayURL)
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	ordered := make([]relayResult, len(candidates))
	for result := range results {
		ordered[result.index] = result
	}

	compatible := make([]string, 0, len(candidates))
	var lastErr error
	for _, result := range ordered {
		if result.err == nil {
			compatible = append(compatible, result.url)
			continue
		}
		lastErr = result.err
		log.Warn().
			Err(result.err).
			Str("relay_url", result.url).
			Msg("filtered incompatible relay")
	}
	if len(compatible) > 0 {
		if len(compatible) != len(candidates) {
			log.Info().
				Int("candidate_count", len(candidates)).
				Int("compatible_count", len(compatible)).
				Strs("relay_urls", compatible).
				Msg("using vetted relay set")
		}
		return compatible, nil
	}
	return nil, fmt.Errorf("no compatible relays available: %w", lastErr)
}

func Expose(ctx context.Context, cfg sdk.ExposeConfig) (*sdk.Exposure, error) {
	relayURLs, err := ResolveRelayURLs(ctx, cfg.RelayURLs, cfg.Discovery, cfg.RootCAPEM)
	if err != nil {
		return nil, err
	}
	cfg.RelayURLs = relayURLs
	cfg.Discovery = false
	return sdk.Expose(ctx, cfg)
}

func checkRelayCompatibility(ctx context.Context, relayURL string, rootCAPEM []byte) error {
	normalizedRelayURL, err := utils.NormalizeRelayURL(relayURL)
	if err != nil {
		return err
	}

	baseURL, err := url.Parse(normalizedRelayURL)
	if err != nil {
		return fmt.Errorf("parse relay url: %w", err)
	}

	probeCtx, cancel := context.WithTimeout(ctx, relayCompatibilityTimeout)
	defer cancel()

	_, httpClient, err := keyless.NewRelayHTTPClient(probeCtx, baseURL, rootCAPEM, relayCompatibilityTimeout)
	if err != nil {
		return err
	}
	defer closeIdleConnections(httpClient)

	var resp types.DomainResponse
	if err := utils.HTTPDoAPIPath(probeCtx, httpClient, baseURL, http.MethodGet, types.PathSDKDomain, nil, nil, &resp); err != nil {
		return fmt.Errorf("check relay compatibility: %w", err)
	}
	if resp.ProtocolVersion != types.ProtocolVersion {
		return fmt.Errorf("relay protocol version mismatch: relay=%q client=%q", resp.ProtocolVersion, types.ProtocolVersion)
	}
	return nil
}

func closeIdleConnections(httpClient *http.Client) {
	if httpClient == nil {
		return
	}
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok || transport == nil {
		return
	}
	transport.CloseIdleConnections()
}
