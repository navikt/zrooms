package models_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/navikt/zrooms/internal/models"
	"github.com/stretchr/testify/assert"
)

// TestWebhookEventProcessing tests the processing of different webhook events
func TestWebhookEventProcessing(t *testing.T) {
	t.Run("ProcessMeetingCreated", func(t *testing.T) {
		// Sample meeting.created event
		eventJSON := `{
			"event": "meeting.created",
			"payload": {
				"account_id": "abc123",
				"object": {
					"uuid": "uuid123",
					"id": "987654321",
					"host_id": "host456",
					"topic": "New Meeting",
					"type": 2,
					"start_time": "2025-05-08T15:00:00Z",
					"duration": 30,
					"timezone": "UTC"
				}
			},
			"event_ts": 1620123456789
		}`

		var event models.WebhookEvent
		err := json.Unmarshal([]byte(eventJSON), &event)
		assert.NoError(t, err)

		// Process the event
		meeting := event.ProcessMeetingCreated()

		// Verify the processed meeting
		assert.Equal(t, "987654321", meeting.ID)
		assert.Equal(t, "New Meeting", meeting.Topic)
		assert.Equal(t, 30, meeting.Duration)
		assert.Equal(t, models.MeetingStatusCreated, meeting.Status)
		assert.Equal(t, "host456", meeting.Host.ID)
		assert.Empty(t, meeting.Participants)
	})

	t.Run("ProcessMeetingStarted", func(t *testing.T) {
		// Sample meeting.started event
		eventJSON := `{
			"event": "meeting.started",
			"payload": {
				"account_id": "abc123",
				"object": {
					"uuid": "uuid123",
					"id": "987654321",
					"host_id": "host456",
					"topic": "Active Meeting",
					"type": 2,
					"duration": 45,
					"timezone": "UTC"
				}
			},
			"event_ts": 1620123456789
		}`

		var event models.WebhookEvent
		err := json.Unmarshal([]byte(eventJSON), &event)
		assert.NoError(t, err)

		// Process the event
		meeting := event.ProcessMeetingStarted()

		// Verify the processed meeting
		assert.Equal(t, "987654321", meeting.ID)
		assert.Equal(t, "Active Meeting", meeting.Topic)
		assert.Equal(t, 45, meeting.Duration)
		assert.Equal(t, models.MeetingStatusStarted, meeting.Status)
		assert.Equal(t, "host456", meeting.Host.ID)
		assert.WithinDuration(t, time.Now(), meeting.StartTime, 2*time.Second)
		assert.Empty(t, meeting.Participants)
	})

	t.Run("ProcessMeetingEnded", func(t *testing.T) {
		// Sample meeting.ended event
		eventJSON := `{
			"event": "meeting.ended",
			"payload": {
				"account_id": "abc123",
				"object": {
					"uuid": "uuid123",
					"id": "987654321",
					"host_id": "host456",
					"topic": "Completed Meeting",
					"type": 2
				}
			},
			"event_ts": 1620123456789
		}`

		var event models.WebhookEvent
		err := json.Unmarshal([]byte(eventJSON), &event)
		assert.NoError(t, err)

		// Process the event
		meeting := event.ProcessMeetingEnded()

		// Verify the processed meeting
		assert.Equal(t, "987654321", meeting.ID)
		assert.Equal(t, "Completed Meeting", meeting.Topic)
		assert.Equal(t, models.MeetingStatusEnded, meeting.Status)
		assert.Equal(t, "host456", meeting.Host.ID)
		assert.WithinDuration(t, time.Now(), meeting.EndTime, 2*time.Second)
	})

	t.Run("ProcessParticipantJoined", func(t *testing.T) {
		// Sample meeting.participant_joined event
		eventJSON := `{
			"event": "meeting.participant_joined",
			"payload": {
				"account_id": "abc123",
				"object": {
					"uuid": "uuid123",
					"id": "987654321",
					"host_id": "host456",
					"participant": {
						"id": "part789",
						"user_id": "user789",
						"user_name": "Jane Doe",
						"email": "jane@example.com"
					}
				}
			},
			"event_ts": 1620123456789
		}`

		var event models.WebhookEvent
		err := json.Unmarshal([]byte(eventJSON), &event)
		assert.NoError(t, err)

		// Process the event
		participant := event.ProcessParticipantJoined()

		// Verify the processed participant
		assert.Equal(t, "user789", participant.ID)
		assert.Equal(t, "Jane Doe", participant.Name)
		assert.Equal(t, "jane@example.com", participant.Email)
		assert.WithinDuration(t, time.Now(), participant.JoinTime, 2*time.Second)
	})

	t.Run("ProcessParticipantLeft", func(t *testing.T) {
		// Sample meeting.participant_left event
		eventJSON := `{
			"event": "meeting.participant_left",
			"payload": {
				"account_id": "abc123",
				"object": {
					"uuid": "uuid123",
					"id": "987654321",
					"host_id": "host456",
					"participant": {
						"id": "part789",
						"user_id": "user789",
						"user_name": "Jane Doe",
						"email": "jane@example.com"
					}
				}
			},
			"event_ts": 1620123456789
		}`

		var event models.WebhookEvent
		err := json.Unmarshal([]byte(eventJSON), &event)
		assert.NoError(t, err)

		// Process the event
		participant := event.ProcessParticipantLeft()

		// Verify the processed participant
		assert.Equal(t, "user789", participant.ID)
		assert.Equal(t, "Jane Doe", participant.Name)
		assert.Equal(t, "jane@example.com", participant.Email)
		assert.WithinDuration(t, time.Now(), participant.LeaveTime, 2*time.Second)
	})
}
