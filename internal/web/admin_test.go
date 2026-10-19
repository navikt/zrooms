package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository"
	"github.com/navikt/zrooms/internal/repository/memory"
	"github.com/navikt/zrooms/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAdminTemplatesShowPersistedActiveMeeting(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()
	base := time.Now().Add(-time.Minute).UnixMilli()
	meeting := models.Meeting{
		ID: "123", UUID: "instance", Topic: "<script>not HTML</script>",
		Host: models.Participant{ID: "host-123"}, OperatorEmail: "creator@example.test",
		StartTime: time.UnixMilli(base),
	}
	for i, kind := range []string{"meeting.created", "meeting.started", "meeting.participant_joined"} {
		_, err := repo.ApplyEvent(ctx, models.MeetingEvent{
			Type: kind, EventTS: base + int64(i), Meeting: meeting, ParticipantID: "guest",
		})
		require.NoError(t, err)
	}
	svc := service.NewMeetingService(repo)
	admin, err := NewAdminHandler(svc, repo, "./templates")
	require.NoError(t, err)
	for _, path := range []string{"/admin", "/admin/meetings", "/admin/meetings/123"} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			switch path {
			case "/admin":
				admin.handleAdminDashboard(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			case "/admin/meetings":
				admin.handleMeetingsList(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			default:
				admin.handleMeetingDetail(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			}
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Contains(t, recorder.Body.String(), "creator@example.test")
			require.Contains(t, recorder.Body.String(), "Active")
			require.NotContains(t, recorder.Body.String(), "<script>not HTML</script>")
			require.Contains(t, recorder.Body.String(), "&lt;script&gt;")
			if strings.HasSuffix(path, "/123") {
				require.Contains(t, recorder.Body.String(), "host-123")
				require.Contains(t, recorder.Body.String(), "1 participants")
			}
		})
	}
	public, err := NewHandler(svc, "./templates")
	require.NoError(t, err)
	defer public.Shutdown()
	recorder := httptest.NewRecorder()
	public.handleIndex(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "creator@example.test")
	require.NotContains(t, recorder.Body.String(), "host-123")
	require.Contains(t, recorder.Body.String(), "every 60s")
}

type failedReadRepository struct{ repository.Repository }

func (failedReadRepository) GetMeeting(context.Context, string) (*models.Meeting, error) {
	return nil, errors.New("bucket unavailable")
}

func TestAdminDoesNotDisguiseStorageFailureAsNotFound(t *testing.T) {
	handler := &AdminHandler{repo: failedReadRepository{}}
	recorder := httptest.NewRecorder()
	handler.handleMeetingDetail(recorder, httptest.NewRequest(http.MethodGet, "/admin/meetings/123", nil))
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "bucket")
}

func TestIntrospectionInvalidTokenIsUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"active":false,"error":"token expired"}`))
	}))
	defer server.Close()
	t.Setenv("NAIS_TOKEN_INTROSPECTION_ENDPOINT", server.URL)
	middleware := NewAuthMiddleware()
	request := httptest.NewRequest(http.MethodGet, "/admin", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	recorder := httptest.NewRecorder()
	middleware.RequireAuth(func(http.ResponseWriter, *http.Request) {
		t.Fatal("invalid token reached admin")
	})(recorder, request)
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}
