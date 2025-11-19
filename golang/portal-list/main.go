package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"gosuda.org/portal/portal/core/cryptoops"
	"gosuda.org/portal/sdk"
)

var rootCmd = &cobra.Command{
	Use:   "portal-list",
	Short: "Portal listing & health check",
	RunE:  run,
}

var (
	flagServerURLs  []string
	flagPortalBase  string
	flagPort        int
	flagName        string
	flagHide        bool
	flagDescription string
	flagOwner       string
	flagTags        string
	flagSitesPath   string
	// computed at runtime: path to sites.json inside data dir
	sitesJSONPath string
)

// portalManager keeps active portal client/listeners per relay URL.
type portalManager struct {
	handler http.Handler
	cred    *cryptoops.Credential
	leases  map[string]*portalLease
}

type portalLease struct {
	relay  string
	client *sdk.RDClient
	ln     net.Listener
}

var gPortalMgr portalManager

func (m *portalManager) Init(handler http.Handler, cred *cryptoops.Credential) {
	m.handler = handler
	m.cred = cred
	if m.leases == nil {
		m.leases = make(map[string]*portalLease)
	}
}

func (m *portalManager) ConnectRelay(ctx context.Context, relayURL string, name, description string, hide bool, owner string, tags []string) error {
	if m.handler == nil || m.cred == nil {
		return fmt.Errorf("portal manager not initialized")
	}
	key := canonicalRelay(relayURL)
	if _, ok := m.leases[key]; ok {
		return nil
	}
	client, err := sdk.NewClient(func(c *sdk.RDClientConfig) { c.BootstrapServers = []string{relayURL} })
	if err != nil {
		return fmt.Errorf("new client: %w", err)
	}
	ln, err := client.Listen(m.cred, name, []string{"http/1.1"},
		sdk.WithDescription(description),
		sdk.WithHide(hide),
		sdk.WithOwner(owner),
		sdk.WithTags(tags),
	)
	if err != nil {
		_ = client.Close()
		return fmt.Errorf("listen: %w", err)
	}
	go func() {
		if err := http.Serve(ln, m.handler); err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
			log.Error().Err(err).Msgf("[portal-list] relay http serve error (%s)", relayURL)
		}
	}()
	m.leases[key] = &portalLease{relay: relayURL, client: client, ln: ln}
	log.Info().Msgf("[portal-list] registered on %s", relayURL)
	return nil
}

func (m *portalManager) ConnectFromSite(ctx context.Context, siteURL string, name, description string, hide bool, owner string, tags []string) (string, error) {
	relay := deriveRelayFromSite(siteURL)
	if relay == "" {
		return "", fmt.Errorf("invalid site URL: %s", siteURL)
	}
	if err := m.ConnectRelay(ctx, relay, name, description, hide, owner, tags); err != nil {
		return "", err
	}
	return relay, nil
}

func (m *portalManager) Shutdown() {
	for k, l := range m.leases {
		_ = l.ln.Close()
		_ = l.client.Close()
		delete(m.leases, k)
	}
}

func canonicalRelay(relay string) string {
	s := strings.ToLower(strings.TrimSpace(relay))
	s = strings.TrimRight(s, "/")
	return s
}

func deriveRelayFromSite(site string) string {
	u, err := url.Parse(normalizeURL(site))
	if err != nil || u.Host == "" {
		return ""
	}
	scheme := "wss"
	if u.Scheme == "http" {
		scheme = "ws"
	}
	return fmt.Sprintf("%s://%s/relay", scheme, u.Host)
}

func init() {
	flags := rootCmd.PersistentFlags()
	relay := firstNonEmpty(os.Getenv("RELAY"), os.Getenv("RELAY_URL"), os.Getenv("SERVER_URL"))
	if relay == "" {
		relay = "wss://portal.gosuda.org/relay"
	}
	flags.StringSliceVar(&flagServerURLs, "server-url", strings.Split(relay, ","), "relay websocket URL(s); repeat or comma-separated (from env RELAY/RELAY_URL/SERVER_URL)")
	flags.StringVar(&flagPortalBase, "portal-base", derivePortalBase(relay), "portal site base URL (optional, used only for SSR listing)")
	flags.IntVar(&flagPort, "port", 8099, "local HTTP port (negative to disable)")
	flags.StringVar(&flagName, "name", "portal-list", "backend display name")
	flags.BoolVar(&flagHide, "hide", false, "hide this lease from portal listings")
	flags.StringVar(&flagDescription, "description", "Portal list viewer (online status)", "lease description")
	flags.StringVar(&flagOwner, "owner", "Portal", "lease owner")
	flags.StringVar(&flagTags, "tags", "portal,viewer", "comma-separated lease tags")
	flags.StringVar(&flagSitesPath, "sites-path", filepath.FromSlash("portal-list/sites"), "sites directory; stores sites.json. Initialize from bootstraps if empty")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute portal-list command")
	}
}

