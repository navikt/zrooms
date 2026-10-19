package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository"
	"github.com/navikt/zrooms/internal/service"
	"github.com/navikt/zrooms/internal/zoom"
)

// AdminHandler manages admin dashboard requests
type AdminHandler struct {
	meetingService *service.MeetingService
	repo           repository.Repository
	templates      *template.Template
	newOwnerClient func(context.Context, string) (ownerEmailClient, error)
	oauth          *zoom.APIManager
}

type ownerEmailClient interface {
	GetUserEmail(context.Context, string) (string, error)
}

// NewAdminHandler creates a new admin handler
func NewAdminHandler(meetingService *service.MeetingService, repo repository.Repository, templatesDir string, oauth *zoom.APIManager) (*AdminHandler, error) {
	// Parse admin templates
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"formatTime":     formatTime,
		"formatDateTime": formatDateTime,
		"statusClass":    statusClass,
		"statusText":     statusText,
		"isActive":       func(status models.MeetingStatus) bool { return status == models.MeetingStatusStarted },
		"slice":          slice,
		"now":            time.Now,
	}).ParseGlob(filepath.Join(templatesDir, "admin", "*.html"))

	if err != nil {
		return nil, fmt.Errorf("failed to parse admin templates: %w", err)
	}

	return &AdminHandler{
		meetingService: meetingService,
		repo:           repo,
		templates:      tmpl,
		oauth:          oauth,
		newOwnerClient: func(ctx context.Context, hostID string) (ownerEmailClient, error) {
			return oauth.ClientForUser(ctx, hostID)
		},
	}, nil
}

// SetupAdminRoutes registers admin routes on the given mux with authentication
func (h *AdminHandler) SetupAdminRoutes(mux *http.ServeMux) {
	auth := NewAuthMiddleware()

	mux.HandleFunc("/admin", auth.RequireAuth(h.handleAdminDashboard))
	mux.HandleFunc("/admin/meetings", auth.RequireAuth(h.handleMeetingsList))
	mux.HandleFunc("/admin/meetings/", auth.RequireAuth(h.handleMeetingDetail))
	mux.HandleFunc("/admin/meetings/delete/", auth.RequireAuth(h.handleDeleteMeeting))
	mux.HandleFunc("/admin/meetings/raw/", auth.RequireAuth(h.handleMeetingRawData))
}

// handleAdminDashboard renders the main admin dashboard
func (h *AdminHandler) handleAdminDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ctx := r.Context()

	// Get all meetings
	allMeetings, err := h.repo.ListAllMeetings(ctx)
	if err != nil {
		log.Printf("Error getting all meetings: %v", err)
		http.Error(w, "Failed to get meetings", http.StatusInternalServerError)
		return
	}

	// Get statistics
	stats := h.calculateStats(allMeetings)
	ownerLookupFailed := h.resolveOwners(ctx, slice(allMeetings, 0, 10))

	// Prepare view model
	viewModel := struct {
		Stats             AdminStats
		Meetings          []*models.Meeting
		LastUpdated       string
		CurrentYear       int
		OwnerLookupFailed bool
	}{
		Stats:             stats,
		Meetings:          allMeetings,
		LastUpdated:       time.Now().Format("2006-01-02 15:04:05"),
		CurrentYear:       time.Now().Year(),
		OwnerLookupFailed: ownerLookupFailed,
	}

	// Render template
	err = h.templates.ExecuteTemplate(w, "dashboard.html", viewModel)
	if err != nil {
		log.Printf("Error rendering admin template: %v", err)
		// Don't call http.Error here as headers may already be written
		return
	}
}

