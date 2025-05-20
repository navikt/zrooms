package web

import (
	"log"
	"net/http"
	"strings"
)

// monitorRequest helps with debugging by printing headers and connection info
func monitorRequest(r *http.Request) {
	log.Printf("SSE REQUEST FROM: %s", r.RemoteAddr)
	log.Printf("SSE REQUEST PROTOCOL: %s", r.Proto)
	log.Printf("SSE REQUEST TLS: %v", r.TLS != nil)
	log.Printf("SSE REQUEST URL: %s", r.URL.String())
	log.Printf("SSE REQUEST HOST: %s", r.Host)
	log.Printf("SSE REQUEST METHOD: %s", r.Method)

	// Print important headers for debugging SSE connection issues
	relevantHeaders := []string{
		"Accept", "Connection", "User-Agent",
		"Accept-Encoding", "X-Forwarded-For",
		"X-Forwarded-Proto", "Upgrade",
		"Origin", "Referer", "Cache-Control",
		"Host", "X-Requested-With", "X-Real-IP",
		"X-HX-Request",
	}

	log.Println("SSE REQUEST HEADERS:")
	for _, header := range relevantHeaders {
		if value := r.Header.Get(header); value != "" {
			log.Printf("  %s: %s", header, value)
		}
	}
}

// logResponseHeaders helps with debugging by logging the response headers
func logResponseHeaders(w http.ResponseWriter) {
	// Try to access headers if possible through type assertion
	if rw, ok := w.(interface {
		Header() http.Header
	}); ok {
		headers := rw.Header()
		log.Println("SSE RESPONSE HEADERS:")
		for k, v := range headers {
			log.Printf("  %s: %s", k, strings.Join(v, ", "))
		}
	}
}
