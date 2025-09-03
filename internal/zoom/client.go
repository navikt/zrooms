package zoom

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/navikt/zrooms/internal/config"
)

// APIClient handles interactions with the Zoom API
type APIClient struct {
	accessToken string
	baseURL     string
	httpClient  *http.Client
}

// NewAPIClient creates a new Zoom API client
func NewAPIClient(accessToken string) *APIClient {
	return &APIClient{
		accessToken: accessToken,
		baseURL:     "https://api.zoom.us/v2",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GetMeetingRawData fetches raw meeting details from Zoom API and returns the JSON bytes
func (c *APIClient) GetMeetingRawData(meetingID string) ([]byte, error) {
	url := fmt.Sprintf("%s/meetings/%s", c.baseURL, meetingID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("zoom API error (status %d): %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// TokenResponse represents the response from Zoom OAuth token endpoint
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
}

// APIManager handles Zoom API access token management using OAuth client credentials
type APIManager struct {
	config      config.ZoomConfig
	accessToken string
	tokenExpiry time.Time
}

// NewAPIManager creates a new Zoom API manager
func NewAPIManager() *APIManager {
	return &APIManager{
		config: config.GetZoomConfig(),
	}
}

// GetClient returns a configured Zoom API client with a valid access token
func (m *APIManager) GetClient() (*APIClient, error) {
	if m.accessToken == "" || time.Now().After(m.tokenExpiry) {
		if err := m.refreshAccessToken(); err != nil {
			return nil, fmt.Errorf("failed to get access token: %w", err)
		}
	}

	return NewAPIClient(m.accessToken), nil
}

// refreshAccessToken gets a new access token using OAuth client credentials flow
func (m *APIManager) refreshAccessToken() error {
	if m.config.ClientID == "" || m.config.ClientSecret == "" {
		return fmt.Errorf("zoom client ID and secret must be configured")
	}

	// Prepare the request data for OAuth client credentials flow
	data := url.Values{}
	data.Set("grant_type", "client_credentials")

	req, err := http.NewRequest("POST", "https://zoom.us/oauth/token", strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create token request: %w", err)
	}

	// Set basic auth with client credentials
	req.SetBasicAuth(m.config.ClientID, m.config.ClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("failed to parse token response: %w", err)
	}

	m.accessToken = tokenResp.AccessToken
	// Set expiry to a bit before the actual expiry to avoid race conditions
	m.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second)

	return nil
}
