package zoom

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetUserEmail(t *testing.T) {
	for _, test := range []struct {
		name      string
		status    int
		body      string
		wantError bool
	}{
		{"success", http.StatusOK, `{"id":"host-id","email":"owner@example.test"}`, false},
		{"wrong user", http.StatusOK, `{"id":"another-host","email":"other@example.test"}`, true},
		{"forbidden", http.StatusForbidden, `{"message":"private response"}`, true},
		{"not found", http.StatusNotFound, `{}`, true},
		{"missing email", http.StatusOK, `{"id":"host-id"}`, true},
		{"malformed", http.StatusOK, `{`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/users/me", r.URL.Path)
				require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			client := NewAPIClient("test-token")
			client.baseURL = server.URL
			email, err := client.GetUserEmail(context.Background(), "host-id")
			if test.wantError {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "private response")
				return
			}
			require.NoError(t, err)
			require.Equal(t, "owner@example.test", email)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err = client.GetUserEmail(ctx, "host-id")
			require.Error(t, err)
			require.NotContains(t, err.Error(), "host-id")
		})
	}
}
