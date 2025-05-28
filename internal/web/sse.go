package web

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

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

	// Set simple SSE headers like NAIS API does
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Content-Type", "text/event-stream")

	// Add minimal CORS support (tests expect these)
	origin := r.Header.Get("Origin")
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Cookie")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	}

	// Handle CORS preflight
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Make sure the response writer supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}
	defer flusher.Flush()

	// Generate a client ID
	clientID := fmt.Sprintf("%d", time.Now().UnixNano())
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

	// Send initial SSE comment to prime the connection (like NAIS API)
	fmt.Fprint(w, ":\n\n")
	flusher.Flush()

	// Send initial connected event
	fmt.Fprintf(w, "event: connected\n")
	fmt.Fprintf(w, "data: {\"id\":\"%s\"}\n\n", clientID)
	flusher.Flush()

	// Send initial load event
	fmt.Fprintf(w, "event: initial-load\n")
	fmt.Fprintf(w, "data: Load initial data\n\n")
	flusher.Flush()

	// Set up simple heartbeat like NAIS API (every 10 seconds)
	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()

	// Create a notification channel for detecting client disconnects
	// Use a context that doesn't inherit from the request context to avoid premature timeouts
	done := make(chan struct{})

	// Start a goroutine to detect when the client disconnects
	go func() {
		<-r.Context().Done()
		close(done)
	}()

	// Keep the connection alive
	for {
		select {
		case <-done:
			log.Printf("Request context done for client %s - clean shutdown", clientID)
			return
		case <-heartbeat.C:
			// Check if we need to send a heartbeat
			// Use the client's lastActive time (which gets updated when real messages are sent)
			sm.clientsMutex.RLock()
			currentClient, exists := sm.clients[clientID]
			if !exists {
				sm.clientsMutex.RUnlock()
				log.Printf("Client %s no longer exists, stopping heartbeat", clientID)
				return
			}
			lastActive := currentClient.lastActive
			sm.clientsMutex.RUnlock()

			// Only send heartbeat if no message sent in last 30 seconds (like NAIS API)
			if time.Since(lastActive) > 30*time.Second {
				_, err := fmt.Fprint(w, ":\n\n")
				if err != nil {
					log.Printf("Error sending heartbeat to client %s: %v", clientID, err)
					return
				}
				flusher.Flush()
			}
		case <-disconnected:
			log.Printf("Client %s disconnected", clientID)
			return
		}
	}
}

// NotifyMeetingUpdate sends meeting updates to all connected clients
func (sm *SSEManager) NotifyMeetingUpdate(meeting *models.Meeting) {
	log.Printf("Publishing SSE update event for meeting %s", meeting.ID)

	// Count active clients for logging
	sm.clientsMutex.RLock()
	clientCount := len(sm.clients)
	sm.clientsMutex.RUnlock()
	log.Printf("Notifying %d active clients about meeting update", clientCount)

	// Send simple trigger event to all clients (HTMX pattern)
	sm.clientsMutex.RLock()
	defer sm.clientsMutex.RUnlock()

	var disconnectedClients []string

	for id, client := range sm.clients {
		// Check if client is still connected
		select {
		case <-client.disconnected:
			disconnectedClients = append(disconnectedClients, id)
			continue
		default:
			// Client is still connected, send the update
		}

		// Send simple SSE event that triggers HTMX to make a new request
		_, err := fmt.Fprintf(client.responseWriter, "event: update\ndata: trigger\n\n")
		if err != nil {
			log.Printf("Error sending SSE event to client %s: %v", id, err)
			disconnectedClients = append(disconnectedClients, id)
			continue
		}

		// Flush immediately and update client activity
		if f, ok := client.responseWriter.(http.Flusher); ok {
			f.Flush()
			client.lastActive = time.Now()
		}
	}

	// Clean up disconnected clients
	if len(disconnectedClients) > 0 {
		sm.clientsMutex.RUnlock()
		sm.clientsMutex.Lock()
		for _, id := range disconnectedClients {
			if client, exists := sm.clients[id]; exists {
				close(client.disconnected)
				delete(sm.clients, id)
				log.Printf("Removed disconnected client during update: %s", id)
			}
		}
		sm.clientsMutex.Unlock()
		sm.clientsMutex.RLock()
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
