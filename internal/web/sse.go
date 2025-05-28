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
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("PANIC in SSE ServeHTTP: %v", rec)
		}
	}()

	// Enable detailed logging for debugging SSE connection issues
	monitorRequest(r)

	// Set CORS headers to make SSE work in various environments
	// Use specific origin instead of wildcard (*) to allow credentials
	origin := r.Header.Get("Origin")
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	} else {
		// In production environments, we should have a list of allowed origins
		// For local development or testing, we'll use the host as fallback
		proto := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			proto = "https"
		}
		// Create a fallback origin from the request host
		fallbackOrigin := proto + "://" + r.Host
		w.Header().Set("Access-Control-Allow-Origin", fallbackOrigin)
		log.Printf("Warning: Origin header not set in SSE request, using fallback: %s", fallbackOrigin)
	}
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Cookie")
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

	// CRITICAL FIX: Prevent HTTP/3 QUIC protocol errors in cloud environments
	// These headers force HTTP/1.1 semantics and prevent protocol upgrade attempts
	w.Header().Set("Alt-Svc", "clear")  // Explicitly disable HTTP/3 QUIC advertising
	w.Header().Set("Vary", "Accept-Encoding")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	
	// Additional headers to prevent protocol issues in GCP/K8s environments
	w.Header().Set("X-Force-HTTP1", "true")  // Custom header for load balancers
	w.Header().Set("Upgrade", "")  // Clear any upgrade headers
	
	// Ensure no chunked encoding which can cause issues with proxies
	w.Header().Set("Transfer-Encoding", "identity")

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

	// Immediately send a heartbeat after connection is established
	timestamp := time.Now().Format(time.RFC3339)
	fmt.Fprintf(w, ": heartbeat %s\n\n", timestamp)
	flusher.Flush()

	// Set up heartbeat ticker to keep the connection alive
	// Make heartbeat interval more aggressive (every 2 seconds)
	heartbeat := time.NewTicker(2 * time.Second)
	defer heartbeat.Stop()

	// Create a notification channel for client context cancellation
	done := r.Context().Done()

	// Keep the connection alive with periodic heartbeats
	consecutiveErrors := 0
	maxConsecutiveErrors := 3
	connectionHealthy := true

	for {
		select {
		case <-done:
			// Client disconnected
			log.Printf("Context done for client %s - clean shutdown", clientID)
			close(disconnected)
			return
		case <-heartbeat.C:
			if !connectionHealthy {
				log.Printf("Connection marked as unhealthy for client %s - stopping", clientID)
				close(disconnected)
				return
			}

			timestamp := time.Now().Format(time.RFC3339)
			// Send a comment as a lightweight ping
			_, err := fmt.Fprintf(w, ": heartbeat %s\n\n", timestamp)
			if err != nil {
				consecutiveErrors++
				log.Printf("Error sending heartbeat to client %s: %v (failures: %d/%d)",
					clientID, err, consecutiveErrors, maxConsecutiveErrors)
				if consecutiveErrors >= maxConsecutiveErrors {
					log.Printf("Too many consecutive errors for client %s, marking connection as unhealthy", clientID)
					connectionHealthy = false
					close(disconnected)
					return
				}
			} else {
				consecutiveErrors = 0
				sm.clientsMutex.Lock()
				if client, exists := sm.clients[clientID]; exists {
					client.lastActive = time.Now()
				}
				sm.clientsMutex.Unlock()
			}
			// Always flush after sending heartbeat
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("Flush panic recovered: %v", r)
					}
				}()
				flusher.Flush()
			}()
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
