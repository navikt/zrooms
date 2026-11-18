package web

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository/memory"
	"github.com/navikt/zrooms/internal/service"
	"github.com/navikt/zrooms/internal/stats"
	"github.com/stretchr/testify/require"
)

func TestDisplayTimes(t *testing.T) {
	oslo, err := time.LoadLocation("Europe/Oslo")
	require.NoError(t, err)
	tmpl, err := template.New("time").Funcs(timeFuncs(oslo)).
		Parse(`{{formatTime .}}|{{formatDateTime .}}|{{timeZone}}`)
	require.NoError(t, err)
	for _, tt := range []struct {
		name string
		time string
		want string
	}{
		{"unset", "", "-|-|Europe/Oslo"},
		{"winter", "2026-01-15T10:00:00Z", "11:00:00|2026-01-15 11:00:00|Europe/Oslo"},
		{"summer", "2026-07-15T10:00:00Z", "12:00:00|2026-07-15 12:00:00|Europe/Oslo"},
		{"year rollover", "2026-12-31T23:30:00Z", "00:30:00|2027-01-01 00:30:00|Europe/Oslo"},
		{"before spring transition", "2026-03-29T00:59:59Z", "01:59:59|2026-03-29 01:59:59|Europe/Oslo"},
		{"after spring transition", "2026-03-29T01:00:00Z", "03:00:00|2026-03-29 03:00:00|Europe/Oslo"},
		{"before autumn transition", "2026-10-25T00:59:59Z", "02:59:59|2026-10-25 02:59:59|Europe/Oslo"},
		{"after autumn transition", "2026-10-25T01:00:00Z", "02:00:00|2026-10-25 02:00:00|Europe/Oslo"},
		{"non UTC input", "2026-07-15T10:00:00-04:00", "16:00:00|2026-07-15 16:00:00|Europe/Oslo"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var value time.Time
			if tt.time != "" {
				value, err = time.Parse(time.RFC3339, tt.time)
				require.NoError(t, err)
			}
			var output strings.Builder
			require.NoError(t, tmpl.Execute(&output, value))
			require.Equal(t, tt.want, output.String())
		})
	}
}

func TestPublicRecurringMeetingHistory(t *testing.T) {
	ctx := context.Background()
	oslo, err := time.LoadLocation("Europe/Oslo")
	require.NoError(t, err)
	recorder := stats.NewRecorder(stats.NewMemoryBackend(), oslo)
	repo := memory.NewRepository().WithOccupancy(recorder)
	svc := service.NewMeetingService(repo)
	start := time.Now().UTC().AddDate(0, 0, -1).Truncate(24 * time.Hour).Add(10 * time.Hour)
	for _, meeting := range []models.Meeting{
		{ID: "123", Type: 3, Topic: "Open room", Host: models.Participant{ID: "owner-a"}},
		{ID: "456", Type: 8, Topic: "Other user's room", Host: models.Participant{ID: "owner-b"}},
		{ID: "789", Type: 2, Topic: "One-off meeting"},
		{ID: "instant", Type: 1, Topic: "Instant meeting"},
		{ID: "unknown", Topic: "Unknown type"},
	} {
		require.NoError(t, svc.ProcessEvent(ctx, models.MeetingEvent{
			Type: "meeting.created", EventTS: start.Add(-time.Hour).UnixMilli(), Meeting: meeting,
		}))
		// Participant events can omit the type and topic from the creation event.
		event := models.MeetingEvent{
			Type: "meeting.participant_joined", EventTS: start.UnixMilli(),
			Meeting:       models.Meeting{ID: meeting.ID, UUID: "occurrence", StartTime: start},
			ParticipantID: "guest",
		}
		require.NoError(t, svc.ProcessEvent(ctx, event))
		event.Type = "meeting.participant_left"
		event.EventTS = start.Add(time.Hour).UnixMilli()
		require.NoError(t, svc.ProcessEvent(ctx, event))
	}
	require.NoError(t, svc.ProcessEvent(ctx, models.MeetingEvent{
		Type: "meeting.created", EventTS: start.UnixMilli(),
		Meeting: models.Meeting{ID: "empty", Type: 3, Topic: "Unused room"},
	}))
	handler, err := NewHandler(svc, "./templates", recorder)
	require.NoError(t, err)
	defer handler.Shutdown()

	for _, path := range []string{"/", "/partial/meetings", "/partial/stats"} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, path, nil)
			switch path {
			case "/":
				handler.handleIndex(response, request)
				require.Contains(t, response.Body.String(), `href="/admin"`)
			case "/partial/meetings":
				handler.HandlePartialMeetingList(response, request)
			default:
				handler.HandlePartialStats(response, request)
			}
			require.Equal(t, http.StatusOK, response.Code)
			body := response.Body.String()
			require.Contains(t, body, "Open room")
			require.Contains(t, body, "Other user&#39;s room")
			require.Contains(t, body, "Unused room")
			require.NotContains(t, body, "One-off meeting")
			require.NotContains(t, body, "Instant meeting")
			require.NotContains(t, body, "Unknown type")
			require.NotContains(t, body, "owner-a")
			require.NotContains(t, body, "owner-b")
			if path != "/partial/stats" {
				require.Contains(t, body, "<td>"+start.In(oslo).Format("2006-01-02 15:04:05")+"</td>")
			}
			if path == "/" {
				require.Contains(t, body, `data-time-zone="Europe/Oslo"`)
			}
			if path != "/partial/meetings" {
				require.Contains(t, body, start.Format("Mon 02 Jan"))
				require.NotContains(t, body, start.AddDate(0, 0, -1).Format("Mon 02 Jan"))
				require.NotContains(t, body, start.AddDate(0, 0, 1).Format("Mon 02 Jan"))
				if path == "/" {
					require.Contains(t, body, `hx-include="#history-filter"`)
				}
			}
		})
	}

	for _, selected := range []string{"123", "456", "empty", "789", "missing"} {
		t.Run("selected "+selected, func(t *testing.T) {
			for _, path := range []string{"/", "/partial/stats"} {
				response := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodGet, path+"?meeting_id="+url.QueryEscape(selected), nil)
				if path == "/" {
					handler.handleIndex(response, request)
				} else {
					handler.HandlePartialStats(response, request)
				}
				require.Equal(t, http.StatusOK, response.Code)
				body := response.Body.String()
				require.Contains(t, body, `value="`+selected+`" selected`)
				if selected == "123" || selected == "456" {
					require.Contains(t, body, "<h3>")
					require.Contains(t, body, `class="meeting-id">`+selected+`</span>`)
					other := "123"
					if selected == other {
						other = "456"
					}
					require.NotContains(t, body, `class="meeting-id">`+other+`</span>`)
				} else {
					require.Contains(t, body, "No activity in the last 7 days for the selected meeting")
				}
			}
		})
	}

	admin, err := NewAdminHandler(svc, repo, "./templates", nil, recorder)
	require.NoError(t, err)
	response := httptest.NewRecorder()
	admin.handleStats(response, httptest.NewRequest(http.MethodGet, "/admin/stats?month="+start.Format(stats.MonthLayout), nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "One-off meeting")
	require.Contains(t, response.Body.String(), "Instant meeting")
	require.Contains(t, response.Body.String(), "Unknown type")
}

