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
func ResolveRelayURLs(ctx context.Context, relayURLs []string, discoveryMode bool, _ []byte) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	resolved, err := discovery.ResolveRelayURLs(relayURLs, discoveryMode)
	if err != nil {
		return nil, err
	}
	if len(relayURLs) == 0 || len(resolved) == 0 {
		return resolved, nil
	}

	return filterCompatibleRelayURLs(ctx, resolved)
}

type ExposeConfig struct {
	RelayURLs       []string
	Discovery       bool
	Identity        types.Identity
	IdentityPath    string
	IdentityJSON    string
	TargetAddr      string
	UDPAddr         string
	UDPEnabled      bool
	TCPEnabled      bool
	BanMITM         bool
	MaxActiveRelays int
	Metadata        types.LeaseMetadata
}

type Exposure struct {
	*sdk.Exposure
}

func (e *Exposure) RunHTTP(ctx context.Context, handler http.Handler, localAddr string) error {
	if e == nil || e.Exposure == nil {
		return errors.New("portalapp: exposure is nil")
	}
	return sdk.RunHTTP(ctx, e.Exposure, handler, localAddr)
}
func (e *Exposure) ActiveRelayURLs() []string {
	if e == nil || e.Exposure == nil {
		return nil
	}
	return e.ActiveRelays()
}

func Expose(ctx context.Context, cfg ExposeConfig) (*Exposure, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	id := cfg.Identity
	if strings.TrimSpace(id.Address) == "" ||
		strings.TrimSpace(id.PublicKey) == "" ||
		strings.TrimSpace(id.PrivateKey) == "" {
		var err error
		id, err = identity.LoadOrCreate(cfg.Identity.Name, cfg.TargetAddr, cfg.IdentityPath, cfg.IdentityJSON)
		if err != nil {
			return nil, fmt.Errorf("load or create identity: %w", err)
		}
	}

	relays, err := ResolveRelayURLs(ctx, cfg.RelayURLs, cfg.Discovery, nil)
	if err != nil {
		return nil, fmt.Errorf("resolve relay urls: %w", err)
	}

	var opts []sdk.Option
	if cfg.Discovery {
		opts = append(opts, sdk.WithDiscovery(cfg.MaxActiveRelays))
	}
	if cfg.UDPEnabled {
		opts = append(opts, sdk.WithUDP())
	}
	if cfg.TCPEnabled {
		opts = append(opts, sdk.WithTCP())
	}
	if cfg.BanMITM {
		opts = append(opts, sdk.WithMITMProtection(true))
	}
	if cfg.Metadata.Description != "" || len(cfg.Metadata.Tags) > 0 || cfg.Metadata.Owner != "" || cfg.Metadata.Hide || cfg.Metadata.Thumbnail != "" {
		opts = append(opts, sdk.WithMetadata(cfg.Metadata))
	}

	rawExp, err := sdk.Expose(ctx, id, relays, opts...)
	if err != nil {
		return nil, err
	}
	return &Exposure{Exposure: rawExp}, nil
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
