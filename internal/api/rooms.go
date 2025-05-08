package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository"
)

// MeetingHandler handles HTTP requests for meeting management
type MeetingHandler struct {
	repo repository.Repository
}

// NewMeetingHandler creates a new meeting handler with the given repository
func NewMeetingHandler(repo repository.Repository) *MeetingHandler {
	return &MeetingHandler{
		repo: repo,
	}
}

// ServeHTTP handles HTTP requests for meeting management
func (h *MeetingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Set common headers
	w.Header().Set("Content-Type", "application/json")

	// Extract meeting ID from path if present
	// Path format: /api/meetings/{meetingID}
	pathParts := strings.Split(r.URL.Path, "/")
	var meetingID string

	// Extract meetingID if it exists in the path
	if len(pathParts) >= 4 && pathParts[3] != "" {
		meetingID = pathParts[3]
	}

	// Route based on HTTP method and path
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/meetings":
		h.listMeetings(w, r)
	case r.Method == http.MethodGet && meetingID != "":
		h.getMeeting(w, r, meetingID)
	case r.Method == http.MethodPost && r.URL.Path == "/api/meetings":
		h.createMeeting(w, r)
	case r.Method == http.MethodDelete && meetingID != "":
		h.deleteMeeting(w, r, meetingID)
	default:
		http.NotFound(w, r)
	}
}

// createMeeting handles POST /api/meetings to create a new meeting
func (h *MeetingHandler) createMeeting(w http.ResponseWriter, r *http.Request) {
	var meeting models.Meeting

	// Decode request body into meeting model
	err := json.NewDecoder(r.Body).Decode(&meeting)
	if err != nil {
		log.Printf("Error decoding meeting request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Validate meeting ID
	if meeting.ID == "" {
		http.Error(w, "Meeting ID is required", http.StatusBadRequest)
		return
	}

	// Save meeting to repository
	err = h.repo.SaveMeeting(r.Context(), &meeting)
	if err != nil {
		log.Printf("Error saving meeting: %v", err)
		http.Error(w, "Error saving meeting", http.StatusInternalServerError)
		return
	}

	// Return created meeting as JSON
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(meeting)
}

// listMeetings handles GET /api/meetings to list all active meetings
func (h *MeetingHandler) listMeetings(w http.ResponseWriter, r *http.Request) {
	meetings, err := h.repo.ListMeetings(r.Context())
	if err != nil {
		log.Printf("Error listing meetings: %v", err)
		http.Error(w, "Error retrieving meetings", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(meetings)
}

// getMeeting handles GET /api/meetings/{meetingID} to get a specific meeting
func (h *MeetingHandler) getMeeting(w http.ResponseWriter, r *http.Request, meetingID string) {
	meeting, err := h.repo.GetMeeting(r.Context(), meetingID)
	if err != nil {
		log.Printf("Error getting meeting %s: %v", meetingID, err)
		http.Error(w, "Meeting not found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(meeting)
}

// deleteMeeting handles DELETE /api/meetings/{meetingID} to delete a meeting
func (h *MeetingHandler) deleteMeeting(w http.ResponseWriter, r *http.Request, meetingID string) {
	// Check if the meeting exists first
	_, err := h.repo.GetMeeting(r.Context(), meetingID)
	if err != nil {
		log.Printf("Error getting meeting %s: %v", meetingID, err)
		http.Error(w, "Meeting not found", http.StatusNotFound)
		return
	}

	// Delete the meeting
	err = h.repo.DeleteMeeting(r.Context(), meetingID)
	if err != nil {
		log.Printf("Error deleting meeting: %v", err)
		http.Error(w, "Error deleting meeting", http.StatusInternalServerError)
		return
	}

	// Return success message
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Meeting deleted successfully",
	})
}
