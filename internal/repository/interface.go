package repository

import (
	"context"
	"errors"
	"time"

	"github.com/navikt/zrooms/internal/models"
)

var (
	ErrNotFound = errors.New("meeting not found")
	ErrConflict = errors.New("meeting changed concurrently")
)

const Retention = 7 * 24 * time.Hour

type Repository interface {
	ApplyEvent(context.Context, models.MeetingEvent) (*models.Meeting, error)
	GetMeeting(context.Context, string) (*models.Meeting, error)
	ListAllMeetings(context.Context) ([]*models.Meeting, error)
	DeleteMeeting(context.Context, string) error
	Check(context.Context) error
}

// Backend uses version 0 for an absent object. Writes must compare and swap.
type Backend interface {
	Read(context.Context, string) (*State, int64, error)
	Write(context.Context, string, *State, int64) error
	List(context.Context) ([]*State, error)
	Check(context.Context) error
}

type Presence struct {
	Present bool  `json:"present"`
	EventTS int64 `json:"event_ts"`
}

// State keeps ordering markers, including departed participants, until expiry.
type State struct {
	SchemaVersion int                 `json:"schema_version"`
	Meeting       models.Meeting      `json:"meeting"`
	LifecycleTS   int64               `json:"lifecycle_ts"`
	OccurrenceTS  int64               `json:"occurrence_ts"`
	MetadataTS    int64               `json:"metadata_ts"`
	OperatorTS    int64               `json:"operator_ts"`
	Participants  map[string]Presence `json:"participants,omitempty"`
	UpdatedAt     time.Time           `json:"updated_at"`
	DeletedAt     int64               `json:"deleted_at,omitempty"`
}
