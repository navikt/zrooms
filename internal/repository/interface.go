// Package repository defines interfaces for data storage
package repository

import (
	"context"

	"github.com/navikt/zrooms/internal/models"
)

// Repository defines the interface for storing and retrieving meeting and room data
type Repository interface {
	// Meeting operations
	SaveMeeting(ctx context.Context, meeting *models.Meeting) error
	GetMeeting(ctx context.Context, id string) (*models.Meeting, error)
	ListMeetings(ctx context.Context) ([]*models.Meeting, error)
	DeleteMeeting(ctx context.Context, id string) error

	// Room operations
	SaveRoom(ctx context.Context, room *models.Room) error
	GetRoom(ctx context.Context, id string) (*models.Room, error)
	ListRooms(ctx context.Context) ([]*models.Room, error)

	// Participant operations - only stores IDs, not PII
	AddParticipantToMeeting(ctx context.Context, meetingID string, participantID string) error
	RemoveParticipantFromMeeting(ctx context.Context, meetingID string, participantID string) error
	CountParticipantsInMeeting(ctx context.Context, meetingID string) (int, error)

	// Room status operations
	GetRoomStatus(ctx context.Context, roomID string) (*models.RoomStatus, error)
	ListRoomStatuses(ctx context.Context) ([]*models.RoomStatus, error)
}
