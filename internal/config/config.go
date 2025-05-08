// Package config provides configuration management for the application
package config

import (
	"os"
)

// ZoomConfig holds all Zoom-related configuration
type ZoomConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	WebhookURL   string
}

// GetZoomConfig loads Zoom configuration from environment variables
func GetZoomConfig() ZoomConfig {
	return ZoomConfig{
		ClientID:     getEnv("ZOOM_CLIENT_ID", ""),
		ClientSecret: getEnv("ZOOM_CLIENT_SECRET", ""),
		RedirectURI:  getEnv("ZOOM_REDIRECT_URI", ""),
		WebhookURL:   getEnv("ZOOM_WEBHOOK_URL", ""),
	}
}

// getEnv retrieves an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// IsZoomConfigValid checks if all required Zoom configuration is present
func (c ZoomConfig) IsZoomConfigValid() bool {
	return c.ClientID != "" && c.ClientSecret != "" && c.RedirectURI != ""
}
