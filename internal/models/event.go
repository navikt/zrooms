package models

import (
	"time"
)

// WebhookEvent represents the base structure of a Zoom webhook event
type WebhookEvent struct {
	Event   string       `json:"event"`
	Payload EventPayload `json:"payload"`
	EventTS int64        `json:"event_ts"` // Unix timestamp in milliseconds
}

// EventPayload contains the common payload structure for Zoom webhook events
type EventPayload struct {
	AccountID string      `json:"account_id"`
	Object    EventObject `json:"object"`
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
	return &Meeting{
		ID:        e.Payload.Object.ID,
		Topic:     e.Payload.Object.Topic,
		StartTime: e.Payload.Object.StartTime,
		Duration:  e.Payload.Object.Duration,
		Status:    MeetingStatusCreated,
		Host: Participant{
			ID: e.Payload.Object.HostID,
		},
		Participants: []Participant{},
	}
}

// ProcessMeetingStarted handles a meeting.started event
func (e *WebhookEvent) ProcessMeetingStarted() *Meeting {
	return &Meeting{
		ID:        e.Payload.Object.ID,
		Topic:     e.Payload.Object.Topic,
		StartTime: time.Now(),
		Duration:  e.Payload.Object.Duration,
		Status:    MeetingStatusStarted,
		Host: Participant{
			ID: e.Payload.Object.HostID,
		},
		Participants: []Participant{},
	}
}

// ProcessMeetingEnded handles a meeting.ended event
func (e *WebhookEvent) ProcessMeetingEnded() *Meeting {
	return &Meeting{
		ID:      e.Payload.Object.ID,
		Topic:   e.Payload.Object.Topic,
		EndTime: time.Now(),
		Status:  MeetingStatusEnded,
		Host: Participant{
			ID: e.Payload.Object.HostID,
		},
	}
}

// ProcessParticipantJoined handles a meeting.participant_joined event
func (e *WebhookEvent) ProcessParticipantJoined() *Participant {
	if e.Payload.Object.Participant == nil {
		return nil
	}

	return &Participant{
		ID:       e.Payload.Object.Participant.UserID,
		Name:     e.Payload.Object.Participant.Name,
		Email:    e.Payload.Object.Participant.Email,
		JoinTime: time.Now(),
	}
}

// ProcessParticipantLeft handles a meeting.participant_left event
func (e *WebhookEvent) ProcessParticipantLeft() *Participant {
	if e.Payload.Object.Participant == nil {
		return nil
	}

	return &Participant{
		ID:        e.Payload.Object.Participant.UserID,
		Name:      e.Payload.Object.Participant.Name,
		Email:     e.Payload.Object.Participant.Email,
		LeaveTime: time.Now(),
	}
}
