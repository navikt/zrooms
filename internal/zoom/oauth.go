package zoom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/navikt/zrooms/internal/config"
	"github.com/navikt/zrooms/internal/repository"
)

// ErrReauthorizationRequired means the owner must use the installation link again.
var ErrReauthorizationRequired = errors.New("Zoom authorization unavailable; owner must authorize again")

// APIManager manages per-user authorizations for the existing public OAuth app.
type APIManager struct {
	config     config.ZoomConfig
	tokens     *TokenStore
	httpClient *http.Client
	tokenURL   string
	apiURL     string
}

// NewAPIManager shares durable authorizations between callbacks and admin requests.
func NewAPIManager(cfg config.ZoomConfig, tokens *TokenStore) *APIManager {
	return &APIManager{
		config: cfg, tokens: tokens,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		tokenURL:   "https://zoom.us/oauth/token",
		apiURL:     "https://api.zoom.us/v2",
	}
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

func (m *APIManager) requestTokens(ctx context.Context, values url.Values) (authorization, error) {
	if !m.config.IsZoomConfigValid() {
		return authorization{}, errors.New("Zoom OAuth is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.tokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return authorization{}, errors.New("invalid Zoom token request")
	}
	req.SetBasicAuth(m.config.ClientID, m.config.ClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return authorization{}, errors.New("Zoom token request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return authorization{}, fmt.Errorf("Zoom token request returned status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return authorization{}, errors.New("invalid Zoom token response")
	}
	var tokens tokenResponse
	if json.Unmarshal(data, &tokens) != nil || tokens.AccessToken == "" || tokens.RefreshToken == "" ||
		tokens.ExpiresIn <= 60 || tokens.ExpiresIn > 86400 || !strings.EqualFold(tokens.TokenType, "bearer") {
		return authorization{}, errors.New("invalid Zoom token response")
	}
	return authorization{
		AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken,
		ExpiresAt: time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second),
	}, nil
}

func (m *APIManager) client(token string) *APIClient {
	return &APIClient{accessToken: token, baseURL: m.apiURL, httpClient: m.httpClient}
}

// Authorize exchanges a state-validated code and binds tokens to /users/me.
func (m *APIManager) Authorize(ctx context.Context, code string, started time.Time) error {
	record, err := m.requestTokens(ctx, url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {m.config.RedirectURI},
	})
	if err != nil {
		return err
	}
	user, err := m.client(record.AccessToken).currentUser(ctx)
	if err != nil {
		return err
	}
	record.AuthorizedAt = started
	for attempt := 0; attempt < 5; attempt++ {
		previous, version, err := m.tokens.read(ctx, user.ID)
		if err != nil {
			return err
		}
		if !previous.RevokedAt.IsZero() && !started.After(previous.RevokedAt) {
			return ErrReauthorizationRequired
		}
		if previous.AuthorizedAt.After(started) {
			return errors.New("a newer Zoom authorization already exists")
		}
		record.RevokedAt = previous.RevokedAt
		err = m.tokens.write(ctx, user.ID, record, version)
		if errors.Is(err, repository.ErrConflict) {
			continue
		}
		return err
	}
	return fmt.Errorf("save Zoom authorization: %w", repository.ErrConflict)
}

// ClientForUser refreshes only the named host's authorization, never another user's.
func (m *APIManager) ClientForUser(ctx context.Context, userID string) (*APIClient, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if strings.TrimSpace(userID) == "" {
		return nil, ErrReauthorizationRequired
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		record, version, err := m.tokens.read(ctx, userID)
		if err != nil {
			return nil, err
		}
		if record.AccessToken == "" || (!record.RevokedAt.IsZero() && !record.AuthorizedAt.After(record.RevokedAt)) {
			return nil, ErrReauthorizationRequired
		}
		if record.Refreshing {
			// A persisted claim prevents reuse of a rotating refresh token across pods.
			// A crashed or ambiguous refresh requires reauthorization, not a blind retry.
			if time.Since(record.UpdatedAt) > 30*time.Second {
				return nil, ErrReauthorizationRequired
			}
			timer := time.NewTimer(25 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
				continue
			}
		}
		if time.Until(record.ExpiresAt) > time.Minute {
			return m.client(record.AccessToken), nil
		}
		record.Refreshing = true
		err = m.tokens.write(ctx, userID, record, version)
		if errors.Is(err, repository.ErrConflict) {
			continue
		}
		if err != nil {
			return nil, err
		}
		claimed, claimedVersion, err := m.tokens.read(ctx, userID)
		if err != nil {
			return nil, err
		}
		// An uninstall or reauthorization may have superseded this refresh claim.
		if !claimed.Refreshing || claimed.RefreshToken != record.RefreshToken || !claimed.AuthorizedAt.Equal(record.AuthorizedAt) {
			continue
		}
		refreshed, err := m.requestTokens(ctx, url.Values{
			"grant_type": {"refresh_token"}, "refresh_token": {record.RefreshToken},
		})
		if err != nil {
			return nil, err
		}
		refreshed.AuthorizedAt = record.AuthorizedAt
		refreshed.RevokedAt = record.RevokedAt
		// Never retry a token rotation against a different generation.
		if err := m.tokens.write(ctx, userID, refreshed, claimedVersion); err != nil {
			return nil, err
		}
		return m.client(refreshed.AccessToken), nil
	}
}

// Revoke removes credentials before meeting cleanup; its marker blocks in-flight callbacks.
func (m *APIManager) Revoke(ctx context.Context, userID string, at time.Time) error {
	for attempt := 0; attempt < 5; attempt++ {
		record, version, err := m.tokens.read(ctx, userID)
		if err != nil {
			return err
		}
		if !record.RevokedAt.Before(at) {
			return nil
		}
		if record.AuthorizedAt.After(at) {
			// Preserve a genuinely newer installation.
			record.RevokedAt = at
		} else {
			record = authorization{RevokedAt: at}
		}
		err = m.tokens.write(ctx, userID, record, version)
		if errors.Is(err, repository.ErrConflict) {
			continue
		}
		return err
	}
	return fmt.Errorf("revoke Zoom authorization: %w", repository.ErrConflict)
}
