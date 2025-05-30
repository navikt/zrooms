package web

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/navikt/zrooms/internal/config"
	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/service"
)

// Handler manages web UI requests
type Handler struct {
	meetingService *service.MeetingService
	templates      *template.Template
	sseManager     *SSEManager
}

// NewHandler creates a new web UI handler
func NewHandler(meetingService *service.MeetingService, templatesDir string) (*Handler, error) {
	// Parse templates
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"formatTime": formatTime,
	}).ParseGlob(filepath.Join(templatesDir, "*.html"))

	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}

	// Create SSE manager (always enabled)
	sseManager := NewSSEManager(meetingService)

	return &Handler{
		meetingService: meetingService,
		templates:      tmpl,
		sseManager:     sseManager,
	}, nil
}

// formatTime is a template helper function to format time
func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("15:04:05")
}

// SetupRoutes registers web UI routes on the given mux
func (h *Handler) SetupRoutes(mux *http.ServeMux) {
	// Serve static files
	fileServer := http.FileServer(http.Dir("./internal/web/static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fileServer))

	// Serve SSE endpoint (always enabled)
	mux.Handle("/events", h.sseManager)

	// Serve index page
	mux.HandleFunc("/", h.handleIndex)

	// Add HTMX partial endpoints
	mux.HandleFunc("/partial/meetings", h.HandlePartialMeetingList)
}

// handleIndex renders the main page with meeting status
func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	// Only handle the root path
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// Get meeting data, including ended meetings
	meetings, err := h.meetingService.GetMeetingStatusData(r.Context(), true)
	if err != nil {
		log.Printf("Error getting meeting data: %v", err)
		http.Error(w, "Failed to get meeting data", http.StatusInternalServerError)
		return
	}

	// Prepare view model
	zoomConfig := config.GetZoomConfig()
	viewModel := struct {
		Meetings    []service.MeetingStatusData
		LastUpdated string
		CurrentYear int
		OAuthURL    string
	}{
		Meetings:    meetings,
		LastUpdated: time.Now().Format("2006-01-02 15:04:05"),
		CurrentYear: time.Now().Year(),
		OAuthURL:    zoomConfig.GetOAuthURL(),
	}

	// Render template
	err = h.templates.ExecuteTemplate(w, "layout.html", viewModel)
	if err != nil {
		log.Printf("Error rendering template: %v", err)
		http.Error(w, "Failed to render page", http.StatusInternalServerError)
	}
}

// HandlePartialMeetingList renders just the meeting list table for HTMX updates
func (h *Handler) HandlePartialMeetingList(w http.ResponseWriter, r *http.Request) {
	// Get meeting data, including ended meetings
	meetings, err := h.meetingService.GetMeetingStatusData(r.Context(), true)
	if err != nil {
		log.Printf("Error getting meeting data: %v", err)
		http.Error(w, "Failed to get meeting data", http.StatusInternalServerError)
		return
	}

	// Prepare view model
	viewModel := struct {
		Meetings []service.MeetingStatusData
	}{
		Meetings: meetings,
	}

	// Render only the meeting_list template part
	err = h.templates.ExecuteTemplate(w, "meeting_list", viewModel)
	if err != nil {
		log.Printf("Error rendering template: %v", err)
		http.Error(w, "Failed to render meeting list", http.StatusInternalServerError)
	}
}

// NotifyMeetingUpdate sends an update notification to all SSE clients
// This should be called whenever a meeting is updated
func (h *Handler) NotifyMeetingUpdate(meeting *models.Meeting) {
	h.sseManager.NotifyMeetingUpdate(meeting)
}

// Shutdown gracefully shuts down the web handler and its SSE manager
func (h *Handler) Shutdown() {
	h.sseManager.Shutdown()
}
