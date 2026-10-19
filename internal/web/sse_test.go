package web

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSSEBroadcastReachesEveryClient(t *testing.T) {
	manager := NewSSEManager()
	server := httptest.NewServer(manager)
	defer server.Close()
	defer manager.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	readers := make([]*bufio.Scanner, 2)
	for i := range readers {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
		require.NoError(t, err)
		resp, err := server.Client().Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
		require.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"))
		readers[i] = bufio.NewScanner(resp.Body)
		require.True(t, readers[i].Scan())
		require.Equal(t, "event: connected", readers[i].Text())
		require.True(t, readers[i].Scan())
		require.True(t, readers[i].Scan())
	}
	manager.NotifyMeetingUpdate(nil)
	for _, reader := range readers {
		require.True(t, reader.Scan())
		require.Equal(t, "event: update", reader.Text())
	}
	manager.Shutdown()
	manager.Shutdown()
}

func TestSSECoalescesUpdatesForSlowClients(t *testing.T) {
	manager := NewSSEManager()
	updates := make(chan struct{}, 1)
	manager.clients[updates] = struct{}{}
	for i := 0; i < 1000; i++ {
		manager.NotifyMeetingUpdate(nil)
	}
	require.Len(t, updates, 1)
}
