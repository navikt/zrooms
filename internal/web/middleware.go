package web

import (
	"net/http"
	"strings"
)

// HTTPProtocolMiddleware prevents HTTP/3 QUIC protocol issues in cloud environments
// This middleware adds headers to prevent browsers from attempting HTTP/3 connections
// which can cause net::ERR_QUIC_PROTOCOL_ERROR in complex proxy setups
func HTTPProtocolMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Disable HTTP/3 QUIC protocol advertising globally
		w.Header().Set("Alt-Svc", "clear")

		// For SSE endpoints, add additional headers to ensure stable connections
		if strings.HasPrefix(r.URL.Path, "/events") {
			// Force HTTP/1.1 semantics for SSE
			w.Header().Set("Connection", "keep-alive")
			w.Header().Set("X-Force-HTTP1", "true")
			w.Header().Set("Upgrade", "")
		}

		// Continue with the next handler
		next.ServeHTTP(w, r)
	})
}

// WrapMuxWithMiddleware wraps an HTTP mux with the protocol middleware
func WrapMuxWithMiddleware(mux *http.ServeMux) http.Handler {
	return HTTPProtocolMiddleware(mux)
}
