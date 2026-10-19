package memory

import (
	"context"
	"sync"
	"time"

	"github.com/navikt/zrooms/internal/repository"
)

type entry struct {
	state   *repository.State
	version int64
}

type backend struct {
	mu       sync.RWMutex
	entries  map[string]entry
	revision int64
}

func NewRepository() *repository.Store {
	return repository.NewStore(&backend{entries: make(map[string]entry)})
}

func clone(state *repository.State) *repository.State {
	copy := *state
	copy.Participants = make(map[string]repository.Presence, len(state.Participants))
	for id, participant := range state.Participants {
		copy.Participants[id] = participant
	}
	return &copy
}

func (b *backend) Read(ctx context.Context, id string) (*repository.State, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	value, ok := b.entries[id]
	if !ok {
		return nil, 0, repository.ErrNotFound
	}
	return clone(value.state), value.version, nil
}

func (b *backend) Write(ctx context.Context, id string, state *repository.State, version int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.entries[id].version != version {
		return repository.ErrConflict
	}
	b.revision++
	b.entries[id] = entry{state: clone(state), version: b.revision}
	return nil
}

func (b *backend) List(ctx context.Context) ([]*repository.State, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	states := make([]*repository.State, 0, len(b.entries))
	for id, value := range b.entries {
		if !time.Now().Before(value.state.UpdatedAt.Add(repository.Retention)) {
			delete(b.entries, id)
			continue
		}
		states = append(states, clone(value.state))
	}
	return states, nil
}

func (b *backend) Check(ctx context.Context) error {
	return ctx.Err()
}
