package service

import (
	"context"
	"time"

	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository"
)

type MeetingUpdateCallback func(*models.Meeting)

type MeetingService struct {
	repo            repository.Repository
	updateCallbacks []MeetingUpdateCallback
}

func NewMeetingService(repo repository.Repository) *MeetingService {
	return &MeetingService{repo: repo}
}

// RegisterUpdateCallback is called during startup, before serving requests.
func (s *MeetingService) RegisterUpdateCallback(callback MeetingUpdateCallback) {
	s.updateCallbacks = append(s.updateCallbacks, callback)
}

func (s *MeetingService) ProcessEvent(ctx context.Context, event models.MeetingEvent) error {
	meeting, err := s.repo.ApplyEvent(ctx, event)
	if err != nil {
		return err
	}

	if meeting != nil {
		for _, callback := range s.updateCallbacks {
			callback(meeting)
		}
	}
	return nil
}

func (s *MeetingService) DeleteMeeting(ctx context.Context, id string) error {
	if err := s.repo.DeleteMeeting(ctx, id); err != nil {
		return err
	}
	for _, callback := range s.updateCallbacks {
		callback(&models.Meeting{ID: id})
	}
	return nil
}

// DeauthorizeUser notifies clients of persisted deletions, including partial cleanup.
func (s *MeetingService) DeauthorizeUser(ctx context.Context, userID string, at time.Time) error {
	deleted, err := s.repo.DeleteMeetingsByHost(ctx, userID, at)
	for _, id := range deleted {
		for _, callback := range s.updateCallbacks {
			callback(&models.Meeting{ID: id})
		}
	}
	return err
}

type MeetingStatusData struct {
	Meeting          *models.Meeting
	Status           string
	ParticipantCount int
	StartedAt        time.Time
}

func (s *MeetingService) GetMeetingStatusData(ctx context.Context, includeEnded bool) ([]MeetingStatusData, error) {
	meetings, err := s.repo.ListAllMeetings(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]MeetingStatusData, 0, len(meetings))
	for _, meeting := range meetings {
		if !includeEnded && meeting.Status == models.MeetingStatusEnded {
			continue
		}
		status := "scheduled"
		switch meeting.Status {
		case models.MeetingStatusStarted:
			status = "in_progress"
		case models.MeetingStatusEnded:
			status = "ended"
		}
		result = append(result, MeetingStatusData{
			Meeting: meeting, Status: status,
			ParticipantCount: meeting.ParticipantCount, StartedAt: meeting.StartTime,
		})
	}
	return result, nil
}
