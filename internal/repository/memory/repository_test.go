package memory_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository"
	"github.com/navikt/zrooms/internal/repository/memory"
	"github.com/stretchr/testify/require"
)

func TestEventLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	base := time.Now().Add(-time.Minute).UnixMilli()
	apply := func(kind string, offset int64, uuid, participant, topic string) *models.Meeting {
		t.Helper()
		event := models.MeetingEvent{
			Type: kind, EventTS: base + offset, ParticipantID: participant,
			Meeting: models.Meeting{
				ID: "123", UUID: uuid, Topic: topic,
				Host: models.Participant{ID: "host"}, OperatorEmail: "operator@example.test",
				StartTime: time.UnixMilli(base), EndTime: time.UnixMilli(base + offset),
			},
		}
		meeting, err := repo.ApplyEvent(ctx, event)
		require.NoError(t, err)
		return meeting
	}

	apply("meeting.created", 1, "", "", "Original topic")
	apply("meeting.started", 10, "first", "", "")
	apply("meeting.participant_joined", 20, "first", "guest-1", "")
	apply("meeting.participant_joined", 21, "first", "guest-2", "")
	require.Nil(t, apply("meeting.participant_joined", 20, "first", "guest-1", ""))
	apply("meeting.updated", 22, "", "", "New topic")
	meeting, err := repo.GetMeeting(ctx, "123")
	require.NoError(t, err)
	require.Equal(t, models.MeetingStatusStarted, meeting.Status)
	require.Equal(t, 2, meeting.ParticipantCount)
	require.Equal(t, "New topic", meeting.Topic)
	require.Equal(t, "host", meeting.Host.ID)
	require.Equal(t, "operator@example.test", meeting.CreatorEmail)
	require.Equal(t, "operator@example.test", meeting.OperatorEmail)

	apply("meeting.participant_left", 40, "first", "guest-1", "")
	require.Nil(t, apply("meeting.participant_joined", 30, "first", "guest-1", ""))
	apply("meeting.ended", 50, "first", "", "")
	require.Nil(t, apply("meeting.started", 10, "first", "", ""))
	require.Nil(t, apply("meeting.participant_joined", 60, "first", "guest-3", ""))
	meeting, err = repo.GetMeeting(ctx, "123")
	require.NoError(t, err)
	require.Equal(t, models.MeetingStatusEnded, meeting.Status)
	require.Zero(t, meeting.ParticipantCount)
	require.Equal(t, time.UnixMilli(base), meeting.StartTime)
	require.Equal(t, time.UnixMilli(base+50), meeting.EndTime)

	apply("meeting.started", 100, "second", "", "")
	apply("meeting.participant_joined", 110, "second", "guest-1", "")
	require.Nil(t, apply("meeting.ended", 50, "first", "", ""))
	meeting, err = repo.GetMeeting(ctx, "123")
	require.NoError(t, err)
	require.Equal(t, "second", meeting.UUID)
	require.Equal(t, models.MeetingStatusStarted, meeting.Status)
	require.Equal(t, 1, meeting.ParticipantCount)
}

func TestParticipantBeforeStartAndConcurrentUpdates(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()
	base := time.Now().Add(-time.Minute).UnixMilli()
	meeting := models.Meeting{ID: "123", UUID: "instance", StartTime: time.UnixMilli(base)}
	_, err := repo.ApplyEvent(ctx, models.MeetingEvent{
		Type: "meeting.participant_joined", EventTS: base + 2, Meeting: meeting, ParticipantID: "first",
	})
	require.NoError(t, err)
	_, err = repo.ApplyEvent(ctx, models.MeetingEvent{Type: "meeting.started", EventTS: base + 1, Meeting: meeting})
	require.NoError(t, err)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.ApplyEvent(ctx, models.MeetingEvent{
				Type: "meeting.participant_joined", EventTS: base + 3 + int64(i),
				Meeting: meeting, ParticipantID: fmt.Sprintf("participant-%d", i),
			})
			// Bounded conflicts are retryable, never successful lost writes.
			if errors.Is(err, repository.ErrConflict) {
				_, err = repo.ApplyEvent(ctx, models.MeetingEvent{
					Type: "meeting.participant_joined", EventTS: base + 3 + int64(i),
					Meeting: meeting, ParticipantID: fmt.Sprintf("participant-%d", i),
				})
			}
			require.NoError(t, err)
		}()
	}
	wg.Wait()
	result, err := repo.GetMeeting(ctx, meeting.ID)
	require.NoError(t, err)
	require.Equal(t, 11, result.ParticipantCount)
}

