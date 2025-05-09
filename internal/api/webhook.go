package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/navikt/zrooms/internal/config"
	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository"
)

// WebhookHandler processes webhook events from Zoom
type WebhookHandler struct {
	repo           repository.Repository
	meetingService MeetingServicer
	secretToken    string
}

// NewWebhookHandler creates a new webhook handler with the given repository and meeting service
func NewWebhookHandler(repo repository.Repository, meetingService MeetingServicer) *WebhookHandler {
	zoomConfig := config.GetZoomConfig()
	return &WebhookHandler{
		repo:           repo,
		meetingService: meetingService,
		secretToken:    zoomConfig.WebhookSecretToken,
	}
}

// NewWebhookHandlerWithSecret creates a webhook handler with the given repository and secret token
// This method is primarily used for testing webhook signature validation
func NewWebhookHandlerWithSecret(repo repository.Repository, meetingService MeetingServicer, secretToken string) *WebhookHandler {
	return &WebhookHandler{
		repo:           repo,
		meetingService: meetingService,
		secretToken:    secretToken,
	}
}

// ServeHTTP handles HTTP requests for the webhook endpoint
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only allow POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify webhook signature if secret token is configured
	if h.secretToken != "" {
		if !h.verifyZoomWebhookSignature(r) {
			log.Printf("Invalid webhook signature")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	} else {
		log.Printf("Warning: Webhook verification disabled - ZOOM_WEBHOOK_SECRET_TOKEN not set")
	}

	// Limit request body size to prevent abuse
	body, err := io.ReadAll(io.LimitReader(r.Body, 1048576)) // 1MB limit
	if err != nil {
		log.Printf("Error reading webhook body: %v", err)
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Parse the webhook event
	var event models.WebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		log.Printf("Error parsing webhook JSON: %v", err)
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	// Create a context with timeout for database operations
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Handle Zoom URL validation challenge response
	if event.Event == "endpoint.url_validation" {
		log.Printf("Received Zoom URL validation challenge")

		// Parse the payload to extract the plainToken
		var validationPayload struct {
			PlainToken string `json:"plainToken"`
		}

		// Unmarshal the raw payload into our validation struct
		if err := json.Unmarshal(event.Payload, &validationPayload); err != nil {
			log.Printf("Error parsing validation payload: %v", err)
			http.Error(w, "Invalid validation request", http.StatusBadRequest)
			return
		}

		if validationPayload.PlainToken == "" {
			log.Printf("Error: Missing plainToken in validation request")
			http.Error(w, "Invalid validation request", http.StatusBadRequest)
			return
		}

		// Generate the hash response using HMAC SHA-256
		hash := hmac.New(sha256.New, []byte(h.secretToken))
		hash.Write([]byte(validationPayload.PlainToken))
		encryptedToken := hex.EncodeToString(hash.Sum(nil))

		// Return the validation response as required by Zoom
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		// Use json.Marshal instead of json.NewEncoder to avoid unwanted newlines
		responseData, err := json.Marshal(map[string]string{
			"plainToken":     validationPayload.PlainToken,
			"encryptedToken": encryptedToken,
		})
		if err != nil {
			log.Printf("Error marshaling validation response: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Write the response directly
		w.Write(responseData)

		log.Printf("Successfully responded to Zoom URL validation challenge")
		return
	}

	// Process the event based on its type
	switch event.Event {
	case "meeting.started":
		h.handleMeetingStarted(ctx, &event)
	case "meeting.ended":
		h.handleMeetingEnded(ctx, &event)
	case "meeting.participant_joined":
		h.handleParticipantJoined(ctx, &event)
	case "meeting.participant_left":
		h.handleParticipantLeft(ctx, &event)
	default:
		// Log unsupported event type but return OK
		log.Printf("Unsupported webhook event type: %s", event.Event)
	}

	// Always return success to Zoom
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"success": true}`)
}

// verifyZoomWebhookSignature validates that the request is actually from Zoom
// using the approach specified in Zoom's webhook documentation.
// It verifies the x-zm-signature header against an HMAC-SHA256 hash of the timestamp and request body
// using the configured webhook secret token.
func (h *WebhookHandler) verifyZoomWebhookSignature(r *http.Request) bool {
	// Get the signature from the header
	signatureHeader := r.Header.Get("x-zm-signature")
	if signatureHeader == "" {
		log.Printf("Missing x-zm-signature header")
		return false
	}

	// Parse the signature format (should be v0=HASH)
	parts := strings.SplitN(signatureHeader, "=", 2)
	if len(parts) != 2 || parts[0] != "v0" {
		log.Printf("Invalid signature format: %s", signatureHeader)
		return false
	}
	receivedSignature := parts[1]

	// Get the timestamp from the header
	timestamp := r.Header.Get("x-zm-request-timestamp")
	if timestamp == "" {
		log.Printf("Missing x-zm-request-timestamp header")
		return false
	}

	// Read the request body for verification
	var body []byte
	var err error
	if r.Body != nil {
		// Create a new buffer to store the body content
		body, err = io.ReadAll(r.Body)
		if err != nil {
			log.Printf("Error reading request body for signature verification: %v", err)
			return false
		}

		// Restore the body so it can be read again
		r.Body = io.NopCloser(strings.NewReader(string(body)))
	}

	// Construct the message string: v0:timestamp:body
	message := fmt.Sprintf("v0:%s:%s", timestamp, string(body))

	// Calculate the expected signature using HMAC-SHA256
	mac := hmac.New(sha256.New, []byte(h.secretToken))
	mac.Write([]byte(message))
	computedHash := mac.Sum(nil)
	computedHex := hex.EncodeToString(computedHash)

	// Compare the computed signature with the received signature
	expectedSignature := computedHex

	// Direct comparison of hex-encoded signatures
	return hmac.Equal([]byte(expectedSignature), []byte(receivedSignature))
}

// handleMeetingStarted processes a meeting.started event
func (h *WebhookHandler) handleMeetingStarted(ctx context.Context, event *models.WebhookEvent) {
	meeting := event.ProcessMeetingStarted()
	if meeting == nil {
		log.Printf("Failed to process meeting.started event")
		return
	}

	log.Printf("Meeting started: ID=%s, Topic=%s", meeting.ID, meeting.Topic)

	// Parse the standard event payload to access object properties
	var payload models.StandardEventPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		log.Printf("Error parsing payload for meeting started event: %v", err)
		return
	}

	// Explicitly ensure the topic is set (fix for failing test)
	if meeting.Topic == "" && payload.Object.Topic != "" {
		meeting.Topic = payload.Object.Topic
	}

	if err := h.repo.SaveMeeting(ctx, meeting); err != nil {
		log.Printf("Error saving meeting: %v", err)
	}

	// Notify meeting service about the started meeting
	if h.meetingService != nil {
		h.meetingService.NotifyMeetingStarted(meeting)
	}
}

// handleMeetingEnded processes a meeting.ended event
func (h *WebhookHandler) handleMeetingEnded(ctx context.Context, event *models.WebhookEvent) {
	meeting := event.ProcessMeetingEnded()
	if meeting == nil {
		log.Printf("Failed to process meeting.ended event")
		return
	}

	// Parse the standard event payload to access object properties
	var payload models.StandardEventPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		log.Printf("Error parsing payload for meeting ended event: %v", err)
		return
	}

	// Get existing meeting to preserve important details
	existingMeeting, err := h.repo.GetMeeting(ctx, meeting.ID)
	if err == nil {
		// Keep topic from existing meeting if it's not set in the new one
		if meeting.Topic == "" {
			if existingMeeting.Topic != "" {
				meeting.Topic = existingMeeting.Topic
			} else if payload.Object.Topic != "" {
				meeting.Topic = payload.Object.Topic
			}
		}
	}

	log.Printf("Meeting ended: ID=%s", meeting.ID)
	if err := h.repo.SaveMeeting(ctx, meeting); err != nil {
		log.Printf("Error updating meeting: %v", err)
	}

	// Notify meeting service about the ended meeting
	if h.meetingService != nil {
		h.meetingService.NotifyMeetingEnded(meeting)
	}
}

// handleParticipantJoined processes a meeting.participant_joined event
func (h *WebhookHandler) handleParticipantJoined(ctx context.Context, event *models.WebhookEvent) {
	participant := event.ProcessParticipantJoined()
	if participant == nil {
		log.Printf("Failed to process participant_joined event")
		return
	}

	// Parse the standard event payload to access object properties
	var payload models.StandardEventPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		log.Printf("Error parsing payload for participant joined event: %v", err)
		return
	}

	meetingID := payload.Object.ID
	participantID := participant.ID

	// Only store the participant ID to avoid storing PII
	log.Printf("Participant joined: MeetingID=%s, ParticipantID=%s", meetingID, participantID)
	if err := h.repo.AddParticipantToMeeting(ctx, meetingID, participantID); err != nil {
		log.Printf("Error adding participant: %v", err)
	}

	// Notify meeting service about the participant joined
	if h.meetingService != nil {
		h.meetingService.NotifyParticipantJoined(meetingID, participantID)
	}
}

// handleParticipantLeft processes a meeting.participant_left event
func (h *WebhookHandler) handleParticipantLeft(ctx context.Context, event *models.WebhookEvent) {
	participant := event.ProcessParticipantLeft()
	if participant == nil {
		log.Printf("Failed to process participant_left event")
		return
	}

	// Parse the standard event payload to access object properties
	var payload models.StandardEventPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		log.Printf("Error parsing payload for participant left event: %v", err)
		return
	}

	meetingID := payload.Object.ID
	participantID := participant.ID

	log.Printf("Participant left: MeetingID=%s, ParticipantID=%s", meetingID, participantID)
	if err := h.repo.RemoveParticipantFromMeeting(ctx, meetingID, participantID); err != nil {
		log.Printf("Error removing participant: %v", err)
	}

	// Notify meeting service about the participant left
	if h.meetingService != nil {
		h.meetingService.NotifyParticipantLeft(meetingID, participantID)
	}
}
