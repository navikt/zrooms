// Package memory provides an in-memory implementation of the repository interface
package memory

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository"
)

// Ensure Repository implements the repository.Repository interface
var _ repository.Repository = (*Repository)(nil)

// ErrNotFound is returned when a requested entity is not found
var ErrNotFound = errors.New("entity not found")

// MeetingState contains minimal information about a meeting's state
type MeetingState struct {
	ID             string // Meeting ID
	Topic          string // Meeting Topic
	RoomID         string // Room ID
	Status         models.MeetingStatus
	StartTime      time.Time
	EndTime        time.Time
	ParticipantIDs map[string]struct{} // Store only participant IDs
}

// Repository implements the repository.Repository interface with in-memory storage
type Repository struct {
	meetingStates map[string]*MeetingState // Stores minimal meeting state data
	rooms         map[string]*models.Room  // Room information
	mu            sync.RWMutex
}

// NewRepository creates a new in-memory repository
func NewRepository() *Repository {
	return &Repository{
		meetingStates: make(map[string]*MeetingState),
		rooms:         make(map[string]*models.Room),
	}
}

// SaveMeeting saves minimal meeting state information to the repository
func (r *Repository) SaveMeeting(ctx context.Context, meeting *models.Meeting) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if the meeting state already exists
	state, exists := r.meetingStates[meeting.ID]
	if !exists {
		// Create a new meeting state with minimal data
		state = &MeetingState{
			ID:             meeting.ID,
			Topic:          meeting.Topic,
			RoomID:         meeting.Room,
			Status:         meeting.Status,
			StartTime:      meeting.StartTime,
			ParticipantIDs: make(map[string]struct{}),
		}
		r.meetingStates[meeting.ID] = state
	} else {
		// Update existing meeting state
		state.Status = meeting.Status
		state.RoomID = meeting.Room

		// Only update topic if it's provided and not empty
		if meeting.Topic != "" {
			state.Topic = meeting.Topic
		}

		// Set end time if the meeting has ended
		if meeting.Status == models.MeetingStatusEnded {
			state.EndTime = meeting.EndTime
		}
	}

	// Update room status if room is specified
	if meeting.Room != "" {
		room, exists := r.rooms[meeting.Room]
		if exists {
			if meeting.Status == models.MeetingStatusStarted {
				room.CurrentMeetingID = meeting.ID
			} else if meeting.Status == models.MeetingStatusEnded {
				room.CurrentMeetingID = ""
			}
			r.rooms[meeting.Room] = room
		}
	}

	return nil
}

// GetMeeting retrieves a meeting by ID
func (r *Repository) GetMeeting(ctx context.Context, id string) (*models.Meeting, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	state, ok := r.meetingStates[id]
	if !ok {
		return nil, ErrNotFound
	}

	// Convert minimal state back to a Meeting model with only the necessary data
	meeting := &models.Meeting{
		ID:           state.ID,
		Topic:        state.Topic,
		Status:       state.Status,
		StartTime:    state.StartTime,
		EndTime:      state.EndTime,
		Room:         state.RoomID,
		Participants: []models.Participant{}, // Empty slice, we don't store participant details
	}

	return meeting, nil
}

// ListMeetings returns all active meetings with minimal information
func (r *Repository) ListMeetings(ctx context.Context) ([]*models.Meeting, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	meetings := make([]*models.Meeting, 0, len(r.meetingStates))
	for _, state := range r.meetingStates {
		// Only include active meetings (not ended)
		if state.Status != models.MeetingStatusEnded {
			meeting := &models.Meeting{
				ID:           state.ID,
				Topic:        state.Topic,
				Status:       state.Status,
				StartTime:    state.StartTime,
				Room:         state.RoomID,
				Participants: []models.Participant{}, // Empty slice, we don't store participant details
			}
			meetings = append(meetings, meeting)
		}
	}

	return meetings, nil
}

