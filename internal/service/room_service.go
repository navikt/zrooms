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
func (s *MeetingService) GetMeetingStatusData(ctx context.Context) ([]MeetingStatusData, error) {
	// Get all meetings directly
	meetings, err := s.repo.ListMeetings(ctx)
	if err != nil {
		return nil, err
	}

	var result []MeetingStatusData

	// Process each meeting
	for _, meeting := range meetings {
		// Skip ended meetings
		if meeting.Status == models.MeetingStatusEnded {
			continue
		}

		// Get participant count for this meeting
		participantCount, err := s.repo.CountParticipantsInMeeting(ctx, meeting.ID)
		if err != nil {
			participantCount = 0 // Default to 0 if there's an error
		}

		// Determine meeting status string
		statusStr := "scheduled"
		if meeting.Status == models.MeetingStatusStarted {
			statusStr = "in_progress"
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
