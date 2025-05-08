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
		Room:         "Room A",
	}

	// Verify initial state
	assert.Equal(t, "123456789", m.ID)
	assert.Equal(t, "Test Meeting", m.Topic)
	assert.Equal(t, models.MeetingStatusStarted, m.Status)
	assert.Equal(t, "Room A", m.Room)
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

func TestRoom(t *testing.T) {
	r := models.Room{
		ID:               "room123",
		Name:             "Meeting Room A",
		Capacity:         10,
		Location:         "3rd Floor",
		CurrentMeetingID: "123456789",
	}

	assert.Equal(t, "room123", r.ID)
	assert.Equal(t, "Meeting Room A", r.Name)
	assert.Equal(t, 10, r.Capacity)
	assert.Equal(t, "3rd Floor", r.Location)
	assert.Equal(t, "123456789", r.CurrentMeetingID)

	// Test room availability
	assert.False(t, r.IsAvailable())

	r.CurrentMeetingID = ""
	assert.True(t, r.IsAvailable())
}
