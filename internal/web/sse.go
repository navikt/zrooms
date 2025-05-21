package web

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
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

	// Check for authentication cookies
	cookies := r.Cookies()
	hasCookies := len(cookies) > 0

	// Log cookie count for debugging
	log.Printf("SSE REQUEST COOKIE COUNT: %d", len(cookies))
	if hasCookies {
		cookieNames := make([]string, 0, len(cookies))
		for _, cookie := range cookies {
			cookieNames = append(cookieNames, cookie.Name)
		}
		log.Printf("SSE REQUEST COOKIE NAMES: %v", cookieNames)
	} else {
		log.Printf("WARNING: No cookies found in SSE request - this may cause authentication issues")
	}

	// Set comprehensive CORS headers to make SSE work in various environments
	// Always use the actual origin if available to support credentials
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Fall back to Referer header if Origin is not set
		referer := r.Header.Get("Referer")
		if referer != "" {
			if refererURL, err := url.Parse(referer); err == nil {
				origin = fmt.Sprintf("%s://%s", refererURL.Scheme, refererURL.Host)
				log.Printf("Using origin from Referer: %s", origin)
			}
		}

		// If still empty, check Host header
		if origin == "" {
			host := r.Header.Get("Host")
			if host != "" {
				// Try to determine if request was over HTTPS
				scheme := "http"
				if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
					scheme = "https"
				}
				origin = fmt.Sprintf("%s://%s", scheme, host)
				log.Printf("Using origin from Host: %s", origin)
			} else {
				// Last resort - allow any origin but log warning
				origin = "*"
				log.Printf("Warning: Using wildcard origin for CORS")
			}
		}
	}

	// Don't use wildcard origin when credentials are needed
	if origin == "*" && hasCookies {
		// When cookies are present, we must specify an explicit origin
		host := r.Host
		scheme := "https"
		if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
			scheme = "http"
		}
		origin = fmt.Sprintf("%s://%s", scheme, host)
		log.Printf("Adjusted origin for credentials: %s", origin)
	}

	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Requested-With, Cookie, Authorization")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Credentials", "true")

	// Handle CORS preflight
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	// Set required headers for SSE
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")                                // More explicit content type
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate, pre-check=0, post-check=0") // Stronger caching directives
	w.Header().Set("Pragma", "no-cache")                                                              // Legacy cache control
	w.Header().Set("Expires", "0")                                                                    // Force expired
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx proxy buffering

	// Set appropriate timeouts for proxies - set smaller timeouts for HTTP/1.1
	if r.ProtoMajor == 1 {
		// HTTP/1.1 specific keep-alive settings
		w.Header().Set("Keep-Alive", "timeout=30, max=100")
	} else {
		// HTTP/2+ settings
		w.Header().Set("Keep-Alive", "timeout=60, max=1000")
	}

	// Specific headers to help with proxy behavior
	w.Header().Set("Vary", "Accept-Encoding")
	w.Header().Set("X-Content-Type-Options", "nosniff")

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

	// For HTTP/1.1, establish the connection with proper format
	// This is important for some proxies and browsers
	if r.ProtoMajor == 1 {
		// HTTP/1.1 needs proper SSE format for some proxies
		// Start with lots of comments to help establish the connection
		for i := 0; i < 20; i++ {
			fmt.Fprintf(w, ": SSE connection setup comment %d\n\n", i)
			flusher.Flush() // Flush after each comment for better proxy handling
		}
	} else {
		// Just a few newlines for HTTP/2+
		fmt.Fprintf(w, "\n\n")
		flusher.Flush()
	}

	// Send retry directives before any events - critical for reconnection
	if r.ProtoMajor == 1 {
		fmt.Fprintf(w, "retry: 3000\n\n") // 3 second retry for HTTP/1.1
	} else {
		fmt.Fprintf(w, "retry: 5000\n\n") // 5 second retry for HTTP/2+
	}
	flusher.Flush()

	// Send initial connected event with ID for better reconnection support
	eventID := fmt.Sprintf("%d", time.Now().UnixNano())
	sse.Encode(w, sse.Event{
		Id:    eventID,
		Event: "connected",
		Data:  map[string]string{"id": clientID},
	})
	flusher.Flush()

	// Send a one-time initial load event (different from update events)
	// Also include an ID for better reconnection support
	sse.Encode(w, sse.Event{
		Id:    fmt.Sprintf("%s-init", eventID),
		Event: "initial-load",
		Data:  "Load initial data",
	})
	flusher.Flush()

	// Additional ping right away for HTTP/1.1 to help establish connection
	if r.ProtoMajor == 1 {
		fmt.Fprintf(w, ": ping\n\n")
		flusher.Flush()
	}

	// For HTTP/1.1, send an additional ping right away to help establish the connection
	if r.ProtoMajor == 1 {
		// Immediate ping after the initial load
		fmt.Fprintf(w, ": ping\n\n")
		flusher.Flush()
	}

	// Set up heartbeat ticker to keep the connection alive
	// For HTTP/1.1, use more frequent heartbeats which is more appropriate for proxies
	// For HTTP/2+, we can use a slightly longer interval
	var heartbeatInterval time.Duration
	if r.ProtoMajor == 1 {
		// Even more frequent for HTTP/1.1 - this is crucial for proxies with aggressive timeouts
		heartbeatInterval = 1 * time.Second
	} else {
		heartbeatInterval = 3 * time.Second // HTTP/2 can handle longer intervals
	}
	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()

	log.Printf("Using %v heartbeat interval for %s protocol", heartbeatInterval, r.Proto)

	// Create a notification channel for client context cancellation
	done := r.Context().Done()

	// Keep track of client activity timing
	lastClientActivity := time.Now()

	// Keep the connection alive with periodic heartbeats
	// Track consecutive failures
	consecutiveErrors := 0
	maxConsecutiveErrors := 3

	// Track connection status
	connectionHealthy := true

	// Create a timeout ticker to detect stale connections
	timeoutTicker := time.NewTicker(30 * time.Second)
	defer timeoutTicker.Stop()

	for {
		select {
		case <-done:
			// Client disconnected
			log.Printf("Context done for client %s - clean shutdown", clientID)
			close(disconnected)
			return
		case <-timeoutTicker.C:
			// Check if the client has been inactive too long
			if time.Since(lastClientActivity) > 90*time.Second {
				log.Printf("Connection timeout for client %s - no activity for %v", clientID, 90*time.Second)
				connectionHealthy = false
				close(disconnected)
				return
			}
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

				// For HTTP/1.1, we need to be more aggressive with keepalives
				if r.ProtoMajor == 1 {
					// Always send an actual event for HTTP/1.1 to help keep the connection alive
					// Some proxies need real events, not just comments

					// Add a comment line before the event for extra data over the wire
					// This can help keep the connection alive with chatty proxies
					_, commentErr := fmt.Fprintf(w, ": pre-keepalive comment %s\n\n", timestamp)
					if commentErr != nil {
						log.Printf("Warning: keepalive comment write error: %v", commentErr)
						// Continue even if comment fails
					}

					// Then send a proper event with ID for resumability
					err := sse.Encode(w, sse.Event{
						Id:    fmt.Sprintf("ka-%d", time.Now().UnixNano()),
						Event: "keepalive",
						Data:  timestamp,
						Retry: 1000, // 1 second retry hint - more aggressive for HTTP/1.1
					})
					if err != nil {
						return fmt.Errorf("keepalive event write error: %w", err)
					}

					// For HTTP/1.1, add an extra empty comment line after events
					// This has been shown to help with some problematic proxies
					_, _ = fmt.Fprintf(w, ":\n\n")
				} else {
					// Just a comment for HTTP/2+
					_, err := fmt.Fprintf(w, ": heartbeat %s\n\n", timestamp)
					if err != nil {
						return fmt.Errorf("heartbeat write error: %w", err)
					}

					// Send an actual event periodically for HTTP/2+
					if time.Now().Unix()%10 == 0 {
						err = sse.Encode(w, sse.Event{
							Id:    fmt.Sprintf("ka-%d", time.Now().UnixNano()),
							Event: "keepalive",
							Data:  timestamp,
							Retry: 3000, // 3 second retry hint for HTTP/2+
						})
						if err != nil {
							return fmt.Errorf("keepalive event write error: %w", err)
						}
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
				lastClientActivity = time.Now()

				sm.clientsMutex.Lock()
				if client, exists := sm.clients[clientID]; exists {
					client.lastActive = lastClientActivity
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
	if accepts == "" || // Accept any content type
		accepts == "*/*" || // Accept any content type
		accepts == "text/event-stream" { // Explicitly accept event stream
		return true
	}

	// For complex Accept headers, properly parse each MIME type
	mimeTypes := splitMimeTypes(accepts)
	for _, mime := range mimeTypes {
		if mime == "*/*" || mime == "text/event-stream" ||
			mime == "text/*" {
			return true
		}
	}

	return false
}

// Helper function to split Accept header into individual MIME types
func splitMimeTypes(accepts string) []string {
	// Split by comma, trim whitespace from each part
	parts := strings.Split(accepts, ",")
	mimeTypes := make([]string, 0, len(parts))

	for _, part := range parts {
		// Remove quality factor if present (;q=0.x)
		if idx := strings.IndexByte(part, ';'); idx != -1 {
			part = part[:idx]
		}

		// Trim whitespace and add to list if not empty
		part = strings.TrimSpace(part)
		if part != "" {
			mimeTypes = append(mimeTypes, part)
		}
	}

	return mimeTypes
}
