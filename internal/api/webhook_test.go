package api_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/navikt/zrooms/internal/api"
	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository/memory"
	"github.com/stretchr/testify/assert"
)

// TestWebhookSignatureValidation tests the webhook signature validation functionality
func TestWebhookSignatureValidation(t *testing.T) {
	// Initialize repository
	repo := memory.NewRepository()

	// Sample test cases for webhook signature validation
	tests := []struct {
		name           string
		webhookPayload string
		secretToken    string
		setupSignature func(req *http.Request, payload string, secretToken string)
		expectSuccess  bool
	}{
		{
			name:           "Valid Signature - Hex Encoded",
			webhookPayload: `{"event": "meeting.started", "payload": {"account_id": "abc123", "object": {"id": "123"}}}`,
			secretToken:    "test_secret_token",
			setupSignature: func(req *http.Request, payload string, secretToken string) {
				// Create a valid signature using hex encoding
				h256 := hmac.New(sha256.New, []byte(secretToken))
				h256.Write([]byte(payload))
				signature := hex.EncodeToString(h256.Sum(nil))
				req.Header.Set("x-zm-signature", "v0="+signature)
			},
			expectSuccess: true,
		},
		{
			name:           "Valid Signature - Base64 Encoded (Zoom Docs Format)",
			webhookPayload: `{"event": "meeting.started", "payload": {"account_id": "abc123", "object": {"id": "123"}}}`,
			secretToken:    "test_secret_token",
			setupSignature: func(req *http.Request, payload string, secretToken string) {
				// Create a valid signature using base64 encoding (as per Zoom docs)
				h256 := hmac.New(sha256.New, []byte(secretToken))
				h256.Write([]byte(payload))
				signature := base64.StdEncoding.EncodeToString(h256.Sum(nil))
				req.Header.Set("x-zm-signature", "v0="+signature)
			},
			expectSuccess: true,
		},
		{
			name:           "Valid Signature - Raw Hash Binary",
			webhookPayload: `{"event": "meeting.started", "payload": {"account_id": "abc123", "object": {"id": "123"}}}`,
			secretToken:    "test_secret_token",
			setupSignature: func(req *http.Request, payload string, secretToken string) {
				// Create a valid signature using the raw hash
				h256 := hmac.New(sha256.New, []byte(secretToken))
				h256.Write([]byte(payload))
				// Use base64 to safely transport binary data in header
				signature := base64.StdEncoding.EncodeToString(h256.Sum(nil))
				req.Header.Set("x-zm-signature", "v0="+signature)
			},
			expectSuccess: true,
		},
		{
			name:           "Invalid Signature",
			webhookPayload: `{"event": "meeting.started", "payload": {"account_id": "abc123", "object": {"id": "123"}}}`,
			secretToken:    "test_secret_token",
			setupSignature: func(req *http.Request, payload string, secretToken string) {
				// Create an invalid signature
				req.Header.Set("x-zm-signature", "v0=invalidsignature")
			},
			expectSuccess: false,
		},
		{
			name:           "Missing Signature Header",
			webhookPayload: `{"event": "meeting.started", "payload": {"account_id": "abc123", "object": {"id": "123"}}}`,
			secretToken:    "test_secret_token",
			setupSignature: func(req *http.Request, payload string, secretToken string) {
				// Don't set any signature header
			},
			expectSuccess: false,
		},
		{
			name:           "Invalid Signature Format",
			webhookPayload: `{"event": "meeting.started", "payload": {"account_id": "abc123", "object": {"id": "123"}}}`,
			secretToken:    "test_secret_token",
			setupSignature: func(req *http.Request, payload string, secretToken string) {
				// Wrong format (missing v0=)
				h256 := hmac.New(sha256.New, []byte(secretToken))
				h256.Write([]byte(payload))
				signature := hex.EncodeToString(h256.Sum(nil))
				req.Header.Set("x-zm-signature", signature) // Missing "v0=" prefix
			},
			expectSuccess: false,
		},
		{
			name:           "Tampered Payload",
			webhookPayload: `{"event": "meeting.started", "payload": {"account_id": "TAMPERED", "object": {"id": "123"}}}`,
			secretToken:    "test_secret_token",
			setupSignature: func(req *http.Request, payload string, secretToken string) {
				// Sign a different payload than what's actually sent
				originalPayload := `{"event": "meeting.started", "payload": {"account_id": "abc123", "object": {"id": "123"}}}`
				h256 := hmac.New(sha256.New, []byte(secretToken))
				h256.Write([]byte(originalPayload))
				signature := hex.EncodeToString(h256.Sum(nil))
				req.Header.Set("x-zm-signature", "v0="+signature)
			},
			expectSuccess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test WebhookHandler with the test secret token
			handler := api.NewWebhookHandlerWithSecret(repo, tt.secretToken)

			// Create a request with the webhook payload
			req := httptest.NewRequest("POST", "/webhook", bytes.NewBufferString(tt.webhookPayload))
			req.Header.Set("Content-Type", "application/json")

			// Setup signature according to test case
			tt.setupSignature(req, tt.webhookPayload, tt.secretToken)

			// Create a response recorder
			rr := httptest.NewRecorder()

			// Process the request
			handler.ServeHTTP(rr, req)

			if tt.expectSuccess {
				assert.Equal(t, http.StatusOK, rr.Code, "Expected successful validation")
				// Additional check to make sure it's really accepting the event
				assert.Contains(t, rr.Body.String(), `"success": true`)
			} else {
				assert.Equal(t, http.StatusUnauthorized, rr.Code, "Expected unauthorized status for invalid signature")
			}
		})
	}
}

