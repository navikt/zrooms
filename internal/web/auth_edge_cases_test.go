package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestSecurityBoundaries tests additional security edge cases
func TestSecurityBoundaries(t *testing.T) {
	mock := NewMockIntrospectionServer()
	defer mock.Close()

	validToken := "valid-security-token"
	authorizedNavIdent := "TESTADMIN123"
	mock.AddValidTokenWithNavIdent(validToken, authorizedNavIdent)

	// Set environment for auth middleware
	oldEnv := os.Getenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT")
	os.Setenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT", mock.URL())
	defer func() {
		if oldEnv != "" {
			os.Setenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT", oldEnv)
		} else {
			os.Unsetenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT")
		}
	}()

	// Set up authorized admin list
	oldAdminEnv := os.Getenv("NAV_IDENT_ADMINS")
	os.Setenv("NAV_IDENT_ADMINS", "TESTADMIN123,ADMIN456")
	defer func() {
		if oldAdminEnv != "" {
			os.Setenv("NAV_IDENT_ADMINS", oldAdminEnv)
		} else {
			os.Unsetenv("NAV_IDENT_ADMINS")
		}
	}()

	auth := NewAuthMiddleware()

	// Simple test handler
	testHandler := auth.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Authenticated"))
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/admin", testHandler)

	t.Run("HTTP Header Injection", func(t *testing.T) {
		// Test various header injection attempts
		maliciousHeaders := map[string]string{
			"Authorization": "Bearer token\r\nX-Injected: evil",
			"X-Custom":      "value\r\nAuthorization: Bearer " + validToken,
		}

		for header, value := range maliciousHeaders {
			req := httptest.NewRequest("GET", "/admin", nil)
			req.Header.Set(header, value)

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			// Should not be authenticated via injection
			if w.Code == http.StatusOK {
				t.Errorf("Header injection attempt succeeded: %s: %s", header, value)
			}
		}
	})

	t.Run("Case Sensitivity", func(t *testing.T) {
		caseVariations := []string{
			"bearer " + validToken,
			"BEARER " + validToken,
			"Bearer " + validToken, // This should work
			"bEaReR " + validToken,
		}

		for _, auth := range caseVariations {
			req := httptest.NewRequest("GET", "/admin", nil)
			req.Header.Set("Authorization", auth)

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			// Only exact "Bearer " should work
			if auth == "Bearer "+validToken {
				if w.Code != http.StatusOK {
					t.Errorf("Valid Bearer token should work, got %d", w.Code)
				}
			} else {
				if w.Code == http.StatusOK {
					t.Errorf("Case variation should not work: %s", auth)
				}
			}
		}
	})

	t.Run("Unicode and Encoding Attacks", func(t *testing.T) {
		unicodeTokens := []string{
			"token\u202eEVIL",   // Right-to-left override
			"token\u200bHIDDEN", // Zero-width space
			"token\ufeffBOM",    // Byte order mark
			"token\u0000NULL",   // Null byte
			"tⲟken",             // Unicode lookalike
			"tok𝐞n",             // Mathematical alphanumeric
		}

		for _, token := range unicodeTokens {
			req := httptest.NewRequest("GET", "/admin", nil)
			req.Header.Set("Authorization", "Bearer "+token)

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			// Should not authenticate with unicode tricks
			if w.Code == http.StatusOK {
				t.Errorf("Unicode token should not authenticate: %s", token)
			}
		}
	})

	t.Run("Content-Type Confusion", func(t *testing.T) {
		// Test if changing content type affects authentication
		req := httptest.NewRequest("GET", "/admin", nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Content-Type should not affect auth for GET requests")
		}
	})

	t.Run("Multiple Authorization Headers", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/admin", nil)
		req.Header.Add("Authorization", "Bearer invalid-token")
		req.Header.Add("Authorization", "Bearer "+validToken)

		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		// Should use the first header (invalid), not the second
		if w.Code == http.StatusOK {
			t.Errorf("Multiple auth headers should not bypass security")
		}
	})

	t.Run("Path Traversal Attempts", func(t *testing.T) {
		pathTraversalAttempts := []string{
			"/admin/../admin/meetings",
			"/admin/./meetings",
			"/admin/meetings/../../../etc/passwd",
			"/admin/meetings/%2e%2e%2fadmin",
			"/admin/meetings/..%2fmeetings",
		}

		for _, path := range pathTraversalAttempts {
			req := httptest.NewRequest("GET", path, nil)
			req.Header.Set("Authorization", "Bearer "+validToken)

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			// Most path traversal attempts should result in 404 (not found)
			// Only /admin should return 200 with "Authenticated"
			body := w.Body.String()
			if w.Code == 200 && body == "Authenticated" && path != "/admin" {
				t.Errorf("Path traversal may have succeeded for: %s (got 200 with 'Authenticated')", path)
			}

			// Response body should never contain actual sensitive file content
			// (This would only happen if path traversal actually worked)
			dangerousPatterns := []string{
				"root:x:0:0:", // /etc/passwd pattern
				"daemon:x:",   // Another passwd pattern
				"#!/bin/bash", // Shell script
				"password",    // Password file content
				"ssh-rsa",     // SSH keys
			}
			for _, pattern := range dangerousPatterns {
				if strings.Contains(body, pattern) {
					t.Errorf("Response contains sensitive file content for path %s: found pattern %s", path, pattern)
				}
			}
		}
	})
}

