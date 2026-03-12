package main

import (
	"embed"
	"os"
	"strings"

	"github.com/gosuda/portal/v2/utils"
)

//go:embed static
var embeddedStatic embed.FS

// getEnv returns the environment variable value or the default value if not set
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvBool returns the environment variable as a boolean or the default value if not set
func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return value == "true" || value == "1" || value == "yes"
	}
	return defaultValue
}

// getEnvSlice returns a comma-separated environment variable as a string slice, filtering empty strings
func getEnvSlice(key string) []string {
	value := os.Getenv(key)
	if value == "" {
		return nil
	}
	// Remove surrounding quotes if present
	value = strings.Trim(value, "'\"")
	parts := utils.SplitCSV(value)
	// Filter out empty strings and trim quotes/whitespace
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		trimmed = strings.Trim(trimmed, "'\"")
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
