package web

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/navikt/zrooms/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockMeetingServiceTLS is a mock specifically for TLS tests
type MockMeetingServiceTLS struct {
	mock.Mock
}

func (m *MockMeetingServiceTLS) GetAllMeetings() ([]*models.Meeting, error) {
	args := m.Called()
	return args.Get(0).([]*models.Meeting), args.Error(1)
}

func (m *MockMeetingServiceTLS) GetMeeting(id string) (*models.Meeting, error) {
	args := m.Called(id)
	return args.Get(0).(*models.Meeting), args.Error(1)
}

func (m *MockMeetingServiceTLS) UpdateMeeting(meeting *models.Meeting) error {
	args := m.Called(meeting)
	return args.Error(0)
}

func (m *MockMeetingServiceTLS) DeleteMeeting(id string) error {
	args := m.Called(id)
	return args.Error(0)
}

func (m *MockMeetingServiceTLS) RegisterUpdateCallback(fn func(*models.Meeting)) {
	m.Called(fn)
}

// TestSSEConnectionWithCredentials tests the SSE connection with various authentication scenarios
func TestSSEConnectionWithCredentials(t *testing.T) {
	// Create a mock meeting service
	mockService := new(MockMeetingServiceTLS)

	// Set up expectations
	mockService.On("GetAllMeetings").Return([]*models.Meeting{}, nil)
	mockService.On("RegisterUpdateCallback", mock.Anything).Return()

	// Create an SSE manager with the mock service
	sseManager := web.NewSSEManager(mockService)

	// Test cases
	tests := []struct {
		name           string
		tls            *tls.ConnectionState
		protocol       string
		headers        map[string]string
		cookies        []*http.Cookie
		expectedStatus int
	}{
		{
			name:     "Direct TLS with Authentication Cookie",
			tls:      &tls.ConnectionState{HandshakeComplete: true},
			protocol: "HTTP/1.1",
			cookies: []*http.Cookie{
				{Name: "sessionid", Value: "test-session"},
				{Name: "authtoken", Value: "test-auth"},
			},
			headers: map[string]string{
				"Accept": "text/event-stream",
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:     "Proxied HTTPS with Authentication Cookie",
			protocol: "HTTP/1.1",
			headers: map[string]string{
				"X-Forwarded-Proto": "https",
				"Accept":            "text/event-stream",
			},
			cookies: []*http.Cookie{
				{Name: "sessionid", Value: "test-session"},
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:     "HTTP with Authentication Cookie",
			protocol: "HTTP/1.1",
			headers: map[string]string{
				"Accept": "text/event-stream",
			},
			cookies: []*http.Cookie{
				{Name: "sessionid", Value: "test-session"},
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:     "HTTP/2 with Authorization Header",
			protocol: "HTTP/2.0",
			headers: map[string]string{
				"Accept":        "text/event-stream",
				"Authorization": "Bearer test-token",
			},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test recorder
			recorder := httptest.NewRecorder()

			// Create a request with the specified configuration
			request := httptest.NewRequest(http.MethodGet, "/events", nil)

			// Set protocol
			if tt.protocol == "HTTP/1.1" {
				request.ProtoMajor = 1
				request.ProtoMinor = 1
			} else if tt.protocol == "HTTP/2.0" {
				request.ProtoMajor = 2
				request.ProtoMinor = 0
			}

			// Set TLS state if provided
			request.TLS = tt.tls

			// Add headers
			for k, v := range tt.headers {
				request.Header.Set(k, v)
			}

			// Add cookies
			for _, cookie := range tt.cookies {
				request.AddCookie(cookie)
			}

			// We can't test the full SSE connection here because it runs in an infinite loop,
			// but we can check that the initial setup and headers are correct
			sseManager.ServeHTTP(recorder, request)

			// Check if the response status code is as expected
			response := recorder.Result()
			assert.Equal(t, tt.expectedStatus, response.StatusCode)

			// Check content type
			assert.Equal(t, "text/event-stream; charset=utf-8", response.Header.Get("Content-Type"))

			// Check if Access-Control-Allow-Credentials is set
			assert.Equal(t, "true", response.Header.Get("Access-Control-Allow-Credentials"))
		})
	}
}
