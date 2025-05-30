// Package api provides the HTTP handlers for the zrooms API
package api

import (
	"encoding/json"
	"net/http"
)

// HealthResponse represents the response for health check endpoints
type HealthResponse struct {
	Status string `json:"status"`
}

// HealthLiveHandler handles Kubernetes liveness probe requests
func HealthLiveHandler(w http.ResponseWriter, r *http.Request) {
	response := HealthResponse{
		Status: "UP",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// HealthReadyHandler handles Kubernetes readiness probe requests
func HealthReadyHandler(w http.ResponseWriter, r *http.Request) {
	response := HealthResponse{
		Status: "UP",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
