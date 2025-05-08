package web

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/navikt/zrooms/internal/service"
)

// Handler manages web UI requests
type Handler struct {
	roomService *service.RoomService
	templates   *template.Template
	refreshRate int // in seconds
}

// NewHandler creates a new web UI handler
func NewHandler(roomService *service.RoomService, templatesDir string, refreshRate int) (*Handler, error) {
	// Parse templates
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"formatTime": formatTime,
	}).ParseGlob(filepath.Join(templatesDir, "*.html"))

	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}

	return &Handler{
		roomService: roomService,
		templates:   tmpl,
		refreshRate: refreshRate,
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

	// Serve index page
	mux.HandleFunc("/", h.handleIndex)
}

// handleIndex renders the main page with room and meeting status
func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	// Only handle the root path
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// Get meeting data
	meetings, err := h.roomService.GetMeetingStatusData(r.Context())
	if err != nil {
		log.Printf("Error getting meeting data: %v", err)
		http.Error(w, "Failed to get meeting data", http.StatusInternalServerError)
		return
	}

	// Prepare view model
	viewModel := struct {
		Meetings    []service.MeetingStatusData
		LastUpdated string
		CurrentYear int
		RefreshRate int
	}{
		Meetings:    meetings,
		LastUpdated: time.Now().Format("2006-01-02 15:04:05"),
		CurrentYear: time.Now().Year(),
		RefreshRate: h.refreshRate,
	}

	// Set refresh header if auto-refresh is enabled
	if h.refreshRate > 0 {
		w.Header().Set("Refresh", fmt.Sprintf("%d", h.refreshRate))
	}

	// Render template
	err = h.templates.ExecuteTemplate(w, "layout.html", viewModel)
	if err != nil {
		log.Printf("Error rendering template: %v", err)
		http.Error(w, "Failed to render page", http.StatusInternalServerError)
	}
}
