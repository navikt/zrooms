package stats

import (
	"sort"
	"time"

	"github.com/navikt/zrooms/internal/models"
)

const (
	SlotDuration = 30 * time.Minute
	SlotsPerDay  = 48
)

type Session struct {
	Start   time.Time
	End     time.Time
	Peak    int
	Ongoing bool
}

func (s Session) Duration() time.Duration {
	return s.End.Sub(s.Start)
}

// Day summarizes occupancy for one local calendar day. Days without activity are included.
type Day struct {
	Date time.Time
	// Slots holds the peak participant count for each 30-minute wall-clock slot.
	Slots           []int
	Active          time.Duration
	ParticipantTime time.Duration
	Peak            int
	Sessions        []Session
}

type Room struct {
	MeetingID       string
	MeetingType     int
	Topic           string
	Days            []Day
	Active          time.Duration
	ParticipantTime time.Duration
}

type segment struct {
	start, end time.Time
	count      int
}

// occupancy turns changes into periods with at least one participant, bounded by [from, min(to, now)).
func occupancy(changes []models.OccupancyChange, from, to, now time.Time) []segment {
	sorted := append([]models.OccupancyChange(nil), changes...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].TS < sorted[j].TS })
	limit := to
	if now.Before(limit) {
		limit = now.In(to.Location())
	}
	var segments []segment
	count := 0
	for i := 0; i < len(sorted); {
		at := sorted[i].TS
		for ; i < len(sorted) && sorted[i].TS == at; i++ {
			count += sorted[i].Delta
		}
		// Lost events can leave the sum negative; never report negative occupancy.
		count = max(count, 0)
		start, end := time.UnixMilli(at).In(from.Location()), limit
		if i < len(sorted) && time.UnixMilli(sorted[i].TS).Before(limit) {
			end = time.UnixMilli(sorted[i].TS).In(from.Location())
		}
		if start.Before(from) {
			start = from
		}
		if count > 0 && end.After(start) {
			segments = append(segments, segment{start: start, end: end, count: count})
		}
	}
	return segments
}

func sessions(segments []segment, now time.Time) []Session {
	var result []Session
	for _, seg := range segments {
		if n := len(result); n > 0 && result[n-1].End.Equal(seg.start) {
			result[n-1].End = seg.end
			result[n-1].Peak = max(result[n-1].Peak, seg.count)
			continue
		}
		result = append(result, Session{Start: seg.start, End: seg.end, Peak: seg.count})
	}
	if n := len(result); n > 0 && !result[n-1].End.Before(now) {
		result[n-1].Ongoing = true
	}
	return result
}

func slotOf(t time.Time) int {
	return t.Hour()*2 + t.Minute()/30
}

func buildRoom(month *Month, from, to, now time.Time) Room {
	room := Room{MeetingID: month.MeetingID, MeetingType: month.MeetingType, Topic: month.Topic}
	segments := occupancy(month.Changes, from, to, now)
	allSessions := sessions(segments, now)
	for date := from; date.Before(to); date = date.AddDate(0, 0, 1) {
		next := date.AddDate(0, 0, 1)
		day := Day{Date: date, Slots: make([]int, SlotsPerDay)}
		for _, seg := range segments {
			start, end := seg.start, seg.end
			if start.Before(date) {
				start = date
			}
			if end.After(next) {
				end = next
			}
			if !end.After(start) {
				continue
			}
			day.Active += end.Sub(start)
			day.ParticipantTime += time.Duration(seg.count) * end.Sub(start)
			day.Peak = max(day.Peak, seg.count)
			first, last := slotOf(start), slotOf(end.Add(-time.Nanosecond))
			for slot := first; slot <= max(first, last); slot++ {
				day.Slots[slot] = max(day.Slots[slot], seg.count)
			}
		}
		for _, session := range allSessions {
			if !session.Start.Before(date) && session.Start.Before(next) {
				day.Sessions = append(day.Sessions, session)
			}
		}
		room.Days = append(room.Days, day)
		room.Active += day.Active
		room.ParticipantTime += day.ParticipantTime
	}
	return room
}
