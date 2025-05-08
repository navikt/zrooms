package api_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/navikt/zrooms/internal/api"
	"github.com/stretchr/testify/assert"
)

func TestOAuthRedirectHandler(t *testing.T) {
	// Set test environment variables
	os.Setenv("ZOOM_CLIENT_ID", "test_client_id")
	os.Setenv("ZOOM_CLIENT_SECRET", "test_client_secret")
	os.Setenv("ZOOM_REDIRECT_URI", "http://localhost:8080/oauth/redirect")

	// Clean up environment variables after tests
	defer func() {
		os.Unsetenv("ZOOM_CLIENT_ID")
		os.Unsetenv("ZOOM_CLIENT_SECRET")
		os.Unsetenv("ZOOM_REDIRECT_URI")
	}()

	tests := []struct {
		name           string
		queryParams    map[string]string
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name: "Successful OAuth Callback",
			queryParams: map[string]string{
				"code":  "some_auth_code",
				"state": "some_state_token",
			},
			expectedStatus: http.StatusOK,
			expectSuccess:  true,
		},
		{
			name: "Missing Authorization Code",
			queryParams: map[string]string{
				"state": "some_state_token",
			},
			expectedStatus: http.StatusBadRequest,
			expectSuccess:  false,
		},
		{
			name: "Missing State Token",
			queryParams: map[string]string{
				"code": "some_auth_code",
			},
			expectedStatus: http.StatusBadRequest,
			expectSuccess:  false,
		},
		{
			name:           "Empty Request",
			queryParams:    map[string]string{},
			expectedStatus: http.StatusBadRequest,
			expectSuccess:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a request with query parameters
			req := httptest.NewRequest("GET", "/oauth/redirect", nil)
			q := req.URL.Query()
			for key, value := range tt.queryParams {
				q.Add(key, value)
			}
			req.URL.RawQuery = q.Encode()

			// Create a response recorder
			rr := httptest.NewRecorder()

			// Create handler
			handler := http.HandlerFunc(api.OAuthRedirectHandler)

			// Serve the request
			handler.ServeHTTP(rr, req)

			// Check status code
			assert.Equal(t, tt.expectedStatus, rr.Code)

			// For successful cases, check for specific success message
			if tt.expectSuccess {
				assert.Contains(t, rr.Body.String(), "Authorization successful")
			}
		})
	}
}
