// Package stats records anonymous meeting occupancy and summarizes it per day.
package stats

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository"
)

const (
	// RetentionMonths is how many months before the current one are kept.
	RetentionMonths = 12
	MonthLayout     = "2006-01"
	refreshAge      = 2 * 24 * time.Hour
)

// Month holds the occupancy changes for one meeting in one calendar month.
type Month struct {
	SchemaVersion int                      `json:"schema_version"`
	MeetingID     string                   `json:"meeting_id"`
	Topic         string                   `json:"topic,omitempty"`
	Month         string                   `json:"month"`
	Changes       []models.OccupancyChange `json:"changes"`
	UpdatedAt     time.Time                `json:"updated_at"`
}

// Backend uses version 0 for an absent object. Writes must compare and swap.
type Backend interface {
	Read(ctx context.Context, meetingID, month string) (*Month, int64, error)
	Write(ctx context.Context, month *Month, version int64) error
	ListMonth(ctx context.Context, month string) ([]*Month, error)
}

// Refresher is implemented by backends whose objects expire unless rewritten.
type Refresher interface {
	Refresh(ctx context.Context, oldestMonth string, createdBefore time.Time) error
}

type Recorder struct {
	backend  Backend
	location *time.Location
	now      func() time.Time
}

func NewRecorder(backend Backend, location *time.Location) *Recorder {
	return &Recorder{backend: backend, location: location, now: time.Now}
}

func (r *Recorder) Location() *time.Location {
	return r.location
}

func (r *Recorder) oldestMonth() string {
	now := r.now().In(r.location)
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, r.location)
	return first.AddDate(0, -RetentionMonths, 0).Format(MonthLayout)
}

// Record appends changes to the month files they belong to.
func (r *Recorder) Record(ctx context.Context, meetingID, topic string, changes []models.OccupancyChange) error {
	oldest := r.oldestMonth()
	byMonth := make(map[string][]models.OccupancyChange)
	for _, change := range changes {
		month := time.UnixMilli(change.TS).In(r.location).Format(MonthLayout)
		if month >= oldest && change.Delta != 0 {
			byMonth[month] = append(byMonth[month], change)
		}
	}
	var errs []error
	for month, monthChanges := range byMonth {
		errs = append(errs, r.append(ctx, meetingID, topic, month, monthChanges))
	}
	return errors.Join(errs...)
}

func (r *Recorder) append(ctx context.Context, meetingID, topic, month string, changes []models.OccupancyChange) error {
	for attempt := 0; attempt < 5; attempt++ {
		current, version, err := r.backend.Read(ctx, meetingID, month)
		if errors.Is(err, repository.ErrNotFound) {
			current = &Month{SchemaVersion: 1, MeetingID: meetingID, Month: month}
		} else if err != nil {
			return err
		}
		current.Changes = append(current.Changes, changes...)
		sort.SliceStable(current.Changes, func(i, j int) bool {
			return current.Changes[i].TS < current.Changes[j].TS
		})
		if topic != "" {
			current.Topic = topic
		}
		current.UpdatedAt = r.now().UTC()
		err = r.backend.Write(ctx, current, version)
		if errors.Is(err, repository.ErrConflict) {
			continue
		}
		return err
	}
	return fmt.Errorf("recording occupancy: %w", repository.ErrConflict)
}

// KeepAlive periodically rewrites retained objects so bucket lifecycle rules do not remove them.
func (r *Recorder) KeepAlive(ctx context.Context, interval time.Duration) {
	refresher, ok := r.backend.(Refresher)
	if !ok {
		return
	}
	for {
		if err := refresher.Refresh(ctx, r.oldestMonth(), r.now().Add(-refreshAge)); err != nil {
			log.Printf("Refreshing occupancy statistics failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// MonthReport summarizes every meeting with recorded occupancy in the month containing at.
func (r *Recorder) MonthReport(ctx context.Context, at time.Time) ([]Room, error) {
	at = at.In(r.location)
	start := time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, r.location)
	return r.Report(ctx, start, start.AddDate(0, 1, 0))
}

// RecentDays summarizes the last n local days, including today.
func (r *Recorder) RecentDays(ctx context.Context, n int) ([]Room, error) {
	now := r.now().In(r.location)
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, r.location).AddDate(0, 0, 1)
	return r.Report(ctx, end.AddDate(0, 0, -n), end)
}

// Report summarizes every meeting with recorded occupancy per local day in [from, to).
func (r *Recorder) Report(ctx context.Context, from, to time.Time) ([]Room, error) {
	from, to = from.In(r.location), to.In(r.location)
	merged := make(map[string]*Month)
	first := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, r.location)
	for month := first; month.Before(to); month = month.AddDate(0, 1, 0) {
		months, err := r.backend.ListMonth(ctx, month.Format(MonthLayout))
		if err != nil {
			return nil, err
		}
		for _, current := range months {
			room, ok := merged[current.MeetingID]
			if !ok {
				merged[current.MeetingID] = current
				continue
			}
			room.Changes = append(room.Changes, current.Changes...)
			if current.Topic != "" {
				room.Topic = current.Topic
			}
		}
	}
	rooms := make([]Room, 0, len(merged))
	for _, month := range merged {
		rooms = append(rooms, buildRoom(month, from, to, r.now()))
	}
	sort.Slice(rooms, func(i, j int) bool { return rooms[i].MeetingID < rooms[j].MeetingID })
	return rooms, nil
}
