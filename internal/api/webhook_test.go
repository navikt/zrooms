package api_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/navikt/zrooms/internal/api"
	"github.com/navikt/zrooms/internal/config"
	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository"
	"github.com/navikt/zrooms/internal/repository/memory"
	"github.com/navikt/zrooms/internal/service"
	"github.com/navikt/zrooms/internal/zoom"
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

type deauthorizationFunc func(context.Context, string, time.Time) error

func (f deauthorizationFunc) DeauthorizeUser(ctx context.Context, id string, at time.Time) error {
	return f(ctx, id, at)
}

type zoomTransportFunc func(*http.Request) (*http.Response, error)

func (f zoomTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDeauthorizationRoute(t *testing.T) {
	t.Setenv("ZOOM_WEBHOOK_SECRET_TOKEN", secret)
	t.Setenv("ZOOM_CLIENT_ID", "public-app")
	t.Setenv("ZOOM_CLIENT_SECRET", "public-app-secret")
	t.Setenv("ZOOM_REDIRECT_URI", "https://zrooms.example.test/oauth/redirect")
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = zoomTransportFunc(func(r *http.Request) (*http.Response, error) {
		var body string
		switch r.URL.Host + r.URL.Path {
		case "zoom.us/oauth/token":
			require.NoError(t, r.ParseForm())
			body = fmt.Sprintf(`{"access_token":%q,"refresh_token":"refresh","expires_in":3600,"token_type":"bearer"}`, r.Form.Get("code"))
		case "api.zoom.us/v2/users/me":
			body = fmt.Sprintf(`{"id":%q,"email":"not-persisted@example.test"}`, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		default:
			t.Fatalf("unexpected Zoom request")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	repo := memory.NewRepository()
	svc := service.NewMeetingService(repo)
	cfg := config.GetZoomConfig()
	oauth := zoom.NewAPIManager(cfg, zoom.NewTokenStore(zoom.NewMemoryTokenBackend(), cfg.ClientSecret))
	mux := api.SetupRoutes(repo, svc, oauth)
	now := time.Now().UTC()
	for _, host := range []string{"owner", "someone-else"} {
		require.NoError(t, oauth.Authorize(context.Background(), host, now.Add(-time.Minute)))
		_, err := repo.ApplyEvent(context.Background(), models.MeetingEvent{
			Type: "meeting.created", EventTS: now.Add(-time.Minute).UnixMilli(),
			Meeting: models.Meeting{ID: host, Host: models.Participant{ID: host}},
		})
		require.NoError(t, err)
	}
	body := fmt.Sprintf(`{"event":"app_deauthorized","payload":{"account_id":"account","user_id":"owner","client_id":"public-app","deauthorization_time":%q}}`,
		now.Format(time.RFC3339Nano))
	for i := 0; i < 2; i++ {
		request := signedRequest(body, now)
		request.URL.Path = "/webhook/deauthorization"
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	}
	_, err := repo.GetMeeting(context.Background(), "owner")
	require.ErrorIs(t, err, repository.ErrNotFound)
	_, err = repo.GetMeeting(context.Background(), "someone-else")
	require.NoError(t, err)
	_, err = oauth.ClientForUser(context.Background(), "owner")
	require.ErrorIs(t, err, zoom.ErrReauthorizationRequired)
	_, err = oauth.ClientForUser(context.Background(), "someone-else")
	require.NoError(t, err)
}

func TestDeauthorizationRejectsInvalidRequests(t *testing.T) {
	now := time.Now()
	valid := fmt.Sprintf(`{"event":"app_deauthorized","payload":{"account_id":"account","user_id":"owner","client_id":"public-app","deauthorization_time":%q}}`,
		now.Format(time.RFC3339Nano))
	for _, test := range []struct {
		name     string
		body     string
		clientID string
		unsigned bool
		status   int
	}{
		{"missing configuration", valid, "", false, http.StatusServiceUnavailable},
		{"unsigned", valid, "public-app", true, http.StatusUnauthorized},
		{"wrong client", valid, "another-app", false, http.StatusUnauthorized},
		{"missing user", strings.Replace(valid, `"user_id":"owner"`, `"user_id":""`, 1), "public-app", false, http.StatusBadRequest},
		{"missing account", strings.Replace(valid, `"account_id":"account"`, `"account_id":""`, 1), "public-app", false, http.StatusBadRequest},
		{"bad timestamp", strings.Replace(valid, now.Format(time.RFC3339Nano), "invalid", 1), "public-app", false, http.StatusBadRequest},
		{"future timestamp", strings.Replace(valid, now.Format(time.RFC3339Nano), now.Add(6*time.Minute).Format(time.RFC3339Nano), 1), "public-app", false, http.StatusBadRequest},
		{"wrong event", strings.Replace(valid, "app_deauthorized", "meeting.created", 1), "public-app", false, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := api.NewDeauthorizationHandler(deauthorizationFunc(func(context.Context, string, time.Time) error {
				t.Fatal("invalid deauthorization reached persistence")
				return nil
			}), secret, test.clientID)
			request := signedRequest(test.body, now)
			if test.unsigned {
				request.Header.Del("x-zm-signature")
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			require.Equal(t, test.status, recorder.Code, recorder.Body.String())
		})
	}
}

func TestDeauthorizationPersistenceFailureIsRetryable(t *testing.T) {
	now := time.Now()
	handler := api.NewDeauthorizationHandler(deauthorizationFunc(func(ctx context.Context, id string, at time.Time) error {
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), 2500*time.Millisecond)
		require.Equal(t, "owner", id)
		require.WithinDuration(t, now, at, time.Millisecond)
		return errors.New("internal storage details")
	}), secret, "public-app")
	body := fmt.Sprintf(`{"event":"app_deauthorized","payload":{"account_id":"account","user_id":"owner","client_id":"public-app","deauthorization_time":%q}}`, now.Format(time.RFC3339Nano))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, signedRequest(body, now))
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "internal storage details")
}
