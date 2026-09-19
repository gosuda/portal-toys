package portalapp

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/gosuda/portal-tunnel/v2/portal/identity"
	"github.com/gosuda/portal-tunnel/v2/sdk"
	"github.com/gosuda/portal-tunnel/v2/types"
	"github.com/gosuda/portal-tunnel/v2/utils"
)

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

// ExposeConfig mirrors the expose flags shared by every toy.
type ExposeConfig struct {
	RelayURLs    []string
	BanMITM      bool
	Discovery    bool
	Identity     types.Identity
	IdentityPath string
	Metadata     types.LeaseMetadata
}

// ResolveRelayURLs normalizes the configured relay inputs. With discovery the
// exposure expands registry candidates itself; an empty explicit list falls
// back to the bootstrap registry for callers that display the relay list.
func ResolveRelayURLs(_ context.Context, relayURLs []string, discovery bool, _ []byte) ([]string, error) {
	if discovery && len(relayURLs) == 0 {
		relayURLs = types.BootstrapRelays
	}
	return utils.NormalizeRelayURLs(relayURLs...)
}

// Expose starts the exposure described by cfg. When an identity path is set,
// the file is the single identity source: loaded when present, created when
// missing; otherwise the caller-provided identity is used as-is.
func Expose(ctx context.Context, cfg ExposeConfig) (*sdk.Exposure, error) {
	listenerIdentity := cfg.Identity
	if cfg.IdentityPath != "" {
		loaded, err := identity.LoadOrCreate(cfg.Identity.Name, "", cfg.IdentityPath, "")
		if err != nil {
			return nil, fmt.Errorf("resolve identity: %w", err)
		}
		listenerIdentity = loaded
	}

	opts := []sdk.Option{
		sdk.WithMITMProtection(cfg.BanMITM),
		sdk.WithMetadata(cfg.Metadata),
	}
	if cfg.Discovery {
		opts = append(opts, sdk.WithDiscovery(0))
	}
	return sdk.Expose(ctx, listenerIdentity, cfg.RelayURLs, opts...)
}
