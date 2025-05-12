// Package memory provides an in-memory implementation of the repository interface
package memory

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/navikt/zrooms/internal/models"
)

// ErrNotFound is returned when a requested entity is not found
var ErrNotFound = errors.New("entity not found")

// MeetingState contains information about a meeting's state
type MeetingState struct {
	ID             string // Meeting ID
	Topic          string // Meeting Topic
	Status         models.MeetingStatus
	StartTime      time.Time
	EndTime        time.Time
	ParticipantIDs map[string]struct{} // Store only participant IDs
}

// Repository implements the repository interface with in-memory storage
type Repository struct {
	meetingStates map[string]*MeetingState // Stores meeting state data
	mu            sync.RWMutex
}

// NewRepository creates a new in-memory repository
func NewRepository() *Repository {
	return &Repository{
		meetingStates: make(map[string]*MeetingState),
	}
}

// SaveMeeting saves meeting state information to the repository
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
			Status:         meeting.Status,
			StartTime:      meeting.StartTime,
			ParticipantIDs: make(map[string]struct{}),
		}
		r.meetingStates[meeting.ID] = state
	} else {
		// Update existing meeting state
		state.Status = meeting.Status

		// Only update topic if it's provided and not empty
		if meeting.Topic != "" {
			state.Topic = meeting.Topic
		}

		// Set end time if the meeting has ended
		if meeting.Status == models.MeetingStatusEnded {
			state.EndTime = meeting.EndTime
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

	// Convert state back to a Meeting model with only the necessary data
	meeting := &models.Meeting{
		ID:           state.ID,
		Topic:        state.Topic,
		Status:       state.Status,
		StartTime:    state.StartTime,
		EndTime:      state.EndTime,
		Participants: []models.Participant{}, // Empty slice, we don't store participant details
	}

	return meeting, nil
}

// ListMeetings returns all active meetings with minimal information
// (does not include ended meetings for backward compatibility)
func (r *Repository) ListMeetings(ctx context.Context) ([]*models.Meeting, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	meetings := make([]*models.Meeting, 0, len(r.meetingStates))
	for _, state := range r.meetingStates {
		// Only include active meetings (not ended) for backward compatibility
		if state.Status != models.MeetingStatusEnded {
			meeting := &models.Meeting{
				ID:           state.ID,
				Topic:        state.Topic,
				Status:       state.Status,
				StartTime:    state.StartTime,
				EndTime:      state.EndTime,
				Participants: []models.Participant{}, // Empty slice, we don't store participant details
			}
			meetings = append(meetings, meeting)
		}
	}

	return meetings, nil
}

// ListAllMeetings returns all meetings with minimal information including ended meetings
func (r *Repository) ListAllMeetings(ctx context.Context) ([]*models.Meeting, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	meetings := make([]*models.Meeting, 0, len(r.meetingStates))
	for _, state := range r.meetingStates {
		// Include all meetings, including ended ones
		meeting := &models.Meeting{
			ID:           state.ID,
			Topic:        state.Topic,
			Status:       state.Status,
			StartTime:    state.StartTime,
			EndTime:      state.EndTime,
			Participants: []models.Participant{}, // Empty slice, we don't store participant details
		}
		meetings = append(meetings, meeting)
	}

	return meetings, nil
}

// DeleteMeeting removes a meeting by ID
func (r *Repository) DeleteMeeting(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, ok := r.meetingStates[id]
	if !ok {
		return ErrNotFound
	}

	// Delete the meeting state
	delete(r.meetingStates, id)

	return nil
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

func (r *Repository) ClearPartipantsInMeeting(ctx context.Context, meetingID string) error {
	// Attempt to fetch the meeting
	meeting, err := r.GetMeeting(ctx, meetingID)
	if err != nil {
		return ErrNotFound
	}

	// Create a copy of the meeting with zero participants
	meeting.Participants = []models.Participant{}

	// Overwrite the original meeting with the new one
	return r.SaveMeeting(ctx, meeting)
}
