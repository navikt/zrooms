package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/navikt/zrooms/internal/api"
	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository/memory"
	"github.com/navikt/zrooms/internal/service"
	"github.com/navikt/zrooms/internal/web"
)

// TestEventCallback captures calls to the meeting service callbacks
type TestEventCallback struct {
	mu     sync.RWMutex
	events []CallbackEvent
}

type CallbackEvent struct {
	Type      string
	Meeting   *models.Meeting
	MeetingID string
	Timestamp time.Time
}

func (t *TestEventCallback) OnMeetingUpdate(meeting *models.Meeting) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, CallbackEvent{
		Type:      "meeting_update",
		Meeting:   meeting,
		Timestamp: time.Now(),
	})
}

func (t *TestEventCallback) GetEvents() []CallbackEvent {
	t.mu.RLock()
	defer t.mu.RUnlock()
	events := make([]CallbackEvent, len(t.events))
	copy(events, t.events)
	return events
}

func (t *TestEventCallback) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = nil
}

func (t *TestEventCallback) WaitForEvents(count int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		t.mu.RLock()
		current := len(t.events)
		t.mu.RUnlock()
		if current >= count {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// IntegrationTestSuite contains the complete application setup for integration testing
type IntegrationTestSuite struct {
	repo           *memory.Repository
	meetingService *service.MeetingService
	webhookHandler *api.WebhookHandler
	webHandler     *web.Handler
	server         *httptest.Server
	callback       *TestEventCallback
}

func setupIntegrationTest(t *testing.T) *IntegrationTestSuite {
	// Create in-memory repository
	repo := memory.NewRepository()

	// Create meeting service
	meetingService := service.NewMeetingService(repo)

	// Create test callback
	callback := &TestEventCallback{}
	meetingService.RegisterUpdateCallback(callback.OnMeetingUpdate)

	// Create web handler - try multiple template paths
	var webHandler *web.Handler
	var err error

	templatePaths := []string{
		"./tests/templates",
		"../tests/templates",
		"./internal/web/templates",
		"../internal/web/templates",
	}

	for _, path := range templatePaths {
		webHandler, err = web.NewHandler(meetingService, path)
		if err == nil {
			break
		}
	}

	if err != nil {
		// For testing, we can continue without templates
		t.Logf("Template loading failed (continuing without web handler): %v", err)
		webHandler = nil
	}

	// Register SSE callback if web handler was created successfully
	if webHandler != nil {
		meetingService.RegisterUpdateCallback(webHandler.NotifyMeetingUpdate)
	}

	// Create webhook handler
	webhookHandler := api.NewWebhookHandler(repo, meetingService)

	// Set up routes
	mux := api.SetupRoutes(repo, meetingService)
	if webHandler != nil {
		webHandler.SetupRoutes(mux)
	}

	// Create test server
	server := httptest.NewServer(mux)

	return &IntegrationTestSuite{
		repo:           repo,
		meetingService: meetingService,
		webhookHandler: webhookHandler,
		webHandler:     webHandler,
		server:         server,
		callback:       callback,
	}
}

func (suite *IntegrationTestSuite) Close() {
	if suite.server != nil {
		suite.server.Close()
	}
}

func (suite *IntegrationTestSuite) sendWebhookEvent(t *testing.T, eventType string, payload interface{}) *http.Response {
	webhookEvent := map[string]interface{}{
		"event":   eventType,
		"payload": payload,
	}

	jsonData, err := json.Marshal(webhookEvent)
	require.NoError(t, err)

	resp, err := http.Post(
		suite.server.URL+"/webhook",
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	require.NoError(t, err)

	return resp
}

// TestCompleteWorkflow tests the entire application workflow for all supported events
func TestCompleteWorkflow(t *testing.T) {
	suite := setupIntegrationTest(t)
	defer suite.Close()

	ctx := context.Background()
	meetingID := "meeting-12345"
	participantID1 := "participant-001"
	participantID2 := "participant-002"

	t.Run("Meeting Started Event", func(t *testing.T) {
		// Clear any previous events
		suite.callback.Clear()

		// Send meeting started event
		payload := map[string]interface{}{
			"account_id": "test-account",
			"object": map[string]interface{}{
				"uuid":     "uuid-12345",
				"id":       meetingID,
				"host_id":  "host-123",
				"topic":    "Integration Test Meeting",
				"type":     2,
				"duration": 60,
			},
		}

		resp := suite.sendWebhookEvent(t, "meeting.started", payload)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		resp.Body.Close()

		// Wait for callback to be triggered
		assert.True(t, suite.callback.WaitForEvents(1, time.Second*2), "Expected callback to be triggered")

		// Verify meeting was saved in repository
		meeting, err := suite.repo.GetMeeting(ctx, meetingID)
		require.NoError(t, err)
		assert.Equal(t, meetingID, meeting.ID)
		assert.Equal(t, "Integration Test Meeting", meeting.Topic)
		assert.Equal(t, models.MeetingStatusStarted, meeting.Status)
		assert.False(t, meeting.StartTime.IsZero())

		// Verify participant count is 0 initially
		count, err := suite.repo.CountParticipantsInMeeting(ctx, meetingID)
		require.NoError(t, err)
		assert.Equal(t, 0, count)

		// Verify meeting service data
		statusData, err := suite.meetingService.GetMeetingStatusData(ctx, false)
		require.NoError(t, err)
		assert.Len(t, statusData, 1)
		assert.Equal(t, "in_progress", statusData[0].Status)
		assert.Equal(t, 0, statusData[0].ParticipantCount)

		// Verify callback was triggered correctly
		events := suite.callback.GetEvents()
		assert.Len(t, events, 1)
		assert.Equal(t, "meeting_update", events[0].Type)
		assert.Equal(t, meetingID, events[0].Meeting.ID)
	})

	t.Run("First Participant Joins", func(t *testing.T) {
		suite.callback.Clear()

		// Send participant joined event
		payload := map[string]interface{}{
			"account_id": "test-account",
			"object": map[string]interface{}{
				"uuid":    "uuid-12345",
				"id":      meetingID,
				"host_id": "host-123",
				"participant": map[string]interface{}{
					"id":        participantID1,
					"user_id":   "user-001",
					"user_name": "Test User 1",
					"email":     "user1@example.com",
				},
			},
		}

		resp := suite.sendWebhookEvent(t, "meeting.participant_joined", payload)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		resp.Body.Close()

		// Wait for callback
		assert.True(t, suite.callback.WaitForEvents(1, time.Second*2))

		// Verify participant was added to repository
		count, err := suite.repo.CountParticipantsInMeeting(ctx, meetingID)
		require.NoError(t, err)
		assert.Equal(t, 1, count)

		// Verify meeting service reflects the change
		statusData, err := suite.meetingService.GetMeetingStatusData(ctx, false)
		require.NoError(t, err)
		assert.Len(t, statusData, 1)
		assert.Equal(t, 1, statusData[0].ParticipantCount)

		// Verify callback was triggered
		events := suite.callback.GetEvents()
		assert.Len(t, events, 1)
		assert.Equal(t, meetingID, events[0].Meeting.ID)
	})

	t.Run("Second Participant Joins", func(t *testing.T) {
		suite.callback.Clear()

		// Send second participant joined event
		payload := map[string]interface{}{
			"account_id": "test-account",
			"object": map[string]interface{}{
				"uuid":    "uuid-12345",
				"id":      meetingID,
				"host_id": "host-123",
				"participant": map[string]interface{}{
					"id":        participantID2,
					"user_id":   "user-002",
					"user_name": "Test User 2",
					"email":     "user2@example.com",
				},
			},
		}

		resp := suite.sendWebhookEvent(t, "meeting.participant_joined", payload)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		resp.Body.Close()

		// Wait for callback
		assert.True(t, suite.callback.WaitForEvents(1, time.Second*2))

		// Verify both participants are counted
		count, err := suite.repo.CountParticipantsInMeeting(ctx, meetingID)
		require.NoError(t, err)
		assert.Equal(t, 2, count)

		// Verify meeting service reflects the change
		statusData, err := suite.meetingService.GetMeetingStatusData(ctx, false)
		require.NoError(t, err)
		assert.Len(t, statusData, 1)
		assert.Equal(t, 2, statusData[0].ParticipantCount)
	})

	t.Run("First Participant Leaves", func(t *testing.T) {
		suite.callback.Clear()

		// Send participant left event
		payload := map[string]interface{}{
			"account_id": "test-account",
			"object": map[string]interface{}{
				"uuid":    "uuid-12345",
				"id":      meetingID,
				"host_id": "host-123",
				"participant": map[string]interface{}{
					"id":        participantID1,
					"user_id":   "user-001",
					"user_name": "Test User 1",
					"email":     "user1@example.com",
				},
			},
		}

		resp := suite.sendWebhookEvent(t, "meeting.participant_left", payload)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		resp.Body.Close()

		// Wait for callback
		assert.True(t, suite.callback.WaitForEvents(1, time.Second*2))

		// Verify participant count decreased
		count, err := suite.repo.CountParticipantsInMeeting(ctx, meetingID)
		require.NoError(t, err)
		assert.Equal(t, 1, count)

		// Verify meeting service reflects the change
		statusData, err := suite.meetingService.GetMeetingStatusData(ctx, false)
		require.NoError(t, err)
		assert.Len(t, statusData, 1)
		assert.Equal(t, 1, statusData[0].ParticipantCount)
	})

	t.Run("Meeting Ends", func(t *testing.T) {
		suite.callback.Clear()

		// Send meeting ended event
		payload := map[string]interface{}{
			"account_id": "test-account",
			"object": map[string]interface{}{
				"uuid":    "uuid-12345",
				"id":      meetingID,
				"host_id": "host-123",
				"topic":   "Integration Test Meeting",
				"type":    2,
			},
		}

		resp := suite.sendWebhookEvent(t, "meeting.ended", payload)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		resp.Body.Close()

		// Wait for callback
		assert.True(t, suite.callback.WaitForEvents(1, time.Second*2))

		// Verify meeting status is ended
		meeting, err := suite.repo.GetMeeting(ctx, meetingID)
		require.NoError(t, err)
		assert.Equal(t, models.MeetingStatusEnded, meeting.Status)
		assert.False(t, meeting.EndTime.IsZero())

		// Verify all participants were cleared
		count, err := suite.repo.CountParticipantsInMeeting(ctx, meetingID)
		require.NoError(t, err)
		assert.Equal(t, 0, count)

		// Verify meeting service reflects ended state (no active meetings)
		statusData, err := suite.meetingService.GetMeetingStatusData(ctx, false)
		require.NoError(t, err)
		assert.Len(t, statusData, 0) // Ended meetings not included in active list

		// Verify meeting appears when including ended meetings
		statusDataWithEnded, err := suite.meetingService.GetMeetingStatusData(ctx, true)
		require.NoError(t, err)
		assert.Len(t, statusDataWithEnded, 1)
		assert.Equal(t, "ended", statusDataWithEnded[0].Status)
		assert.Equal(t, 0, statusDataWithEnded[0].ParticipantCount)
	})
}

// TestMultipleMeetings tests handling multiple concurrent meetings
func TestMultipleMeetings(t *testing.T) {
	suite := setupIntegrationTest(t)
	defer suite.Close()

	ctx := context.Background()
	meeting1ID := "meeting-001"
	meeting2ID := "meeting-002"

	// Start first meeting
	payload1 := map[string]interface{}{
		"account_id": "test-account",
		"object": map[string]interface{}{
			"uuid":     "uuid-001",
			"id":       meeting1ID,
			"host_id":  "host-001",
			"topic":    "Meeting 1",
			"type":     2,
			"duration": 60,
		},
	}

	resp := suite.sendWebhookEvent(t, "meeting.started", payload1)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Start second meeting
	payload2 := map[string]interface{}{
		"account_id": "test-account",
		"object": map[string]interface{}{
			"uuid":     "uuid-002",
			"id":       meeting2ID,
			"host_id":  "host-002",
			"topic":    "Meeting 2",
			"type":     2,
			"duration": 30,
		},
	}

	resp = suite.sendWebhookEvent(t, "meeting.started", payload2)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Add participants to both meetings
	// Meeting 1: 2 participants
	for i := 1; i <= 2; i++ {
		participantPayload := map[string]interface{}{
			"account_id": "test-account",
			"object": map[string]interface{}{
				"uuid":    "uuid-001",
				"id":      meeting1ID,
				"host_id": "host-001",
				"participant": map[string]interface{}{
					"id":        fmt.Sprintf("participant-1-%02d", i),
					"user_id":   fmt.Sprintf("user-1-%02d", i),
					"user_name": fmt.Sprintf("User 1-%d", i),
					"email":     fmt.Sprintf("user1-%d@example.com", i),
				},
			},
		}

		resp = suite.sendWebhookEvent(t, "meeting.participant_joined", participantPayload)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		resp.Body.Close()
	}

	// Meeting 2: 3 participants
	for i := 1; i <= 3; i++ {
		participantPayload := map[string]interface{}{
			"account_id": "test-account",
			"object": map[string]interface{}{
				"uuid":    "uuid-002",
				"id":      meeting2ID,
				"host_id": "host-002",
				"participant": map[string]interface{}{
					"id":        fmt.Sprintf("participant-2-%02d", i),
					"user_id":   fmt.Sprintf("user-2-%02d", i),
					"user_name": fmt.Sprintf("User 2-%d", i),
					"email":     fmt.Sprintf("user2-%d@example.com", i),
				},
			},
		}

		resp = suite.sendWebhookEvent(t, "meeting.participant_joined", participantPayload)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		resp.Body.Close()
	}

	// Give time for all events to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify both meetings exist and have correct participant counts
	statusData, err := suite.meetingService.GetMeetingStatusData(ctx, false)
	require.NoError(t, err)
	assert.Len(t, statusData, 2)

	// Find meetings by ID and verify counts
	var meeting1Data, meeting2Data *service.MeetingStatusData
	for i := range statusData {
		if statusData[i].Meeting.ID == meeting1ID {
			meeting1Data = &statusData[i]
		} else if statusData[i].Meeting.ID == meeting2ID {
			meeting2Data = &statusData[i]
		}
	}

	require.NotNil(t, meeting1Data, "Meeting 1 should exist")
	require.NotNil(t, meeting2Data, "Meeting 2 should exist")

	assert.Equal(t, 2, meeting1Data.ParticipantCount)
	assert.Equal(t, 3, meeting2Data.ParticipantCount)
	assert.Equal(t, "in_progress", meeting1Data.Status)
	assert.Equal(t, "in_progress", meeting2Data.Status)

	// End first meeting
	endPayload1 := map[string]interface{}{
		"account_id": "test-account",
		"object": map[string]interface{}{
			"uuid":    "uuid-001",
			"id":      meeting1ID,
			"host_id": "host-001",
			"topic":   "Meeting 1",
			"type":    2,
		},
	}

	resp = suite.sendWebhookEvent(t, "meeting.ended", endPayload1)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Give time for event to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify only one active meeting remains
	statusData, err = suite.meetingService.GetMeetingStatusData(ctx, false)
	require.NoError(t, err)
	assert.Len(t, statusData, 1)
	assert.Equal(t, meeting2ID, statusData[0].Meeting.ID)
	assert.Equal(t, 3, statusData[0].ParticipantCount)
}

// TestUnsupportedEvents tests that unsupported events are handled gracefully
func TestUnsupportedEvents(t *testing.T) {
	suite := setupIntegrationTest(t)
	defer suite.Close()

	// Send an unsupported event type
	payload := map[string]interface{}{
		"account_id": "test-account",
		"object": map[string]interface{}{
			"id": "meeting-12345",
		},
	}

	resp := suite.sendWebhookEvent(t, "meeting.unsupported_event", payload)
	assert.Equal(t, http.StatusOK, resp.StatusCode) // Should still return OK
	resp.Body.Close()

	// Verify no meetings were created
	ctx := context.Background()
	statusData, err := suite.meetingService.GetMeetingStatusData(ctx, false)
	require.NoError(t, err)
	assert.Len(t, statusData, 0)
}

// TestCallbackPropagation verifies that all callbacks are properly triggered
func TestCallbackPropagation(t *testing.T) {
	suite := setupIntegrationTest(t)
	defer suite.Close()

	meetingID := "callback-test-meeting"

	// Start meeting
	payload := map[string]interface{}{
		"account_id": "test-account",
		"object": map[string]interface{}{
			"uuid":     "uuid-callback",
			"id":       meetingID,
			"host_id":  "host-callback",
			"topic":    "Callback Test Meeting",
			"type":     2,
			"duration": 60,
		},
	}

	resp := suite.sendWebhookEvent(t, "meeting.started", payload)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Wait for callback and verify it was triggered
	assert.True(t, suite.callback.WaitForEvents(1, time.Second*2))
	events := suite.callback.GetEvents()
	assert.Len(t, events, 1)
	assert.Equal(t, "meeting_update", events[0].Type)
	assert.Equal(t, meetingID, events[0].Meeting.ID)
	assert.Equal(t, models.MeetingStatusStarted, events[0].Meeting.Status)

	// Clear events for next test
	suite.callback.Clear()

	// Add participant
	participantPayload := map[string]interface{}{
		"account_id": "test-account",
		"object": map[string]interface{}{
			"uuid":    "uuid-callback",
			"id":      meetingID,
			"host_id": "host-callback",
			"participant": map[string]interface{}{
				"id":        "callback-participant",
				"user_id":   "callback-user",
				"user_name": "Callback User",
				"email":     "callback@example.com",
			},
		},
	}

	resp = suite.sendWebhookEvent(t, "meeting.participant_joined", participantPayload)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Verify participant join callback
	assert.True(t, suite.callback.WaitForEvents(1, time.Second*2))
	events = suite.callback.GetEvents()
	assert.Len(t, events, 1)
	assert.Equal(t, meetingID, events[0].Meeting.ID)

	// Clear and test participant leave
	suite.callback.Clear()

	resp = suite.sendWebhookEvent(t, "meeting.participant_left", participantPayload)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Verify participant leave callback
	assert.True(t, suite.callback.WaitForEvents(1, time.Second*2))
	events = suite.callback.GetEvents()
	assert.Len(t, events, 1)
	assert.Equal(t, meetingID, events[0].Meeting.ID)

	// Clear and test meeting end
	suite.callback.Clear()

	endPayload := map[string]interface{}{
		"account_id": "test-account",
		"object": map[string]interface{}{
			"uuid":    "uuid-callback",
			"id":      meetingID,
			"host_id": "host-callback",
			"topic":   "Callback Test Meeting",
			"type":    2,
		},
	}

	resp = suite.sendWebhookEvent(t, "meeting.ended", endPayload)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Verify meeting end callback
	assert.True(t, suite.callback.WaitForEvents(1, time.Second*2))
	events = suite.callback.GetEvents()
	assert.Len(t, events, 1)
	assert.Equal(t, meetingID, events[0].Meeting.ID)
	assert.Equal(t, models.MeetingStatusEnded, events[0].Meeting.Status)
}
