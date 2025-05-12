package service

import (
	"context"
	"log"
	"time"

	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository"
)

// MeetingUpdateCallback is a function type for meeting update callbacks
type MeetingUpdateCallback func(*models.Meeting)

// MeetingService provides business logic for working with meetings
type MeetingService struct {
	repo            repository.Repository
	updateCallbacks []MeetingUpdateCallback
}

// NewMeetingService creates a new MeetingService with the given repository
func NewMeetingService(repo repository.Repository) *MeetingService {
	return &MeetingService{
		repo:            repo,
		updateCallbacks: make([]MeetingUpdateCallback, 0),
	}
}

// RegisterUpdateCallback registers a callback function to be called when meeting data changes
func (s *MeetingService) RegisterUpdateCallback(callback MeetingUpdateCallback) {
	s.updateCallbacks = append(s.updateCallbacks, callback)
}

// notifyUpdate calls all registered callbacks with the updated meeting
func (s *MeetingService) notifyUpdate(meeting *models.Meeting) {
	for _, callback := range s.updateCallbacks {
		callback(meeting)
	}
}

// MeetingStatusData represents data for the web UI
type MeetingStatusData struct {
	Meeting          *models.Meeting
	Status           string
	ParticipantCount int
	StartedAt        time.Time
}

// GetMeetingStatusData returns meeting data formatted for the web UI
// If includeEnded is true, ended meetings will be included with 0 participants
func (s *MeetingService) GetMeetingStatusData(ctx context.Context, includeEnded bool) ([]MeetingStatusData, error) {
	var meetings []*models.Meeting
	var err error

	if includeEnded {
		// Get all meetings including ended ones
		meetings, err = s.repo.ListAllMeetings(ctx)
	} else {
		// Get only active meetings (not ended) for backward compatibility
		meetings, err = s.repo.ListMeetings(ctx)
	}

	if err != nil {
		return nil, err
	}

	var result []MeetingStatusData

	// Process each meeting
	for _, meeting := range meetings {
		// Get participant count for this meeting
		participantCount, err := s.repo.CountParticipantsInMeeting(ctx, meeting.ID)
		if err != nil {
			participantCount = 0 // Default to 0 if there's an error
		}

		// For ended meetings, always set participant count to 0
		if meeting.Status == models.MeetingStatusEnded {
			participantCount = 0
		}

		// Determine meeting status string
		statusStr := "scheduled"
		if meeting.Status == models.MeetingStatusStarted {
			statusStr = "in_progress"
		} else if meeting.Status == models.MeetingStatusEnded {
			statusStr = "ended"
		}

		// Add to result
		result = append(result, MeetingStatusData{
			Meeting:          meeting,
			Status:           statusStr,
			ParticipantCount: participantCount,
			StartedAt:        meeting.StartTime,
		})

		// Notify update callbacks
		s.notifyUpdate(meeting)
	}

	return result, nil
}

// GetAllMeetings returns all meetings
func (s *MeetingService) GetAllMeetings() ([]*models.Meeting, error) {
	return s.repo.ListAllMeetings(context.Background())
}

// GetMeeting returns a meeting by ID
func (s *MeetingService) GetMeeting(id string) (*models.Meeting, error) {
	return s.repo.GetMeeting(context.Background(), id)
}

// CreateMeeting creates a new meeting
func (s *MeetingService) CreateMeeting(meeting *models.Meeting) error {
	err := s.repo.SaveMeeting(context.Background(), meeting)
	if err != nil {
		return err
	}

	// Notify all registered callbacks about the new meeting
	s.notifyUpdate(meeting)
	return nil
}

// UpdateMeeting updates an existing meeting
func (s *MeetingService) UpdateMeeting(meeting *models.Meeting) error {
	err := s.repo.SaveMeeting(context.Background(), meeting)
	if err != nil {
		return err
	}

	// Notify all registered callbacks about the update
	s.notifyUpdate(meeting)
	return nil
}

// DeleteMeeting deletes a meeting by ID
func (s *MeetingService) DeleteMeeting(id string) error {
	// Get the meeting first so we can send notification
	meeting, err := s.repo.GetMeeting(context.Background(), id)
	if err != nil {
		return err
	}

	err = s.repo.DeleteMeeting(context.Background(), id)
	if err != nil {
		return err
	}

	// Notify all registered callbacks about the deletion
	s.notifyUpdate(meeting)
	return nil
}

// UpdateParticipantCount updates a meeting's participant count and notifies listeners
func (s *MeetingService) UpdateParticipantCount(meetingID string) error {
	meeting, err := s.repo.GetMeeting(context.Background(), meetingID)
	if err != nil {
		return err
	}

	// No need to update the meeting object here, just notify that it changed
	// The participant count is calculated dynamically in GetMeetingStatusData
	s.notifyUpdate(meeting)
	return nil
}

