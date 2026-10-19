package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/navikt/zrooms/internal/models"
)

type Store struct {
	backend Backend
	now     func() time.Time
}

func NewStore(backend Backend) *Store {
	return &Store{backend: backend, now: time.Now}
}

func (s *Store) Check(ctx context.Context) error {
	return s.backend.Check(ctx)
}

func (s *Store) expired(state *State) bool {
	return !s.now().Before(state.UpdatedAt.Add(Retention))
}

func (s *Store) GetMeeting(ctx context.Context, id string) (*models.Meeting, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	state, _, err := s.backend.Read(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.expired(state) || state.DeletedAt != 0 {
		return nil, ErrNotFound
	}
	return state.view(), nil
}

func (s *Store) ListAllMeetings(ctx context.Context) ([]*models.Meeting, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	states, err := s.backend.List(ctx)
	if err != nil {
		return nil, err
	}
	meetings := make([]*models.Meeting, 0, len(states))
	for _, state := range states {
		if !s.expired(state) && state.DeletedAt == 0 {
			meetings = append(meetings, state.view())
		}
	}
	sort.Slice(meetings, func(i, j int) bool {
		if meetings[i].UpdatedAt.Equal(meetings[j].UpdatedAt) {
			return meetings[i].ID < meetings[j].ID
		}
		return meetings[i].UpdatedAt.After(meetings[j].UpdatedAt)
	})
	return meetings, nil
}

func (s *Store) ApplyEvent(ctx context.Context, event models.MeetingEvent) (*models.Meeting, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := event.Validate(); err != nil {
		return nil, err
	}
	if time.UnixMilli(event.EventTS).Before(s.now().Add(-Retention)) {
		return nil, nil
	}
	for attempt := 0; attempt < 5; attempt++ {
		state, version, err := s.backend.Read(ctx, event.Meeting.ID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		if errors.Is(err, ErrNotFound) || s.expired(state) {
			state = &State{SchemaVersion: 1, Meeting: models.Meeting{ID: event.Meeting.ID}}
		}
		if state.DeletedAt != 0 {
			if event.EventTS <= state.DeletedAt {
				return nil, nil
			}
			state = &State{SchemaVersion: 1, Meeting: models.Meeting{ID: event.Meeting.ID}}
		}
		if !state.apply(event) {
			return nil, nil
		}
		state.UpdatedAt = s.now().UTC()
		err = s.backend.Write(ctx, event.Meeting.ID, state, version)
		if errors.Is(err, ErrConflict) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return state.view(), nil
	}
	return nil, fmt.Errorf("applying event: %w", ErrConflict)
}

func (s *Store) DeleteMeeting(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for attempt := 0; attempt < 5; attempt++ {
		state, version, err := s.backend.Read(ctx, id)
		if err != nil {
			return err
		}
		if s.expired(state) || state.DeletedAt != 0 {
			return ErrNotFound
		}
		// Remove content but retain a short-lived marker against delayed retries.
		state = &State{
			SchemaVersion: 1, Meeting: models.Meeting{ID: id},
			DeletedAt: s.now().UnixMilli(), UpdatedAt: s.now().UTC(),
		}
		err = s.backend.Write(ctx, id, state, version)
		if errors.Is(err, ErrConflict) {
			continue
		}
		return err
	}
	return fmt.Errorf("deleting meeting: %w", ErrConflict)
}

// DeleteMeetingsByHost returns persisted deletions even when cleanup fails partway.
func (s *Store) DeleteMeetingsByHost(ctx context.Context, hostID string, at time.Time) ([]string, error) {
	if strings.TrimSpace(hostID) == "" || len(hostID) > 256 || at.UnixMilli() <= 0 {
		return nil, fmt.Errorf("host ID and deauthorization time required")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	states, err := s.backend.List(ctx)
	if err != nil {
		return nil, err
	}
	var deleted []string
	for _, state := range states {
		if state.Meeting.Host.ID != hostID || state.DeletedAt != 0 || s.expired(state) {
			continue
		}
		changed, err := s.deleteHostMeeting(ctx, state.Meeting.ID, hostID, at.UnixMilli())
		if changed {
			deleted = append(deleted, state.Meeting.ID)
		}
		if err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}

func (s *Store) deleteHostMeeting(ctx context.Context, id, hostID string, cutoff int64) (bool, error) {
	for attempt := 0; attempt < 5; attempt++ {
		state, version, err := s.backend.Read(ctx, id)
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if s.expired(state) || state.DeletedAt != 0 || state.Meeting.Host.ID != hostID {
			return false, nil
		}
		latest := max(state.MetadataTS, state.LifecycleTS, state.OperatorTS, state.OccurrenceTS)
		for _, participant := range state.Participants {
			latest = max(latest, participant.EventTS)
		}
		// A delayed uninstall callback must not delete state accepted after reinstallation.
		if latest > cutoff {
			return false, nil
		}
		marker := &State{
			SchemaVersion: 1, Meeting: models.Meeting{ID: id},
			DeletedAt: cutoff, UpdatedAt: s.now().UTC(),
		}
		err = s.backend.Write(ctx, id, marker, version)
		if errors.Is(err, ErrConflict) {
			continue
		}
		return err == nil, err
	}
	return false, fmt.Errorf("deleting host meeting: %w", ErrConflict)
}

func (s *State) view() *models.Meeting {
	meeting := s.Meeting
	meeting.UpdatedAt = s.UpdatedAt
	meeting.Participants = nil
	meeting.ParticipantCount = 0
	if meeting.Status == models.MeetingStatusStarted {
		for _, participant := range s.Participants {
			if participant.Present {
				meeting.ParticipantCount++
			}
		}
	}
	return &meeting
}

func (s *State) apply(event models.MeetingEvent) bool {
	meeting := &s.Meeting
	isMetadata := event.Type == "meeting.created" || event.Type == "meeting.updated"
	if isMetadata {
		changed := false
		if event.Type == "meeting.created" && meeting.CreatorEmail == "" && event.Meeting.OperatorEmail != "" {
			meeting.CreatorEmail = event.Meeting.OperatorEmail
			changed = true
		}
		if event.EventTS > s.MetadataTS {
			if event.Meeting.Topic != "" {
				meeting.Topic = event.Meeting.Topic
			}
			if event.Meeting.Host.ID != "" {
				meeting.Host.ID = event.Meeting.Host.ID
			}
			meeting.Duration = event.Meeting.Duration
			if meeting.Status != models.MeetingStatusStarted && meeting.Status != models.MeetingStatusEnded {
				meeting.StartTime = event.Meeting.StartTime
			}
			s.MetadataTS = event.EventTS
			changed = true
		}
		if s.updateOperator(event) {
			changed = true
		}
		return changed
	}
	if meeting.UUID != "" && meeting.UUID != event.Meeting.UUID {
		// Only a newer start/join may replace the current occurrence.
		if event.EventTS <= max(s.LifecycleTS, s.OccurrenceTS) || event.Type == "meeting.ended" || event.Type == "meeting.participant_left" {
			return false
		}
		meeting.UUID = event.Meeting.UUID
		meeting.Status = models.MeetingStatusCreated
		meeting.StartTime = time.Time{}
		meeting.EndTime = time.Time{}
		s.LifecycleTS = 0
		s.OccurrenceTS = 0
		s.Participants = nil
	}
	if meeting.Status == models.MeetingStatusEnded {
		return false
	}
	meeting.UUID = event.Meeting.UUID
	if s.OccurrenceTS == 0 || event.EventTS < s.OccurrenceTS {
		s.OccurrenceTS = event.EventTS
	}
	switch event.Type {
	case "meeting.started", "meeting.ended":
		if event.EventTS < s.LifecycleTS {
			return false
		}
		if event.Type == "meeting.started" {
			if s.LifecycleTS != 0 {
				return false
			}
			meeting.Status = models.MeetingStatusStarted
			meeting.StartTime = event.Meeting.StartTime
			meeting.EndTime = time.Time{}
		} else {
			meeting.Status = models.MeetingStatusEnded
			if meeting.StartTime.IsZero() {
				meeting.StartTime = event.Meeting.StartTime
			}
			meeting.EndTime = event.Meeting.EndTime
			s.Participants = nil
		}
		s.LifecycleTS = event.EventTS
	case "meeting.participant_joined", "meeting.participant_left":
		if s.Participants == nil {
			s.Participants = make(map[string]Presence)
		}
		previous, exists := s.Participants[event.ParticipantID]
		joined := event.Type == "meeting.participant_joined"
		if exists && (event.EventTS < previous.EventTS || (event.EventTS == previous.EventTS && (!previous.Present || joined))) {
			return false
		}
		s.Participants[event.ParticipantID] = Presence{Present: joined, EventTS: event.EventTS}
		// A participant event may precede meeting.started in delivery.
		if meeting.Status != models.MeetingStatusStarted {
			meeting.Status = models.MeetingStatusStarted
			meeting.StartTime = event.Meeting.StartTime
		}
	}
	if event.Meeting.Topic != "" && event.EventTS > s.MetadataTS {
		meeting.Topic = event.Meeting.Topic
		s.MetadataTS = event.EventTS
	}
	if meeting.Host.ID == "" {
		meeting.Host.ID = event.Meeting.Host.ID
	}
	if meeting.Duration == 0 {
		meeting.Duration = event.Meeting.Duration
	}
	s.updateOperator(event)
	return true
}

func (s *State) updateOperator(event models.MeetingEvent) bool {
	if event.Meeting.OperatorEmail != "" && event.EventTS > s.OperatorTS {
		s.Meeting.OperatorEmail = event.Meeting.OperatorEmail
		s.OperatorTS = event.EventTS
		return true
	}
	return false
}
