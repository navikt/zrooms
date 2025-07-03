package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock implementation of MeetingServicer
type MockMeetingService struct {
	mock.Mock
}

func (m *MockMeetingService) GetMeetingStatusData(ctx context.Context, includeEnded bool) ([]service.MeetingStatusData, error) {
	args := m.Called(ctx, includeEnded)
	return args.Get(0).([]service.MeetingStatusData), args.Error(1)
}

func (m *MockMeetingService) NotifyMeetingStarted(meeting *models.Meeting) {
	m.Called(meeting)
}

func (m *MockMeetingService) NotifyMeetingEnded(meeting *models.Meeting) {
	m.Called(meeting)
}

func (m *MockMeetingService) NotifyParticipantJoined(meetingID string, participantID string) {
	m.Called(meetingID, participantID)
}

func (m *MockMeetingService) NotifyParticipantLeft(meetingID string, participantID string) {
	m.Called(meetingID, participantID)
}

// CreateTestMeeting creates a sample meeting for testing
func CreateTestMeeting() *models.Meeting {
	return &models.Meeting{
		ID:        "96722590573",
		Topic:     "AppSec & Friends",
		StartTime: time.Time{},
		EndTime:   time.Date(2025, 5, 9, 10, 8, 6, 151404620, time.FixedZone("", 2*60*60)),
		Duration:  0,
		Status:    models.MeetingStatusEnded, // Status 3 corresponds to ended
		Host: models.Participant{
			ID:       "",
			Name:     "",
			Email:    "",
			JoinTime: time.Time{},
		},
		Participants: []models.Participant{},
	}
}

func TestNewSSEManager(t *testing.T) {
	// Create a mock meeting service
	mockService := new(MockMeetingService)

	// Create an SSE manager
	sseManager := NewSSEManager(mockService)

	// Verify the manager was created with the expected fields
	assert.NotNil(t, sseManager)
	assert.Equal(t, mockService, sseManager.meetingService)
	assert.NotNil(t, sseManager.broadcast)
}

func TestSSEServeHTTP_CORSPreflight(t *testing.T) {
	// Create a mock meeting service
	mockService := new(MockMeetingService)

	// Create an SSE manager
	sseManager := NewSSEManager(mockService)

	// Create a test recorder
	recorder := httptest.NewRecorder()

	// Create an OPTIONS request for CORS preflight
	request := httptest.NewRequest(http.MethodOptions, "/events", nil)
	request.Header.Set("Origin", "http://example.com")

	// Serve the request
	sseManager.ServeHTTP(recorder, request)

	// Check that CORS headers are set
	assert.Equal(t, "http://example.com", recorder.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", recorder.Header().Get("Access-Control-Allow-Credentials"))
	assert.Equal(t, "Content-Type, Authorization, Cookie", recorder.Header().Get("Access-Control-Allow-Headers"))
	assert.Equal(t, "GET, OPTIONS", recorder.Header().Get("Access-Control-Allow-Methods"))

	// Check that status is OK
	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestSSEServeHTTP_EventStream(t *testing.T) {
	// Create a mock meeting service
	mockService := new(MockMeetingService)

	// Create test meeting data
	meeting := CreateTestMeeting()
	testStatusData := []service.MeetingStatusData{
		{
			Meeting:          meeting, // meeting is a pointer, Meeting field expects a pointer
			ParticipantCount: 0,
		},
	}

	// Set up expectation for GetMeetingStatusData
	mockService.On("GetMeetingStatusData", mock.Anything, mock.AnythingOfType("bool")).Return(testStatusData, nil).Maybe()

	// Create an SSE manager
	sseManager := NewSSEManager(mockService)

	// Create a test recorder
	recorder := httptest.NewRecorder()

	// Create a cancellable context to simulate client disconnection
	ctx, cancel := context.WithCancel(context.Background())

	// Create a GET request with Accept header set for event-stream
	request := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Origin", "http://example.com")
	// Add a test cookie to simulate credentials
	request.AddCookie(&http.Cookie{
		Name:  "test_auth",
		Value: "test_value",
	})

	// Create a done channel to simulate disconnection after checking events
	done := make(chan struct{})

	// Serve the request in a goroutine (will block until client disconnect)
	go func() {
		sseManager.ServeHTTP(recorder, request)
		close(done)
	}()

	// Short delay to ensure events are sent
	time.Sleep(100 * time.Millisecond)

	// Check response headers
	assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", recorder.Header().Get("Cache-Control"))
	assert.Equal(t, "keep-alive", recorder.Header().Get("Connection"))

	// Check CORS headers to ensure credentials are allowed
	assert.Equal(t, "http://example.com", recorder.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", recorder.Header().Get("Access-Control-Allow-Credentials"))

	// Check response body - should contain SSE format events
	responseBody := recorder.Body.String()
	t.Logf("Response body: %s", responseBody)

	// The correct SSE format has "event: eventname" with space after colon
	assert.Contains(t, responseBody, "event: connected")

	// The data should include connected status for the connected event
	assert.Contains(t, responseBody, `data: connected`)

	// Simulate client disconnect by cancelling the context
	cancel()

	// Wait for ServeHTTP to complete
	<-done

}

func TestNotifyMeetingUpdate(t *testing.T) {
	// Create a mock meeting service
	mockService := new(MockMeetingService)

	// Create test meeting data
	meeting := CreateTestMeeting()

	// Create an SSE manager
	sseManager := NewSSEManager(mockService)

	// Call NotifyMeetingUpdate
	sseManager.NotifyMeetingUpdate(meeting)

	// Check that a message was sent to the broadcast channel
	select {
	case message := <-sseManager.broadcast:
		assert.Contains(t, message, "event: update")
		assert.Contains(t, message, "data: update")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected message on broadcast channel")
	}
}

func TestSSEManager_Shutdown(t *testing.T) {
	// Create a mock meeting service
	mockService := new(MockMeetingService)

	// Create an SSE manager
	sseManager := NewSSEManager(mockService)

	// Create a test recorder
	recorder := httptest.NewRecorder()

	// Create a request
	request := httptest.NewRequest(http.MethodGet, "/events", nil)
	request.Header.Set("Accept", "text/event-stream")

	// Start serving SSE in a goroutine
	done := make(chan bool)
	go func() {
		sseManager.ServeHTTP(recorder, request)
		done <- true
	}()

	// Give the SSE connection a moment to start
	time.Sleep(50 * time.Millisecond)

	// Shutdown the SSE manager
	sseManager.Shutdown()

	// Wait for ServeHTTP to complete (should exit due to shutdown)
	select {
	case <-done:
		// Good! ServeHTTP exited
	case <-time.After(2 * time.Second):
		t.Fatal("ServeHTTP did not exit after shutdown within timeout")
	}

	// Verify that the shutdown channel is closed
	select {
	case <-sseManager.shutdown:
		// Good! Channel is closed
	default:
		t.Fatal("Shutdown channel should be closed")
	}
}