func TestWebhookHandler(t *testing.T) {
	// Initialize repository
	repo := memory.NewRepository()
	ctx := context.Background()

	// Pre-populate with some data for "meeting.started" and "participant_joined" tests
	room := &models.Room{
		ID:       "room123",
		Name:     "Test Room",
		Capacity: 10,
	}
	_ = repo.SaveRoom(ctx, room)

	// Sample meeting for "meeting.ended" test
	existingMeeting := &models.Meeting{
		ID:        "123456789",
		Topic:     "Test Meeting",
		Status:    models.MeetingStatusStarted,
		StartTime: time.Now(),
		Room:      "room123",
	}
	_ = repo.SaveMeeting(ctx, existingMeeting)

	// Sample test cases for different webhook event types
	tests := []struct {
		name               string
		webhookPayload     string
		expectedStatusCode int
		validateFunc       func(t *testing.T, repo *memory.Repository)
	}{
		{
			name: "Meeting Started Event",
			webhookPayload: `{
				"event": "meeting.started",
				"payload": {
					"account_id": "abc123",
					"object": {
						"uuid": "uuid123",
						"id": "987654321",
						"host_id": "host123",
						"topic": "Test Meeting",
						"type": 2,
						"start_time": "2025-05-08T15:00:00Z",
						"duration": 60,
						"timezone": "UTC"
					}
				},
				"event_ts": 1620123456789
			}`,
			expectedStatusCode: http.StatusOK,
			validateFunc: func(t *testing.T, repo *memory.Repository) {
				// Verify meeting was saved with started status
				meeting, err := repo.GetMeeting(ctx, "987654321")
				assert.NoError(t, err)
				assert.Equal(t, models.MeetingStatusStarted, meeting.Status)
				assert.Equal(t, "Test Meeting", meeting.Topic)
			},
		},
		{
			name: "Meeting Ended Event",
			webhookPayload: `{
				"event": "meeting.ended",
				"payload": {
					"account_id": "abc123",
					"object": {
						"uuid": "uuid123",
						"id": "123456789",
						"host_id": "host123",
						"topic": "Test Meeting",
						"type": 2
					}
				},
				"event_ts": 1620123456789
			}`,
			expectedStatusCode: http.StatusOK,
			validateFunc: func(t *testing.T, repo *memory.Repository) {
				// Verify meeting was updated with ended status
				meeting, err := repo.GetMeeting(ctx, "123456789")
				assert.NoError(t, err)
				assert.Equal(t, models.MeetingStatusEnded, meeting.Status)
			},
		},
		{
			name: "Participant Joined Event",
			webhookPayload: `{
				"event": "meeting.participant_joined",
				"payload": {
					"account_id": "abc123",
					"object": {
						"uuid": "uuid123",
						"id": "123456789",
						"host_id": "host123",
						"participant": {
							"id": "part123",
							"user_id": "user123",
							"user_name": "Test User",
							"email": "user@example.com"
						}
					}
				},
				"event_ts": 1620123456789
			}`,
			expectedStatusCode: http.StatusOK,
			validateFunc: func(t *testing.T, repo *memory.Repository) {
				// Verify participant count increased
				count, err := repo.CountParticipantsInMeeting(ctx, "123456789")
				assert.NoError(t, err)
				assert.GreaterOrEqual(t, count, 1)
			},
		},
		{
			name: "Participant Left Event",
			webhookPayload: `{
				"event": "meeting.participant_left",
				"payload": {
					"account_id": "abc123",
					"object": {
						"uuid": "uuid123",
						"id": "123456789",
						"host_id": "host123",
						"participant": {
							"id": "part123",
							"user_id": "user123",
							"user_name": "Test User",
							"email": "user@example.com"
						}
					}
				},
				"event_ts": 1620123456789
			}`,
			expectedStatusCode: http.StatusOK,
			validateFunc: func(t *testing.T, repo *memory.Repository) {
				// No specific validation needed for participant left event
			},
		},
		{
			name:               "Invalid JSON Payload",
			webhookPayload:     `{"invalid json}`,
			expectedStatusCode: http.StatusBadRequest,
			validateFunc:       func(t *testing.T, repo *memory.Repository) {},
		},
		{
			name:               "Unsupported Event Type",
			webhookPayload:     `{"event": "unsupported.event", "payload": {}}`,
			expectedStatusCode: http.StatusOK, // We still return OK even for unsupported events
			validateFunc:       func(t *testing.T, repo *memory.Repository) {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a request with the webhook payload
			req := httptest.NewRequest("POST", "/webhook", bytes.NewBufferString(tt.webhookPayload))
			req.Header.Set("Content-Type", "application/json")

			// Add a signature header for validation in a real scenario
			// For testing we'll skip actual signature validation
			req.Header.Set("X-Zoom-Signature", "mock_signature")

			// Create a response recorder
			rr := httptest.NewRecorder()

			// Create the handler
			handler := api.NewWebhookHandler(repo)

			// Process the request
			handler.ServeHTTP(rr, req)

			// Check status code
			assert.Equal(t, tt.expectedStatusCode, rr.Code)

			// Run validation function
			tt.validateFunc(t, repo)
		})
	}
}
