package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/navikt/zrooms/internal/models"
	"github.com/stretchr/testify/require"
)

type testBackend struct {
	state *State
	write func(*State, int64) error
	err   error
}

func (b *testBackend) Read(context.Context, string) (*State, int64, error) {
	if b.err != nil {
		return nil, 0, b.err
	}
	if b.state == nil {
		return nil, 0, ErrNotFound
	}
	copy := *b.state
	copy.Participants = make(map[string]Presence)
	for id, p := range b.state.Participants {
		copy.Participants[id] = p
	}
	return &copy, 1, nil
}
func (b *testBackend) Write(_ context.Context, _ string, s *State, version int64) error {
	return b.write(s, version)
}
func (b *testBackend) List(context.Context) ([]*State, error) {
	return []*State{b.state}, b.err
}
func (b *testBackend) Check(context.Context) error { return b.err }

func TestExpiryAndStorageErrors(t *testing.T) {
	now := time.Now().UTC()
	backend := &testBackend{state: &State{
		SchemaVersion: 1, Meeting: models.Meeting{ID: "123", Topic: "Expired"},
		UpdatedAt: now.Add(-Retention),
	}}
	store := NewStore(backend)
	store.now = func() time.Time { return now }
	_, err := store.GetMeeting(context.Background(), "123")
	require.ErrorIs(t, err, ErrNotFound)
	meetings, err := store.ListAllMeetings(context.Background())
	require.NoError(t, err)
	require.Empty(t, meetings)
	updated, err := store.ApplyEvent(context.Background(), models.MeetingEvent{
		Type: "meeting.created", EventTS: now.Add(-Retention - time.Second).UnixMilli(),
		Meeting: models.Meeting{ID: "123"},
	})
	require.NoError(t, err)
	require.Nil(t, updated)

	backend.err = errors.New("storage unavailable")
	_, err = store.ListAllMeetings(context.Background())
	require.ErrorIs(t, err, backend.err)
	require.ErrorIs(t, store.Check(context.Background()), backend.err)
}

func TestConflictsReloadBeforeRetry(t *testing.T) {
	now := time.Now().UTC()
	backend := &testBackend{state: &State{
		SchemaVersion: 1, Meeting: models.Meeting{ID: "123", Status: models.MeetingStatusCreated},
		UpdatedAt: now,
	}}
	writes := 0
	backend.write = func(state *State, version int64) error {
		require.EqualValues(t, 1, version)
		writes++
		if writes == 1 {
			backend.state.Meeting.Topic = "Concurrent metadata"
			backend.state.MetadataTS = now.UnixMilli() + 2
			return ErrConflict
		}
		backend.state = state
		return nil
	}
	updated, err := NewStore(backend).ApplyEvent(context.Background(), models.MeetingEvent{
		Type: "meeting.started", EventTS: now.UnixMilli(), Meeting: models.Meeting{ID: "123", UUID: "instance"},
	})
	require.NoError(t, err)
	require.Equal(t, 2, writes)
	require.Equal(t, "Concurrent metadata", updated.Topic)
	require.Equal(t, models.MeetingStatusStarted, updated.Status)

	backend.write = func(*State, int64) error { return ErrConflict }
	_, err = NewStore(backend).ApplyEvent(context.Background(), models.MeetingEvent{
		Type: "meeting.ended", EventTS: now.UnixMilli() + 10, Meeting: models.Meeting{ID: "123", UUID: "instance"},
	})
	require.ErrorIs(t, err, ErrConflict)
}
