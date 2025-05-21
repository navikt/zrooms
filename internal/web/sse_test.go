package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/navikt/zrooms/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock implementation of MeetingServicer
type MockMeetingService struct {
	mock.Mock
}

func (m *MockMeetingService) GetAllMeetings() ([]*models.Meeting, error) {
	args := m.Called()
	return args.Get(0).([]*models.Meeting), args.Error(1)
}

func (m *MockMeetingService) GetMeeting(id string) (*models.Meeting, error) {
	args := m.Called(id)
	return args.Get(0).(*models.Meeting), args.Error(1)
}

func (m *MockMeetingService) UpdateMeeting(meeting *models.Meeting) error {
	args := m.Called(meeting)
	return args.Error(0)
}

func (m *MockMeetingService) DeleteMeeting(id string) error {
	args := m.Called(id)
	return args.Error(0)
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
	assert.NotNil(t, sseManager.clients)
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
	meetings := []*models.Meeting{meeting}

	// Set up expectation for GetAllMeetings - Note this is now optional with HTMX approach
	// but keeping it for backward compatibility with tests
	mockService.On("GetAllMeetings").Return(meetings, nil).Maybe()

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
	assert.Equal(t, "no-cache, no-transform", recorder.Header().Get("Cache-Control"))
	assert.Equal(t, "keep-alive", recorder.Header().Get("Connection"))
	
	// Check CORS headers to ensure credentials are allowed
	assert.Equal(t, "http://example.com", recorder.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", recorder.Header().Get("Access-Control-Allow-Credentials"))

	// Check response body - should contain SSE format events
	responseBody := recorder.Body.String()
	t.Logf("Response body: %s", responseBody)

	// The SSE format from gin-contrib/sse has "event:eventname" without space
	assert.Contains(t, responseBody, "event:connected")
	assert.Contains(t, responseBody, "event:initial-load")

	// The data should include client ID for the connected event
	assert.Contains(t, responseBody, `data:{"id":`)

	// With HTMX implementation, the initial-load event should contain the expected data
	assert.Contains(t, responseBody, `Load initial data`)

	// Simulate client disconnect by cancelling the context
	cancel()

	// Wait for ServeHTTP to complete
	<-done

	// We no longer need to verify GetAllMeetings was called since
	// our HTMX implementation uses a different approach
	// mockService.AssertExpectations(t)
}

func TestNotifyMeetingUpdate(t *testing.T) {
	// Create a mock meeting service
	mockService := new(MockMeetingService)

	// Create test meeting data
	meeting := CreateTestMeeting()

	// In our HTMX implementation, we don't use GetAllMeetings anymore
	// mockService.On("GetAllMeetings").Return(meetings, nil)

	// Create an SSE manager
	sseManager := NewSSEManager(mockService)

	// Create a test client - this tests the manager's internal state only
	// as we can't verify the output without a real connection
	clientID := "test-client"
	responseRecorder := httptest.NewRecorder()
	testClient := &SSEClient{
		id:             clientID,
		responseWriter: responseRecorder,
		disconnected:   make(chan struct{}),
	}

	// Add the test client to the manager
	sseManager.clientsMutex.Lock()
	sseManager.clients[clientID] = testClient
	sseManager.clientsMutex.Unlock()

	// Call NotifyMeetingUpdate
	sseManager.NotifyMeetingUpdate(meeting)

	// Check that the client received the update event
	responseBody := responseRecorder.Body.String()
	assert.Contains(t, responseBody, "event:update")
	assert.Contains(t, responseBody, "Update available")
}

func TestIsEventStreamSupported(t *testing.T) {
	// Test with empty Accept header
	emptyRequest := httptest.NewRequest(http.MethodGet, "/events", nil)
	assert.True(t, isEventStreamSupported(emptyRequest), "Empty Accept header should be supported")

	// Test with wildcard Accept header
	wildcardRequest := httptest.NewRequest(http.MethodGet, "/events", nil)
	wildcardRequest.Header.Set("Accept", "*/*")
	assert.True(t, isEventStreamSupported(wildcardRequest), "Wildcard Accept header should be supported")

	// Test with explicit event-stream Accept header
	eventStreamRequest := httptest.NewRequest(http.MethodGet, "/events", nil)
	eventStreamRequest.Header.Set("Accept", "text/event-stream")
	assert.True(t, isEventStreamSupported(eventStreamRequest), "text/event-stream Accept header should be supported")

	// Test with incompatible Accept header
	incompatibleRequest := httptest.NewRequest(http.MethodGet, "/events", nil)
	incompatibleRequest.Header.Set("Accept", "application/json")
	assert.False(t, isEventStreamSupported(incompatibleRequest), "application/json Accept header should not be supported")
}
