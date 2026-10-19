package api

import (
	"context"
	"net/http"
	"time"

	"github.com/navikt/zrooms/internal/config"
	"github.com/navikt/zrooms/internal/repository"
	"github.com/navikt/zrooms/internal/service"
	"github.com/navikt/zrooms/internal/zoom"
)

// SetupRoutes configures the HTTP routes for the API
func SetupRoutes(repo repository.Repository, meetingService *service.MeetingService, oauth *zoom.APIManager) *http.ServeMux {
	mux := http.NewServeMux()

	// Health check endpoints for Kubernetes
	mux.HandleFunc("/health/live", HealthLiveHandler)
	mux.HandleFunc("/health/ready", HealthReadyHandler(repo))

	// OAuth endpoint for Zoom app installation
	zoomConfig := config.GetZoomConfig()
	oauthHandler := NewOAuthHandler(zoomConfig, oauth)
	mux.HandleFunc("/oauth/start", oauthHandler.Start)
	mux.HandleFunc("/oauth/redirect", oauthHandler.Redirect)

	// Zoom webhook endpoint
	webhookHandler := NewWebhookHandler(meetingService, zoomConfig.WebhookSecretToken)
	mux.Handle("/webhook", webhookHandler)
	mux.Handle("/webhook/deauthorization", NewDeauthorizationHandler(
		&deauthorizationCleanup{oauth: oauth, meetings: meetingService}, zoomConfig.WebhookSecretToken, zoomConfig.ClientID))

	return mux
}

type deauthorizationCleanup struct {
	oauth    *zoom.APIManager
	meetings *service.MeetingService
}

func (d *deauthorizationCleanup) DeauthorizeUser(ctx context.Context, userID string, at time.Time) error {
	if err := d.oauth.Revoke(ctx, userID, at); err != nil {
		return err
	}
	return d.meetings.DeauthorizeUser(ctx, userID, at)
}
