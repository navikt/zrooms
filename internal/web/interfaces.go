package web

import (
	"context"

	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/service"
)

// MeetingServicer defines the contract for meeting services used by web handlers
type MeetingServicer interface {
	// Web UI data retrieval
	GetMeetingStatusData(ctx context.Context, includeEnded bool) ([]service.MeetingStatusData, error)

	// Webhook notification methods
	NotifyMeetingStarted(meeting *models.Meeting)
	NotifyMeetingUpdated(meeting *models.Meeting)
	NotifyMeetingEnded(meeting *models.Meeting)
	NotifyParticipantJoined(meetingID string, participantID string)
	NotifyParticipantLeft(meetingID string, participantID string)
}
