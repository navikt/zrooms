// Package redis_test provides tests for the Redis repository
package redis_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/navikt/zrooms/internal/config"
	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestRedis(t *testing.T) (*redis.Repository, *miniredis.Miniredis, func()) {
	// Create a miniredis server
	mr, err := miniredis.Run()
	require.NoError(t, err)

	// Configure Redis client to use miniredis
	cfg := config.RedisConfig{
		Enabled:    true,
		Host:       mr.Host(),
		Port:       mr.Port(),
		Username:   "",
		Password:   "",
		DB:         0,
		KeyPrefix:  "test:",
		MeetingTTL: time.Hour * 24,
	}

	// Create repository
	repo, err := redis.NewRepository(cfg)
	require.NoError(t, err)

	cleanup := func() {
		mr.Close()
	}

	return repo, mr, cleanup
}

// TestRedisWithURI tests connection with URI format
func TestRedisWithURI(t *testing.T) {
	// Create a miniredis server
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	// Configure Redis client using URI
	uri := fmt.Sprintf("redis://%s:%s", mr.Host(), mr.Port())
	cfg := config.RedisConfig{
		Enabled:    true,
		URI:        uri,
		KeyPrefix:  "test:",
		MeetingTTL: time.Hour * 24,
	}

	// Create repository
	repo, err := redis.NewRepository(cfg)
	require.NoError(t, err)
	defer repo.Close()

	// Basic test to verify connection works
	ctx := context.Background()
	meeting := &models.Meeting{
		ID:        "testURI",
		Topic:     "URI Test",
		Status:    models.MeetingStatusStarted,
		StartTime: time.Now(),
	}

	// Save and retrieve
	err = repo.SaveMeeting(ctx, meeting)
	require.NoError(t, err)

	retrieved, err := repo.GetMeeting(ctx, meeting.ID)
	require.NoError(t, err)
	assert.Equal(t, meeting.ID, retrieved.ID)
	assert.Equal(t, meeting.Topic, retrieved.Topic)
}

func TestMeetingRepository(t *testing.T) {
	repo, _, cleanup := setupTestRedis(t)
	defer cleanup()

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
	repo, _, cleanup := setupTestRedis(t)
	defer cleanup()

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
	repo, _, cleanup := setupTestRedis(t)
	defer cleanup()

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

	// Test that ended meetings are excluded from ListMeetings
	t.Run("EndedMeetingsNotListed", func(t *testing.T) {
		meetings, err := repo.ListMeetings(ctx)
		assert.NoError(t, err)

		// Check if the ended meeting is excluded from active meetings list
		for _, m := range meetings {
			assert.NotEqual(t, meeting.ID, m.ID, "Ended meeting should not be in active meetings list")
		}
	})

	// Test that ended meetings are included in ListAllMeetings
	t.Run("EndedMeetingsInListAll", func(t *testing.T) {
		meetings, err := repo.ListAllMeetings(ctx)
		assert.NoError(t, err)

		found := false
		for _, m := range meetings {
			if m.ID == meeting.ID {
				found = true
				break
			}
		}
		assert.True(t, found, "Ended meeting should be included in ListAllMeetings")
	})
}