func TestDeleteDoesNotReplayOldContent(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()
	event := models.MeetingEvent{
		Type: "meeting.created", EventTS: time.Now().Add(-time.Minute).UnixMilli(),
		Meeting: models.Meeting{ID: "123", Topic: "Delete this topic"},
	}
	_, err := repo.ApplyEvent(ctx, event)
	require.NoError(t, err)
	require.NoError(t, repo.DeleteMeeting(ctx, event.Meeting.ID))
	_, err = repo.GetMeeting(ctx, event.Meeting.ID)
	require.ErrorIs(t, err, repository.ErrNotFound)
	updated, err := repo.ApplyEvent(ctx, event)
	require.NoError(t, err)
	require.Nil(t, updated)
	meetings, err := repo.ListAllMeetings(ctx)
	require.NoError(t, err)
	require.Empty(t, meetings)
}

func TestDeauthorizationDeletesOnlyOwnedMeetingsAndAllowsRetry(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()
	base := time.Now().Add(-time.Minute)
	events := []models.MeetingEvent{
		{Type: "meeting.created", EventTS: base.UnixMilli(), Meeting: models.Meeting{
			ID: "owned", Host: models.Participant{ID: "owner"}, OperatorEmail: "delegate@example.test",
		}},
		{Type: "meeting.started", EventTS: base.UnixMilli(), Meeting: models.Meeting{
			ID: "active", UUID: "occurrence", Host: models.Participant{ID: "owner"},
		}},
		{Type: "meeting.created", EventTS: base.UnixMilli(), Meeting: models.Meeting{
			ID: "other", Host: models.Participant{ID: "other-owner"}, OperatorEmail: "owner@example.test",
		}},
		{Type: "meeting.created", EventTS: base.UnixMilli(), Meeting: models.Meeting{ID: "unknown"}},
	}
	for _, event := range events {
		_, err := repo.ApplyEvent(ctx, event)
		require.NoError(t, err)
	}
	cutoff := base.Add(10 * time.Second)
	deleted, err := repo.DeleteMeetingsByHost(ctx, "owner", cutoff)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"owned", "active"}, deleted)
	deleted, err = repo.DeleteMeetingsByHost(ctx, "owner", cutoff)
	require.NoError(t, err)
	require.Empty(t, deleted)
	meetings, err := repo.ListAllMeetings(ctx)
	require.NoError(t, err)
	require.Len(t, meetings, 2)
	for _, event := range events[:2] {
		replayed, err := repo.ApplyEvent(ctx, event)
		require.NoError(t, err)
		require.Nil(t, replayed)
		_, err = repo.GetMeeting(ctx, event.Meeting.ID)
		require.ErrorIs(t, err, repository.ErrNotFound)
	}
	newEvent := events[0]
	newEvent.EventTS = cutoff.Add(time.Second).UnixMilli()
	_, err = repo.ApplyEvent(ctx, newEvent)
	require.NoError(t, err)
	deleted, err = repo.DeleteMeetingsByHost(ctx, "owner", cutoff)
	require.NoError(t, err)
	require.Empty(t, deleted)
	_, err = repo.GetMeeting(ctx, "owned")
	require.NoError(t, err)
	_, err = repo.DeleteMeetingsByHost(ctx, "", cutoff)
	require.Error(t, err)
}

func TestDelayedMetadataAndParticipantFirstOccurrence(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()
	base := time.Now().Add(-time.Minute).UnixMilli()
	apply := func(kind string, offset int64, uuid, topic, operator string) *models.Meeting {
		t.Helper()
		updated, err := repo.ApplyEvent(ctx, models.MeetingEvent{
			Type: kind, EventTS: base + offset, ParticipantID: "guest",
			Meeting: models.Meeting{
				ID: "123", UUID: uuid, Topic: topic, OperatorEmail: operator,
				StartTime: time.UnixMilli(base + offset),
			},
		})
		require.NoError(t, err)
		return updated
	}
	apply("meeting.started", 20, "first", "Runtime topic", "")
	apply("meeting.participant_joined", 30, "first", "", "")
	apply("meeting.updated", 25, "", "Updated topic", "editor@example.test")
	apply("meeting.created", 10, "", "Older topic", "creator@example.test")
	meeting, err := repo.GetMeeting(ctx, "123")
	require.NoError(t, err)
	require.Equal(t, "Updated topic", meeting.Topic)
	require.Equal(t, "creator@example.test", meeting.CreatorEmail)
	require.Equal(t, "editor@example.test", meeting.OperatorEmail)
	apply("meeting.participant_joined", 100, "second", "", "")
	require.Nil(t, apply("meeting.participant_joined", 40, "first", "", ""))
	apply("meeting.started", 90, "second", "", "")
	meeting, err = repo.GetMeeting(ctx, "123")
	require.NoError(t, err)
	require.Equal(t, "second", meeting.UUID)
	require.Equal(t, 1, meeting.ParticipantCount)
	require.Equal(t, time.UnixMilli(base+90), meeting.StartTime)
	require.Nil(t, apply("meeting.started", 110, "second", "", ""))
}
