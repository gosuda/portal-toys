package main

import (
    "context"
    "fmt"
    "net"
    "net/http"
    "net/http/httputil"
    "net/url"
    "os"
    "os/signal"
    "strings"
    "syscall"

    "github.com/rs/zerolog/log"
    "github.com/spf13/cobra"

    "gosuda.org/portal/sdk"
)

var rootCmd = &cobra.Command{
    Use:   "vscode-chat",
    Short: "Portal demo: VSCode Web relay proxy",
    RunE:  runVSCodeRelay,
}

var (
    flagServerURLs []string
    flagName       string
    flagTargetHost string
    flagTargetPort int
)

func init() {
    flags := rootCmd.PersistentFlags()
    flags.StringSliceVar(&flagServerURLs, "server-url", strings.Split(os.Getenv("RELAY"), ","), "relay websocket URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
    flags.StringVar(&flagName, "name", "vscode-relay", "Display name shown on server UI")
    flags.StringVar(&flagTargetHost, "target-host", "127.0.0.1", "Local host where VSCode Web listens")
    flags.IntVar(&flagTargetPort, "target-port", 8100, "Local port where VSCode Web listens")
}

func main() {
    if err := rootCmd.Execute(); err != nil {
        log.Fatal().Err(err).Msg("execute vscode-chat command")
    }
}

func runVSCodeRelay(cmd *cobra.Command, args []string) error {
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()

    // Build reverse proxy to the local VSCode Web instance
    backendURL, err := url.Parse(fmt.Sprintf("http://%s:%d", flagTargetHost, flagTargetPort))
    if err != nil {
        return fmt.Errorf("parse target url: %w", err)
    }
    proxy := httputil.NewSingleHostReverseProxy(backendURL)
    // Trust X-Forwarded headers for ws and origin handling
    proxy.Director = func(req *http.Request) {
        req.URL.Scheme = backendURL.Scheme
        req.URL.Host = backendURL.Host
        // Strip relay peer prefix if present
        const prefix = "/peer/"
        if strings.HasPrefix(req.URL.Path, prefix) {
            rest := strings.TrimPrefix(req.URL.Path, prefix)
            if i := strings.IndexByte(rest, '/'); i >= 0 {
                req.URL.Path = rest[i:]
            }
        }
        // Preserve original Host for backend
        req.Host = backendURL.Host
        // Add forwarding headers
        req.Header.Set("X-Forwarded-Host", req.Host)
        req.Header.Set("X-Forwarded-Proto", "http")
    }

    // Create credentials and relay clients, then listen and serve proxy over each relay
    cred := sdk.NewCredential()
    var clients []*sdk.RDClient
    var listeners []net.Listener
    for _, raw := range flagServerURLs {
        if raw == "" { continue }
        for _, p := range strings.Split(raw, ",") {
            u := strings.TrimSpace(p)
            if u == "" { continue }
            client, err := sdk.NewClient(func(c *sdk.RDClientConfig) { c.BootstrapServers = []string{u} })
            if err != nil {
                log.Error().Err(err).Str("url", u).Msg("new relay client failed")
                continue
            }
            clients = append(clients, client)
            ln, err := client.Listen(cred, flagName, []string{"vscode-chat"})
            if err != nil {
                log.Error().Err(err).Str("url", u).Msg("relay listen failed")
                continue
            }
            listeners = append(listeners, ln)
        }
    }
    if len(listeners) == 0 {
        return fmt.Errorf("no valid relay servers provided via --server-url or RELAY/RELAY_URL env")
    }
    for i, ln := range listeners {
        idx := i
        go func() {
            if err := http.Serve(ln, proxy); err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
                log.Error().Err(err).Int("listener", idx).Msg("[vscode-relay] http serve error")
            }
        }()
    }

    <-ctx.Done()
    for _, ln := range listeners { _ = ln.Close() }
    for _, c := range clients { _ = c.Close() }
    log.Info().Msg("[vscode-relay] shutdown complete")
    return nil
}
