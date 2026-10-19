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
)

// APIClient makes requests with one Zoom user's access token.
type APIClient struct {
	accessToken string
	baseURL     string
	httpClient  *http.Client
}

// NewAPIClient creates a Zoom client with a bounded request timeout.
func NewAPIClient(accessToken string) *APIClient {
	return &APIClient{
		accessToken: accessToken,
		baseURL:     "https://api.zoom.us/v2",
		httpClient:  &http.Client{Timeout: 10 * time.Second},
	}
}

type zoomUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

func (c *APIClient) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, errors.New("invalid Zoom API request")
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.New("Zoom API request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Zoom API request returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return nil, errors.New("invalid Zoom API response")
	}
	return body, nil
}

func (c *APIClient) currentUser(ctx context.Context) (zoomUser, error) {
	body, err := c.get(ctx, "/users/me")
	if err != nil {
		return zoomUser{}, err
	}
	var user zoomUser
	if json.Unmarshal(body, &user) != nil || strings.TrimSpace(user.ID) == "" || len(user.ID) > 256 || strings.TrimSpace(user.Email) == "" {
		return zoomUser{}, errors.New("invalid Zoom user response")
	}
	return user, nil
}

// GetUserEmail verifies the access token belongs to the requested meeting host.
func (c *APIClient) GetUserEmail(ctx context.Context, userID string) (string, error) {
	if strings.TrimSpace(userID) == "" {
		return "", errors.New("Zoom user ID is required")
	}
	user, err := c.currentUser(ctx)
	if err != nil {
		return "", err
	}
	if user.ID != userID {
		return "", errors.New("Zoom authorization does not match meeting owner")
	}
	return user.Email, nil
}

// GetMeetingRawData fetches a meeting using its host's authorization.
func (c *APIClient) GetMeetingRawData(ctx context.Context, meetingID string) ([]byte, error) {
	if strings.TrimSpace(meetingID) == "" {
		return nil, errors.New("meeting ID is required")
	}
	return c.get(ctx, "/meetings/"+url.PathEscape(meetingID))
}