// NotifyMeetingStarted handles notifications when a meeting starts
func (s *MeetingService) NotifyMeetingStarted(meeting *models.Meeting) {
	// Ensure the meeting has status Started
	meeting.Status = models.MeetingStatusStarted

	// Set meeting start time if not already set
	if meeting.StartTime.IsZero() {
		meeting.StartTime = time.Now()
	}

	// First save the meeting to ensure it exists and status is updated
	ctx := context.Background()
	if err := s.repo.SaveMeeting(ctx, meeting); err != nil {
		log.Printf("Error saving started meeting state: %v", err)
	}

	// Then clear all participants using the safe method
	s.ClearMeetingParticipants(meeting.ID)

	// Notify all registered callbacks about the meeting starting
	s.notifyUpdate(meeting)
}

// NotifyMeetingEnded handles notifications when a meeting ends
func (s *MeetingService) NotifyMeetingEnded(meeting *models.Meeting) {
	// Ensure the meeting has status Ended
	meeting.Status = models.MeetingStatusEnded

	// Set meeting end time if not already set
	if meeting.EndTime.IsZero() {
		meeting.EndTime = time.Now()
	}

	// First save the meeting to ensure it exists and has the correct status and endTime
	ctx := context.Background()
	if err := s.repo.SaveMeeting(ctx, meeting); err != nil {
		log.Printf("Error saving ended meeting state: %v", err)
	}

	// Then clear all participants using the safe method
	s.ClearMeetingParticipants(meeting.ID)

	// Notify all registered callbacks about the meeting ending
	s.notifyUpdate(meeting)
}

// NotifyParticipantJoined handles notifications when a participant joins a meeting
func (s *MeetingService) NotifyParticipantJoined(meetingID string, participantID string) {
	// Get the meeting first
	meeting, err := s.repo.GetMeeting(context.Background(), meetingID)
	if err != nil {
		log.Printf("Error getting meeting for participant joined notification: %v", err)
		return
	}

	// Notify about the change
	s.notifyUpdate(meeting)
}

// NotifyParticipantLeft handles notifications when a participant leaves a meeting
func (s *MeetingService) NotifyParticipantLeft(meetingID string, participantID string) {
	// Get the meeting first
	meeting, err := s.repo.GetMeeting(context.Background(), meetingID)
	if err != nil {
		log.Printf("Error getting meeting for participant left notification: %v", err)
		return
	}

	// Notify about the change
	s.notifyUpdate(meeting)
}

// ClearMeetingParticipants removes all participants from a meeting while preserving meeting data
// Returns the number of participants cleared
func (s *MeetingService) ClearMeetingParticipants(meetingID string) int {
	ctx := context.Background()

	// First, check if meeting exists and get its data
	meeting, err := s.repo.GetMeeting(ctx, meetingID)
	if err != nil {
		log.Printf("Error getting meeting %s to clear participants: %v", meetingID, err)
		return 0
	}

	// Check if there are any participants to clear
	count, err := s.repo.CountParticipantsInMeeting(ctx, meetingID)
	if err != nil || count == 0 {
		return 0
	}

	// Log the operation
	log.Printf("Clearing %d participants from meeting %s", count, meetingID)

	// Instead of deleting and recreating the meeting, which might lose some data,
	// we'll remove each participant individually
	participantCounter := 0

	// Use direct participant removal for each participant
	for _, participant := range meeting.Participants {
		if err := s.repo.RemoveParticipantFromMeeting(ctx, meetingID, participant.ID); err != nil {
			log.Printf("Error removing participant %s: %v", participant.ID, err)
		} else {
			participantCounter++
		}
	}

	// If we didn't clear all participants through the direct approach,
	// try a different approach by updating the meeting with empty participants
	if participantCounter < count {
		// Get the remaining count
		remainingCount, _ := s.repo.CountParticipantsInMeeting(ctx, meetingID)
		if remainingCount > 0 {
			log.Printf("Trying alternative approach to clear %d remaining participants", remainingCount)

			// Keep trying to remove participants until all are gone or we give up
			// This is needed because the Participants slice might not contain all participant IDs
			maxAttempts := 10
			attempt := 0

			for attempt < maxAttempts {
				// Get updated meeting to see remaining participants
				updatedMeeting, err := s.repo.GetMeeting(ctx, meetingID)
				if err != nil {
					break
				}

				// If no more participants, we're done
				currentCount, _ := s.repo.CountParticipantsInMeeting(ctx, meetingID)
				if currentCount == 0 {
					break
				}

				// For any remaining participants, try to remove them
				for _, participant := range updatedMeeting.Participants {
					s.repo.RemoveParticipantFromMeeting(ctx, meetingID, participant.ID)
				}

				attempt++
			}
		}
	}

	// Verify final count
	finalCount, _ := s.repo.CountParticipantsInMeeting(ctx, meetingID)
	if finalCount > 0 {
		log.Printf("Warning: After clearing, there are still %d participants in meeting %s", finalCount, meetingID)
	}

	return count
}
