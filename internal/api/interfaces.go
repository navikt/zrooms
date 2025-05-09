package api

import (
	"github.com/navikt/zrooms/internal/models"
)

// MeetingServicer defines the interface for meeting service operations needed by API handlers
type MeetingServicer interface {
	// Basic CRUD operations
	GetAllMeetings() ([]*models.Meeting, error)
	GetMeeting(id string) (*models.Meeting, error)
	UpdateMeeting(meeting *models.Meeting) error
	DeleteMeeting(id string) error

	// Notification methods for webhook events
	NotifyMeetingStarted(meeting *models.Meeting)
	NotifyMeetingEnded(meeting *models.Meeting)
	NotifyParticipantJoined(meetingID string, participantID string)
	NotifyParticipantLeft(meetingID string, participantID string)
}
