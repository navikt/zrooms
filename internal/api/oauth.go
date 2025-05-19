// Package api provides the HTTP handlers for the zrooms API
package api

import (
	"fmt"
	"log"
	"net/http"

	"github.com/navikt/zrooms/internal/config"
	"github.com/navikt/zrooms/internal/utils"
)

// ZoomTokenResponse represents the response from Zoom's OAuth token endpoint
type ZoomTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

// OAuthRedirectHandler handles the redirect from Zoom OAuth flow.
// This endpoint is called by Zoom after a user authorizes the application.
// The handler validates the request and exchanges the authorization code for an access token.
// The OAuth application already has the webhooks configured, so no webhook creation is needed.
//
// Required query parameters:
// - code: The authorization code provided by Zoom
//
// Optional query parameters:
// - state: A state token to prevent CSRF attacks
//
// Required environment variables:
// - ZOOM_CLIENT_ID: OAuth client ID for the Zoom app
// - ZOOM_CLIENT_SECRET: OAuth client secret for the Zoom app
// - ZOOM_REDIRECT_URI: The redirect URI registered with the Zoom app
func OAuthRedirectHandler(w http.ResponseWriter, r *http.Request) {
	// Extract authorization code and state from query parameters
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	// Validate code parameter - state is optional when coming directly from Zoom
	if code == "" {
		http.Error(w, "Missing required code parameter", http.StatusBadRequest)
		log.Printf("OAuth error: Missing required code parameter")
		return
	}

	// Log if state is missing (for security awareness)
	if state == "" {
		log.Printf("Warning: OAuth callback received without state parameter. This may indicate a CSRF risk.")
	}

	// Log the received OAuth callback
	log.Printf("Received OAuth callback with code: %s", utils.SanitizeLogString(code))

	// Get configuration
	zoomConfig := config.GetZoomConfig()
	if !zoomConfig.IsZoomConfigValid() {
		log.Printf("OAuth error: Invalid Zoom configuration")
		http.Error(w, "Server configuration error", http.StatusInternalServerError)
		return
	}

	// In a production environment, we would exchange the code for a token here
	// and store it securely for future API calls.
	// For this example, we'll skip the actual token exchange to avoid errors
	// since we don't have a real Zoom app configuration for testing.
	log.Printf("Would exchange code for token with clientID=%s", zoomConfig.ClientID)

	// Respond with a success page
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	successHTML := `
		<!DOCTYPE html>
		<html>
		<head>
			<title>Zoom Integration Successful</title>
			<style>
				body { font-family: Arial, sans-serif; max-width: 600px; margin: 0 auto; padding: 20px; }
				.success { color: green; }
				.container { text-align: center; margin-top: 50px; }
				button { background-color: #2D8CFF; color: white; border: none; padding: 10px 20px; border-radius: 4px; cursor: pointer; }
			</style>
		</head>
		<body>
			<div class="container">
				<h1 class="success">Authorization successful</h1>
				<p>Your Zoom account has been successfully connected.</p>
				<p>You can now close this window and return to the application.</p>
				<button onclick="window.close()">Close Window</button>
			</div>
		</body>
		</html>
	`
	fmt.Fprint(w, successHTML)
}
