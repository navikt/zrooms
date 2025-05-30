package service

import (
	"context"
	"log"
	"time"

	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository"
	"github.com/navikt/zrooms/internal/utils"
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
	}

	return result, nil
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

	err := s.repo.ClearPartipantsInMeeting(ctx, meeting.ID)
	if err != nil {
		log.Printf("Error clearing participants for meeting %s: %v", utils.SanitizeLogString(meeting.ID), err)

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
