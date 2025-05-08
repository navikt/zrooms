// Package redis provides a Redis/Valkey implementation of the repository interface
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/navikt/zrooms/internal/config"
	"github.com/navikt/zrooms/internal/models"
	"github.com/redis/go-redis/v9"
)

// Common errors
var (
	ErrNotFound = errors.New("entity not found")
)

// meetingState is the internal model for storing meeting state in Redis
type meetingState struct {
	ID             string // Meeting ID
	Topic          string // Meeting Topic
	Status         models.MeetingStatus
	StartTime      time.Time
	EndTime        time.Time
	ParticipantIDs []string // Store only participant IDs
}

// Repository implements the repository interface with Redis storage
type Repository struct {
	client    *redis.Client
	keyPrefix string
	ttl       time.Duration
}

// NewRepository creates a new Redis repository
func NewRepository(cfg config.RedisConfig) (*Repository, error) {
	var client *redis.Client

	// Use URI if provided, otherwise build connection from individual parameters
	if cfg.URI != "" {
		// Parse options from URI string
		opt, err := redis.ParseURL(cfg.URI)
		if err != nil {
			return nil, fmt.Errorf("failed to parse Redis URI: %w", err)
		}

		// Use DB from config if not specified in the URI
		if opt.DB == 0 {
			opt.DB = cfg.DB
		}

		// Use password from config if not in URI or if empty in URI
		if opt.Password == "" && cfg.Password != "" {
			opt.Password = cfg.Password
		}

		// Create client with options from URI
		client = redis.NewClient(opt)
	} else {
		// Build connection options from individual parameters
		address := fmt.Sprintf("%s:%s", cfg.Host, cfg.Port)

		// Create client with explicit options
		client = redis.NewClient(&redis.Options{
			Addr:     address,
			Username: cfg.Username,
			Password: cfg.Password,
			DB:       cfg.DB,
		})
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &Repository{
		client:    client,
		keyPrefix: cfg.KeyPrefix,
		ttl:       cfg.MeetingTTL,
	}, nil
}

// Close closes the Redis connection
func (r *Repository) Close() error {
	return r.client.Close()
}

// meetingKey returns the Redis key for a meeting
func (r *Repository) meetingKey(id string) string {
	return fmt.Sprintf("%smeetings:%s", r.keyPrefix, id)
}

// participantSetKey returns the Redis key for a meeting's participants set
func (r *Repository) participantSetKey(meetingID string) string {
	return fmt.Sprintf("%smeetings:%s:participants", r.keyPrefix, meetingID)
}

// SaveMeeting saves meeting state information to the repository
func (r *Repository) SaveMeeting(ctx context.Context, meeting *models.Meeting) error {
	state := meetingState{
		ID:        meeting.ID,
		Topic:     meeting.Topic,
		Status:    meeting.Status,
		StartTime: meeting.StartTime,
		EndTime:   meeting.EndTime,
	}

	// Convert state to JSON
	data, err := json.Marshal(&state)
	if err != nil {
		return fmt.Errorf("failed to marshal meeting: %w", err)
	}

	// Save to Redis with TTL
	key := r.meetingKey(meeting.ID)
	cmd := r.client.Set(ctx, key, data, r.ttl)
	if err := cmd.Err(); err != nil {
		return fmt.Errorf("failed to save meeting: %w", err)
	}

	return nil
}

// GetMeeting retrieves a meeting by ID
func (r *Repository) GetMeeting(ctx context.Context, id string) (*models.Meeting, error) {
	key := r.meetingKey(id)
	data, err := r.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get meeting: %w", err)
	}

	var state meetingState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal meeting: %w", err)
	}

	// Convert state back to a Meeting model
	meeting := &models.Meeting{
		ID:           state.ID,
		Topic:        state.Topic,
		Status:       state.Status,
		StartTime:    state.StartTime,
		EndTime:      state.EndTime,
		Participants: []models.Participant{}, // Empty slice, we don't store participant details
	}

	return meeting, nil
}

