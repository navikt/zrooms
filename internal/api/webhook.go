package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository"
)

// WebhookHandler processes webhook events from Zoom
type WebhookHandler struct {
	repo repository.Repository
}

// NewWebhookHandler creates a new webhook handler with the given repository
func NewWebhookHandler(repo repository.Repository) *WebhookHandler {
	return &WebhookHandler{
		repo: repo,
	}
}

// ServeHTTP handles HTTP requests for the webhook endpoint
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only allow POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
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
