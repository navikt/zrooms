package api

import (
	"net/http"

	"github.com/navikt/zrooms/internal/repository"
	"github.com/navikt/zrooms/internal/service"
)

// SetupRoutes configures the HTTP routes for the API
func SetupRoutes(repo repository.Repository, meetingService *service.MeetingService) *http.ServeMux {
	mux := http.NewServeMux()

	// Health check endpoints for Kubernetes
	mux.HandleFunc("/health/live", HealthLiveHandler)
	mux.HandleFunc("/health/ready", HealthReadyHandler)

	// OAuth endpoint for Zoom app installation
	mux.HandleFunc("/oauth/redirect", OAuthRedirectHandler)

	// Zoom webhook endpoint
	webhookHandler := NewWebhookHandler(repo, meetingService)
	mux.Handle("/webhook", webhookHandler)

	return mux
}
