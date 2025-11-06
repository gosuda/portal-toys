package main

import (
	"fmt"
	"net/http"

	"github.com/rs/zerolog/log"
)

func main() {
	handler := NewHandler("chatter")
	fmt.Println("Starting server on :8081")
	if err := http.ListenAndServe(":8081", handler); err != nil {
		log.Fatal().Err(err).Msg("failed to start server")
	}
}
