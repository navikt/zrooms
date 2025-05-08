// Package config provides configuration management for the application
package config

import (
	"os"
	"strconv"
	"time"
)

// ZoomConfig holds all Zoom-related configuration
type ZoomConfig struct {
	ClientID           string
	ClientSecret       string
	RedirectURI        string
	WebhookURL         string
	WebhookSecretToken string
}

// RedisConfig holds Redis/Valkey configuration
type RedisConfig struct {
	Enabled bool
	// URI is prioritized if provided, otherwise individual connection parameters are used
	URI       string
	Host      string
	Port      string
	Username  string
	Password  string
	DB        int
	KeyPrefix string
	// TTL for meetings (0 means no expiration)
	MeetingTTL time.Duration
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

// GetRedisConfig loads Redis/Valkey configuration from environment variables
func GetRedisConfig() RedisConfig {
	// Parse TTL from environment variable (in hours)
	ttlHours, _ := strconv.Atoi(getEnv("REDIS_MEETING_TTL_HOURS", "168")) // Default 7 days
	ttl := time.Duration(ttlHours) * time.Hour

	// Parse DB index
	db, _ := strconv.Atoi(getEnv("REDIS_DB", "0"))

	return RedisConfig{
		Enabled:    getEnvBool("REDIS_ENABLED", false),
		URI:        getEnv("REDIS_URI_ZROOMS", ""),
		Host:       getEnv("REDIS_HOST_ZROOMS", getEnv("REDIS_ADDRESS", "localhost")),
		Port:       getEnv("REDIS_PORT_ZROOMS", "6379"),
		Username:   getEnv("REDIS_USERNAME_ZROOMS", ""),
		Password:   getEnv("REDIS_PASSWORD_ZROOMS", getEnv("REDIS_PASSWORD", "")),
		DB:         db,
		KeyPrefix:  getEnv("REDIS_KEY_PREFIX", "zrooms:"),
		MeetingTTL: ttl,
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

// getEnvBool retrieves a boolean environment variable
func getEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	b, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue
	}
	return b
}

// IsZoomConfigValid checks if all required Zoom configuration is present
func (c ZoomConfig) IsZoomConfigValid() bool {
	return c.ClientID != "" && c.ClientSecret != "" && c.RedirectURI != ""
}
