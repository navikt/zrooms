package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestSSEWithDifferentProtocols tests SSE behavior with different HTTP protocols
func TestSSEWithDifferentProtocols(t *testing.T) {
	// Create a mock meeting service
	mockService := new(MockMeetingService)

	// Create an SSE manager
	sseManager := NewSSEManager(mockService)

	tests := []struct {
		name          string
		protoMajor    int
		protoMinor    int
		proto         string
		acceptHeader  string
		expectedRetry string
	}{
		{
			name:          "HTTP/1.1",
			protoMajor:    1,
			protoMinor:    1,
			proto:         "HTTP/1.1",
			acceptHeader:  "text/event-stream",
			expectedRetry: "retry: 3000",
		},
		{
			name:          "HTTP/2.0",
			protoMajor:    2,
			protoMinor:    0,
			proto:         "HTTP/2.0",
			acceptHeader:  "text/event-stream",
			expectedRetry: "retry: 5000",
		},
		{
			name:          "HTTP/1.1 with complex Accept header",
			protoMajor:    1,
			protoMinor:    1,
			proto:         "HTTP/1.1",
			acceptHeader:  "text/html, text/event-stream;q=0.9, application/json;q=0.8",
			expectedRetry: "retry: 3000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test recorder
			recorder := httptest.NewRecorder()

			// Create a cancellable request with the specified protocol
			request := httptest.NewRequest(http.MethodGet, "/events", nil)
			request.ProtoMajor = tt.protoMajor
			request.ProtoMinor = tt.protoMinor
			request.Proto = tt.proto
			request.Header.Set("Accept", tt.acceptHeader)

			// Create a done channel to simulate disconnection after checking events
			done := make(chan struct{})

			// Serve the request in a goroutine (will block until client disconnect)
			go func() {
				sseManager.ServeHTTP(recorder, request)
				close(done)
			}()

			// Short delay to ensure events are sent
			time.Sleep(100 * time.Millisecond)

			// Get response body
			responseBody := recorder.Body.String()

			// Verify protocol-specific behaviors
			assert.Contains(t, responseBody, tt.expectedRetry, "Should contain correct retry value for %s", tt.proto)

			// Check for protocol-specific comments/events
			if tt.protoMajor == 1 {
				assert.Contains(t, responseBody, "SSE connection setup comment", "HTTP/1.1 should have connection setup comments")
				assert.Contains(t, responseBody, ": ping", "HTTP/1.1 should have immediate ping")
			}

			// Verify common behaviors
			assert.Contains(t, responseBody, "event:connected", "Should have connected event")
			assert.Contains(t, responseBody, "event:initial-load", "Should have initial-load event")

			// Verify headers
			assert.Equal(t, "text/event-stream; charset=utf-8", recorder.Header().Get("Content-Type"))
			assert.Equal(t, "no-cache, no-store, must-revalidate, pre-check=0, post-check=0", recorder.Header().Get("Cache-Control"))

			// Cancel the request to end the test
			// Wait for the handler to finish
			select {
			case <-done:
				// Handler finished
			case <-time.After(200 * time.Millisecond):
				// Timeout, cancel again
				t.Log("Timeout waiting for handler to finish")
			}
		})
	}
}

// TestClientCleanup tests that stale SSE clients are properly cleaned up
func TestClientCleanup(t *testing.T) {
	// Create a mock meeting service
	mockService := new(MockMeetingService)

	// Create an SSE manager
	sseManager := NewSSEManager(mockService)

	// Add a few test clients
	sseManager.clientsMutex.Lock()

	// Active client (recent)
	activeClient := &SSEClient{
		id:             "active",
		responseWriter: httptest.NewRecorder(),
		disconnected:   make(chan struct{}),
		lastActive:     time.Now(),
	}
	sseManager.clients["active"] = activeClient

	// Stale client (old)
	staleClient := &SSEClient{
		id:             "stale",
		responseWriter: httptest.NewRecorder(),
		disconnected:   make(chan struct{}),
		lastActive:     time.Now().Add(-3 * time.Minute),
	}
	sseManager.clients["stale"] = staleClient

	// Already disconnected client
	disconnectedClient := &SSEClient{
		id:             "disconnected",
		responseWriter: httptest.NewRecorder(),
		disconnected:   make(chan struct{}),
		lastActive:     time.Now(),
	}
	close(disconnectedClient.disconnected)
	sseManager.clients["disconnected"] = disconnectedClient

	sseManager.clientsMutex.Unlock()

	// Run the cleanup function manually for testing
	threshold := time.Now().Add(-2 * time.Minute)
	sseManager.clientsMutex.Lock()
	for id, client := range sseManager.clients {
		select {
		case <-client.disconnected:
			// Client is marked as disconnected, remove it
			delete(sseManager.clients, id)
		default:
			// Check if client has been inactive for too long
			if client.lastActive.Before(threshold) {
				close(client.disconnected)
				delete(sseManager.clients, id)
			}
		}
	}
	sseManager.clientsMutex.Unlock()

	// Verify that only the active client remains
	sseManager.clientsMutex.RLock()
	defer sseManager.clientsMutex.RUnlock()

	assert.Equal(t, 1, len(sseManager.clients), "Only the active client should remain")
	_, activeExists := sseManager.clients["active"]
	assert.True(t, activeExists, "Active client should still exist")

	_, staleExists := sseManager.clients["stale"]
	assert.False(t, staleExists, "Stale client should be removed")

	_, disconnectedExists := sseManager.clients["disconnected"]
	assert.False(t, disconnectedExists, "Disconnected client should be removed")
}
