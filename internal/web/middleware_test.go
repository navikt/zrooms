package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHTTPProtocolMiddleware(t *testing.T) {
	// Create a test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	})

	// Wrap with the middleware
	wrappedHandler := HTTPProtocolMiddleware(testHandler)

	t.Run("DisablesHTTP3Globally", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/test", nil)

		wrappedHandler.ServeHTTP(recorder, request)

		// Check that Alt-Svc is set to clear (disables HTTP/3)
		assert.Equal(t, "clear", recorder.Header().Get("Alt-Svc"))
	})

	t.Run("AddsSSESpecificHeadersForEventsEndpoint", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/events", nil)

		wrappedHandler.ServeHTTP(recorder, request)

		// Check SSE-specific headers
		assert.Equal(t, "clear", recorder.Header().Get("Alt-Svc"))
		assert.Equal(t, "keep-alive", recorder.Header().Get("Connection"))
		assert.Equal(t, "true", recorder.Header().Get("X-Force-HTTP1"))
		assert.Equal(t, "", recorder.Header().Get("Upgrade"))
	})

	t.Run("DoesNotAddSSEHeadersForNonEventsEndpoint", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/meetings", nil)

		wrappedHandler.ServeHTTP(recorder, request)

		// Check that only global headers are set
		assert.Equal(t, "clear", recorder.Header().Get("Alt-Svc"))
		assert.Empty(t, recorder.Header().Get("Connection"))
		assert.Empty(t, recorder.Header().Get("X-Force-HTTP1"))
	})
}

func TestWrapMuxWithMiddleware(t *testing.T) {
	// Create a test mux
	mux := http.NewServeMux()
	mux.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with middleware
	wrappedHandler := WrapMuxWithMiddleware(mux)

	// Test that the wrapper works
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/test", nil)

	wrappedHandler.ServeHTTP(recorder, request)

	// Check that middleware headers are applied
	assert.Equal(t, "clear", recorder.Header().Get("Alt-Svc"))
	assert.Equal(t, http.StatusOK, recorder.Code)
}
