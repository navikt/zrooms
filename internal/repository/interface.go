// Package repository defines interfaces for data storage
package repository

import (
	"context"

	"github.com/navikt/zrooms/internal/models"
)

// Repository defines the interface for storing and retrieving meeting data
type Repository interface {
	// Meeting operations
	SaveMeeting(ctx context.Context, meeting *models.Meeting) error
	GetMeeting(ctx context.Context, id string) (*models.Meeting, error)
	ListMeetings(ctx context.Context) ([]*models.Meeting, error)
	ListAllMeetings(ctx context.Context) ([]*models.Meeting, error)
	DeleteMeeting(ctx context.Context, id string) error

	// Participant operations - only stores IDs, not PII
	AddParticipantToMeeting(ctx context.Context, meetingID string, participantID string) error
	RemoveParticipantFromMeeting(ctx context.Context, meetingID string, participantID string) error
	CountParticipantsInMeeting(ctx context.Context, meetingID string) (int, error)
}