func TestAuthenticationTiming(t *testing.T) {
	mock := NewMockIntrospectionServer()
	defer mock.Close()

	validToken := "timing-test-token"
	authorizedNavIdent := "TIMINGTEST123"
	mock.AddValidTokenWithNavIdent(validToken, authorizedNavIdent)

	// Set environment for auth middleware
	oldEnv := os.Getenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT")
	os.Setenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT", mock.URL())
	defer func() {
		if oldEnv != "" {
			os.Setenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT", oldEnv)
		} else {
			os.Unsetenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT")
		}
	}()

	// Set up authorized admin list
	oldAdminEnv := os.Getenv("NAV_IDENT_ADMINS")
	os.Setenv("NAV_IDENT_ADMINS", "TIMINGTEST123,ADMIN456")
	defer func() {
		if oldAdminEnv != "" {
			os.Setenv("NAV_IDENT_ADMINS", oldAdminEnv)
		} else {
			os.Unsetenv("NAV_IDENT_ADMINS")
		}
	}()

	auth := NewAuthMiddleware()
	testHandler := auth.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Authenticated"))
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/admin", testHandler)

	t.Run("Timing Attack Resistance", func(t *testing.T) {
		// Measure timing for invalid vs missing tokens
		iterations := 5

		// Test missing token
		var missingTokenTimes []time.Duration
		for i := 0; i < iterations; i++ {
			start := time.Now()

			req := httptest.NewRequest("GET", "/admin", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			missingTokenTimes = append(missingTokenTimes, time.Since(start))
		}

		// Test invalid token (requires network call)
		var invalidTokenTimes []time.Duration
		for i := 0; i < iterations; i++ {
			start := time.Now()

			req := httptest.NewRequest("GET", "/admin", nil)
			req.Header.Set("Authorization", "Bearer invalid-token")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			invalidTokenTimes = append(invalidTokenTimes, time.Since(start))
		}

		// Calculate averages
		var avgMissing, avgInvalid time.Duration
		for _, d := range missingTokenTimes {
			avgMissing += d
		}
		avgMissing /= time.Duration(len(missingTokenTimes))

		for _, d := range invalidTokenTimes {
			avgInvalid += d
		}
		avgInvalid /= time.Duration(len(invalidTokenTimes))

		t.Logf("Average time for missing token: %v", avgMissing)
		t.Logf("Average time for invalid token: %v", avgInvalid)

		// Invalid token should take longer (network call), but not excessively
		if avgInvalid < avgMissing {
			t.Errorf("Invalid token processing should take longer than missing token")
		}

		// Should not take more than 30 seconds even with network call
		if avgInvalid > 30*time.Second {
			t.Errorf("Token validation taking too long: %v", avgInvalid)
		}
	})
}

func TestErrorMessageSecurity(t *testing.T) {
	mock := NewMockIntrospectionServer()
	defer mock.Close()

	// Set environment for auth middleware
	oldEnv := os.Getenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT")
	os.Setenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT", mock.URL())
	defer func() {
		if oldEnv != "" {
			os.Setenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT", oldEnv)
		} else {
			os.Unsetenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT")
		}
	}()

	auth := NewAuthMiddleware()
	testHandler := auth.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Authenticated"))
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/admin", testHandler)

	t.Run("Error Message Information Disclosure", func(t *testing.T) {
		// Test that error messages don't leak sensitive information
		testCases := []struct {
			name        string
			setupFunc   func()
			authHeader  string
			expectPaths []string // Paths that should NOT appear in error messages
		}{
			{
				name:       "Invalid Token",
				authHeader: "Bearer invalid-token-12345",
				expectPaths: []string{
					mock.URL(),
					"azuread",
					"introspection",
					"internal/",
				},
			},
			{
				name: "Introspection Server Error",
				setupFunc: func() {
					mock.SetShouldFail(true)
				},
				authHeader: "Bearer any-token",
				expectPaths: []string{
					mock.URL(),
					"connection refused",
					"500",
				},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				if tc.setupFunc != nil {
					tc.setupFunc()
					defer mock.SetShouldFail(false)
				}

				req := httptest.NewRequest("GET", "/admin", nil)
				req.Header.Set("Authorization", tc.authHeader)

				w := httptest.NewRecorder()
				mux.ServeHTTP(w, req)

				body := w.Body.String()
				for _, path := range tc.expectPaths {
					if strings.Contains(body, path) {
						t.Errorf("Error message contains sensitive information: %s", path)
					}
				}

				// Should not contain internal Go paths
				internalPaths := []string{
					"github.com/navikt/zrooms",
					"internal/web",
					"auth.go",
					"admin.go",
				}
				for _, path := range internalPaths {
					if strings.Contains(body, path) {
						t.Errorf("Error message contains internal path: %s", path)
					}
				}
			})
		}
	})
}

