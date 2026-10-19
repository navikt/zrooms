package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/navikt/zrooms/internal/config"
	"github.com/stretchr/testify/require"
)

type authorizeFunc func(context.Context, string, time.Time) error

func (f authorizeFunc) Authorize(ctx context.Context, code string, started time.Time) error {
	return f(ctx, code, started)
}

func oauthTestConfig() config.ZoomConfig {
	return config.ZoomConfig{
		ClientID: "public-client", ClientSecret: "public-secret",
		RedirectURI: "https://zrooms.example.test/oauth/redirect",
	}
}

func startOAuth(t *testing.T, handler *OAuthHandler) (*http.Cookie, string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.Start(recorder, httptest.NewRequest(http.MethodGet, "/oauth/start", nil))
	require.Equal(t, http.StatusSeeOther, recorder.Code)
	location, err := url.Parse(recorder.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "https", location.Scheme)
	require.Equal(t, "zoom.us", location.Host)
	require.Equal(t, "public-client", location.Query().Get("client_id"))
	state := location.Query().Get("state")
	require.Len(t, state, 43)
	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 1)
	require.True(t, cookies[0].HttpOnly)
	require.True(t, cookies[0].Secure)
	require.Equal(t, http.SameSiteLaxMode, cookies[0].SameSite)
	require.Equal(t, "/oauth", cookies[0].Path)
	require.Equal(t, 600, cookies[0].MaxAge)
	return cookies[0], state
}

func TestOAuthExchangeBeforeSuccess(t *testing.T) {
	calls := 0
	handler := NewOAuthHandler(oauthTestConfig(), authorizeFunc(func(ctx context.Context, code string, started time.Time) error {
		calls++
		require.Equal(t, "test-code", code)
		require.WithinDuration(t, time.Now(), started, time.Second)
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), 15*time.Second)
		return nil
	}))
	cookie, state := startOAuth(t, handler)
	request := httptest.NewRequest(http.MethodGet, "/oauth/redirect?code=test-code&state="+state, nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	handler.Redirect(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, calls)
	require.Contains(t, recorder.Body.String(), "Authorization successful")
	require.NotContains(t, recorder.Body.String(), "test-code")
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	require.Equal(t, "no-referrer", recorder.Header().Get("Referrer-Policy"))
	require.Equal(t, -1, recorder.Result().Cookies()[0].MaxAge)
}

func TestOAuthRejectsUnboundCallbacks(t *testing.T) {
	handler := NewOAuthHandler(oauthTestConfig(), authorizeFunc(func(context.Context, string, time.Time) error {
		t.Fatal("unverified callback reached token exchange")
		return nil
	}))
	cookie, state := startOAuth(t, handler)
	for _, test := range []struct {
		name   string
		cookie string
		query  string
		method string
		status int
	}{
		{"missing cookie", "", "?code=code&state=" + state, http.MethodGet, http.StatusBadRequest},
		{"missing state", cookie.Value, "?code=code", http.MethodGet, http.StatusBadRequest},
		{"wrong state", cookie.Value, "?code=code&state=attacker", http.MethodGet, http.StatusBadRequest},
		{"tampered cookie", cookie.Value + "x", "?code=code&state=" + state, http.MethodGet, http.StatusBadRequest},
		{"missing code", cookie.Value, "?state=" + state, http.MethodGet, http.StatusBadRequest},
		{"denied", cookie.Value, "?state=" + state + "&error=access_denied", http.MethodGet, http.StatusBadRequest},
		{"wrong method", cookie.Value, "?code=code&state=" + state, http.MethodPost, http.StatusMethodNotAllowed},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "/oauth/redirect"+test.query, nil)
			if test.cookie != "" {
				request.AddCookie(&http.Cookie{Name: oauthCookieName, Value: test.cookie})
			}
			recorder := httptest.NewRecorder()
			handler.Redirect(recorder, request)
			require.Equal(t, test.status, recorder.Code)
			require.NotContains(t, recorder.Body.String(), "Authorization successful")
		})
	}
	for _, issued := range []time.Time{time.Now().Add(-11 * time.Minute), time.Now().Add(time.Minute)} {
		value := state + "." + strconv.FormatInt(issued.UnixMilli(), 10)
		request := httptest.NewRequest(http.MethodGet, "/oauth/redirect?code=code&state="+state, nil)
		request.AddCookie(&http.Cookie{Name: oauthCookieName, Value: value + "." + handler.sign(value)})
		recorder := httptest.NewRecorder()
		handler.Redirect(recorder, request)
		require.Equal(t, http.StatusBadRequest, recorder.Code)
	}
}

func TestOAuthFailureDoesNotReportSuccess(t *testing.T) {
	handler := NewOAuthHandler(oauthTestConfig(), authorizeFunc(func(context.Context, string, time.Time) error {
		return errors.New("storage unavailable")
	}))
	cookie, state := startOAuth(t, handler)
	request := httptest.NewRequest(http.MethodGet, "/oauth/redirect?code=private-code&state="+state, nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	handler.Redirect(recorder, request)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "storage unavailable")
	require.NotContains(t, recorder.Body.String(), "private-code")
	require.NotContains(t, recorder.Body.String(), "Authorization successful")
}

func TestOAuthRefusesUnsafeRedirectConfiguration(t *testing.T) {
	for _, redirect := range []string{"http://example.test/oauth/redirect", "//example.test/oauth/redirect", "https://user:secret@example.test/oauth/redirect"} {
		cfg := oauthTestConfig()
		cfg.RedirectURI = redirect
		handler := NewOAuthHandler(cfg, nil)
		recorder := httptest.NewRecorder()
		handler.Start(recorder, httptest.NewRequest(http.MethodGet, "/oauth/start", nil))
		require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
		require.False(t, strings.Contains(recorder.Header().Get("Location"), "zoom.us"))
	}
}
