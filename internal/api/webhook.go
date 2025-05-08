package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
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
	repo        repository.Repository
	secretToken string
}

// NewWebhookHandler creates a new webhook handler with the given repository
func NewWebhookHandler(repo repository.Repository) *WebhookHandler {
	zoomConfig := config.GetZoomConfig()
	return &WebhookHandler{
		repo:        repo,
		secretToken: zoomConfig.WebhookSecretToken,
	}
}

// NewWebhookHandlerWithSecret creates a webhook handler with the given repository and secret token
// This method is primarily used for testing webhook signature validation
func NewWebhookHandlerWithSecret(repo repository.Repository, secretToken string) *WebhookHandler {
	return &WebhookHandler{
		repo:        repo,
		secretToken: secretToken,
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
// by checking the x-zm-signature header against the message's HMAC using our secret token
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

	// Calculate the expected signature
	h256 := hmac.New(sha256.New, []byte(h.secretToken))
	h256.Write(body)
	expectedSignature := hex.EncodeToString(h256.Sum(nil))

	// Direct comparison with hex encoded signature
	if hmac.Equal([]byte(expectedSignature), []byte(receivedSignature)) {
		return true
	}

	// Try base64 decode in case Zoom sent a base64 encoded signature
	decodedSignature, err := base64.StdEncoding.DecodeString(receivedSignature)
	if err == nil {
		// Compare with the raw binary hash
		return hmac.Equal(h256.Sum(nil), decodedSignature)
	}

	// No match found
	log.Printf("Signature validation failed")
	return false
}

// handleMeetingStarted processes a meeting.started event
func (h *WebhookHandler) handleMeetingStarted(ctx context.Context, event *models.WebhookEvent) {
	meeting := event.ProcessMeetingStarted()
	if meeting == nil {
		log.Printf("Failed to process meeting.started event")
		return
	}

	log.Printf("Meeting started: ID=%s, Topic=%s", meeting.ID, meeting.Topic)

	// Explicitly ensure the topic is set (fix for failing test)
	if meeting.Topic == "" && event.Payload.Object.Topic != "" {
		meeting.Topic = event.Payload.Object.Topic
	}

	// Assign to a room - in a real implementation, this would be more sophisticated
	// For now, we'll just use a default room if none is assigned
	if meeting.Room == "" {
		// Get an available room or use a default
		rooms, err := h.repo.ListRooms(ctx)
		if err == nil && len(rooms) > 0 {
			meeting.Room = rooms[0].ID
		} else {
			meeting.Room = "default-room"
		}
	}

	if err := h.repo.SaveMeeting(ctx, meeting); err != nil {
		log.Printf("Error saving meeting: %v", err)
	}
}

// handleMeetingEnded processes a meeting.ended event
func (h *WebhookHandler) handleMeetingEnded(ctx context.Context, event *models.WebhookEvent) {
	meeting := event.ProcessMeetingEnded()
	if meeting == nil {
		log.Printf("Failed to process meeting.ended event")
		return
	}

	// Get existing meeting to preserve room and other details
	existingMeeting, err := h.repo.GetMeeting(ctx, meeting.ID)
	if err == nil {
		// Keep important fields from existing meeting
		meeting.Room = existingMeeting.Room
		meeting.Topic = existingMeeting.Topic
		if meeting.Topic == "" {
			meeting.Topic = event.Payload.Object.Topic
		}
	}

	log.Printf("Meeting ended: ID=%s", meeting.ID)
	if err := h.repo.SaveMeeting(ctx, meeting); err != nil {
		log.Printf("Error updating meeting: %v", err)
	}
}

// handleParticipantJoined processes a meeting.participant_joined event
func (h *WebhookHandler) handleParticipantJoined(ctx context.Context, event *models.WebhookEvent) {
	participant := event.ProcessParticipantJoined()
	if participant == nil {
		log.Printf("Failed to process participant_joined event")
		return
	}

	meetingID := event.Payload.Object.ID
	participantID := participant.ID

	// Only store the participant ID to avoid storing PII
	log.Printf("Participant joined: MeetingID=%s, ParticipantID=%s", meetingID, participantID)
	if err := h.repo.AddParticipantToMeeting(ctx, meetingID, participantID); err != nil {
		log.Printf("Error adding participant: %v", err)
	}
}

// handleParticipantLeft processes a meeting.participant_left event
func (h *WebhookHandler) handleParticipantLeft(ctx context.Context, event *models.WebhookEvent) {
	participant := event.ProcessParticipantLeft()
	if participant == nil {
		log.Printf("Failed to process participant_left event")
		return
	}

	meetingID := event.Payload.Object.ID
	participantID := participant.ID

	log.Printf("Participant left: MeetingID=%s, ParticipantID=%s", meetingID, participantID)
	if err := h.repo.RemoveParticipantFromMeeting(ctx, meetingID, participantID); err != nil {
		log.Printf("Error removing participant: %v", err)
	}
}
