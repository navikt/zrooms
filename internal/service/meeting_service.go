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

	// Get all participants in the meeting
	ctx := context.Background()

	// Clear all participants to ensure count is reset to 0 at the beginning of the meeting
	// This is done by getting all participant IDs and removing them one by one
	meetingFromDB, err := s.repo.GetMeeting(ctx, meeting.ID)
	if err == nil && meetingFromDB != nil {
		// Get participant count first
		count, err := s.repo.CountParticipantsInMeeting(ctx, meeting.ID)
		if err == nil && count > 0 {
			log.Printf("Resetting %d participants for started meeting %s", count, meeting.ID)

			// Clear all participants from the meeting
			for _, participant := range meetingFromDB.Participants {
				err := s.repo.RemoveParticipantFromMeeting(ctx, meeting.ID, participant.ID)
				if err != nil {
					log.Printf("Error removing participant %s from started meeting %s: %v", participant.ID, meeting.ID, err)
				}
			}
		}
	}

	// Update the meeting in the repository to save its started state
	if err := s.repo.SaveMeeting(ctx, meeting); err != nil {
		log.Printf("Error saving started meeting state: %v", err)
	}

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

	// Get all participants in the meeting
	ctx := context.Background()

	// Clear all participants to ensure count is reset to 0
	// This is done by getting all participant IDs and removing them one by one
	meetingFromDB, err := s.repo.GetMeeting(ctx, meeting.ID)
	if err == nil && meetingFromDB != nil {
		// Get participant count first
		count, err := s.repo.CountParticipantsInMeeting(ctx, meeting.ID)
		if err == nil && count > 0 {
			log.Printf("Resetting %d participants for ended meeting %s", count, meeting.ID)

			// Clear all participants from the meeting
			for _, participant := range meetingFromDB.Participants {
				err := s.repo.RemoveParticipantFromMeeting(ctx, meeting.ID, participant.ID)
				if err != nil {
					log.Printf("Error removing participant %s from ended meeting %s: %v", participant.ID, meeting.ID, err)
				}
			}
		}
	}

	// Update the meeting in the repository to save its ended state
	if err := s.repo.SaveMeeting(ctx, meeting); err != nil {
		log.Printf("Error saving ended meeting state: %v", err)
	}

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
