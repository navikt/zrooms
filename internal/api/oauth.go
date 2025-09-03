// Package api provides the HTTP handlers for the zrooms API
package api

import (
	"fmt"
	"log"
	"net/http"
)

// OAuthRedirectHandler handles the redirect from Zoom OAuth flow.
// This endpoint is called by Zoom after a user authorizes the application.
// The OAuth application already has the webhooks configured, so no webhook creation is needed.
//
// Required query parameters:
// - code: The authorization code provided by Zoom
func OAuthRedirectHandler(w http.ResponseWriter, r *http.Request) {
	// Extract authorization code and state from query parameters
	code := r.URL.Query().Get("code")

	// Validate code parameter - state is optional when coming directly from Zoom
	if code == "" {
		http.Error(w, "Missing required code parameter", http.StatusBadRequest)
		log.Printf("OAuth error: Missing required code parameter")
		return
	}

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
