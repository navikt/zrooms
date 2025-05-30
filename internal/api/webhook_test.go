package api_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/navikt/zrooms/internal/api"
	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository/memory"
	"github.com/navikt/zrooms/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockMeetingService is a mock implementation of the MeetingServicer interface for testing
type MockMeetingService struct {
	mock.Mock
}

func (m *MockMeetingService) GetMeetingStatusData(ctx context.Context, includeEnded bool) ([]service.MeetingStatusData, error) {
	args := m.Called(ctx, includeEnded)
	return args.Get(0).([]service.MeetingStatusData), args.Error(1)
}

func (m *MockMeetingService) NotifyMeetingStarted(meeting *models.Meeting) {
	m.Called(meeting)
}

func (m *MockMeetingService) NotifyMeetingEnded(meeting *models.Meeting) {
	m.Called(meeting)
}

func (m *MockMeetingService) NotifyParticipantJoined(meetingID string, participantID string) {
	m.Called(meetingID, participantID)
}

func (m *MockMeetingService) NotifyParticipantLeft(meetingID string, participantID string) {
	m.Called(meetingID, participantID)
}

// TestWebhookSignatureValidation tests the webhook signature validation functionality
func TestWebhookSignatureValidation(t *testing.T) {
	// Initialize repository and meeting service
	repo := memory.NewRepository()
	mockService := new(MockMeetingService)

	// Sample test cases for webhook signature validation
	tests := []struct {
		name           string
		webhookPayload string
		secretToken    string
		setupSignature func(req *http.Request, payload string, secretToken string)
		expectSuccess  bool
	}{
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
			handler := api.NewWebhookHandlerWithSecret(repo, mockService, tt.secretToken)

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
	// Initialize meeting service
	meetingService := service.NewMeetingService(repo)
	ctx := context.Background()

	// Sample meeting for "meeting.ended" test
	existingMeeting := &models.Meeting{
		ID:        "123456789",
		Topic:     "Test Meeting",
		Status:    models.MeetingStatusStarted,
		StartTime: time.Now(),
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
						"start_time": "2023-05-08T15:00:00Z",
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
			handler := api.NewWebhookHandler(repo, meetingService)

			// Process the request
			handler.ServeHTTP(rr, req)

			// Check status code
			assert.Equal(t, tt.expectedStatusCode, rr.Code)

			// Run validation function
			tt.validateFunc(t, repo)
		})
	}
}

func TestWebhookURLValidation(t *testing.T) {
	// Initialize repository
	repo := memory.NewRepository()
	// Initialize mock meeting service
	mockService := new(MockMeetingService)

	// Sample validation request from Zoom documentation
	validationPayload := `{
	  "payload": {
	    "plainToken": "qgg8vlvZRS6UYooatFL8Aw"
	  },
	  "event_ts": 1654503849680,
	  "event": "endpoint.url_validation"
	}`

	// Set up the webhook secret token for testing
	secretToken := "webhook_secret_token"
	timestamp := "1739923528"

	// Create a test request
	req := httptest.NewRequest("POST", "/webhook", bytes.NewBufferString(validationPayload))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("x-zm-request-timestamp", timestamp)

	// Create a valid signature for the test payload using the new format (v0:timestamp:body)
	message := fmt.Sprintf("v0:%s:%s", timestamp, validationPayload)
	mac := hmac.New(sha256.New, []byte(secretToken))
	mac.Write([]byte(message))
	computedHash := mac.Sum(nil)
	computedHex := hex.EncodeToString(computedHash)
	req.Header.Set("x-zm-signature", "v0="+computedHex)

	// Create a response recorder
	rr := httptest.NewRecorder()

	// Create the handler with the test secret token
	handler := api.NewWebhookHandlerWithSecret(repo, mockService, secretToken)

	// Process the request
	handler.ServeHTTP(rr, req)

	// Check the response
	assert.Equal(t, http.StatusOK, rr.Code, "Should return 200 OK for URL validation challenge")

	// Verify the response contains the expected validation fields
	var response map[string]string
	err := json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err, "Should return valid JSON")

	// Check that the response contains plainToken and encryptedToken
	assert.Contains(t, response, "plainToken", "Response should contain plainToken")
	assert.Contains(t, response, "encryptedToken", "Response should contain encryptedToken")
	assert.Equal(t, "qgg8vlvZRS6UYooatFL8Aw", response["plainToken"], "plainToken should match the request")

	// Validate that the encryptedToken is correctly calculated
	h := hmac.New(sha256.New, []byte(secretToken))
	h.Write([]byte("qgg8vlvZRS6UYooatFL8Aw"))
	expectedToken := hex.EncodeToString(h.Sum(nil))
	assert.Equal(t, expectedToken, response["encryptedToken"], "encryptedToken should be correctly calculated")
}

