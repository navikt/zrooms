package stats

import (
	"context"
	"sync"

	"github.com/navikt/zrooms/internal/models"
	"github.com/navikt/zrooms/internal/repository"
)

type memoryEntry struct {
	month   Month
	version int64
}

// MemoryBackend is volatile storage for local development and tests.
type MemoryBackend struct {
	mu       sync.Mutex
	entries  map[string]memoryEntry
	revision int64
}

func NewMemoryBackend() *MemoryBackend {
	return &MemoryBackend{entries: make(map[string]memoryEntry)}
}

func cloneMonth(month Month) *Month {
	month.Changes = append([]models.OccupancyChange(nil), month.Changes...)
	return &month
}

func (b *MemoryBackend) Read(ctx context.Context, meetingID, month string) (*Month, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	entry, ok := b.entries[meetingID+"/"+month]
	if !ok {
		return nil, 0, repository.ErrNotFound
	}
	return cloneMonth(entry.month), entry.version, nil
}

func (b *MemoryBackend) Write(ctx context.Context, month *Month, version int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	key := month.MeetingID + "/" + month.Month
	if b.entries[key].version != version {
		return repository.ErrConflict
	}
	b.revision++
	b.entries[key] = memoryEntry{month: *cloneMonth(*month), version: b.revision}
	return nil
}

func (b *MemoryBackend) ListMonth(ctx context.Context, month string) ([]*Month, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var months []*Month
	for _, entry := range b.entries {
		if entry.month.Month == month {
			months = append(months, cloneMonth(entry.month))
		}
	}
	return months, nil
}
