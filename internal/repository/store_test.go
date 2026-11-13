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

func TestDeauthorizationRechecksOwnershipOnConflict(t *testing.T) {
	now := time.Now().UTC()
	backend := &testBackend{state: &State{
		SchemaVersion: 1, Meeting: models.Meeting{ID: "123", Host: models.Participant{ID: "owner"}},
		MetadataTS: now.Add(-time.Minute).UnixMilli(), UpdatedAt: now,
	}}
	writes := 0
	backend.write = func(*State, int64) error {
		writes++
		backend.state.Meeting.Host.ID = "new-owner"
		return ErrConflict
	}
	deleted, err := NewStore(backend).DeleteMeetingsByHost(context.Background(), "owner", now)
	require.NoError(t, err)
	require.Empty(t, deleted)
	require.Equal(t, 1, writes)
	require.Equal(t, "new-owner", backend.state.Meeting.Host.ID)

	backend.state.Meeting.Host.ID = "owner"
	backend.write = func(*State, int64) error { return ErrConflict }
	_, err = NewStore(backend).DeleteMeetingsByHost(context.Background(), "owner", now)
	require.ErrorIs(t, err, ErrConflict)
	backend.err = errors.New("storage unavailable")
	_, err = NewStore(backend).DeleteMeetingsByHost(context.Background(), "owner", now)
	require.ErrorIs(t, err, backend.err)
}

type recordedOccupancy struct {
	changes []models.OccupancyChange
}

func (r *recordedOccupancy) Record(_ context.Context, _ models.Meeting, changes []models.OccupancyChange) error {
	r.changes = append(r.changes, changes...)
	return nil
}

func TestOccupancyChangesFollowAcceptedPresence(t *testing.T) {
	now := time.Now().UTC()
	base := now.Add(-time.Hour).UnixMilli()
	backend := &testBackend{}
	backend.write = func(state *State, _ int64) error {
		backend.state = state
		return nil
	}

	recorder := &recordedOccupancy{}
	store := NewStore(backend).WithOccupancy(recorder)
	store.now = func() time.Time { return now }
	apply := func(kind string, ts int64, uuid, participant string) {
		t.Helper()
		_, err := store.ApplyEvent(context.Background(), models.MeetingEvent{
			Type: kind, EventTS: ts, ParticipantID: participant,
			Meeting: models.Meeting{ID: "123", UUID: uuid},
		})
		require.NoError(t, err)
	}
	apply("meeting.started", base, "first", "")
	apply("meeting.participant_joined", base+20, "first", "b")
	apply("meeting.participant_joined", base+10, "first", "a")
	apply("meeting.participant_joined", base+10, "first", "a") // Retried delivery.
	apply("meeting.participant_left", base+30, "first", "b")
	// A newer occurrence without meeting.ended closes "a" at the last known activity.
	apply("meeting.participant_joined", base+100, "second", "c")
	apply("meeting.ended", base+200, "second", "")
	require.Equal(t, []models.OccupancyChange{
		{TS: base + 20, Delta: 1}, {TS: base + 10, Delta: 1}, {TS: base + 30, Delta: -1},
		{TS: base + 30, Delta: -1}, {TS: base + 100, Delta: 1}, {TS: base + 200, Delta: -1},
	}, recorder.changes)

	recorder.changes = nil
	apply("meeting.participant_joined", base+300, "third", "d")
	require.NoError(t, store.DeleteMeeting(context.Background(), "123"))
	require.Equal(t, []models.OccupancyChange{{TS: base + 300, Delta: 1}, {TS: base + 300, Delta: -1}}, recorder.changes)
}

func TestMeetingTypeOrderingAndOmittedType(t *testing.T) {
	now := time.Now().UTC()
	base := now.Add(-time.Hour).UnixMilli()
	backend := &testBackend{}
	backend.write = func(state *State, _ int64) error {
		backend.state = state
		return nil
	}
	store := NewStore(backend)
	store.now = func() time.Time { return now }
	for _, step := range []struct {
		kind        string
		offset      int64
		meetingType int
		want        int
	}{
		{"meeting.created", 10, 3, 3},
		{"meeting.participant_joined", 30, 0, 3},
		// Metadata delivered after a newer participant event still supplies its type.
		{"meeting.updated", 20, 8, 8},
		{"meeting.updated", 15, 2, 8},
		{"meeting.participant_left", 40, 0, 8},
		{"meeting.updated", 50, 2, 2},
	} {
		_, err := store.ApplyEvent(context.Background(), models.MeetingEvent{
			Type: step.kind, EventTS: base + step.offset, ParticipantID: "guest",
			Meeting: models.Meeting{ID: "123", UUID: "occurrence", Type: step.meetingType, Topic: "Room"},
		})
		require.NoError(t, err)
		meeting, err := store.GetMeeting(context.Background(), "123")
		require.NoError(t, err)
		require.Equal(t, step.want, meeting.Type)
	}
}
