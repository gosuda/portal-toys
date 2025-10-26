package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	sdk "github.com/gosuda/relaydns/sdk/go"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const (
		serverURL  = "http://173.249.203.13:8080"
		target     = "127.0.0.1:8081"
		name       = "Chatter BBS"
		relayMulti = "/ip4/173.249.203.13/tcp/4001/p2p/12D3KooWPUTbN3WD5ew5QvZhNxJ9ckvEQJ2QQHvWEVXQHZsiZk3D"
	)

	client, err := sdk.NewClient(ctx, sdk.ClientConfig{
		Name:       name,
		TargetTCP:  target,
		ServerURL:  serverURL,
		Bootstraps: []string{relayMulti},
	})
	if err != nil {
		log.Fatal().Err(err).Msg("new client failed")
	}

	if err := client.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("start client failed")
	}

	log.Info().Msgf("[client] connected to relay %s and forwarding %s as %s", relayMulti, target, name)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Info().Msg("[client] shutting down...")
	if err := client.Close(); err != nil {
		log.Warn().Err(err).Msg("[client] close error")
	}

	log.Info().Msg("[client] shutdown complete")
	time.Sleep(time.Second)
}