func run(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// compute sites JSON path from data path
	sitesJSONPath = filepath.Join(flagSitesPath, "sites.json")
	// Ensure sites list exists; initialize from bootstraps if empty
	if _, err := loadSitesOrInit(sitesJSONPath, flagServerURLs); err != nil {
		log.Warn().Err(err).Msg("[portal-list] initialize sites from bootstraps failed")
	}

	mux := NewHandler()

	// Portal manager and primary registrations
	cred := sdk.NewCredential()
	gPortalMgr.Init(mux, cred)
	tags := strings.Split(flagTags, ",")
	// Connect to each relay provided in --server-url
	for _, relayURL := range flagServerURLs {
		relayURL = strings.TrimSpace(relayURL)
		if relayURL == "" {
			continue
		}
		if err := gPortalMgr.ConnectRelay(ctx, relayURL, flagName, flagDescription, flagHide, flagOwner, tags); err != nil {
			log.Warn().Err(err).Msgf("[portal-list] failed to register on %s", relayURL)
		}
	}
	// Connect to each site from sites.json as relays (derived)
	if sites, err := readSites(sitesJSONPath); err == nil {
		for _, s := range sites {
			if _, err := gPortalMgr.ConnectFromSite(ctx, s, flagName, flagDescription, flagHide, flagOwner, tags); err != nil {
				log.Warn().Err(err).Msgf("[portal-list] failed to register via site %s", s)
			}
		}
	}

	// Optional local HTTP
	var httpSrv *http.Server
	if flagPort >= 0 {
		httpSrv = &http.Server{Addr: fmt.Sprintf(":%d", flagPort), Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
		log.Info().Msgf("[portal-list] serving locally at http://127.0.0.1:%d", flagPort)
		go func() {
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Warn().Err(err).Msg("[portal-list] local http stopped")
			}
		}()
	}

	// Shutdown watcher
	go func() {
		<-ctx.Done()
		gPortalMgr.Shutdown()
		if httpSrv != nil {
			sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := httpSrv.Shutdown(sctx); err != nil && err != context.Canceled {
				log.Warn().Err(err).Msg("[portal-list] local http shutdown error")
			}
		}
	}()

	<-ctx.Done()
	log.Info().Msg("[portal-list] shutdown complete")
	return nil
}

func derivePortalBase(relay string) string {
	// Accept comma-separated; derive from the first non-empty
	first := strings.TrimSpace(strings.Split(firstNonEmpty(relay, ""), ",")[0])
	if first == "" {
		return "https://portal.gosuda.org/"
	}
	u, err := url.Parse(first)
	if err != nil {
		return "https://portal.gosuda.org/"
	}
	scheme := "https"
	if u.Scheme == "ws" {
		scheme = "http"
	}
	host := u.Host
	if host == "" {
		host = u.Path // fallback
	}
	return fmt.Sprintf("%s://%s/", scheme, host)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// handleHealth: direct URL reachability from sites.json (no SSR parsing)
// (moved to view.go) func handleHealth

// (moved to view.go) func guessNameFromURL

// handleSites supports GET (list) and POST (add url) operations.
// (moved to view.go) func handleSites

// Normalized portal item and health result
// (moved to view.go) type PortalCard

// extractPortalItems attempts to normalize SSR entries into PortalCard skeletons
// (moved to view.go) func extractPortalItems

// healthCheckItems runs a quick HTTP health check for each portal link
// (moved to view.go) func healthCheckItems

// (moved to view.go) func normalizeURL

// (moved to view.go) func isHealthy

// Sites list persistence
// (moved to view.go) func loadSitesOrInit

// (moved to view.go) func readSites

// (moved to view.go) func writeSites

// (moved to view.go) func hasNonEmpty