// handleMeetingsList renders a detailed list of all meetings
func (h *AdminHandler) handleMeetingsList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ctx := r.Context()

	// Get all meetings
	allMeetings, err := h.repo.ListAllMeetings(ctx)
	if err != nil {
		log.Printf("Error getting all meetings: %v", err)
		http.Error(w, "Failed to get meetings", http.StatusInternalServerError)
		return
	}

	// Get participant counts for each meeting
	ownerLookupFailed := h.resolveOwners(ctx, allMeetings)
	meetingsWithCounts := make([]MeetingWithParticipants, 0, len(allMeetings))
	for _, meeting := range allMeetings {
		meetingsWithCounts = append(meetingsWithCounts, MeetingWithParticipants{
			Meeting:          meeting,
			ParticipantCount: meeting.ParticipantCount,
		})
	}

	// Prepare view model
	viewModel := struct {
		Meetings          []MeetingWithParticipants
		LastUpdated       string
		CurrentYear       int
		OwnerLookupFailed bool
	}{
		Meetings:          meetingsWithCounts,
		LastUpdated:       time.Now().Format("2006-01-02 15:04:05"),
		CurrentYear:       time.Now().Year(),
		OwnerLookupFailed: ownerLookupFailed,
	}

	// Render template
	err = h.templates.ExecuteTemplate(w, "meetings.html", viewModel)
	if err != nil {
		log.Printf("Error rendering meetings template: %v", err)
		// Don't call http.Error here as headers may already be written
		return
	}
}

// handleMeetingDetail shows details for a specific meeting
func (h *AdminHandler) handleMeetingDetail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	// Extract meeting ID from URL path
	meetingID := r.URL.Path[len("/admin/meetings/"):]
	if meetingID == "" {
		http.Error(w, "Meeting ID required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Get the meeting
	meeting, err := h.repo.GetMeeting(ctx, meetingID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			http.Error(w, "Meeting not found", http.StatusNotFound)
		} else {
			log.Printf("Error getting meeting: %v", err)
			http.Error(w, "Unable to load meeting", http.StatusServiceUnavailable)
		}
		return
	}

	// Prepare view model
	ownerLookupFailed := h.resolveOwners(ctx, []*models.Meeting{meeting})
	viewModel := struct {
		Meeting           *models.Meeting
		ParticipantCount  int
		HostID            string // Add this field for template compatibility
		LastUpdated       string
		CurrentYear       int
		OwnerLookupFailed bool
	}{
		Meeting:           meeting,
		ParticipantCount:  meeting.ParticipantCount,
		HostID:            meeting.Host.ID, // Extract host ID for easy template access
		LastUpdated:       time.Now().Format("2006-01-02 15:04:05"),
		CurrentYear:       time.Now().Year(),
		OwnerLookupFailed: ownerLookupFailed,
	}

	// Render template
	err = h.templates.ExecuteTemplate(w, "meeting_detail.html", viewModel)
	if err != nil {
		log.Printf("Error rendering meeting detail template: %v", err)
		// Don't call http.Error here as headers may already be written
		// Just log the error - the template may have partially rendered
		return
	}
}

func (h *AdminHandler) resolveOwners(ctx context.Context, meetings []*models.Meeting) bool {
	hosts := make(map[string]string)
	for _, meeting := range meetings {
		if meeting.Host.ID != "" {
			hosts[meeting.Host.ID] = ""
		}
	}
	if len(hosts) == 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	failed := false
	for id := range hosts {
		client, err := h.newOwnerClient(ctx, id)
		if err != nil {
			log.Printf("Owner email authorization unavailable: %v", err)
			failed = true
			continue
		}
		email, err := client.GetUserEmail(ctx, id)
		if err != nil {
			log.Printf("Owner email lookup failed: %v", err)
			failed = true
			continue
		}
		hosts[id] = email
	}
	for _, meeting := range meetings {
		meeting.Host.Email = hosts[meeting.Host.ID]
	}
	return failed
}

