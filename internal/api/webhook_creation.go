// Package api provides webhook creation functionality
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/navikt/zrooms/internal/config"
)

// WebhookCreateRequest represents the request to create a webhook
type WebhookCreateRequest struct {
	URL    string   `json:"url"`
	Auth   string   `json:"auth_username,omitempty"`
	Events []string `json:"events"`
}

// WebhookCreateResponse represents the response from creating a webhook
type WebhookCreateResponse struct {
	WebhookID string   `json:"webhook_id"`
	URL       string   `json:"url"`
	Events    []string `json:"events"`
	Status    string   `json:"status"`
}

// ZoomWebhookCreator handles creation of webhooks via Zoom API
type ZoomWebhookCreator struct {
	baseURL string
}

// NewZoomWebhookCreator creates a new webhook creator
func NewZoomWebhookCreator() *ZoomWebhookCreator {
	return &ZoomWebhookCreator{
		baseURL: "https://api.zoom.us/v2",
	}
}

// CreateWebhookForUser creates a webhook for a specific user using their OAuth token
func (zwc *ZoomWebhookCreator) CreateWebhookForUser(accessToken string) (*WebhookCreateResponse, error) {
	zoomConfig := config.GetZoomConfig()

	// Define the events we want to subscribe to
	events := []string{
		"meeting.started",
		"meeting.ended",
		"meeting.participant_joined",
		"meeting.participant_left",
	}

	webhookRequest := WebhookCreateRequest{
		URL:    zoomConfig.WebhookURL,
		Events: events,
	}

	requestBody, err := json.Marshal(webhookRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal webhook request: %w", err)
	}

	req, err := http.NewRequest("POST", zwc.baseURL+"/webhooks", bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("webhook creation failed with status: %d", resp.StatusCode)
	}

	var webhookResponse WebhookCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&webhookResponse); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &webhookResponse, nil
}
