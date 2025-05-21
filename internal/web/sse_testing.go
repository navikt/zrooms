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

	// Log TLS information in more detail
	directTLS := r.TLS != nil
	log.Printf("SSE REQUEST DIRECT TLS: %v", directTLS)

	// Check proxy headers for TLS information
	forwardedProto := r.Header.Get("X-Forwarded-Proto")
	isProxiedTLS := forwardedProto == "https"
	log.Printf("SSE REQUEST X-FORWARDED-PROTO: %s", forwardedProto)
	log.Printf("SSE REQUEST PROXIED TLS: %v", isProxiedTLS)

	// Determine effective security
	effectivelySecure := directTLS || isProxiedTLS
	log.Printf("SSE REQUEST EFFECTIVELY SECURE: %v", effectivelySecure)

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
		"X-HX-Request", "Cookie", "Authorization",
		"X-Forwarded-Host", "X-Forwarded-Server",
		"Forwarded",
	}

	log.Println("SSE REQUEST HEADERS:")
	for _, header := range relevantHeaders {
		if value := r.Header.Get(header); value != "" {
			// Mask sensitive data in cookies for security
			if header == "Cookie" || header == "Authorization" {
				log.Printf("  %s: [PRESENT BUT MASKED]", header)
			} else {
				log.Printf("  %s: %s", header, value)
			}
		}
	}

	// Log cookies presence for debugging (without exposing values)
	cookies := r.Cookies()
	if len(cookies) > 0 {
		cookieNames := make([]string, 0, len(cookies))
		for _, cookie := range cookies {
			cookieNames = append(cookieNames, cookie.Name)
		}
		log.Printf("SSE REQUEST COOKIES (names): %v", cookieNames)
		log.Printf("SSE REQUEST COOKIE COUNT: %d", len(cookies))
	} else {
		log.Printf("SSE REQUEST COOKIES: None found")
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
