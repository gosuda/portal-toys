package main

import (
	"embed"
	"os"
)

//go:embed static
var embeddedStatic embed.FS

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
