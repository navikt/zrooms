// Package api provides the HTTP handlers for the zrooms API
package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"
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
func HealthReadyHandler(checker interface{ Check(context.Context) error }) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		w.Header().Set("Content-Type", "application/json")
		if err := checker.Check(ctx); err != nil {
			log.Printf("Readiness check failed: %v", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(HealthResponse{Status: "DOWN"})
			return
		}
		_ = json.NewEncoder(w).Encode(HealthResponse{Status: "UP"})
	}
}
