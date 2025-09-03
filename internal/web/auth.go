package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// TokenIntrospectionRequest represents the payload sent to the introspection endpoint
type TokenIntrospectionRequest struct {
	IdentityProvider string `json:"identity_provider"`
	Token            string `json:"token"`
}

// TokenIntrospectionResponse represents the response from the introspection endpoint
type TokenIntrospectionResponse struct {
	Active bool                   `json:"active"`
	Claims map[string]interface{} `json:"claims,omitempty"`
	Error  string                 `json:"error,omitempty"`
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
		valid, navIdent, err := auth.validateToken(token)
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
			log.Printf("Unauthorized access attempt from NAVident: %s", navIdent)
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}

		// Token is valid, proceed to the handler
		next(w, r)
	}
}

// validateToken validates the token with the introspection endpoint and returns NAVident
func (auth *AuthMiddleware) validateToken(token string) (bool, string, error) {
	// Prepare the introspection request
	reqBody := TokenIntrospectionRequest{
		IdentityProvider: "azuread",
		Token:            token,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return false, "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", auth.introspectionEndpoint, bytes.NewBuffer(jsonBody))
	if err != nil {
		return false, "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Send the request
	resp, err := auth.httpClient.Do(req)
	if err != nil {
		return false, "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return false, "", fmt.Errorf("introspection endpoint returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var introspectionResp TokenIntrospectionResponse
	if err := json.Unmarshal(respBody, &introspectionResp); err != nil {
		return false, "", fmt.Errorf("failed to parse response: %w", err)
	}

	if introspectionResp.Error != "" {
		return false, "", fmt.Errorf("introspection error: %s", introspectionResp.Error)
	}

	// Safe logging of claims structure (keys and types only)
	if introspectionResp.Claims != nil {
		claimKeys := make([]string, 0, len(introspectionResp.Claims))
		for key, value := range introspectionResp.Claims {
			claimKeys = append(claimKeys, fmt.Sprintf("%s(%T)", key, value))
		}
		log.Printf("Token validation successful, found %d claims: [%s]", len(introspectionResp.Claims), strings.Join(claimKeys, ", "))
	} else {
		log.Printf("Token validation successful, but no claims found in response")
	}

	// Extract NAVident from claims
	var navIdent string
	if introspectionResp.Claims != nil {
		// Try different possible claim names for NAVident
		possibleNavIdentClaims := []string{"NAVident", "navident", "nav_ident", "preferred_username", "sub", "upn"}
		
		for _, claimName := range possibleNavIdentClaims {
			if navIdentClaim, exists := introspectionResp.Claims[claimName]; exists {
				if navIdentStr, ok := navIdentClaim.(string); ok && navIdentStr != "" {
					navIdent = navIdentStr
					log.Printf("Found NAVident in claim '%s': %s", claimName, navIdent)
					break
				} else {
					log.Printf("Claim '%s' exists but is not a valid string: %T", claimName, navIdentClaim)
				}
			}
		}
		
		if navIdent == "" {
			log.Printf("NAVident not found in any expected claim names: %v", possibleNavIdentClaims)
		}
	} else {
		log.Printf("No claims found in token response")
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
			log.Printf("Authorization granted for NAVident: %s", navIdent)
			return true
		}
	}

	log.Printf("Authorization denied: NAVident '%s' not found in admin list (checked %d admins)", navIdent, len(admins))
	return false
}
