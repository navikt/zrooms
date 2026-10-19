package api_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/navikt/zrooms/internal/api"
	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository/memory"
	"github.com/navikt/zrooms/internal/service"
	"github.com/stretchr/testify/require"
)

const secret = "webhook-test-secret"

func signedRequest(body string, timestamp time.Time) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	ts := strconv.FormatInt(timestamp.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "v0:%s:%s", ts, body)
	r.Header.Set("x-zm-request-timestamp", ts)
	r.Header.Set("x-zm-signature", "v0="+hex.EncodeToString(mac.Sum(nil)))
	return r
}

func TestWebhookLifecycle(t *testing.T) {
	repo := memory.NewRepository()
	handler := api.NewWebhookHandler(service.NewMeetingService(repo), secret)
	now := time.Now().Add(-time.Minute)
	send := func(event string, offset int64, object, operator string) {
		t.Helper()
		body := fmt.Sprintf(`{"event":%q,"event_ts":%d,"payload":{"operator":%q,"object":%s}}`,
			event, now.UnixMilli()+offset, operator, object)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, signedRequest(body, time.Now()))
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	}
	send("meeting.created", 1, `{"id":123,"topic":"Scheduled","host_id":"host"}`, "creator@example.test")
	send("meeting.started", 2, `{"id":"123","uuid":"instance","start_time":"2026-10-01T09:00:00Z"}`, "")
	send("meeting.participant_joined", 3, `{"id":"123","uuid":"instance","participant":{"id":"","user_id":"guest-1","email":"not-persisted@example.test"}}`, "")
	send("meeting.participant_joined", 4, `{"id":"123","uuid":"instance","participant":{"id":"","user_id":"guest-2"}}`, "")
	send("meeting.updated", 5, `{"id":123,"topic":"Updated"}`, "editor@example.test")
	meeting, err := repo.GetMeeting(context.Background(), "123")
	require.NoError(t, err)
	require.Equal(t, models.MeetingStatusStarted, meeting.Status)
	require.Equal(t, 2, meeting.ParticipantCount)
	require.Equal(t, "Updated", meeting.Topic)
	require.Equal(t, "creator@example.test", meeting.CreatorEmail)
	require.Equal(t, "editor@example.test", meeting.OperatorEmail)
	require.Equal(t, "host", meeting.Host.ID)
	send("meeting.ended", 6, `{"id":"123","uuid":"instance","end_time":"2026-10-01T10:00:00Z"}`, "")
	meeting, err = repo.GetMeeting(context.Background(), "123")
	require.NoError(t, err)
	require.Zero(t, meeting.ParticipantCount)
	require.Equal(t, 2026, meeting.StartTime.Year())
	require.Equal(t, 10, meeting.EndTime.Hour())
}

type processorFunc func(context.Context, models.MeetingEvent) error

func (f processorFunc) ProcessEvent(ctx context.Context, event models.MeetingEvent) error {
	return f(ctx, event)
}

func TestWebhookRejectsInvalidRequests(t *testing.T) {
	now := time.Now()
	valid := fmt.Sprintf(`{"event":"meeting.started","event_ts":%d,"payload":{"object":{"id":"123","uuid":"instance"}}}`, now.UnixMilli())
	tests := []struct {
		name   string
		body   string
		secret string
		change func(*http.Request)
		status int
	}{
		{name: "missing secret", body: valid, status: http.StatusServiceUnavailable},
		{name: "missing signature", body: valid, secret: secret, change: func(r *http.Request) { r.Header.Del("x-zm-signature") }, status: http.StatusUnauthorized},
		{name: "tampered body", body: valid, secret: secret, change: func(r *http.Request) { r.Header.Set("x-zm-signature", "v0=0000") }, status: http.StatusUnauthorized},
		{name: "expired signature", body: valid, secret: secret, change: func(r *http.Request) { r.Header = signedRequest(valid, now.Add(-6*time.Minute)).Header }, status: http.StatusUnauthorized},
		{name: "future signature", body: valid, secret: secret, change: func(r *http.Request) { r.Header = signedRequest(valid, now.Add(6*time.Minute)).Header }, status: http.StatusUnauthorized},
		{name: "wrong method", body: valid, secret: secret, change: func(r *http.Request) { r.Method = http.MethodGet }, status: http.StatusMethodNotAllowed},
		{name: "oversized signed body", body: strings.Repeat(" ", (1<<20)+1), secret: secret, status: http.StatusRequestEntityTooLarge},
		{name: "malformed JSON", body: `{"event":`, secret: secret, status: http.StatusBadRequest},
		{name: "missing event type", body: `{}`, secret: secret, status: http.StatusBadRequest},
		{name: "missing ID", body: `{"event":"meeting.created","event_ts":1,"payload":{"object":{}}}`, secret: secret, status: http.StatusBadRequest},
		{name: "missing event timestamp", body: `{"event":"meeting.created","payload":{"object":{"id":"123"}}}`, secret: secret, status: http.StatusBadRequest},
		{name: "missing participant identity", body: `{"event":"meeting.participant_joined","event_ts":1,"payload":{"object":{"id":"123","uuid":"instance","participant":{}}}}`, secret: secret, status: http.StatusBadRequest},
		{name: "unsupported", body: `{"event":"something.else","payload":{}}`, secret: secret, status: http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := api.NewWebhookHandler(processorFunc(func(context.Context, models.MeetingEvent) error {
				t.Fatal("invalid webhook reached persistence")
				return nil
			}), tt.secret)
			req := signedRequest(tt.body, now)
			if tt.change != nil {
				tt.change(req)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			require.Equal(t, tt.status, recorder.Code, recorder.Body.String())
		})
	}
}

func TestWebhookRetriesPersistenceFailure(t *testing.T) {
	handler := api.NewWebhookHandler(processorFunc(func(ctx context.Context, _ models.MeetingEvent) error {
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), 2500*time.Millisecond)
		return errors.New("internal bucket details")
	}), secret)
	body := fmt.Sprintf(`{"event":"meeting.created","event_ts":%d,"payload":{"object":{"id":"123"}}}`, time.Now().UnixMilli())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, signedRequest(body, time.Now()))
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "bucket details")
}

func TestWebhookURLValidation(t *testing.T) {
	handler := api.NewWebhookHandler(nil, secret)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, signedRequest(`{"event":"endpoint.url_validation","payload":{"plainToken":"challenge"}}`, time.Now()))
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("challenge"))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, fmt.Sprintf(`{"plainToken":"challenge","encryptedToken":%q}`, hex.EncodeToString(mac.Sum(nil))), recorder.Body.String())
}