// DeleteMeeting removes a meeting by ID
func (r *Repository) DeleteMeeting(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	state, ok := r.meetingStates[id]
	if !ok {
		return ErrNotFound
	}

	// If this meeting is assigned to a room, clear the room's current meeting
	if state.RoomID != "" {
		if room, exists := r.rooms[state.RoomID]; exists && room.CurrentMeetingID == id {
			room.CurrentMeetingID = ""
			r.rooms[state.RoomID] = room
		}
	}

	// Delete the meeting state
	delete(r.meetingStates, id)

	return nil
}

// SaveRoom saves a room to the repository
func (r *Repository) SaveRoom(ctx context.Context, room *models.Room) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Make a copy to avoid external modifications
	roomCopy := *room
	r.rooms[room.ID] = &roomCopy

	return nil
}

// GetRoom retrieves a room by ID
func (r *Repository) GetRoom(ctx context.Context, id string) (*models.Room, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	room, ok := r.rooms[id]
	if !ok {
		return nil, ErrNotFound
	}

	// Return a copy to avoid external modifications
	roomCopy := *room
	return &roomCopy, nil
}

// ListRooms returns all rooms
func (r *Repository) ListRooms(ctx context.Context) ([]*models.Room, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	rooms := make([]*models.Room, 0, len(r.rooms))
	for _, room := range r.rooms {
		roomCopy := *room
		rooms = append(rooms, &roomCopy)
	}

	return rooms, nil
}

// AddParticipantToMeeting adds a participant ID to a meeting
// We only store the participant ID, not any personal information
func (r *Repository) AddParticipantToMeeting(ctx context.Context, meetingID string, participantID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if meeting exists
	state, ok := r.meetingStates[meetingID]
	if !ok {
		return ErrNotFound
	}

	// Add participant ID to the meeting
	state.ParticipantIDs[participantID] = struct{}{}

	return nil
}

// RemoveParticipantFromMeeting removes a participant ID from a meeting
func (r *Repository) RemoveParticipantFromMeeting(ctx context.Context, meetingID string, participantID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if meeting exists
	state, ok := r.meetingStates[meetingID]
	if !ok {
		return ErrNotFound
	}

	// Remove participant ID from the meeting
	delete(state.ParticipantIDs, participantID)

	return nil
}

// CountParticipantsInMeeting counts the number of participants in a meeting
func (r *Repository) CountParticipantsInMeeting(ctx context.Context, meetingID string) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Check if meeting exists
	state, ok := r.meetingStates[meetingID]
	if !ok {
		return 0, ErrNotFound
	}

	return len(state.ParticipantIDs), nil
}

// GetRoomStatus gets the current status of a room including meeting information
func (r *Repository) GetRoomStatus(ctx context.Context, roomID string) (*models.RoomStatus, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Check if room exists
	room, ok := r.rooms[roomID]
	if !ok {
		return nil, ErrNotFound
	}

	status := &models.RoomStatus{
		RoomID:           roomID,
		RoomName:         room.Name,
		Available:        room.CurrentMeetingID == "",
		CurrentMeetingID: room.CurrentMeetingID,
	}

	// If room has an active meeting, get participant count and topic
	if room.CurrentMeetingID != "" {
		if state, ok := r.meetingStates[room.CurrentMeetingID]; ok {
			status.ParticipantCount = len(state.ParticipantIDs)
			status.MeetingStartTime = state.StartTime
			status.MeetingTopic = state.Topic
		}
	}

	return status, nil
}

// ListRoomStatuses gets the status of all rooms
func (r *Repository) ListRoomStatuses(ctx context.Context) ([]*models.RoomStatus, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	statuses := make([]*models.RoomStatus, 0, len(r.rooms))
	for roomID, room := range r.rooms {
		status := &models.RoomStatus{
			RoomID:           roomID,
			RoomName:         room.Name,
			Available:        room.CurrentMeetingID == "",
			CurrentMeetingID: room.CurrentMeetingID,
		}

		// If room has an active meeting, get participant count and topic
		if room.CurrentMeetingID != "" {
			if state, ok := r.meetingStates[room.CurrentMeetingID]; ok {
				status.ParticipantCount = len(state.ParticipantIDs)
				status.MeetingStartTime = state.StartTime
				status.MeetingTopic = state.Topic
			}
		}

		statuses = append(statuses, status)
	}

	return statuses, nil
}
