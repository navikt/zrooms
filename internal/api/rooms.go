package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository"
)

// RoomHandler handles HTTP requests for room management
type RoomHandler struct {
	repo repository.Repository
}

// NewRoomHandler creates a new room handler with the given repository
func NewRoomHandler(repo repository.Repository) *RoomHandler {
	return &RoomHandler{
		repo: repo,
	}
}

// ServeHTTP handles HTTP requests for room management
func (h *RoomHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Set common headers
	w.Header().Set("Content-Type", "application/json")

	// Extract room ID from path if present
	// Path format: /api/rooms/{roomID}/meetings/{meetingID} or /api/rooms/{roomID}
	pathParts := strings.Split(r.URL.Path, "/")
	var roomID, meetingID string

	// Extract roomID and meetingID if they exist in the path
	if len(pathParts) >= 4 && pathParts[3] != "" {
		roomID = pathParts[3]
	}
	if len(pathParts) >= 6 && pathParts[5] != "" {
		meetingID = pathParts[5]
	}

	// Route based on HTTP method and path
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/rooms":
		h.createRoom(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/api/rooms":
		h.listRooms(w, r)
	case r.Method == http.MethodGet && roomID != "" && !strings.Contains(r.URL.Path, "/meetings/"):
		h.getRoom(w, r, roomID)
	case r.Method == http.MethodPut && roomID != "" && meetingID != "":
		h.associateMeetingWithRoom(w, r, roomID, meetingID)
	default:
		http.NotFound(w, r)
	}
}

// createRoom handles POST /api/rooms to create a new room
func (h *RoomHandler) createRoom(w http.ResponseWriter, r *http.Request) {
	var room models.Room
	
	// Decode request body into room model
	err := json.NewDecoder(r.Body).Decode(&room)
	if err != nil {
		log.Printf("Error decoding room request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()
	
	// Validate room ID
	if room.ID == "" {
		http.Error(w, "Room ID is required", http.StatusBadRequest)
		return
	}
	
	// Save room to repository
	err = h.repo.SaveRoom(r.Context(), &room)
	if err != nil {
		log.Printf("Error saving room: %v", err)
		http.Error(w, "Error saving room", http.StatusInternalServerError)
		return
	}
	
	// Return created room as JSON
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(room)
}

// listRooms handles GET /api/rooms to list all rooms
func (h *RoomHandler) listRooms(w http.ResponseWriter, r *http.Request) {
	rooms, err := h.repo.ListRooms(r.Context())
	if err != nil {
		log.Printf("Error listing rooms: %v", err)
		http.Error(w, "Error retrieving rooms", http.StatusInternalServerError)
		return
	}
	
	json.NewEncoder(w).Encode(rooms)
}

// getRoom handles GET /api/rooms/{roomID} to get a specific room
func (h *RoomHandler) getRoom(w http.ResponseWriter, r *http.Request, roomID string) {
	room, err := h.repo.GetRoom(r.Context(), roomID)
	if err != nil {
		log.Printf("Error getting room %s: %v", roomID, err)
		http.Error(w, "Room not found", http.StatusNotFound)
		return
	}
	
	json.NewEncoder(w).Encode(room)
}

// associateMeetingWithRoom handles PUT /api/rooms/{roomID}/meetings/{meetingID}
// to associate a meeting with a room
func (h *RoomHandler) associateMeetingWithRoom(w http.ResponseWriter, r *http.Request, roomID, meetingID string) {
	// First check if the room exists
	room, err := h.repo.GetRoom(r.Context(), roomID)
	if err != nil {
		log.Printf("Error getting room %s: %v", roomID, err)
		http.Error(w, "Room not found", http.StatusNotFound)
		return
	}
	
	// Get the meeting if it exists
	meeting, err := h.repo.GetMeeting(r.Context(), meetingID)
	if err != nil {
		log.Printf("Error getting meeting %s: %v", meetingID, err)
		http.Error(w, "Meeting not found", http.StatusNotFound)
		return
	}
	
	// Update the room with the meeting ID
	room.CurrentMeetingID = meetingID
	err = h.repo.SaveRoom(r.Context(), room)
	if err != nil {
		log.Printf("Error updating room: %v", err)
		http.Error(w, "Error updating room", http.StatusInternalServerError)
		return
	}
	
	// Update the meeting with the room ID
	meeting.Room = roomID
	err = h.repo.SaveMeeting(r.Context(), meeting)
	if err != nil {
		log.Printf("Error updating meeting: %v", err)
		http.Error(w, "Error updating meeting", http.StatusInternalServerError)
		return
	}
	
	// Return success message
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Meeting associated with room successfully",
	})
}