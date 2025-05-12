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
}

// SSEManager handles server-sent events to clients
type SSEManager struct {
	clients        map[string]*SSEClient
	clientsMutex   sync.RWMutex
	meetingService MeetingServicer
}

// NewSSEManager creates a new server-sent events manager
func NewSSEManager(meetingService MeetingServicer) *SSEManager {
	return &SSEManager{
		clients:        make(map[string]*SSEClient),
		meetingService: meetingService,
	}
}

// ServeHTTP implements the http.Handler interface for SSE connections
func (sm *SSEManager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable proxy buffering

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
	log.Printf("SSE client connected: %s", clientID)

	// Create a channel for detecting client disconnects
	disconnected := make(chan struct{})

	// Create a new client and register it
	client := &SSEClient{
		id:             clientID,
		responseWriter: w,
		disconnected:   disconnected,
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
	fmt.Fprintf(w, "retry: 10000\n") // 10 second retry
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
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	// Create a notification channel for client context cancellation
	done := r.Context().Done()

	// Keep the connection alive with periodic heartbeats
	for {
		select {
		case <-done:
			// Client disconnected
			close(disconnected)
			return
		case <-heartbeat.C:
			// Send heartbeat comment as per SSE spec
			// The comment doesn't trigger an event but keeps the connection alive
			fmt.Fprintf(w, ": heartbeat %s\n\n", time.Now().Format(time.RFC3339))
			flusher.Flush()
			log.Printf("Heartbeat sent to client: %s", clientID)
		}
	}
}

// NotifyMeetingUpdate sends meeting updates to all connected clients
func (sm *SSEManager) NotifyMeetingUpdate(meeting *models.Meeting) {
	// Log the event being published for debugging
	log.Printf("Publishing SSE update event for meeting %s", meeting.ID)

	// Generate a unique event ID based on current timestamp
	eventID := fmt.Sprintf("%d", time.Now().UnixNano())

	// Publish the event to all clients
	sm.clientsMutex.RLock()
	defer sm.clientsMutex.RUnlock()

	for id, client := range sm.clients {
		// Check if client is still connected
		select {
		case <-client.disconnected:
			// Client has disconnected but not been removed yet
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
					close(c.disconnected)
				}
			}()

			// Add SSE comment line as keepalive before the event
			// This helps maintain the connection and prevents protocol errors
			_, err := fmt.Fprintf(c.responseWriter, ": keepalive %s\n\n", time.Now().Format(time.RFC3339))
			if err != nil {
				log.Printf("Error sending keepalive to client %s: %v", clientID, err)
				close(c.disconnected)
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
				close(c.disconnected)
				return
			}

			// Flush the response writer to ensure data is sent immediately
			if f, ok := c.responseWriter.(http.Flusher); ok {
				f.Flush()
			}
		}(id, client)
	}
}

// Helper function to check if the client accepts event streams
func isEventStreamSupported(r *http.Request) bool {
	accepts := r.Header.Get("Accept")
	return accepts == "" || // Accept any content type
		accepts == "*/*" || // Accept any content type
		accepts == "text/event-stream" // Explicitly accept event streams
}
