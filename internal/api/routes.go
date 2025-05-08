package api

import (
	"net/http"

	"github.com/navikt/zrooms/internal/repository"
)

// SetupRoutes configures the HTTP routes for the API
func SetupRoutes(repo repository.Repository) *http.ServeMux {
	mux := http.NewServeMux()

	// Health check endpoints for Kubernetes
	mux.HandleFunc("/health/live", HealthLiveHandler)
	mux.HandleFunc("/health/ready", HealthReadyHandler)

	// OAuth endpoint for Zoom app installation
	mux.HandleFunc("/oauth/redirect", OAuthRedirectHandler)

	// Zoom webhook endpoint
	webhookHandler := NewWebhookHandler(repo)
	mux.Handle("/webhook", webhookHandler)

	// Room management endpoints
	roomHandler := NewRoomHandler(repo)
	mux.Handle("/api/rooms", roomHandler)
	mux.Handle("/api/rooms/", roomHandler)

	return mux
}
