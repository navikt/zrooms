package zoom

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/navikt/zrooms/internal/config"
	"github.com/navikt/zrooms/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testOAuthManager(t *testing.T, handler http.HandlerFunc) *APIManager {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := config.ZoomConfig{ClientID: "existing-client", ClientSecret: "existing-secret", RedirectURI: "https://zrooms.example.test/oauth/redirect"}
	manager := NewAPIManager(cfg, NewTokenStore(NewMemoryTokenBackend(), cfg.ClientSecret))
	manager.httpClient = server.Client()
	manager.tokenURL = server.URL + "/oauth/token"
	manager.apiURL = server.URL
	return manager
}

func writeTestTokens(w http.ResponseWriter) {
	_, _ = io.WriteString(w, `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600,"token_type":"bearer"}`)
}

func TestAuthorizationUsesExistingCredentialsAndPersistsEncryptedUserTokens(t *testing.T) {
	manager := testOAuthManager(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			require.Equal(t, http.MethodPost, r.Method)
			require.NoError(t, r.ParseForm())
			require.Equal(t, "authorization_code", r.Form.Get("grant_type"))
			require.Equal(t, "private-code", r.Form.Get("code"))
			require.Equal(t, "https://zrooms.example.test/oauth/redirect", r.Form.Get("redirect_uri"))
			id, secret, ok := r.BasicAuth()
			require.True(t, ok)
			require.Equal(t, "existing-client", id)
			require.Equal(t, "existing-secret", secret)
			writeTestTokens(w)
		case "/users/me":
			require.Equal(t, "Bearer new-access", r.Header.Get("Authorization"))
			_, _ = io.WriteString(w, `{"id":"owner","email":"owner@example.test","first_name":"not retained"}`)
		case "/meetings/123":
			require.Equal(t, "Bearer new-access", r.Header.Get("Authorization"))
			_, _ = io.WriteString(w, `{"id":123}`)
		default:
			t.Errorf("unexpected Zoom request path")
			w.WriteHeader(http.StatusNotFound)
		}
	})
	ctx := context.Background()
	started := time.Now()
	require.NoError(t, manager.Authorize(ctx, "private-code", started))
	encrypted, _, err := manager.tokens.backend.Read(ctx, "owner")
	require.NoError(t, err)
	for _, secret := range []string{"new-access", "new-refresh", "owner@example.test", "not retained", "private-code"} {
		require.NotContains(t, string(encrypted), secret)
	}
	// A new manager reads the same persisted grant; no process-local token cache is required.
	restarted := NewAPIManager(manager.config, NewTokenStore(manager.tokens.backend, manager.config.ClientSecret))
	restarted.apiURL, restarted.httpClient = manager.apiURL, manager.httpClient
	client, err := restarted.ClientForUser(ctx, "owner")
	require.NoError(t, err)
	email, err := client.GetUserEmail(ctx, "owner")
	require.NoError(t, err)
	require.Equal(t, "owner@example.test", email)
	raw, err := client.GetMeetingRawData(ctx, "123")
	require.NoError(t, err)
	require.JSONEq(t, `{"id":123}`, string(raw))
	_, err = restarted.ClientForUser(ctx, "different-owner")
	require.ErrorIs(t, err, ErrReauthorizationRequired)

	// Both the encryption key and the record's user binding are authenticated.
	wrongKey := NewTokenStore(manager.tokens.backend, "different-secret")
	_, _, err = wrongKey.read(ctx, "owner")
	require.Error(t, err)
	require.NoError(t, manager.tokens.backend.Write(ctx, "another-user", encrypted, 0))
	_, _, err = manager.tokens.read(ctx, "another-user")
	require.Error(t, err)
}

