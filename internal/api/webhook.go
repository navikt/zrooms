package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/navikt/zrooms/internal/models"
)

const maxWebhookBody = 1 << 20

type EventProcessor interface {
	ProcessEvent(context.Context, models.MeetingEvent) error
}

type WebhookHandler struct {
	processor   EventProcessor
	secretToken string
}

func NewWebhookHandler(processor EventProcessor, secretToken string) *WebhookHandler {
	return &WebhookHandler{processor: processor, secretToken: secretToken}
}

func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.secretToken == "" {
		log.Print("Webhook rejected: verification is not configured")
		http.Error(w, "Webhook unavailable", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBody)
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var sizeError *http.MaxBytesError
		if errors.As(err, &sizeError) {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
		}
		return
	}
	if !h.verifySignature(r.Header, body) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var webhook models.WebhookEvent
	if err := json.Unmarshal(body, &webhook); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}
	if webhook.Event == "endpoint.url_validation" {
		h.validateEndpoint(w, webhook.Payload)
		return
	}
	event, err := webhook.MeetingEvent()
	if errors.Is(err, models.ErrUnsupportedEvent) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil || event.EventTS > time.Now().Add(5*time.Minute).UnixMilli() {
		http.Error(w, "Invalid event payload", http.StatusBadRequest)
		return
	}
	// Zoom requires an acknowledgment within three seconds and retries 5xx responses.
	ctx, cancel := context.WithTimeout(r.Context(), 2500*time.Millisecond)
	defer cancel()
	if err := h.processor.ProcessEvent(ctx, event); err != nil {
		log.Printf("Webhook persistence failed: %v", err)
		http.Error(w, "Unable to persist event", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"success":true}`)
}

func (h *WebhookHandler) verifySignature(headers http.Header, body []byte) bool {
	timestamp := headers.Get("x-zm-request-timestamp")
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}
	requestTime := time.Unix(seconds, 0)
	if requestTime.Before(time.Now().Add(-5*time.Minute)) || requestTime.After(time.Now().Add(5*time.Minute)) {
		return false
	}
	signature := headers.Get("x-zm-signature")
	if !strings.HasPrefix(signature, "v0=") {
		return false
	}
	received, err := hex.DecodeString(strings.TrimPrefix(signature, "v0="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(h.secretToken))
	fmt.Fprintf(mac, "v0:%s:", timestamp)
	_, _ = mac.Write(body)
	return hmac.Equal(mac.Sum(nil), received)
}

func (h *WebhookHandler) validateEndpoint(w http.ResponseWriter, payload json.RawMessage) {
	var challenge struct {
		PlainToken string `json:"plainToken"`
	}
	if err := json.Unmarshal(payload, &challenge); err != nil || challenge.PlainToken == "" {
		http.Error(w, "Invalid validation request", http.StatusBadRequest)
		return
	}
	mac := hmac.New(sha256.New, []byte(h.secretToken))
	_, _ = mac.Write([]byte(challenge.PlainToken))
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"plainToken": challenge.PlainToken, "encryptedToken": hex.EncodeToString(mac.Sum(nil)),
	}); err != nil {
		log.Printf("Writing webhook validation response failed: %v", err)
	}
}
