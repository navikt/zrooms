package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository"
	"github.com/navikt/zrooms/internal/repository/memory"
	"github.com/navikt/zrooms/internal/service"
	"github.com/stretchr/testify/require"
)

type failingRepository struct {
	repository.Repository
	err error
}

func (r failingRepository) ApplyEvent(context.Context, models.MeetingEvent) (*models.Meeting, error) {
	return nil, r.err
}
func (r failingRepository) ListAllMeetings(context.Context) ([]*models.Meeting, error) {
	return nil, r.err
}

func TestPersistenceBeforeNotification(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	svc := service.NewMeetingService(repo)
	event := models.MeetingEvent{
		Type: "meeting.started", EventTS: time.Now().UnixMilli(),
		Meeting: models.Meeting{ID: "123", UUID: "instance", Topic: "Meeting"},
	}
	calls := 0
	svc.RegisterUpdateCallback(func(meeting *models.Meeting) {
		saved, err := repo.GetMeeting(ctx, meeting.ID)
		require.NoError(t, err)
		require.Equal(t, models.MeetingStatusStarted, saved.Status)
		calls++
	})
	require.NoError(t, svc.ProcessEvent(ctx, event))
	require.NoError(t, svc.ProcessEvent(ctx, event))
	require.Equal(t, 1, calls)

	storageErr := errors.New("storage unavailable")
	failed := service.NewMeetingService(failingRepository{err: storageErr})
	failed.RegisterUpdateCallback(func(*models.Meeting) { t.Error("notified before persistence") })
	require.ErrorIs(t, failed.ProcessEvent(ctx, event), storageErr)
	_, err := failed.GetMeetingStatusData(ctx, true)
	require.ErrorIs(t, err, storageErr)

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, svc.ProcessEvent(cancelled, event), context.Canceled)
}

func TestMeetingStatusData(t *testing.T) {
	repo := memory.NewRepository()
	svc := service.NewMeetingService(repo)
	ctx := context.Background()
	for i, kind := range []string{"meeting.created", "meeting.started", "meeting.ended"} {
		_, err := repo.ApplyEvent(ctx, models.MeetingEvent{
			Type: kind, EventTS: time.Now().UnixMilli() + int64(i),
			Meeting: models.Meeting{ID: kind, UUID: kind},
		})
		require.NoError(t, err)
	}

	active, err := svc.GetMeetingStatusData(ctx, false)
	require.NoError(t, err)
	require.Len(t, active, 2)
	all, err := svc.GetMeetingStatusData(ctx, true)
	require.NoError(t, err)
	require.Len(t, all, 3)
	require.Equal(t, "ended", all[0].Status)
}

func TestDeauthorizationNotifiesAfterDeletion(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	svc := service.NewMeetingService(repo)
	at := time.Now()
	_, err := repo.ApplyEvent(ctx, models.MeetingEvent{
		Type: "meeting.created", EventTS: at.Add(-time.Minute).UnixMilli(),
		Meeting: models.Meeting{ID: "123", Host: models.Participant{ID: "owner"}},
	})
	require.NoError(t, err)
	calls := 0
	svc.RegisterUpdateCallback(func(meeting *models.Meeting) {
		_, err := repo.GetMeeting(ctx, meeting.ID)
		require.ErrorIs(t, err, repository.ErrNotFound)
		calls++
	})
	require.NoError(t, svc.DeauthorizeUser(ctx, "owner", at))
	require.NoError(t, svc.DeauthorizeUser(ctx, "owner", at))
	require.Equal(t, 1, calls)
}

type partialDeauthorizationRepository struct{ repository.Repository }

func (partialDeauthorizationRepository) DeleteMeetingsByHost(context.Context, string, time.Time) ([]string, error) {
	return []string{"persisted-deletion"}, errors.New("storage unavailable")
}

func TestDeauthorizationNotifiesPartialDeletionBeforeRetry(t *testing.T) {
	svc := service.NewMeetingService(partialDeauthorizationRepository{})
	var notified []string
	svc.RegisterUpdateCallback(func(meeting *models.Meeting) { notified = append(notified, meeting.ID) })
	require.ErrorContains(t, svc.DeauthorizeUser(context.Background(), "owner", time.Now()), "storage unavailable")
	require.Equal(t, []string{"persisted-deletion"}, notified)
}
