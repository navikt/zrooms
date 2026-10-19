package web

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/navikt/zrooms/internal/models"
)

type SSEManager struct {
	mu       sync.Mutex
	clients  map[chan struct{}]struct{}
	shutdown chan struct{}
	once     sync.Once
}

func NewSSEManager() *SSEManager {
	return &SSEManager{clients: make(map[chan struct{}]struct{}), shutdown: make(chan struct{})}
}

func (sm *SSEManager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/event-stream")
	controller := http.NewResponseController(w)
	send := func(message string) error {
		if err := controller.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return err
		}
		if _, err := fmt.Fprint(w, message); err != nil {
			return err
		}
		return controller.Flush()
	}
	updates := make(chan struct{}, 1)
	sm.mu.Lock()
	sm.clients[updates] = struct{}{}
	sm.mu.Unlock()
	defer func() {
		sm.mu.Lock()
		delete(sm.clients, updates)
		sm.mu.Unlock()
	}()
	if err := send("event: connected\ndata: connected\n\n"); err != nil {
		log.Printf("SSE connection failed: %v", err)
		return
	}
	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-sm.shutdown:
			return
		case <-heartbeat.C:
			if err := send(":\n\n"); err != nil {
				return
			}
		case <-updates:
			if err := send("event: update\ndata: update\n\n"); err != nil {
				return
			}
		}
	}
}

func (sm *SSEManager) NotifyMeetingUpdate(_ *models.Meeting) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for updates := range sm.clients {
		select {
		case updates <- struct{}{}:
		default:
			// One pending invalidation is sufficient: clients fetch the full view.
		}
	}
}

func (sm *SSEManager) Shutdown() {
	sm.once.Do(func() { close(sm.shutdown) })
}
