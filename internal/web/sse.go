package web

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-contrib/sse"
	"github.com/navikt/zrooms/internal/models"
)

// SSEClient represents a connected client receiving server-sent events
type SSEClient struct {
	id             string
	responseWriter http.ResponseWriter
	disconnected   chan struct{}
	lastActive     time.Time // Track when the client was last active
}

// SSEManager handles server-sent events to clients
type SSEManager struct {
	clients        map[string]*SSEClient
	clientsMutex   sync.RWMutex
	meetingService MeetingServicer
}

// NewSSEManager creates a new server-sent events manager
func NewSSEManager(meetingService MeetingServicer) *SSEManager {
	manager := &SSEManager{
		clients:        make(map[string]*SSEClient),
		meetingService: meetingService,
	}

	// Start a cleanup goroutine to regularly remove stale clients
	go manager.cleanupStaleSessions()

	return manager
}

// cleanupStaleSessions periodically removes clients that haven't been active
func (sm *SSEManager) cleanupStaleSessions() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		threshold := time.Now().Add(-2 * time.Minute)

		sm.clientsMutex.Lock()
		for id, client := range sm.clients {
			select {
			case <-client.disconnected:
				// Client is marked as disconnected, remove it
				delete(sm.clients, id)
				log.Printf("Removed disconnected SSE client: %s", id)
			default:
				// Check if client has been inactive for too long
				if client.lastActive.Before(threshold) {
					close(client.disconnected)
					delete(sm.clients, id)
					log.Printf("Removed stale SSE client: %s (inactive since %v)", id, client.lastActive)
				}
			}
		}
		sm.clientsMutex.Unlock()
	}
}

// ServeHTTP implements the http.Handler interface for SSE connections
func (sm *SSEManager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Enable detailed logging for debugging SSE connection issues
	monitorRequest(r)

	// Set CORS headers to make SSE work in various environments
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")

	// Handle CORS preflight
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Set required headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx proxy buffering

	// Set appropriate timeouts for proxies
	w.Header().Set("Keep-Alive", "timeout=60, max=1000")

	// Specific headers for handling HTTP/2 and QUIC protocols
	w.Header().Set("Vary", "Accept-Encoding")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	// Do NOT set Transfer-Encoding: chunked - can cause issues with HTTP/2 and QUIC
	// HTTP/2 and HTTP/3 (QUIC) use their own framing mechanisms

	// Log response headers for debugging
	logResponseHeaders(w)

	// Check if client accepts SSE
	if !isEventStreamSupported(r) {
		http.Error(w, "This endpoint requires EventStream support", http.StatusNotAcceptable)
		return
	}

	// Make sure the response writer supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Generate a client ID
	clientID := fmt.Sprintf("%d", time.Now().UnixNano())

	// Log the new connection
	log.Printf("SSE client connected: %s from %s", clientID, r.RemoteAddr)

	// Create a channel for detecting client disconnects
	disconnected := make(chan struct{})

	// Create a new client and register it
	client := &SSEClient{
		id:             clientID,
		responseWriter: w,
		disconnected:   disconnected,
		lastActive:     time.Now(),
	}

	// Register the client
	sm.clientsMutex.Lock()
	sm.clients[clientID] = client
	sm.clientsMutex.Unlock()

	// Clean up client when disconnected
	defer func() {
		sm.clientsMutex.Lock()
		delete(sm.clients, clientID)
		sm.clientsMutex.Unlock()
		log.Printf("SSE client disconnected: %s", clientID)
	}()

	// Write a few newlines to prime the connection
	fmt.Fprintf(w, "\n\n")
	flusher.Flush()

	// Send initial connected event with retry directive
	fmt.Fprintf(w, "retry: 5000\n") // 5 second retry (reduced from 10s)
	sse.Encode(w, sse.Event{
		Event: "connected",
		Data:  map[string]string{"id": clientID},
	})
	flusher.Flush()

	// Send a one-time initial load event (different from update events)
	sse.Encode(w, sse.Event{
		Event: "initial-load",
		Data:  "Load initial data",
	})
	flusher.Flush()

	// Set up heartbeat ticker to keep the connection alive
	// Reduce heartbeat interval from 15 seconds to 5 seconds to prevent proxy timeouts
	heartbeat := time.NewTicker(5 * time.Second)
	defer heartbeat.Stop()

	// Create a notification channel for client context cancellation
	done := r.Context().Done()

	// Keep the connection alive with periodic heartbeats
	// Track consecutive failures
	consecutiveErrors := 0
	maxConsecutiveErrors := 3

	// Track connection status
	connectionHealthy := true

	for {
		select {
		case <-done:
			// Client disconnected
			log.Printf("Context done for client %s - clean shutdown", clientID)
			close(disconnected)
			return
		case <-heartbeat.C:
			// If we've determined the connection is unhealthy, stop trying
			if !connectionHealthy {
				log.Printf("Connection marked as unhealthy for client %s - stopping", clientID)
				close(disconnected)
				return
			}

			// Attempt to send heartbeat with proper error handling
			timestamp := time.Now().Format(time.RFC3339)

			// Function to safely attempt writes and handle errors
			attemptWrite := func() error {
				// Try-catch equivalent for safer writes
				defer func() {
					if r := recover(); r != nil {
						log.Printf("Recovered from panic in SSE write: %v", r)
						consecutiveErrors++
					}
				}()

				// Send a comment as a lightweight ping
				_, err := fmt.Fprintf(w, ": heartbeat %s\n\n", timestamp)
				if err != nil {
					return fmt.Errorf("heartbeat write error: %w", err)
				}

				// Send an actual event periodically
				if time.Now().Unix()%10 == 0 {
					err = sse.Encode(w, sse.Event{
						Event: "keepalive",
						Data:  timestamp,
					})
					if err != nil {
						return fmt.Errorf("keepalive event write error: %w", err)
					}
				}

				// Try to flush, but don't fail if it doesn't work
				// Some proxies might handle the flush differently
				safeFlush := func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("Flush panic recovered: %v", r)
						}
					}()
					flusher.Flush()
				}

				safeFlush()
				return nil
			}

			// Try to write and track errors
			if err := attemptWrite(); err != nil {
				consecutiveErrors++
				log.Printf("Error sending heartbeat to client %s: %v (failures: %d/%d)",
					clientID, err, consecutiveErrors, maxConsecutiveErrors)

				// If we've had too many consecutive failures, close the connection
				if consecutiveErrors >= maxConsecutiveErrors {
					log.Printf("Too many consecutive errors for client %s, marking connection as unhealthy", clientID)
					connectionHealthy = false
					close(disconnected)
					return
				}
			} else {
				// Reset consecutive errors on success
				consecutiveErrors = 0

				// Update the client's last active time on successful write
				sm.clientsMutex.Lock()
				if client, exists := sm.clients[clientID]; exists {
					client.lastActive = time.Now()
				}
				sm.clientsMutex.Unlock()
			}
		}
	}
}

