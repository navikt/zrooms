package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// TokenIntrospectionResponse represents the response from the introspection endpoint
// NAIS introspection returns claims at the top level, not in a nested claims field
type TokenIntrospectionResponse struct {
	Active            bool   `json:"active"`
	NAVident          string `json:"NAVident,omitempty"`
	PreferredUsername string `json:"preferred_username,omitempty"`
	Sub               string `json:"sub,omitempty"`
	Error             string `json:"error,omitempty"`
}

// AuthMiddleware provides authentication for admin routes
type AuthMiddleware struct {
	introspectionEndpoint string
	httpClient            *http.Client
}

// NewAuthMiddleware creates a new authentication middleware
func NewAuthMiddleware() *AuthMiddleware {
	introspectionEndpoint := os.Getenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT")

	return &AuthMiddleware{
		introspectionEndpoint: introspectionEndpoint,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// RequireAuth is a middleware that validates Bearer tokens
func (auth *AuthMiddleware) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check if introspection endpoint is configured
		if auth.introspectionEndpoint == "" {
			log.Printf("Warning: NAIS_TOKEN_INTROSPECTION_ENDPOINT not configured - admin access disabled")
			http.Error(w, "Authentication not configured", http.StatusServiceUnavailable)
			return
		}

		// Extract Bearer token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Authorization header required", http.StatusUnauthorized)
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, "Bearer token required", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == "" {
			http.Error(w, "Token cannot be empty", http.StatusUnauthorized)
			return
		}

		// Validate token with introspection endpoint
		valid, navIdent, err := auth.validateToken(r.Context(), token)
		if err != nil {
			log.Printf("Token validation error: %v", err)
			http.Error(w, "Token validation failed", http.StatusInternalServerError)
			return
		}

		if !valid {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		// Check if NAVident is in the admin list
		if !auth.isAuthorizedAdmin(navIdent) {
			log.Print("Admin authorization denied")
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}

		// Token is valid, proceed to the handler
		next(w, r)
	}
}

// validateToken validates the token with the introspection endpoint and returns NAVident
func (auth *AuthMiddleware) validateToken(ctx context.Context, token string) (bool, string, error) {
	data := url.Values{"identity_provider": {"entra_id"}, "token": {token}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, auth.introspectionEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return false, "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Send the request
	resp, err := auth.httpClient.Do(req)
	if err != nil {
		return false, "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return false, "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return false, "", fmt.Errorf("introspection endpoint returned status %d", resp.StatusCode)
	}

	// Parse response
	var introspectionResp TokenIntrospectionResponse
	if err := json.Unmarshal(respBody, &introspectionResp); err != nil {
		return false, "", fmt.Errorf("failed to parse response: %w", err)
	}

	if introspectionResp.Error != "" {
		return false, "", nil
	}

	var navIdent string
	if introspectionResp.NAVident != "" {
		navIdent = introspectionResp.NAVident
	}

	if navIdent == "" {
		log.Printf("NAVident not found in token response")
	}

	return introspectionResp.Active, navIdent, nil
}

// isAuthorizedAdmin checks if the given NAVident is in the list of authorized admins
func (auth *AuthMiddleware) isAuthorizedAdmin(navIdent string) bool {
	if navIdent == "" {
		log.Printf("Authorization denied: NAVident is empty")
		return false
	}

	adminList := os.Getenv("NAV_IDENT_ADMINS")
	if adminList == "" {
		log.Printf("Warning: NAV_IDENT_ADMINS not configured - no admins authorized")
		return false
	}

	// Split comma-separated list and check each admin
	admins := strings.Split(adminList, ",")
	for _, admin := range admins {
		admin = strings.TrimSpace(admin)
		if admin == navIdent {
			return true
		}
	}

	log.Print("Admin identity is not authorized")
	return false
}
