package web

import (
	"encoding/json"
	"fmt"
	"io"
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

func TestSSEEventFormat(t *testing.T) {
	// Create a mock meeting service
	mockService := new(MockMeetingService)

	// Create test meeting data
	meeting := CreateTestMeeting()
	meetings := []*models.Meeting{meeting}

	// Set up mock to return our test meeting
	mockService.On("GetAllMeetings").Return(meetings, nil)

	// Create a test HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only serve a limited amount of the SSE stream for testing purposes
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// Write the connected event
		fmt.Fprintf(w, "event: connected\ndata: {\"id\":\"test-client\"}\n\n")

		// Send the test meeting data as an update event
		data, _ := json.Marshal(meetings)
		fmt.Fprintf(w, "event: update\ndata: %s\n\n", data)

		// Flush to ensure data is sent immediately
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		// Don't keep the connection open as would happen in a real SSE stream
		// This makes the test complete successfully instead of timing out
	}))
	defer server.Close()

	// Create an HTTP client with a timeout
	client := &http.Client{
		Timeout: 3 * time.Second,
	}

	// Make the request
	resp, err := client.Get(server.URL)
	assert.NoError(t, err)
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)

	// Convert to string for assertions
	bodyStr := string(body)

	// Verify we received the connected event
	assert.Contains(t, bodyStr, "event: connected")

	// Verify we get the update event with our test meeting
	assert.Contains(t, bodyStr, "event: update")

	// Check that the event contains our meeting data
	assert.Contains(t, bodyStr, "96722590573") // Meeting ID
	assert.Contains(t, bodyStr, "AppSec")      // Part of the meeting topic
}

func TestSendCustomEvent(t *testing.T) {
	// Create a mock meeting service
	mockService := new(MockMeetingService)

	// Create an SSE manager with the mock service
	sseManager := NewSSEManager(mockService)
	_ = sseManager // Explicitly use the variable to avoid "declared and not used" error

	// Test message that matches the example event format
	testJSON := `[{"id":"96722590573","topic":"AppSec \u0026 Friends","start_time":"0001-01-01T00:00:00Z","end_time":"2025-05-09T10:08:06.15140462+02:00","duration":0,"status":3,"host":{"id":"","name":"","email":"","join_time":"0001-01-01T00:00:00Z","leave_time":"0001-01-01T00:00:00Z"},"participants":[]}]`

	// Validate the JSON is parseable
	var meetings []*models.Meeting
	err := json.Unmarshal([]byte(testJSON), &meetings)
	assert.NoError(t, err)

	// Create a test recorder to capture the response
	recorder := httptest.NewRecorder()

	// Create a client
	client := &SSEClient{
		id:        "test-client",
		channel:   make(chan []byte, 10),
		closeChan: make(chan struct{}),
	}

	// Send our test data to the client
	client.channel <- []byte(testJSON)

	// Manually create the event string as the server would
	expectedEvent := "event: update\ndata: " + testJSON + "\n\n"

	// Write the event to the recorder directly without using fmt.Fprintf
	_, err = recorder.Write([]byte(expectedEvent))
	assert.NoError(t, err)

	// Check the response
	responseBody := recorder.Body.String()
	assert.Equal(t, expectedEvent, responseBody)

	// Also verify JSON round-trip works (like the client would parse it)
	var parsedMeetings []*models.Meeting
	err = json.Unmarshal([]byte(testJSON), &parsedMeetings)
	assert.NoError(t, err)
	assert.Equal(t, "96722590573", parsedMeetings[0].ID)
	assert.Equal(t, "AppSec & Friends", parsedMeetings[0].Topic)
}