// TestWebhookHandlerNotifiesService tests that the webhook handler calls the appropriate service methods
func TestWebhookHandlerNotifiesService(t *testing.T) {
	// Initialize repository
	repo := memory.NewRepository()
	// Initialize mock meeting service
	mockService := new(MockMeetingService)
	ctx := context.Background()

	// Sample meeting
	meetingID := "123456789"
	existingMeeting := &models.Meeting{
		ID:        meetingID,
		Topic:     "Test Meeting",
		Status:    models.MeetingStatusStarted,
		StartTime: time.Now(),
	}
	_ = repo.SaveMeeting(ctx, existingMeeting)

	// Setup mock expectations
	mockService.On("NotifyMeetingStarted", mock.Anything).Return()
	mockService.On("NotifyMeetingEnded", mock.Anything).Return()
	mockService.On("NotifyParticipantJoined", meetingID, "part123").Return()
	mockService.On("NotifyParticipantLeft", meetingID, "part123").Return()

	// Test cases for notifications
	tests := []struct {
		name           string
		webhookPayload string
		verify         func(t *testing.T, mockService *MockMeetingService)
	}{
		{
			name: "Meeting Started Event Notification",
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
						"start_time": "2025-05-08T15:00:00Z"
					}
				}
			}`,
			verify: func(t *testing.T, mockService *MockMeetingService) {
				mockService.AssertCalled(t, "NotifyMeetingStarted", mock.Anything)
			},
		},
		{
			name: "Meeting Ended Event Notification",
			webhookPayload: `{
				"event": "meeting.ended",
				"payload": {
					"account_id": "abc123",
					"object": {
						"uuid": "uuid123",
						"id": "123456789",
						"host_id": "host123",
						"topic": "Test Meeting"
					}
				}
			}`,
			verify: func(t *testing.T, mockService *MockMeetingService) {
				mockService.AssertCalled(t, "NotifyMeetingEnded", mock.Anything)
			},
		},
		{
			name: "Participant Joined Event Notification",
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
							"user_name": "Test User"
						}
					}
				}
			}`,
			verify: func(t *testing.T, mockService *MockMeetingService) {
				mockService.AssertCalled(t, "NotifyParticipantJoined", meetingID, "part123")
			},
		},
		{
			name: "Participant Left Event Notification",
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
							"user_name": "Test User"
						}
					}
				}
			}`,
			verify: func(t *testing.T, mockService *MockMeetingService) {
				mockService.AssertCalled(t, "NotifyParticipantLeft", meetingID, "part123")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a request with the webhook payload
			req := httptest.NewRequest("POST", "/webhook", bytes.NewBufferString(tt.webhookPayload))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Zoom-Signature", "mock_signature") // Skip validation for this test

			// Create a response recorder
			rr := httptest.NewRecorder()

			// Create the handler with the mock service
			handler := api.NewWebhookHandler(repo, mockService)

			// Process the request
			handler.ServeHTTP(rr, req)

			// Verify service was called correctly
			tt.verify(t, mockService)
		})
	}
}
