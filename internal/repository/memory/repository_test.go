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
		Room:      "room101",
	}

	// Test SaveMeeting and GetMeeting
	t.Run("SaveAndGetMeeting", func(t *testing.T) {
		err := repo.SaveMeeting(ctx, meeting)
		assert.NoError(t, err)

		savedMeeting, err := repo.GetMeeting(ctx, meeting.ID)
		assert.NoError(t, err)
		assert.Equal(t, meeting.ID, savedMeeting.ID)
		assert.Equal(t, meeting.Status, savedMeeting.Status)
		assert.Equal(t, meeting.Room, savedMeeting.Room)
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

func TestRoomRepository(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()

	// Test room
	room := &models.Room{
		ID:       "room101",
		Name:     "Room 101",
		Capacity: 10,
		Location: "1st Floor",
	}

	// Test SaveRoom and GetRoom
	t.Run("SaveAndGetRoom", func(t *testing.T) {
		err := repo.SaveRoom(ctx, room)
		assert.NoError(t, err)

		savedRoom, err := repo.GetRoom(ctx, room.ID)
		assert.NoError(t, err)
		assert.Equal(t, room.ID, savedRoom.ID)
		assert.Equal(t, room.Name, savedRoom.Name)
		assert.Equal(t, room.Capacity, savedRoom.Capacity)
	})

	// Test ListRooms
	t.Run("ListRooms", func(t *testing.T) {
		rooms, err := repo.ListRooms(ctx)
		assert.NoError(t, err)
		assert.Len(t, rooms, 1)
		assert.Equal(t, room.ID, rooms[0].ID)
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
		Room:      "room101",
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

func TestRoomStatus(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()

	// Create a room
	room := &models.Room{
		ID:       "room202",
		Name:     "Conference Room",
		Capacity: 20,
	}
	err := repo.SaveRoom(ctx, room)
	assert.NoError(t, err)

	// Test GetRoomStatus with no meeting
	t.Run("EmptyRoomStatus", func(t *testing.T) {
		status, err := repo.GetRoomStatus(ctx, room.ID)
		assert.NoError(t, err)
		assert.Equal(t, room.ID, status.RoomID)
		assert.Equal(t, room.Name, status.RoomName)
		assert.True(t, status.Available)
		assert.Empty(t, status.CurrentMeetingID)
		assert.Zero(t, status.ParticipantCount)
	})

	// Create a meeting for the room
	meeting := &models.Meeting{
		ID:        "meeting789",
		Status:    models.MeetingStatusStarted,
		StartTime: time.Now(),
		Room:      room.ID,
	}
	err = repo.SaveMeeting(ctx, meeting)
	assert.NoError(t, err)

	// Add participants
	err = repo.AddParticipantToMeeting(ctx, meeting.ID, "user1")
	assert.NoError(t, err)
	err = repo.AddParticipantToMeeting(ctx, meeting.ID, "user2")
	assert.NoError(t, err)

	// Test GetRoomStatus with an active meeting
	t.Run("OccupiedRoomStatus", func(t *testing.T) {
		status, err := repo.GetRoomStatus(ctx, room.ID)
		assert.NoError(t, err)
		assert.Equal(t, room.ID, status.RoomID)
		assert.False(t, status.Available)
		assert.Equal(t, meeting.ID, status.CurrentMeetingID)
		assert.Equal(t, 2, status.ParticipantCount)
		assert.WithinDuration(t, meeting.StartTime, status.MeetingStartTime, time.Second)
	})

	// Test ListRoomStatuses
	t.Run("ListRoomStatuses", func(t *testing.T) {
		statuses, err := repo.ListRoomStatuses(ctx)
		assert.NoError(t, err)
		assert.Len(t, statuses, 1)
		assert.Equal(t, room.ID, statuses[0].RoomID)
		assert.Equal(t, 2, statuses[0].ParticipantCount)
	})

	// End the meeting
	meeting.Status = models.MeetingStatusEnded
	meeting.EndTime = time.Now()
	err = repo.SaveMeeting(ctx, meeting)
	assert.NoError(t, err)

	// Test room status after meeting has ended
	t.Run("RoomStatusAfterMeetingEnded", func(t *testing.T) {
		status, err := repo.GetRoomStatus(ctx, room.ID)
		assert.NoError(t, err)
		assert.True(t, status.Available)
		assert.Empty(t, status.CurrentMeetingID)
	})
}
