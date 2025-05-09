package web

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/navikt/zrooms/internal/models"
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
	// Set required headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	// Disable GZIP compression which can cause issues with SSE
	w.Header().Set("Content-Encoding", "identity")
	// Add X-Accel-Buffering header for proxies like Nginx
	w.Header().Set("X-Accel-Buffering", "no")

	// Flush headers to establish SSE connection
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}
	flusher.Flush()

	// Create a new client
	clientID := fmt.Sprintf("%d", time.Now().UnixNano())
	client := &SSEClient{
		id:        clientID,
		channel:   make(chan []byte, 10), // Buffer up to 10 messages
		closeChan: make(chan struct{}),
	}

	// Register client
	sm.clientsMutex.Lock()
	sm.clients[clientID] = client
	sm.clientsMutex.Unlock()

	// Clean up on disconnect
	defer func() {
		sm.clientsMutex.Lock()
		delete(sm.clients, clientID)
		sm.clientsMutex.Unlock()
		close(client.channel)
		log.Printf("SSE client disconnected: %s", clientID)
	}()

	// Send initial data
	sm.sendMeetingDataToClient(client)

	// Notify client that connection is established - follow the SSE spec strictly
	fmt.Fprintf(w, "event: connected\ndata: {\"id\":\"%s\"}\n\n", clientID)
	flusher.Flush()

	log.Printf("SSE client connected: %s", clientID)

	// Keep connection alive with more frequent pings (15 seconds instead of 30)
	pingTicker := time.NewTicker(15 * time.Second)
	defer pingTicker.Stop()

	// Monitor the connection
	for {
		select {
		case <-r.Context().Done():
			// Client disconnected
			return
		case <-client.closeChan:
			// Client is being closed
			return
		case data := <-client.channel:
			// Send event to client with proper error handling
			_, writeErr := fmt.Fprintf(w, "event: update\ndata: %s\n\n", data)
			if writeErr != nil {
				log.Printf("Error writing to SSE stream for client %s: %v", clientID, writeErr)
				return
			}
			flusher.Flush()
		case <-pingTicker.C:
			// Send ping to keep connection alive
			_, writeErr := fmt.Fprintf(w, ": ping\n\n")
			if writeErr != nil {
				log.Printf("Error sending ping to SSE stream for client %s: %v", clientID, writeErr)
				return
			}
			flusher.Flush()
		}
	}
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

	sm.clientsMutex.RLock()
	defer sm.clientsMutex.RUnlock()

	for _, client := range sm.clients {
		// Non-blocking send to client channel
		select {
		case client.channel <- data:
			// Successfully sent
		default:
			// Channel buffer full, log a warning
			log.Printf("SSE client channel full, skipping update for client %s", client.id)
		}
	}
}

// Send meeting data to a specific client
func (sm *SSEManager) sendMeetingDataToClient(client *SSEClient) {
	meetings, err := sm.meetingService.GetAllMeetings()
	if err != nil {
		log.Printf("Error getting meeting data for SSE: %v", err)
		return
	}

	data, err := json.Marshal(meetings)
	if err != nil {
		log.Printf("Error marshaling meeting data for SSE: %v", err)
		return
	}

	client.channel <- data
}
