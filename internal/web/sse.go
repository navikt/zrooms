package web

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/navikt/zrooms/internal/models"
	"github.com/r3labs/sse/v2"
)

// SSEClient represents a connected client receiving server-sent events
type SSEClient struct {
	id        string
	channel   chan []byte
	closeChan chan struct{}
}

// SSEManager handles server-sent events to clients
type SSEManager struct {
	clients        map[string]*SSEClient
	clientsMutex   sync.RWMutex
	meetingService MeetingServicer
	server         *sse.Server
}

// NewSSEManager creates a new server-sent events manager
func NewSSEManager(meetingService MeetingServicer) *SSEManager {
	// Create a new SSE server
	server := sse.New()

	// Configure the server
	server.AutoReplay = false       // Don't replay missed events
	server.CreateStream("meetings") // Create a named stream for meetings

	return &SSEManager{
		clients:        make(map[string]*SSEClient),
		meetingService: meetingService,
		server:         server,
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

	// Generate a client ID
	clientID := fmt.Sprintf("%d", time.Now().UnixNano())

	// Log the new connection
	log.Printf("SSE client connected: %s", clientID)

	// The stream parameter is required by the r3labs/sse library
	// Add it to the request if not already present
	q := r.URL.Query()
	if !q.Has("stream") {
		// Default to the meetings stream
		q.Set("stream", "meetings")
		r.URL.RawQuery = q.Encode()
	}

	// Create a channel for detecting client disconnects
	disconnected := make(chan bool)

	// Send initial connection and data events in a separate goroutine
	// to avoid blocking the main connection handling
	go func() {
		// Short delay to ensure SSE connection is established
		time.Sleep(100 * time.Millisecond)

		// Send connected event
		connectData, _ := json.Marshal(map[string]string{"id": clientID})
		connectEvent := &sse.Event{
			Event: []byte("connected"),
			Data:  connectData,
		}
		sm.server.Publish("meetings", connectEvent)

		// Get and send initial meeting data
		meetings, err := sm.meetingService.GetAllMeetings()
		if err != nil {
			log.Printf("Error getting meeting data for new SSE client %s: %v", clientID, err)
			return
		}

		data, err := json.Marshal(meetings)
		if err != nil {
			log.Printf("Error marshaling meeting data for SSE client %s: %v", clientID, err)
			return
		}

		updateEvent := &sse.Event{
			Event: []byte("update"),
			Data:  data,
		}
		sm.server.Publish("meetings", updateEvent)
	}()

	// Handle the SSE connection with the third-party library
	sm.server.ServeHTTP(w, r)

	// When ServeHTTP returns, the client has disconnected
	log.Printf("SSE client disconnected: %s", clientID)
	close(disconnected)
}

// NotifyMeetingUpdate sends meeting updates to all connected clients
func (sm *SSEManager) NotifyMeetingUpdate(meeting *models.Meeting) {
	meetings, err := sm.meetingService.GetAllMeetings()
	if err != nil {
		log.Printf("Error getting meeting data for SSE update: %v", err)
		return
	}

	data, err := json.Marshal(meetings)
	if err != nil {
		log.Printf("Error marshaling meeting data for SSE: %v", err)
		return
	}

	// Create an SSE event with proper format according to the SSE spec
	event := &sse.Event{
		// Event field must come first in SSE format
		Event: []byte("update"),
		
		// Data contains the JSON payload
		Data: data,
		
		// ID is optional, but if included should be set properly
		// Using a timestamp as ID ensures uniqueness
		ID: []byte(fmt.Sprintf("%d", time.Now().UnixNano())),
	}

	// Log the event being published for debugging
	logData := string(data)
	if len(logData) > 100 {
		logData = logData[:100] + "..." // Truncate long payloads in logs
	}
	log.Printf("Publishing SSE update event: %s", logData)

	// Publish the event to all clients
	sm.server.Publish("meetings", event)
}