// ListMeetings returns all active meetings (not ended)
func (r *Repository) ListMeetings(ctx context.Context) ([]*models.Meeting, error) {
	// Get all meeting keys
	pattern := r.meetingKey("*")
	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list meetings: %w", err)
	}

	if len(keys) == 0 {
		return []*models.Meeting{}, nil
	}

	// Use MGET to retrieve all meeting data in a single roundtrip
	values, err := r.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get meeting data: %w", err)
	}

	meetings := make([]*models.Meeting, 0, len(values))

	// Process each meeting
	for _, v := range values {
		if v == nil {
			continue
		}

		strData, ok := v.(string)
		if !ok {
			continue
		}

		var state meetingState
		if err := json.Unmarshal([]byte(strData), &state); err != nil {
			continue
		}

		// Skip ended meetings for backward compatibility
		if state.Status == models.MeetingStatusEnded {
			continue
		}

		meeting := &models.Meeting{
			ID:           state.ID,
			Topic:        state.Topic,
			Status:       state.Status,
			StartTime:    state.StartTime,
			EndTime:      state.EndTime,
			Participants: []models.Participant{}, // Empty slice, we don't store participant details
		}

		meetings = append(meetings, meeting)
	}

	return meetings, nil
}

// ListAllMeetings returns all meetings, including ended ones
func (r *Repository) ListAllMeetings(ctx context.Context) ([]*models.Meeting, error) {
	// Get all meeting keys
	pattern := r.meetingKey("*")
	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list meetings: %w", err)
	}

	if len(keys) == 0 {
		return []*models.Meeting{}, nil
	}

	// Use MGET to retrieve all meeting data in a single roundtrip
	values, err := r.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get meeting data: %w", err)
	}

	meetings := make([]*models.Meeting, 0, len(values))

	// Process each meeting
	for _, v := range values {
		if v == nil {
			continue
		}

		strData, ok := v.(string)
		if !ok {
			continue
		}

		var state meetingState
		if err := json.Unmarshal([]byte(strData), &state); err != nil {
			continue
		}

		meeting := &models.Meeting{
			ID:           state.ID,
			Topic:        state.Topic,
			Status:       state.Status,
			StartTime:    state.StartTime,
			EndTime:      state.EndTime,
			Participants: []models.Participant{}, // Empty slice, we don't store participant details
		}

		meetings = append(meetings, meeting)
	}

	return meetings, nil
}

// DeleteMeeting removes a meeting by ID
func (r *Repository) DeleteMeeting(ctx context.Context, id string) error {
	key := r.meetingKey(id)
	participantsKey := r.participantSetKey(id)

	// Check if the meeting exists
	exists, err := r.client.Exists(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to check if meeting exists: %w", err)
	}
	if exists == 0 {
		return ErrNotFound
	}

	// Use a pipeline to delete both keys in one operation
	pipe := r.client.Pipeline()
	pipe.Del(ctx, key)
	pipe.Del(ctx, participantsKey)
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete meeting: %w", err)
	}

	return nil
}

// AddParticipantToMeeting adds a participant ID to a meeting
func (r *Repository) AddParticipantToMeeting(ctx context.Context, meetingID, participantID string) error {
	// Check if the meeting exists
	exists, err := r.client.Exists(ctx, r.meetingKey(meetingID)).Result()
	if err != nil {
		return fmt.Errorf("failed to check if meeting exists: %w", err)
	}
	if exists == 0 {
		return ErrNotFound
	}

	// Add participant to the set
	key := r.participantSetKey(meetingID)
	err = r.client.SAdd(ctx, key, participantID).Err()
	if err != nil {
		return fmt.Errorf("failed to add participant: %w", err)
	}

	// Set TTL on the participants set to match the meeting TTL
	if r.ttl > 0 {
		err = r.client.Expire(ctx, key, r.ttl).Err()
		if err != nil {
			return fmt.Errorf("failed to set expiry on participants: %w", err)
		}
	}

	return nil
}

// RemoveParticipantFromMeeting removes a participant ID from a meeting
func (r *Repository) RemoveParticipantFromMeeting(ctx context.Context, meetingID, participantID string) error {
	// Check if the meeting exists
	exists, err := r.client.Exists(ctx, r.meetingKey(meetingID)).Result()
	if err != nil {
		return fmt.Errorf("failed to check if meeting exists: %w", err)
	}
	if exists == 0 {
		return ErrNotFound
	}

	// Remove participant from the set
	err = r.client.SRem(ctx, r.participantSetKey(meetingID), participantID).Err()
	if err != nil {
		return fmt.Errorf("failed to remove participant: %w", err)
	}

	return nil
}

// CountParticipantsInMeeting counts the number of participants in a meeting
func (r *Repository) CountParticipantsInMeeting(ctx context.Context, meetingID string) (int, error) {
	// Check if the meeting exists
	exists, err := r.client.Exists(ctx, r.meetingKey(meetingID)).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to check if meeting exists: %w", err)
	}
	if exists == 0 {
		return 0, ErrNotFound
	}

	// Get the count of participants
	count, err := r.client.SCard(ctx, r.participantSetKey(meetingID)).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to count participants: %w", err)
	}

	return int(count), nil
}