// NotifyMeetingUpdate sends meeting updates to all connected clients
func (sm *SSEManager) NotifyMeetingUpdate(meeting *models.Meeting) {
	// Log the event being published for debugging
	log.Printf("Publishing SSE update event for meeting %s", meeting.ID)

	// Generate a unique event ID based on current timestamp
	eventID := fmt.Sprintf("%d", time.Now().UnixNano())

	// Count active clients for logging
	clientCount := 0
	sm.clientsMutex.RLock()
	for range sm.clients {
		clientCount++
	}
	sm.clientsMutex.RUnlock()
	log.Printf("Notifying %d active clients about meeting update", clientCount)

	// Publish the event to all clients
	sm.clientsMutex.RLock()

	// Create a list to track clients that need to be removed
	var disconnectedClients []string

	for id, client := range sm.clients {
		// Check if client is still connected
		select {
		case <-client.disconnected:
			// Client has disconnected but not been removed yet
			disconnectedClients = append(disconnectedClients, id)
			continue
		default:
			// Client is still connected
		}

		// Use a separate function to handle errors for each client
		// This prevents errors with one client from affecting others
		func(clientID string, c *SSEClient) {
			defer func() {
				// Recover from panics that might occur when writing to closed connections
				if r := recover(); r != nil {
					log.Printf("Recovered from panic sending SSE to client %s: %v", clientID, r)
					// Mark client as disconnected if there was a panic
					disconnectedClients = append(disconnectedClients, clientID)
				}
			}()

			// Add SSE comment line as keepalive before the event
			// This helps maintain the connection and prevents protocol errors
			_, err := fmt.Fprintf(c.responseWriter, ": update-keepalive %s\n\n", time.Now().Format(time.RFC3339))
			if err != nil {
				log.Printf("Error sending keepalive to client %s: %v", clientID, err)
				disconnectedClients = append(disconnectedClients, clientID)
				return
			}

			// Send the event - this will trigger the htmx request via hx-trigger="sse:update"
			err = sse.Encode(c.responseWriter, sse.Event{
				Id:    eventID,
				Event: "update",
				Data:  "Update available", // Simple message - htmx will use the trigger
			})

			if err != nil {
				log.Printf("Error sending SSE event to client %s: %v", clientID, err)
				disconnectedClients = append(disconnectedClients, clientID)
				return
			}

			// Flush the response writer to ensure data is sent immediately
			if f, ok := c.responseWriter.(http.Flusher); ok {
				f.Flush()

				// Update client's last active time on successful flush
				c.lastActive = time.Now()
			}
		}(id, client)
	}

	sm.clientsMutex.RUnlock()

	// Clean up any clients that were identified as disconnected
	if len(disconnectedClients) > 0 {
		sm.clientsMutex.Lock()
		for _, id := range disconnectedClients {
			if client, exists := sm.clients[id]; exists {
				close(client.disconnected)
				delete(sm.clients, id)
				log.Printf("Removed disconnected client during update: %s", id)
			}
		}
		sm.clientsMutex.Unlock()
	}
}

// Helper function to check if the client accepts event streams
func isEventStreamSupported(r *http.Request) bool {
	accepts := r.Header.Get("Accept")

	// Common accept headers that include event-stream
	return accepts == "" || // Accept any content type
		accepts == "*/*" || // Accept any content type
		accepts == "text/event-stream" || // Explicitly accept event streams
		contains(accepts, "text/event-stream") // Accept multiple types including event streams
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s != "" && (s == substr || s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || s[len(substr):] == substr)
}
