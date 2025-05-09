package web

import (
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
	assert.NotNil(t, sseManager.server)
	assert.Equal(t, mockService, sseManager.meetingService)
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

	// Serve the request
	sseManager.ServeHTTP(recorder, request)

	// Check that CORS headers are set
	assert.Equal(t, "*", recorder.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "Content-Type", recorder.Header().Get("Access-Control-Allow-Headers"))
	assert.Equal(t, "GET, OPTIONS", recorder.Header().Get("Access-Control-Allow-Methods"))

	// Check that status is OK
	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestNotifyMeetingUpdate(t *testing.T) {
	// Create a mock meeting service
	mockService := new(MockMeetingService)

	// Create test meeting data
	meeting := CreateTestMeeting()
	meetings := []*models.Meeting{meeting}

	// Set up expectation for GetAllMeetings
	mockService.On("GetAllMeetings").Return(meetings, nil)

	// Create an SSE manager
	sseManager := NewSSEManager(mockService)

	// Call NotifyMeetingUpdate
	sseManager.NotifyMeetingUpdate(meeting)

	// Verify that GetAllMeetings was called
	mockService.AssertExpectations(t)
}
