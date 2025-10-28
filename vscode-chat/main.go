package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"

	sdk "github.com/gosuda/relaydns/sdk/go"
)

func main() {
	var (
		serverURL  string
		name       string
		targetHost string
		targetPort int
	)

	flag.StringVar(&serverURL, "server-url", "http://relaydns.gosuda.org", "RelayDNS admin base URL to fetch multiaddrs from /health")
	flag.StringVar(&name, "name", "vscode-relay", "Display name shown on RelayDNS server UI")
	flag.StringVar(&targetHost, "target-host", "127.0.0.1", "Local host where VSCode Web listens")
	flag.IntVar(&targetPort, "target-port", 8100, "Local port where VSCode Web listens")
	flag.Parse()

	target := net.JoinHostPort(targetHost, fmt.Sprintf("%d", targetPort))

	// Pre-flight check: see if target is reachable (best-effort)
	d := net.Dialer{Timeout: 1 * time.Second}
	conn, err := d.Dial("tcp", target)
	if err != nil {
		log.Warn().Str("target", target).Msg("target not reachable yet; continuing to advertise anyway")
	} else {
		_ = conn.Close()
		log.Info().Str("target", target).Msg("target reachable")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start RelayDNS client advertising the local web IDE
	client, err := sdk.NewClient(ctx, sdk.ClientConfig{
		Name:      name,
		TargetTCP: target,
		ServerURL: serverURL,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("new relaydns client")
	}
	if err := client.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("start relaydns client")
	}
	log.Info().Msgf("[vscode-relay] advertising %s via RelayDNS as '%s'", target, name)

	// Graceful shutdown
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Info().Msg("[vscode-relay] shutting down...")

	if err := client.Close(); err != nil {
		log.Warn().Err(err).Msg("client close error")
	}
	log.Info().Msg("[vscode-relay] shutdown complete")
}
