package models_test

import (
	"testing"
	"time"

	"github.com/navikt/zrooms/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestMeeting(t *testing.T) {
	// Test meeting creation
	m := models.Meeting{
		ID:        "123456789",
		Topic:     "Test Meeting",
		StartTime: time.Now(),
		Duration:  60, // minutes
		Status:    models.MeetingStatusStarted,
		Host: models.Participant{
			ID:    "host123",
			Name:  "Host User",
			Email: "host@example.com",
		},
		Participants: []models.Participant{},
	}

	// Verify initial state
	assert.Equal(t, "123456789", m.ID)
	assert.Equal(t, "Test Meeting", m.Topic)
	assert.Equal(t, models.MeetingStatusStarted, m.Status)
	assert.Equal(t, 0, len(m.Participants))

	// Test adding participants
	participant := models.Participant{
		ID:    "user123",
		Name:  "Test User",
		Email: "user@example.com",
	}

	m.AddParticipant(participant)
	assert.Equal(t, 1, len(m.Participants))
	assert.Equal(t, "user123", m.Participants[0].ID)

	// Test removing participants
	removed := m.RemoveParticipant("user123")
	assert.True(t, removed)
	assert.Equal(t, 0, len(m.Participants))

	// Test removing non-existent participant
	removed = m.RemoveParticipant("nonexistent")
	assert.False(t, removed)
}

func TestParticipant(t *testing.T) {
	p := models.Participant{
		ID:       "user123",
		Name:     "Test User",
		Email:    "user@example.com",
		JoinTime: time.Now(),
	}

	assert.Equal(t, "user123", p.ID)
	assert.Equal(t, "Test User", p.Name)
	assert.Equal(t, "user@example.com", p.Email)
	assert.False(t, p.JoinTime.IsZero())
}

func TestMeetingStatus(t *testing.T) {
	// Test all status values
	statuses := []models.MeetingStatus{
		models.MeetingStatusCreated,
		models.MeetingStatusUpdated,
		models.MeetingStatusStarted,
		models.MeetingStatusEnded,
	}

	expectedStrings := []string{
		"created",
		"updated",
		"started",
		"ended",
	}

	for i, status := range statuses {
		assert.Equal(t, expectedStrings[i], status.String())
	}
}

// TestMeetingLifecycle tests the complete lifecycle of a meeting
func TestMeetingLifecycle(t *testing.T) {
	// Create a meeting
	m := &models.Meeting{
		ID:        "lifecycle123",
		Topic:     "Lifecycle Test",
		Status:    models.MeetingStatusCreated,
		StartTime: time.Now().Add(1 * time.Hour), // Scheduled for future
		Duration:  30,
	}

	// Meeting should initially have no participants
	assert.Equal(t, 0, len(m.Participants))

	// Transition to started status
	m.Status = models.MeetingStatusStarted
	m.StartTime = time.Now() // Update start time to now
	assert.Equal(t, models.MeetingStatusStarted, m.Status)

	// Add participants
	m.AddParticipant(models.Participant{ID: "user1", Name: "User 1"})
	m.AddParticipant(models.Participant{ID: "user2", Name: "User 2"})
	assert.Equal(t, 2, len(m.Participants))

	// Remove a participant
	m.RemoveParticipant("user1")
	assert.Equal(t, 1, len(m.Participants))
	assert.Equal(t, "user2", m.Participants[0].ID)

	// End the meeting
	m.Status = models.MeetingStatusEnded
	m.EndTime = time.Now()
	assert.Equal(t, models.MeetingStatusEnded, m.Status)
	assert.False(t, m.EndTime.IsZero())
}
