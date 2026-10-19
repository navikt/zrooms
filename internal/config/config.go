// Package config provides configuration management for the application
package config

import (
	"fmt"
	"net/url"
	"os"
)

// ZoomConfig holds all Zoom-related configuration
type ZoomConfig struct {
	ClientID           string
	ClientSecret       string
	RedirectURI        string
	WebhookURL         string
	WebhookSecretToken string
}

// GetZoomConfig loads Zoom configuration from environment variables
func GetZoomConfig() ZoomConfig {
	return ZoomConfig{
		ClientID:           getEnv("ZOOM_CLIENT_ID", ""),
		ClientSecret:       getEnv("ZOOM_CLIENT_SECRET", ""),
		RedirectURI:        getEnv("ZOOM_REDIRECT_URI", ""),
		WebhookURL:         getEnv("ZOOM_WEBHOOK_URL", ""),
		WebhookSecretToken: getEnv("ZOOM_WEBHOOK_SECRET_TOKEN", ""),
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

// GetOAuthURL generates the Zoom OAuth authorization URL
func (c ZoomConfig) GetOAuthURL() string {
	if !c.IsZoomConfigValid() {
		return ""
	}
	query := url.Values{
		"response_type": {"code"}, "client_id": {c.ClientID}, "redirect_uri": {c.RedirectURI},
	}
	return "https://zoom.us/oauth/authorize?" + query.Encode()
}

func Validate() error {
	if GetZoomConfig().WebhookSecretToken == "" {
		return fmt.Errorf("ZOOM_WEBHOOK_SECRET_TOKEN is required")
	}
	if os.Getenv("NAIS_CLUSTER_NAME") != "" && os.Getenv("GCS_BUCKET") == "" {
		return fmt.Errorf("GCS_BUCKET is required on NAIS; refusing volatile storage")
	}
	return nil
}
