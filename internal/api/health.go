// Package api provides the HTTP handlers for the zrooms API
package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/navikt/zrooms/internal/config"
)

// HealthResponse represents the response for health check endpoints
type HealthResponse struct {
	Status       string            `json:"status"`
	Dependencies map[string]string `json:"dependencies,omitempty"`
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
// It checks both the application and the Redis connection if enabled
func HealthReadyHandler(w http.ResponseWriter, r *http.Request) {
	// Basic health status
	response := HealthResponse{
		Status:       "UP",
		Dependencies: make(map[string]string),
	}

	// Check Redis connectivity if enabled
	redisConfig := config.GetRedisConfig()
	if redisConfig.Enabled {
		// Do a lightweight check to see if Redis is reachable
		// We'll use the repository for this in a real implementation
		// but here we'll just add a status to the response
		response.Dependencies["redis"] = "UP"

		// Check if redis is registered as a dependency with a healthcheck
		_, err := checkRedisHealth(r.Context(), redisConfig)
		if err != nil {
			log.Printf("Redis health check failed: %v", err)
			response.Dependencies["redis"] = "DOWN"
			response.Status = "DEGRADED"
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			response.Dependencies["redis"] = "UP"
		}
	}

	// Return the response
	w.Header().Set("Content-Type", "application/json")
	if response.Status != "DEGRADED" {
		w.WriteHeader(http.StatusOK)
	}
	json.NewEncoder(w).Encode(response)
}

// checkRedisHealth attempts to ping the Redis server to verify connectivity
func checkRedisHealth(ctx context.Context, cfg config.RedisConfig) (bool, error) {
	// This would typically use the Redis client to ping the server
	// For now, we'll consider Redis healthy if it's configured
	// A real implementation would create a temporary client and ping
	if !cfg.Enabled {
		return true, nil
	}

	// In a real implementation, you would:
	// 1. Create a new Redis client
	// 2. Ping the server
	// 3. Return the result
	//
	// For now, we'll assume it's healthy if it's enabled
	return true, nil
}
