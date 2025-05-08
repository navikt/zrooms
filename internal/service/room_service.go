package service

import (
	"context"
	"time"

	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository"
)

// RoomService provides business logic for working with rooms and meetings
type RoomService struct {
	repo repository.Repository
}

// NewRoomService creates a new RoomService with the given repository
func NewRoomService(repo repository.Repository) *RoomService {
	return &RoomService{
		repo: repo,
	}
}

// GetAllRoomStatuses returns all room statuses with their current meeting information
func (s *RoomService) GetAllRoomStatuses(ctx context.Context) ([]*models.RoomStatus, error) {
	return s.repo.ListRoomStatuses(ctx)
}

// MeetingStatusData represents data for the web UI
type MeetingStatusData struct {
	Room             *models.Room
	Meeting          *models.Meeting
	Status           string
	ParticipantCount int
	StartedAt        time.Time
}

// GetMeetingStatusData returns meeting data formatted for the web UI
func (s *RoomService) GetMeetingStatusData(ctx context.Context) ([]MeetingStatusData, error) {
	// Get room statuses
	roomStatuses, err := s.repo.ListRoomStatuses(ctx)
	if err != nil {
		return nil, err
	}

	var result []MeetingStatusData

	// Process each room status
	for _, status := range roomStatuses {
		if status.CurrentMeetingID == "" {
			// Room is available, no meeting in progress
			continue
		}

		// Get meeting details
		meeting, err := s.repo.GetMeeting(ctx, status.CurrentMeetingID)
		if err != nil {
			continue // Skip if meeting not found
		}

		// Get room details
		room, err := s.repo.GetRoom(ctx, status.RoomID)
		if err != nil {
			continue // Skip if room not found
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
			Room:             room,
			Meeting:          meeting,
			Status:           statusStr,
			ParticipantCount: status.ParticipantCount,
			StartedAt:        status.MeetingStartTime,
		})
	}

	return result, nil
}
