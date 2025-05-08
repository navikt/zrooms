package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository/memory"
	"github.com/stretchr/testify/assert"
)

func TestMeetingRepository(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()

	// Test meeting with minimal data
	meeting := &models.Meeting{
		ID:        "meeting123",
		Status:    models.MeetingStatusStarted,
		StartTime: time.Now(),
	}

	// Test SaveMeeting and GetMeeting
	t.Run("SaveAndGetMeeting", func(t *testing.T) {
		err := repo.SaveMeeting(ctx, meeting)
		assert.NoError(t, err)

		savedMeeting, err := repo.GetMeeting(ctx, meeting.ID)
		assert.NoError(t, err)
		assert.Equal(t, meeting.ID, savedMeeting.ID)
		assert.Equal(t, meeting.Status, savedMeeting.Status)
		assert.Empty(t, savedMeeting.Participants, "Should not store participant details")
	})

	// Test ListMeetings
	t.Run("ListMeetings", func(t *testing.T) {
		meetings, err := repo.ListMeetings(ctx)
		assert.NoError(t, err)
		assert.Len(t, meetings, 1)
		assert.Equal(t, meeting.ID, meetings[0].ID)
	})

	// Test DeleteMeeting
	t.Run("DeleteMeeting", func(t *testing.T) {
		err := repo.DeleteMeeting(ctx, meeting.ID)
		assert.NoError(t, err)

		_, err = repo.GetMeeting(ctx, meeting.ID)
		assert.Error(t, err)
	})
}

func TestParticipantOperations(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()

	// Create a meeting first
	meeting := &models.Meeting{
		ID:        "meeting456",
		Status:    models.MeetingStatusStarted,
		StartTime: time.Now(),
	}
	err := repo.SaveMeeting(ctx, meeting)
	assert.NoError(t, err)

	// Test participant operations with only IDs
	t.Run("ParticipantOperations", func(t *testing.T) {
		// Add participants by ID only
		err := repo.AddParticipantToMeeting(ctx, meeting.ID, "user123")
		assert.NoError(t, err)

		// Count participants
		count, err := repo.CountParticipantsInMeeting(ctx, meeting.ID)
		assert.NoError(t, err)
		assert.Equal(t, 1, count)

		// Add another participant
		err = repo.AddParticipantToMeeting(ctx, meeting.ID, "user456")
		assert.NoError(t, err)

		count, err = repo.CountParticipantsInMeeting(ctx, meeting.ID)
		assert.NoError(t, err)
		assert.Equal(t, 2, count)

		// Remove a participant
		err = repo.RemoveParticipantFromMeeting(ctx, meeting.ID, "user123")
		assert.NoError(t, err)

		count, err = repo.CountParticipantsInMeeting(ctx, meeting.ID)
		assert.NoError(t, err)
		assert.Equal(t, 1, count)
	})
}

func TestMeetingWithParticipants(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()

	// Create a meeting
	meeting := &models.Meeting{
		ID:        "meeting789",
		Topic:     "Important Discussion",
		Status:    models.MeetingStatusStarted,
		StartTime: time.Now(),
	}
	err := repo.SaveMeeting(ctx, meeting)
	assert.NoError(t, err)

	// Add participants
	err = repo.AddParticipantToMeeting(ctx, meeting.ID, "user1")
	assert.NoError(t, err)
	err = repo.AddParticipantToMeeting(ctx, meeting.ID, "user2")
	assert.NoError(t, err)

	// Test participant count
	t.Run("ParticipantCount", func(t *testing.T) {
		count, err := repo.CountParticipantsInMeeting(ctx, meeting.ID)
		assert.NoError(t, err)
		assert.Equal(t, 2, count)
	})

	// End the meeting
	meeting.Status = models.MeetingStatusEnded
	meeting.EndTime = time.Now()
	err = repo.SaveMeeting(ctx, meeting)
	assert.NoError(t, err)

	// Test that ended meetings are not included in ListMeetings
	t.Run("EndedMeetingsNotListed", func(t *testing.T) {
		meetings, err := repo.ListMeetings(ctx)
		assert.NoError(t, err)

		// Check if the ended meeting is excluded from active meetings list
		for _, m := range meetings {
			assert.NotEqual(t, meeting.ID, m.ID, "Ended meeting should not be in active meetings list")
		}
	})
}
