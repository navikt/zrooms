package stats

import (
	"context"
	"testing"
	"time"

	"github.com/navikt/zrooms/internal/models"
	"github.com/stretchr/testify/require"
)

func TestMonthReportSummarizesDailyOccupancy(t *testing.T) {
	oslo, err := time.LoadLocation("Europe/Oslo")
	require.NoError(t, err)
	at := func(day, hour, minute int) int64 {
		return time.Date(2026, time.November, day, hour, minute, 0, 0, oslo).UnixMilli()
	}
	recorder := NewRecorder(NewMemoryBackend(), oslo)
	recorder.now = func() time.Time { return time.Date(2026, time.November, 11, 14, 0, 0, 0, oslo) }
	ctx := context.Background()
	// Monday: one participant for six hours. Delivered out of order.
	require.NoError(t, recorder.Record(ctx, "123", "Room", []models.OccupancyChange{{TS: at(9, 15, 0), Delta: -1}}))
	require.NoError(t, recorder.Record(ctx, "123", "", []models.OccupancyChange{{TS: at(9, 9, 0), Delta: 1}}))
	// Wednesday: one before lunch, three all afternoon, still ongoing.
	require.NoError(t, recorder.Record(ctx, "123", "", []models.OccupancyChange{
		{TS: at(11, 8, 0), Delta: 1}, {TS: at(11, 11, 0), Delta: -1},
		{TS: at(11, 12, 15), Delta: 1}, {TS: at(11, 12, 15), Delta: 1}, {TS: at(11, 12, 20), Delta: 1},
	}))
	// An unmatched leave from lost events must not create negative occupancy.
	require.NoError(t, recorder.Record(ctx, "other", "", []models.OccupancyChange{{TS: at(2, 10, 0), Delta: -1}}))
	// Changes beyond retention are discarded.
	require.NoError(t, recorder.Record(ctx, "old", "", []models.OccupancyChange{
		{TS: time.Date(2025, time.October, 1, 10, 0, 0, 0, oslo).UnixMilli(), Delta: 1},
	}))

	rooms, err := recorder.MonthReport(ctx, time.Date(2026, time.November, 30, 0, 0, 0, 0, oslo))
	require.NoError(t, err)
	require.Len(t, rooms, 2)
	room := rooms[0]
	require.Equal(t, "Room", room.Topic)
	require.Zero(t, rooms[1].Active)
	require.Len(t, room.Days, 30)
	require.Zero(t, room.Days[9].Active)

	monday := room.Days[8]
	require.Equal(t, time.Monday, monday.Date.Weekday())
	require.Equal(t, 6*time.Hour, monday.Active)
	require.Equal(t, 6*time.Hour, monday.ParticipantTime)
	require.Equal(t, 1, monday.Peak)
	require.Equal(t, []Session{{Start: time.UnixMilli(at(9, 9, 0)).In(oslo), End: time.UnixMilli(at(9, 15, 0)).In(oslo), Peak: 1}}, monday.Sessions)
	require.Equal(t, 0, monday.Slots[17])
	require.Equal(t, 1, monday.Slots[18])
	require.Equal(t, 1, monday.Slots[29])
	require.Equal(t, 0, monday.Slots[30])

	wednesday := room.Days[10]
	require.Equal(t, 3*time.Hour+105*time.Minute, wednesday.Active)
	require.Equal(t, 3*time.Hour+5*time.Minute*2+100*time.Minute*3, wednesday.ParticipantTime)
	require.Equal(t, 3, wednesday.Peak)
	require.Equal(t, 1, wednesday.Slots[16])
	require.Equal(t, 0, wednesday.Slots[22])
	require.Equal(t, 3, wednesday.Slots[24])
	require.Equal(t, 3, wednesday.Slots[27])
	require.Equal(t, 0, wednesday.Slots[28])
	require.Len(t, wednesday.Sessions, 2)
	require.True(t, wednesday.Sessions[1].Ongoing)
	require.Equal(t, 105*time.Minute, wednesday.Sessions[1].Duration())

	old, err := recorder.MonthReport(ctx, time.Date(2025, time.October, 1, 0, 0, 0, 0, oslo))
	require.NoError(t, err)
	require.Empty(t, old)
}

func TestRecentDaysSpansMonthFiles(t *testing.T) {
	oslo, err := time.LoadLocation("Europe/Oslo")
	require.NoError(t, err)
	recorder := NewRecorder(NewMemoryBackend(), oslo)
	recorder.now = func() time.Time { return time.Date(2026, time.November, 3, 12, 0, 0, 0, oslo) }
	ctx := context.Background()
	require.NoError(t, recorder.Record(ctx, "123", "Room", []models.OccupancyChange{
		{TS: time.Date(2026, time.October, 31, 23, 0, 0, 0, oslo).UnixMilli(), Delta: 1},
		{TS: time.Date(2026, time.November, 1, 1, 0, 0, 0, oslo).UnixMilli(), Delta: -1},
	}))

	rooms, err := recorder.RecentDays(ctx, 7)
	require.NoError(t, err)
	require.Len(t, rooms, 1)
	days := rooms[0].Days
	require.Len(t, days, 7)
	require.Equal(t, time.Date(2026, time.October, 28, 0, 0, 0, 0, oslo), days[0].Date)
	require.Equal(t, time.Hour, days[3].Active)
	require.Equal(t, time.Hour, days[4].Active)
	require.Equal(t, 1, days[4].Slots[1])
	require.Equal(t, 2*time.Hour, rooms[0].Active)
}
