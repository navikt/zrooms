package web

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository"
	"github.com/navikt/zrooms/internal/service"
)

// AdminHandler manages admin dashboard requests
type AdminHandler struct {
	meetingService *service.MeetingService
	repo           repository.Repository
	templates      *template.Template
}

// NewAdminHandler creates a new admin handler
func NewAdminHandler(meetingService *service.MeetingService, repo repository.Repository, templatesDir string) (*AdminHandler, error) {
	// Parse admin templates
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"formatTime":     formatTime,
		"formatDateTime": formatDateTime,
		"statusClass":    statusClass,
		"statusText":     statusText,
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
	}, nil
}

// SetupAdminRoutes registers admin routes on the given mux with authentication
func (h *AdminHandler) SetupAdminRoutes(mux *http.ServeMux) {
	auth := NewAuthMiddleware()

	mux.HandleFunc("/admin", auth.RequireAuth(h.handleAdminDashboard))
	mux.HandleFunc("/admin/meetings", auth.RequireAuth(h.handleMeetingsList))
	mux.HandleFunc("/admin/meetings/", auth.RequireAuth(h.handleMeetingDetail))
	mux.HandleFunc("/admin/meetings/delete/", auth.RequireAuth(h.handleDeleteMeeting))
}

// handleAdminDashboard renders the main admin dashboard
func (h *AdminHandler) handleAdminDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get all meetings
	allMeetings, err := h.repo.ListAllMeetings(ctx)
	if err != nil {
		log.Printf("Error getting all meetings: %v", err)
		http.Error(w, "Failed to get meetings", http.StatusInternalServerError)
		return
	}

	// Get statistics
	stats := h.calculateStats(ctx, allMeetings)

	// Prepare view model
	viewModel := struct {
		Stats       AdminStats
		Meetings    []*models.Meeting
		LastUpdated string
		CurrentYear int
	}{
		Stats:       stats,
		Meetings:    allMeetings,
		LastUpdated: time.Now().Format("2006-01-02 15:04:05"),
		CurrentYear: time.Now().Year(),
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
	ctx := r.Context()

	// Get all meetings
	allMeetings, err := h.repo.ListAllMeetings(ctx)
	if err != nil {
		log.Printf("Error getting all meetings: %v", err)
		http.Error(w, "Failed to get meetings", http.StatusInternalServerError)
		return
	}

	// Get participant counts for each meeting
	meetingsWithCounts := make([]MeetingWithParticipants, 0, len(allMeetings))
	for _, meeting := range allMeetings {
		count, err := h.repo.CountParticipantsInMeeting(ctx, meeting.ID)
		if err != nil {
			count = 0 // Default to 0 if there's an error
		}

		meetingsWithCounts = append(meetingsWithCounts, MeetingWithParticipants{
			Meeting:          meeting,
			ParticipantCount: count,
		})
	}

	// Prepare view model
	viewModel := struct {
		Meetings    []MeetingWithParticipants
		LastUpdated string
		CurrentYear int
	}{
		Meetings:    meetingsWithCounts,
		LastUpdated: time.Now().Format("2006-01-02 15:04:05"),
		CurrentYear: time.Now().Year(),
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
		log.Printf("Error getting meeting %s: %v", meetingID, err)
		http.Error(w, "Meeting not found", http.StatusNotFound)
		return
	}

	// Get participant count
	participantCount, err := h.repo.CountParticipantsInMeeting(ctx, meetingID)
	if err != nil {
		participantCount = 0
	}

	// Prepare view model
	viewModel := struct {
		Meeting          *models.Meeting
		ParticipantCount int
		HostID           string // Add this field for template compatibility
		LastUpdated      string
		CurrentYear      int
	}{
		Meeting:          meeting,
		ParticipantCount: participantCount,
		HostID:           meeting.Host.ID, // Extract host ID for easy template access
		LastUpdated:      time.Now().Format("2006-01-02 15:04:05"),
		CurrentYear:      time.Now().Year(),
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
	err := h.repo.DeleteMeeting(ctx, meetingID)
	if err != nil {
		log.Printf("Error deleting meeting %s: %v", meetingID, err)
		http.Error(w, "Failed to delete meeting", http.StatusInternalServerError)
		return
	}

	// Redirect back to meetings list
	http.Redirect(w, r, "/admin/meetings", http.StatusSeeOther)
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
func (h *AdminHandler) calculateStats(ctx context.Context, meetings []*models.Meeting) AdminStats {
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
			count, err := h.repo.CountParticipantsInMeeting(ctx, meeting.ID)
			if err == nil {
				stats.TotalParticipants += count
			}
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
