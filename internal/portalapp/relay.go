package portalapp

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/gosuda/portal-tunnel/v2/sdk"
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

// ResolveRelayURLs expands the configured relay inputs using discovery.
func ResolveRelayURLs(ctx context.Context, relayURLs []string, discovery bool, _ []byte) ([]string, error) {
	return utils.ResolvePortalRelayURLs(ctx, relayURLs, discovery)
}

func Expose(ctx context.Context, cfg sdk.ExposeConfig) (*sdk.Exposure, error) {
	return sdk.Expose(ctx, cfg)
}
