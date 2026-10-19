package models_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/navikt/zrooms/internal/models"
	"github.com/stretchr/testify/require"
)

func TestMeetingEventContract(t *testing.T) {
	tests := []struct {
		name        string
		json        string
		id          string
		participant string
	}{
		{name: "numeric updated ID", json: `{"event":"meeting.updated","event_ts":1000,"payload":{"operator":"creator@example.test","object":{"id":1234567890123,"topic":"Updated"}}}`, id: "1234567890123"},
		{name: "anonymous participant", json: `{"event":"meeting.participant_joined","event_ts":1000,"payload":{"object":{"id":"123","uuid":"instance","participant":{"id":"","user_id":"guest-session","user_name":"Not retained","email":"not-retained@example.test"}}}}`, id: "123", participant: "guest-session"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var webhook models.WebhookEvent
			require.NoError(t, json.Unmarshal([]byte(tt.json), &webhook))
			event, err := webhook.MeetingEvent()
			require.NoError(t, err)
			require.Equal(t, tt.id, event.Meeting.ID)
			require.Equal(t, tt.participant, event.ParticipantID)
			require.Empty(t, event.Meeting.Participants)
			require.Empty(t, event.Meeting.Host.Email)
			if tt.participant != "" {
				require.Equal(t, time.UnixMilli(1000).UTC(), event.Meeting.StartTime)
			}
		})
	}
}