func TestPublicHistoryUsesPersistedTypeAfterMeetingExpiry(t *testing.T) {
	recorder := stats.NewRecorder(stats.NewMemoryBackend(), time.UTC)
	ctx := context.Background()
	start := time.Now().Add(-2 * time.Hour)
	for _, meeting := range []models.Meeting{
		{ID: "recorded", Type: 8, Topic: "Historical room"},
		{ID: "legacy", Topic: "Legacy room"},
		{ID: "unclassified", Topic: "Unclassified history"},
	} {
		require.NoError(t, recorder.Record(ctx, meeting, []models.OccupancyChange{
			{TS: start.UnixMilli(), Delta: 1},
			{TS: start.Add(time.Hour).UnixMilli(), Delta: -1},
		}))
	}
	handler := &Handler{stats: recorder}
	view, err := handler.publicHistory(ctx, "", []service.MeetingStatusData{
		{Meeting: &models.Meeting{ID: "legacy", Type: 3}},
	})
	require.NoError(t, err)
	require.Len(t, view.Rooms, 2)
	require.Equal(t, "legacy", view.Rooms[0].MeetingID)
	require.Equal(t, "recorded", view.Rooms[1].MeetingID)
}

type unavailableHistoryBackend struct {
	stats.Backend
}

func (unavailableHistoryBackend) ListMonth(context.Context, string) ([]*stats.Month, error) {
	return nil, errors.New("statistics unavailable")
}

func TestHistoryFailureKeepsPublicMeetingStatusAvailable(t *testing.T) {
	repo := memory.NewRepository()
	_, err := repo.ApplyEvent(context.Background(), models.MeetingEvent{
		Type: "meeting.created", EventTS: time.Now().UnixMilli(),
		Meeting: models.Meeting{ID: "123", Type: 3, Topic: "Available room"},
	})
	require.NoError(t, err)
	handler, err := NewHandler(service.NewMeetingService(repo), "./templates",
		stats.NewRecorder(unavailableHistoryBackend{stats.NewMemoryBackend()}, time.UTC))
	require.NoError(t, err)
	defer handler.Shutdown()

	response := httptest.NewRecorder()
	handler.handleIndex(response, httptest.NewRequest(http.MethodGet, "/?meeting_id=123", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "Available room")
	require.Contains(t, response.Body.String(), `role="alert"`)
	require.Contains(t, response.Body.String(), "Unable to load meeting history")
	require.Contains(t, response.Body.String(), `value="123" selected`)
	require.NotContains(t, response.Body.String(), "No activity")

	response = httptest.NewRecorder()
	handler.HandlePartialStats(response, httptest.NewRequest(http.MethodGet, "/partial/stats", nil))
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
}