func TestConcurrentAuthenticationRequests(t *testing.T) {
	mock := NewMockIntrospectionServer()
	defer mock.Close()

	validToken := "concurrent-test-token"
	authorizedNavIdent := "CONCURRENTTEST123"
	mock.AddValidTokenWithNavIdent(validToken, authorizedNavIdent)

	// Set environment for auth middleware
	oldEnv := os.Getenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT")
	os.Setenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT", mock.URL())
	defer func() {
		if oldEnv != "" {
			os.Setenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT", oldEnv)
		} else {
			os.Unsetenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT")
		}
	}()

	// Set up authorized admin list
	oldAdminEnv := os.Getenv("NAV_IDENT_ADMINS")
	os.Setenv("NAV_IDENT_ADMINS", "CONCURRENTTEST123,ADMIN456")
	defer func() {
		if oldAdminEnv != "" {
			os.Setenv("NAV_IDENT_ADMINS", oldAdminEnv)
		} else {
			os.Unsetenv("NAV_IDENT_ADMINS")
		}
	}()

	auth := NewAuthMiddleware()
	testHandler := auth.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Authenticated"))
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/admin", testHandler)

	t.Run("Concurrent Authentication", func(t *testing.T) {
		const numWorkers = 50
		const requestsPerWorker = 10

		results := make(chan int, numWorkers*requestsPerWorker)

		// Start concurrent workers
		for w := 0; w < numWorkers; w++ {
			go func(workerID int) {
				for r := 0; r < requestsPerWorker; r++ {
					req := httptest.NewRequest("GET", "/admin", nil)

					// Mix of valid and invalid tokens
					if r%2 == 0 {
						req.Header.Set("Authorization", "Bearer "+validToken)
					} else {
						req.Header.Set("Authorization", "Bearer invalid-token")
					}

					w := httptest.NewRecorder()
					mux.ServeHTTP(w, req)
					results <- w.Code
				}
			}(w)
		}

		// Collect results
		statusCounts := make(map[int]int)
		for i := 0; i < numWorkers*requestsPerWorker; i++ {
			status := <-results
			statusCounts[status]++
		}

		// Verify expected results
		expectedValid := numWorkers * requestsPerWorker / 2
		expectedInvalid := numWorkers * requestsPerWorker / 2

		if statusCounts[200] != expectedValid {
			t.Errorf("Expected %d valid responses, got %d", expectedValid, statusCounts[200])
		}

		if statusCounts[401] != expectedInvalid {
			t.Errorf("Expected %d invalid responses, got %d", expectedInvalid, statusCounts[401])
		}

		// Should not have any server errors from concurrency issues
		if statusCounts[500] > 0 {
			t.Errorf("Got %d server errors from concurrent requests", statusCounts[500])
		}
	})
}

func TestEnvironmentVariableSecurity(t *testing.T) {
	t.Run("Missing Introspection Endpoint", func(t *testing.T) {
		// Save current env var
		oldEnv := os.Getenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT")
		defer func() {
			if oldEnv != "" {
				os.Setenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT", oldEnv)
			}
		}()

		// Unset the environment variable
		os.Unsetenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT")

		auth := NewAuthMiddleware()

		req := httptest.NewRequest("GET", "/admin", nil)
		req.Header.Set("Authorization", "Bearer any-token")

		w := httptest.NewRecorder()
		handler := auth.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Should not reach here"))
		})

		handler(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("Should return 503 when introspection endpoint not configured, got %d", w.Code)
		}

		body := w.Body.String()
		if strings.Contains(body, "Should not reach here") {
			t.Errorf("Should not execute handler when auth not configured")
		}
	})

	t.Run("Empty Introspection Endpoint", func(t *testing.T) {
		oldEnv := os.Getenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT")
		defer func() {
			if oldEnv != "" {
				os.Setenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT", oldEnv)
			}
		}()

		os.Setenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT", "")

		auth := NewAuthMiddleware()

		req := httptest.NewRequest("GET", "/admin", nil)
		req.Header.Set("Authorization", "Bearer any-token")

		w := httptest.NewRecorder()
		handler := auth.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		handler(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("Should return 503 when introspection endpoint is empty, got %d", w.Code)
		}
	})
}
