package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestZoomConfig_GetOAuthURL(t *testing.T) {
	tests := []struct {
		name     string
		config   ZoomConfig
		expected string
	}{
		{
			name: "valid config generates correct OAuth URL",
			config: ZoomConfig{
				ClientID:     "A6AnK9iR4KLnJS0fw7pNw",
				RedirectURI:  "https://zrooms.nav.no/oauth/redirect",
				ClientSecret: "secret123", // Required for IsZoomConfigValid
			},
			expected: "https://zoom.us/oauth/authorize?response_type=code&client_id=A6AnK9iR4KLnJS0fw7pNw&redirect_uri=https://zrooms.nav.no/oauth/redirect",
		},
		{
			name: "missing client ID returns empty string",
			config: ZoomConfig{
				ClientID:     "",
				RedirectURI:  "https://zrooms.nav.no/oauth/redirect",
				ClientSecret: "secret123",
			},
			expected: "",
		},
		{
			name: "missing redirect URI returns empty string",
			config: ZoomConfig{
				ClientID:     "A6AnK9iR4KLnJS0fw7pNw",
				RedirectURI:  "",
				ClientSecret: "secret123",
			},
			expected: "",
		},
		{
			name: "missing client secret returns empty string",
			config: ZoomConfig{
				ClientID:     "A6AnK9iR4KLnJS0fw7pNw",
				RedirectURI:  "https://zrooms.nav.no/oauth/redirect",
				ClientSecret: "",
			},
			expected: "",
		},
		{
			name: "localhost development config",
			config: ZoomConfig{
				ClientID:     "dev_client_id",
				RedirectURI:  "http://localhost:8080/oauth/redirect",
				ClientSecret: "dev_secret",
			},
			expected: "https://zoom.us/oauth/authorize?response_type=code&client_id=dev_client_id&redirect_uri=http://localhost:8080/oauth/redirect",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetOAuthURL()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestZoomConfig_IsZoomConfigValid(t *testing.T) {
	tests := []struct {
		name     string
		config   ZoomConfig
		expected bool
	}{
		{
			name: "valid config",
			config: ZoomConfig{
				ClientID:     "client123",
				ClientSecret: "secret456",
				RedirectURI:  "https://example.com/callback",
			},
			expected: true,
		},
		{
			name: "missing client ID",
			config: ZoomConfig{
				ClientID:     "",
				ClientSecret: "secret456",
				RedirectURI:  "https://example.com/callback",
			},
			expected: false,
		},
		{
			name: "missing client secret",
			config: ZoomConfig{
				ClientID:     "client123",
				ClientSecret: "",
				RedirectURI:  "https://example.com/callback",
			},
			expected: false,
		},
		{
			name: "missing redirect URI",
			config: ZoomConfig{
				ClientID:     "client123",
				ClientSecret: "secret456",
				RedirectURI:  "",
			},
			expected: false,
		},
		{
			name:     "empty config",
			config:   ZoomConfig{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.IsZoomConfigValid()
			assert.Equal(t, tt.expected, result)
		})
	}
}
