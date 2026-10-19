package models

import (
	"encoding/json"
	"time"
)

type WebhookEvent struct {
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload"`
	EventTS int64           `json:"event_ts"`
}

type StandardEventPayload struct {
	AccountID string      `json:"account_id"`
	Object    EventObject `json:"object"`
	Operator  string      `json:"operator,omitempty"`
}

type EventObject struct {
	UUID        string            `json:"uuid"`
	ID          string            `json:"id"`
	HostID      string            `json:"host_id"`
	Topic       string            `json:"topic"`
	Type        int               `json:"type"`
	StartTime   time.Time         `json:"start_time,omitempty"`
	EndTime     time.Time         `json:"end_time,omitempty"`
	Duration    int               `json:"duration"`
	Timezone    string            `json:"timezone,omitempty"`
	Participant *ParticipantEvent `json:"participant,omitempty"`
}

type ParticipantEvent struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Name      string    `json:"user_name"`
	Email     string    `json:"email"`
	JoinTime  time.Time `json:"join_time,omitempty"`
	LeaveTime time.Time `json:"leave_time,omitempty"`
}
