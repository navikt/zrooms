package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/navikt/zrooms/internal/api"
	"github.com/navikt/zrooms/internal/repository/memory"
	"github.com/stretchr/testify/assert"
)

func TestHealthLive(t *testing.T) {
	// Create a new request
	req, err := http.NewRequest("GET", "/health/live", nil)
	assert.NoError(t, err)

	// Create a response recorder
	rr := httptest.NewRecorder()

	// Create the handler
	handler := http.HandlerFunc(api.HealthLiveHandler)

	// Serve the request
	handler.ServeHTTP(rr, req)

	// Check the status code
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check the content type
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	// Check the response body
	var response map[string]string
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "UP", response["status"])
}

func TestHealthReady(t *testing.T) {
	// Create a new request
	req, err := http.NewRequest("GET", "/health/ready", nil)
	assert.NoError(t, err)

	// Create a response recorder
	rr := httptest.NewRecorder()

	// Create the handler
	handler := api.HealthReadyHandler(memory.NewRepository())

	// Serve the request
	handler.ServeHTTP(rr, req)

	// Check the status code
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check the content type
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	// Check the response body
	var response map[string]string
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "UP", response["status"])
}

type failedCheck struct{}

func (failedCheck) Check(context.Context) error { return errors.New("bucket unavailable") }

func TestReadinessReportsStorageFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	api.HealthReadyHandler(failedCheck{}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.JSONEq(t, `{"status":"DOWN"}`, recorder.Body.String())
}
