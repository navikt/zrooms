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

	// Store the client ID in the request context for the SSE library to use
	r.Header.Set("clientID", clientID)

	// Log the new connection
	log.Printf("SSE client connected: %s", clientID)

	// Create a channel to detect when the client disconnects
	disconnected := make(chan bool)

	// Send initial data to the client
	go func() {
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

		event := &sse.Event{
			Event: []byte("update"),
			Data:  data,
		}

		// Wait a brief moment for the connection to establish
		time.Sleep(100 * time.Millisecond)

		// Publish to the meetings stream
		sm.server.Publish("meetings", event)

		// Also send a connected event to the client
		connectData, _ := json.Marshal(map[string]string{"id": clientID})
		connectEvent := &sse.Event{
			Event: []byte("connected"),
			Data:  connectData,
		}
		sm.server.Publish("meetings", connectEvent)
	}()

	// Handle the SSE connection with the third-party library
	sm.server.ServeHTTP(w, r)

	// When ServeHTTP returns, the client has disconnected
	log.Printf("SSE client disconnected: %s", clientID)

	// Signal that the client disconnected
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

	// Create an SSE event
	event := &sse.Event{
		Event: []byte("update"),
		Data:  data,
	}

	// Publish the event to all clients
	sm.server.Publish("meetings", event)
	log.Printf("Published meeting update event to SSE clients")
}
