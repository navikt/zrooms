// Package repository defines interfaces for data storage
package repository

import (
	"context"
	"fmt"
	"log"

	"github.com/navikt/zrooms/internal/config"
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
	ClearPartipantsInMeeting(ctx context.Context, meetingID string) error
}

// NewRepository creates a repository based on configuration
func NewRepository(cfg config.RedisConfig) (Repository, error) {
	if cfg.Enabled {
		// Format the address based on host and port if not using URI
		connectionInfo := cfg.URI
		if connectionInfo == "" {
			connectionInfo = fmt.Sprintf("%s:%s", cfg.Host, cfg.Port)
		}

		log.Printf("Using Redis repository at %s", connectionInfo)
		repo, err := newRedisRepository(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create Redis repository: %w", err)
		}
		return repo, nil
	}

	log.Printf("Using in-memory repository")
	return newMemoryRepository(), nil
}

// Implementation constructors are imported dynamically to avoid circular dependencies
// These are replaced with actual implementations when the factory is used

var newRedisRepository = func(cfg config.RedisConfig) (Repository, error) {
	// This function will be replaced by the actual implementation from redis package
	return nil, fmt.Errorf("Redis repository not implemented")
}

var newMemoryRepository = func() Repository {
	// This function will be replaced by the actual implementation from memory package
	return nil
}
