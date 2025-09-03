package models

import (
	"encoding/json"
	"time"
)

// WebhookEvent represents the base structure of a Zoom webhook event
type WebhookEvent struct {
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload"`  // Use RawMessage for flexibility with different payload types
	EventTS int64           `json:"event_ts"` // Unix timestamp in milliseconds
}

// StandardEventPayload contains the common payload structure for regular Zoom webhook events
type StandardEventPayload struct {
	AccountID string      `json:"account_id"`
	Object    EventObject `json:"object"`
	Operator  string      `json:"operator,omitempty"` // Email address of the user who performed the action
}

// EventObject contains details about the meeting object in a Zoom webhook event
type EventObject struct {
	UUID        string            `json:"uuid"`
	ID          string            `json:"id"`
	HostID      string            `json:"host_id"`
	Topic       string            `json:"topic"`
	Type        int               `json:"type"`
	StartTime   time.Time         `json:"start_time,omitempty"`
	Duration    int               `json:"duration"`
	Timezone    string            `json:"timezone,omitempty"`
	Participant *ParticipantEvent `json:"participant,omitempty"`
}

// ParticipantEvent contains details about a participant in participant-related events
type ParticipantEvent struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Name      string    `json:"user_name"`
	Email     string    `json:"email"`
	JoinTime  time.Time `json:"join_time,omitempty"`
	LeaveTime time.Time `json:"leave_time,omitempty"`
}

// ProcessMeetingCreated handles a meeting.created event
func (e *WebhookEvent) ProcessMeetingCreated() *Meeting {
	var payload StandardEventPayload
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return nil
	}

	meeting := &Meeting{
		ID:            payload.Object.ID,
		Topic:         payload.Object.Topic,
		StartTime:     payload.Object.StartTime,
		Duration:      payload.Object.Duration,
		Status:        MeetingStatusCreated,
		OperatorEmail: payload.Operator,
		Host: Participant{
			ID: payload.Object.HostID,
		},
		Participants: []Participant{},
	}

	return meeting
}

// ProcessMeetingStarted handles a meeting.started event
func (e *WebhookEvent) ProcessMeetingStarted() *Meeting {
	var payload StandardEventPayload
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return nil
	}

	meeting := &Meeting{
		ID:        payload.Object.ID,
		Topic:     payload.Object.Topic,
		StartTime: time.Now(),
		Duration:  payload.Object.Duration,
		Status:    MeetingStatusStarted,
		Host: Participant{
			ID: payload.Object.HostID,
		},
		Participants: []Participant{},
	}

	return meeting
}

func (e *WebhookEvent) ProcessMeetingUpdated() *Meeting {
	var payload StandardEventPayload
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return nil
	}

	meeting := &Meeting{
		ID:            payload.Object.ID,
		Topic:         payload.Object.Topic,
		StartTime:     payload.Object.StartTime,
		Duration:      payload.Object.Duration,
		Status:        MeetingStatusUpdated,
		OperatorEmail: payload.Operator,
		Host: Participant{
			ID: payload.Object.HostID,
		},
		Participants: []Participant{},
	}

	return meeting
}

// ProcessMeetingEnded handles a meeting.ended event
func (e *WebhookEvent) ProcessMeetingEnded() *Meeting {
	var payload StandardEventPayload
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return nil
	}

	meeting := &Meeting{
		ID:            payload.Object.ID,
		Topic:         payload.Object.Topic,
		EndTime:       time.Now(),
		Status:        MeetingStatusEnded,
		OperatorEmail: payload.Operator,
		Host: Participant{
			ID: payload.Object.HostID,
		},
	}

	return meeting
}

// ProcessParticipantJoined handles a meeting.participant_joined event
func (e *WebhookEvent) ProcessParticipantJoined() *Participant {
	var payload StandardEventPayload
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return nil
	}

	if payload.Object.Participant == nil {
		return nil
	}

	return &Participant{
		ID:       payload.Object.Participant.ID, // Use ID instead of UserID
		Name:     payload.Object.Participant.Name,
		Email:    payload.Object.Participant.Email,
		JoinTime: time.Now(),
	}
}

// ProcessParticipantLeft handles a meeting.participant_left event
func (e *WebhookEvent) ProcessParticipantLeft() *Participant {
	var payload StandardEventPayload
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return nil
	}

	if payload.Object.Participant == nil {
		return nil
	}

	return &Participant{
		ID:        payload.Object.Participant.ID, // Use ID instead of UserID
		Name:      payload.Object.Participant.Name,
		Email:     payload.Object.Participant.Email,
		LeaveTime: time.Now(),
	}
}
