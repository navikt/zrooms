package web

import "github.com/navikt/zrooms/internal/models"

// MeetingServicer defines the contract for meeting services used by web handlers
type MeetingServicer interface {
	GetAllMeetings() ([]*models.Meeting, error)
	GetMeeting(id string) (*models.Meeting, error)
	UpdateMeeting(meeting *models.Meeting) error
	DeleteMeeting(id string) error
}
