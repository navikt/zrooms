package service

import (
	"context"
	"time"

	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository"
)

// MeetingService provides business logic for working with meetings
type MeetingService struct {
	repo repository.Repository
}

// NewMeetingService creates a new MeetingService with the given repository
func NewMeetingService(repo repository.Repository) *MeetingService {
	return &MeetingService{
		repo: repo,
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
