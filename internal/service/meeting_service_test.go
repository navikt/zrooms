package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository/memory"
	"github.com/navikt/zrooms/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockUpdateCallback is a mock for testing callbacks
type MockUpdateCallback struct {
	mock.Mock
}

func (m *MockUpdateCallback) OnUpdate(meeting *models.Meeting) {
	m.Called(meeting)
}

func TestMeetingService_GetMeetingStatusData(t *testing.T) {
	// Create repository and service
	repo := memory.NewRepository()
	meetingService := service.NewMeetingService(repo)
	ctx := context.Background()

	// Set up test data - add meetings with different statuses
	now := time.Now()

	// Meeting 1: In progress with 2 participants
	meeting1 := &models.Meeting{
		ID:        "meeting1",
		Topic:     "Current Meeting",
		Status:    models.MeetingStatusStarted,
		StartTime: now.Add(-30 * time.Minute),
	}
	require.NoError(t, repo.SaveMeeting(ctx, meeting1))
	require.NoError(t, repo.AddParticipantToMeeting(ctx, meeting1.ID, "user1"))
	require.NoError(t, repo.AddParticipantToMeeting(ctx, meeting1.ID, "user2"))

	// Meeting 2: Scheduled for future
	meeting2 := &models.Meeting{
		ID:        "meeting2",
		Topic:     "Future Meeting",
		Status:    models.MeetingStatusCreated,
		StartTime: now.Add(1 * time.Hour),
	}
	require.NoError(t, repo.SaveMeeting(ctx, meeting2))

	// Meeting 3: Already ended (should be excluded from results)
	meeting3 := &models.Meeting{
		ID:        "meeting3",
		Topic:     "Past Meeting",
		Status:    models.MeetingStatusEnded,
		StartTime: now.Add(-2 * time.Hour),
		EndTime:   now.Add(-1 * time.Hour),
	}
	require.NoError(t, repo.SaveMeeting(ctx, meeting3))

	// Execute the method being tested, pass false to exclude ended meetings
	result, err := meetingService.GetMeetingStatusData(ctx, false)
	require.NoError(t, err)

	// Should return 2 meetings (the active and scheduled ones)
	assert.Len(t, result, 2, "Should return 2 meetings (excluding the ended one)")

	// Find and verify meeting1 data
	var meeting1Data *service.MeetingStatusData
	var meeting2Data *service.MeetingStatusData

	for i := range result {
		if result[i].Meeting.ID == meeting1.ID {
			meeting1Data = &result[i]
		} else if result[i].Meeting.ID == meeting2.ID {
			meeting2Data = &result[i]
		}
	}

	// Verify meeting 1 data (in progress)
	require.NotNil(t, meeting1Data, "Meeting 1 should be in the results")
	assert.Equal(t, meeting1.ID, meeting1Data.Meeting.ID)
	assert.Equal(t, meeting1.Topic, meeting1Data.Meeting.Topic)
	assert.Equal(t, "in_progress", meeting1Data.Status)
	assert.Equal(t, 2, meeting1Data.ParticipantCount)
	assert.Equal(t, meeting1.StartTime, meeting1Data.StartedAt)

	// Verify meeting 2 data (scheduled)
	require.NotNil(t, meeting2Data, "Meeting 2 should be in the results")
	assert.Equal(t, meeting2.ID, meeting2Data.Meeting.ID)
	assert.Equal(t, meeting2.Topic, meeting2Data.Meeting.Topic)
	assert.Equal(t, "scheduled", meeting2Data.Status)
	assert.Equal(t, 0, meeting2Data.ParticipantCount)
	assert.Equal(t, meeting2.StartTime, meeting2Data.StartedAt)

	// Verify meeting 3 (ended) is not in the results
	for _, data := range result {
		assert.NotEqual(t, meeting3.ID, data.Meeting.ID, "Ended meeting should not be in the results")
	}

	// Now test includeEnded=true to include ended meetings
	resultWithEnded, err := meetingService.GetMeetingStatusData(ctx, true)
	require.NoError(t, err)

	// Should return 3 meetings (all meetings including the ended one)
	assert.Len(t, resultWithEnded, 3, "Should return 3 meetings (including the ended one)")

	// Find ended meeting data
	var endedMeetingData *service.MeetingStatusData
	for i := range resultWithEnded {
		if resultWithEnded[i].Meeting.ID == meeting3.ID {
			endedMeetingData = &resultWithEnded[i]
			break
		}
	}

	// Verify ended meeting data
	require.NotNil(t, endedMeetingData, "Ended meeting should be in the results when includeEnded=true")
	assert.Equal(t, meeting3.ID, endedMeetingData.Meeting.ID)
	assert.Equal(t, "ended", endedMeetingData.Status)
	assert.Equal(t, 0, endedMeetingData.ParticipantCount)
}

// TestMeetingService_UpdateCallbacks tests the callback mechanism for meeting updates
func TestMeetingService_UpdateCallbacks(t *testing.T) {
	// Create repository and service
	repo := memory.NewRepository()
	meetingService := service.NewMeetingService(repo)
	ctx := context.Background()

	// Create a test meeting and add it to repository directly
	meeting := &models.Meeting{
		ID:        "test-meeting",
		Topic:     "Test Meeting",
		Status:    models.MeetingStatusCreated,
		StartTime: time.Now(),
	}
	require.NoError(t, repo.SaveMeeting(ctx, meeting))

	// Create mock callback
	mockCallback := new(MockUpdateCallback)

	// Register callback
	meetingService.RegisterUpdateCallback(func(m *models.Meeting) {
		mockCallback.OnUpdate(m)
	})

	// Setup expectations - callback should be called for each operation
	mockCallback.On("OnUpdate", mock.Anything).Return()

	// Test webhook notification operations that should trigger callbacks

	// 1. Notify about a participant joining
	meetingService.NotifyParticipantJoined(meeting.ID, "user1")

	// 2. Notify about a participant leaving
	meetingService.NotifyParticipantLeft(meeting.ID, "user1")

	// 3. Notify about meeting starting
	meetingService.NotifyMeetingStarted(meeting)

	// 4. Notify about meeting ending
	meeting.Status = models.MeetingStatusEnded
	meetingService.NotifyMeetingEnded(meeting)

	// Verify callback was called the expected number of times (4 operations)
	mockCallback.AssertNumberOfCalls(t, "OnUpdate", 4)
}
