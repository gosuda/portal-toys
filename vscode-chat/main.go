package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/rs/zerolog/log"

	sdk "github.com/gosuda/relaydns/sdk"
)

func main() {
	var (
		serverURL  string
		name       string
		targetHost string
		targetPort int
	)

	flag.StringVar(&serverURL, "server-url", "wss://relaydns.gosuda.org/relay", "RelayDNS relay websocket URL")
	flag.StringVar(&name, "name", "vscode-relay", "Display name shown on RelayDNS server UI")
	flag.StringVar(&targetHost, "target-host", "127.0.0.1", "Local host where VSCode Web listens")
	flag.IntVar(&targetPort, "target-port", 8100, "Local port where VSCode Web listens")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Build reverse proxy to the local VSCode Web instance
	backendURL, err := url.Parse(fmt.Sprintf("http://%s:%d", targetHost, targetPort))
	if err != nil {
		log.Fatal().Err(err).Msg("parse target url")
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

	// Create credential and relay client, then listen and serve proxy over relay
	client, err := sdk.NewClient(func(c *sdk.RDClientConfig) {
		c.BootstrapServers = []string{serverURL}
	})
	if err != nil {
		log.Fatal().Err(err).Msg("new relay client")
	}
	defer client.Close()

	cred := sdk.NewCredential()
	listener, err := client.Listen(cred, name, []string{"vscode-chat"})
	if err != nil {
		log.Fatal().Err(err).Msg("relay listen")
	}

	go func() {
		if err := http.Serve(listener, proxy); err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
			log.Error().Err(err).Msg("[vscode-relay] http serve error")
		}
	}()

	<-ctx.Done()
	_ = listener.Close()
	log.Info().Msg("[vscode-relay] shutdown complete")
}