// handleDeleteMeeting deletes a meeting (POST only)
func (h *AdminHandler) handleDeleteMeeting(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract meeting ID from URL path
	meetingID := r.URL.Path[len("/admin/meetings/delete/"):]
	if meetingID == "" {
		http.Error(w, "Meeting ID required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Delete the meeting
	err := h.meetingService.DeleteMeeting(ctx, meetingID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			http.Error(w, "Meeting not found", http.StatusNotFound)
		} else {
			log.Printf("Error deleting meeting: %v", err)
			http.Error(w, "Failed to delete meeting", http.StatusServiceUnavailable)
		}
		return
	}

	// Redirect back to meetings list
	http.Redirect(w, r, "/admin/meetings", http.StatusSeeOther)
}

// handleMeetingRawData fetches and displays raw Zoom API data for a meeting
func (h *AdminHandler) handleMeetingRawData(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	// Extract meeting ID from URL path
	meetingID := strings.TrimPrefix(r.URL.Path, "/admin/meetings/raw/")
	if meetingID == "" {
		http.Error(w, "Meeting ID required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	meeting, err := h.repo.GetMeeting(ctx, meetingID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			http.Error(w, "Meeting not found", http.StatusNotFound)
		} else {
			log.Printf("Unable to load meeting for Zoom API request: %v", err)
			http.Error(w, "Unable to load meeting", http.StatusServiceUnavailable)
		}
		return
	}
	client, err := h.oauth.ClientForUser(ctx, meeting.Host.ID)
	if err != nil {
		log.Printf("Error creating Zoom API client: %v", err)
		http.Error(w, "Zoom API unavailable", http.StatusServiceUnavailable)
		return
	}

	// Fetch raw meeting details from Zoom API
	rawData, err := client.GetMeetingRawData(ctx, meetingID)
	if err != nil {
		log.Printf("Error fetching meeting details for %s: %v", meetingID, err)
		http.Error(w, "Unable to fetch meeting details", http.StatusBadGateway)
		return
	}

	// Pretty format the JSON
	var prettyJSON interface{}
	if err := json.Unmarshal(rawData, &prettyJSON); err != nil {
		log.Printf("Error parsing JSON for meeting %s: %v", meetingID, err)
		// Return raw data if parsing fails
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=meeting_%s_raw.json", meetingID))
		w.Write(rawData)
		return
	}

	// Format as pretty JSON
	prettyBytes, err := json.MarshalIndent(prettyJSON, "", "  ")
	if err != nil {
		log.Printf("Error formatting JSON for meeting %s: %v", meetingID, err)
		// Return raw data if formatting fails
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=meeting_%s_raw.json", meetingID))
		w.Write(rawData)
		return
	}

	// Set content type and write response
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=meeting_%s_raw.json", meetingID))
	w.Write(prettyBytes)
}

// AdminStats holds statistics for the admin dashboard
type AdminStats struct {
	TotalMeetings     int
	ActiveMeetings    int
	EndedMeetings     int
	ScheduledMeetings int
	TotalParticipants int
}

// MeetingWithParticipants combines meeting data with participant count
type MeetingWithParticipants struct {
	Meeting          *models.Meeting
	ParticipantCount int
}

// calculateStats computes statistics for the admin dashboard
func (h *AdminHandler) calculateStats(meetings []*models.Meeting) AdminStats {
	stats := AdminStats{
		TotalMeetings: len(meetings),
	}

	for _, meeting := range meetings {
		switch meeting.Status {
		case models.MeetingStatusStarted:
			stats.ActiveMeetings++
		case models.MeetingStatusEnded:
			stats.EndedMeetings++
		default:
			stats.ScheduledMeetings++
		}

		// Count participants for active meetings
		if meeting.Status == models.MeetingStatusStarted {
			stats.TotalParticipants += meeting.ParticipantCount
		}
	}

	return stats
}

// Template helper functions

// formatDateTime formats a time for display in admin interface
func formatDateTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04:05")
}

// statusClass returns CSS class for meeting status
func statusClass(status models.MeetingStatus) string {
	switch status {
	case models.MeetingStatusStarted:
		return "status-active"
	case models.MeetingStatusEnded:
		return "status-ended"
	default:
		return "status-scheduled"
	}
}

// statusText returns human-readable status text
func statusText(status models.MeetingStatus) string {
	switch status {
	case models.MeetingStatusStarted:
		return "Active"
	case models.MeetingStatusEnded:
		return "Ended"
	default:
		return "Scheduled"
	}
}

// slice returns a slice of meetings up to the specified limit
func slice(meetings []*models.Meeting, start, end int) []*models.Meeting {
	if start < 0 {
		start = 0
	}
	if end > len(meetings) {
		end = len(meetings)
	}
	if start >= end {
		return []*models.Meeting{}
	}
	return meetings[start:end]
}
