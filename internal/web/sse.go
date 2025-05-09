package web

import (
	"encoding/json"
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

	// Send initial connected event
	sse.Encode(w, sse.Event{
		Event: "connected",
		Data:  map[string]string{"id": clientID},
	})
	flusher.Flush()

	// Get and send initial meeting data
	meetings, err := sm.meetingService.GetAllMeetings()
	if err != nil {
		log.Printf("Error getting meeting data for new SSE client %s: %v", clientID, err)
		return
	}

	sse.Encode(w, sse.Event{
		Event: "update",
		Data:  meetings,
	})
	flusher.Flush()

	// Keep the connection open until the client disconnects
	<-r.Context().Done()
	close(disconnected)
}

// NotifyMeetingUpdate sends meeting updates to all connected clients
func (sm *SSEManager) NotifyMeetingUpdate(meeting *models.Meeting) {
	meetings, err := sm.meetingService.GetAllMeetings()
	if err != nil {
		log.Printf("Error getting meeting data for SSE update: %v", err)
		return
	}

	// Log the event being published for debugging
	data, _ := json.Marshal(meetings)
	logData := string(data)
	if len(logData) > 100 {
		logData = logData[:100] + "..." // Truncate long payloads in logs
	}
	log.Printf("Publishing SSE update event: %s", logData)

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

		// Send the event
		err := sse.Encode(client.responseWriter, sse.Event{
			Id:    eventID,
			Event: "update",
			Data:  meetings,
		})

		if f, ok := client.responseWriter.(http.Flusher); ok {
			f.Flush()
		}

		if err != nil {
			log.Printf("Error sending SSE event to client %s: %v", id, err)
		}
	}
}

// Helper function to check if the client accepts event streams
func isEventStreamSupported(r *http.Request) bool {
	accepts := r.Header.Get("Accept")
	return accepts == "" || // Accept any content type
		accepts == "*/*" || // Accept any content type
		accepts == "text/event-stream" // Explicitly accept event streams
}
