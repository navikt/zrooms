package web

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/navikt/zrooms/internal/models"
)

// SSEManager handles server-sent events to clients using a broadcast channel
type SSEManager struct {
	broadcast      chan string
	shutdown       chan struct{}
	meetingService MeetingServicer
}

// NewSSEManager creates a new server-sent events manager with broadcast channel
func NewSSEManager(meetingService MeetingServicer) *SSEManager {
	return &SSEManager{
		broadcast:      make(chan string, 100), // Buffered channel to prevent blocking
		shutdown:       make(chan struct{}),
		meetingService: meetingService,
	}
}

// ServeHTTP implements the http.Handler interface for SSE connections
func (sm *SSEManager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("PANIC in SSE ServeHTTP: %v", rec)
		}
	}()

	// Set simple SSE headers
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Content-Type", "text/event-stream")

	// Add minimal CORS support
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

	log.Printf("SSE client connected from %s", r.RemoteAddr)
	defer log.Printf("SSE client disconnected")

	// Send initial SSE comment to prime the connection
	fmt.Fprint(w, ":\n\n")
	flusher.Flush()

	// Send initial connected event
	fmt.Fprint(w, "event: connected\ndata: {\"connected\":true}\n\n")
	flusher.Flush()

	// Send initial load event
	fmt.Fprint(w, "event: initial-load\ndata: Load initial data\n\n")
	flusher.Flush()

	// Set up heartbeat (every 10 seconds)
	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()

	// Keep the connection alive and listen for broadcasts
	for {
		select {
		case <-r.Context().Done():
			log.Printf("SSE client context done - clean shutdown")
			return
		case <-sm.shutdown:
			log.Printf("SSE manager shutting down - closing connection")
			return
		case <-heartbeat.C:
			// Send heartbeat comment
			_, err := fmt.Fprint(w, ":\n\n")
			if err != nil {
				log.Printf("Error sending heartbeat: %v", err)
				return
			}
			flusher.Flush()
		case message := <-sm.broadcast:
			// Received a broadcast message, send it to this client
			_, err := fmt.Fprint(w, message)
			if err != nil {
				log.Printf("Error sending broadcast message: %v", err)
				return
			}
			flusher.Flush()
		}
	}
}

// NotifyMeetingUpdate sends meeting updates to all connected clients via broadcast channel
func (sm *SSEManager) NotifyMeetingUpdate(meeting *models.Meeting) {
	log.Printf("Publishing SSE update event for meeting %s", meeting.ID)

	// Create the SSE message
	message := "event: update\ndata: trigger\n\n"

	// Send to broadcast channel (non-blocking due to buffer)
	select {
	case sm.broadcast <- message:
		log.Printf("Broadcast message sent to channel")
	default:
		log.Printf("Broadcast channel full, dropping message")
	}
}

// Shutdown gracefully shuts down the SSE manager by closing the shutdown channel
func (sm *SSEManager) Shutdown() {
	log.Printf("Shutting down SSE manager")
	close(sm.shutdown)
}