func TestConcurrentRefreshRotatesTokenOnce(t *testing.T) {
	var calls atomic.Int32
	manager := testOAuthManager(t, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		require.Equal(t, "refresh_token", r.Form.Get("grant_type"))
		require.Equal(t, "old-refresh", r.Form.Get("refresh_token"))
		calls.Add(1)
		writeTestTokens(w)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := time.Now().Add(-time.Hour)
	require.NoError(t, manager.tokens.write(ctx, "owner", authorization{
		AccessToken: "expired", RefreshToken: "old-refresh", ExpiresAt: time.Now().Add(-time.Second), AuthorizedAt: started,
	}, 0))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// A separate manager simulates concurrent pods sharing the token store.
			other := NewAPIManager(manager.config, manager.tokens)
			other.tokenURL = manager.tokenURL
			other.httpClient = manager.httpClient
			client, err := other.ClientForUser(ctx, "owner")
			if !assert.NoError(t, err) {
				return
			}
			require.Equal(t, "new-access", client.accessToken)
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, calls.Load())
	record, _, err := manager.tokens.read(ctx, "owner")
	require.NoError(t, err)
	require.Equal(t, "new-refresh", record.RefreshToken)
	require.False(t, record.Refreshing)
	require.Equal(t, started.UnixMilli(), record.AuthorizedAt.UnixMilli())
}

func TestDeauthorizationCannotBeUndoneByInFlightRefresh(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	manager := testOAuthManager(t, func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		writeTestTokens(w)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, manager.tokens.write(ctx, "owner", authorization{
		AccessToken: "expired", RefreshToken: "old-refresh", ExpiresAt: time.Now().Add(-time.Second),
		AuthorizedAt: time.Now().Add(-time.Hour),
	}, 0))
	result := make(chan error, 1)
	go func() {
		_, err := manager.ClientForUser(ctx, "owner")
		result <- err
	}()
	<-entered
	require.NoError(t, manager.Revoke(ctx, "owner", time.Now()))
	close(release)
	require.ErrorIs(t, <-result, repository.ErrConflict)
	record, _, err := manager.tokens.read(ctx, "owner")
	require.NoError(t, err)
	require.Empty(t, record.AccessToken)
	require.Empty(t, record.RefreshToken)
	_, err = manager.ClientForUser(ctx, "owner")
	require.ErrorIs(t, err, ErrReauthorizationRequired)
}

func TestRevocationBlocksOlderCallbacksAndPreservesNewInstallation(t *testing.T) {
	manager := testOAuthManager(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" {
			writeTestTokens(w)
		} else {
			_, _ = io.WriteString(w, `{"id":"owner","email":"owner@example.test"}`)
		}
	})
	ctx := context.Background()
	cutoff := time.Now().Add(-time.Minute)
	require.NoError(t, manager.Revoke(ctx, "owner", cutoff))
	require.ErrorIs(t, manager.Authorize(ctx, "old-code", cutoff.Add(-time.Second)), ErrReauthorizationRequired)
	require.NoError(t, manager.Authorize(ctx, "new-code", cutoff.Add(time.Second)))
	require.NoError(t, manager.Revoke(ctx, "owner", cutoff))
	_, err := manager.ClientForUser(ctx, "owner")
	require.NoError(t, err)
	require.Error(t, manager.Authorize(ctx, "out-of-order-code", cutoff.Add(time.Millisecond)))
	require.NoError(t, manager.Revoke(ctx, "owner", time.Now()))
	_, err = manager.ClientForUser(ctx, "owner")
	require.ErrorIs(t, err, ErrReauthorizationRequired)
}

func TestFailedRefreshIsNotBlindlyRetried(t *testing.T) {
	calls := 0
	manager := testOAuthManager(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, `private-response-with-refresh-token`, http.StatusBadRequest)
	})
	ctx := context.Background()
	require.NoError(t, manager.tokens.write(ctx, "owner", authorization{
		AccessToken: "expired", RefreshToken: "old-refresh", ExpiresAt: time.Now().Add(-time.Second),
		AuthorizedAt: time.Now().Add(-time.Hour),
	}, 0))
	_, err := manager.ClientForUser(ctx, "owner")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private-response")
	waitCtx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = manager.ClientForUser(waitCtx, "owner")
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, calls)
}

type failedTokenBackend struct {
	TokenBackend
}

func (failedTokenBackend) Write(context.Context, string, []byte, int64) error {
	return errors.New("storage unavailable")
}

func TestAuthorizationDoesNotSucceedWhenPersistenceFails(t *testing.T) {
	manager := testOAuthManager(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" {
			writeTestTokens(w)
		} else {
			_, _ = io.WriteString(w, `{"id":"owner","email":"owner@example.test"}`)
		}
	})
	manager.tokens.backend = failedTokenBackend{TokenBackend: manager.tokens.backend}
	require.ErrorContains(t, manager.Authorize(context.Background(), "code", time.Now()), "storage unavailable")
}
