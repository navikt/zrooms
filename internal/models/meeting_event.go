package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidEvent     = errors.New("invalid meeting event")
	ErrUnsupportedEvent = errors.New("unsupported meeting event")
)

type MeetingEvent struct {
	Type          string
	EventTS       int64
	Meeting       Meeting
	ParticipantID string
}

func (e MeetingEvent) Validate() error {
	switch e.Type {
	case "meeting.created", "meeting.updated":
	case "meeting.started", "meeting.ended":
		if e.Meeting.UUID == "" {
			return fmt.Errorf("%w: meeting UUID required", ErrInvalidEvent)
		}
	case "meeting.participant_joined", "meeting.participant_left":
		if e.Meeting.UUID == "" || strings.TrimSpace(e.ParticipantID) == "" || len(e.ParticipantID) > 256 {
			return fmt.Errorf("%w: meeting UUID and participant identifier required", ErrInvalidEvent)
		}
	default:
		return ErrUnsupportedEvent
	}
	if strings.TrimSpace(e.Meeting.ID) == "" || len(e.Meeting.ID) > 256 || e.EventTS <= 0 {
		return fmt.Errorf("%w: meeting ID and event timestamp required", ErrInvalidEvent)
	}
	if len(e.Meeting.UUID) > 256 {
		return fmt.Errorf("%w: meeting UUID too long", ErrInvalidEvent)
	}
	return nil
}

func (e WebhookEvent) MeetingEvent() (MeetingEvent, error) {
	if e.Event == "" {
		return MeetingEvent{}, fmt.Errorf("%w: event type required", ErrInvalidEvent)
	}
	switch e.Event {
	case "meeting.created", "meeting.updated", "meeting.started", "meeting.ended", "meeting.participant_joined", "meeting.participant_left":
	default:
		return MeetingEvent{}, ErrUnsupportedEvent
	}
	var payload StandardEventPayload
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return MeetingEvent{}, fmt.Errorf("%w: malformed payload", ErrInvalidEvent)
	}
	eventTime := time.UnixMilli(e.EventTS).UTC()
	event := MeetingEvent{
		Type: e.Event, EventTS: e.EventTS,
		Meeting: Meeting{
			ID: payload.Object.ID, UUID: payload.Object.UUID,
			Topic: payload.Object.Topic, Host: Participant{ID: payload.Object.HostID},
			OperatorEmail: payload.Operator, Duration: payload.Object.Duration,
			StartTime: payload.Object.StartTime, EndTime: payload.Object.EndTime,
		},
	}
	if event.Meeting.StartTime.IsZero() && (event.Type == "meeting.started" || event.Type == "meeting.participant_joined" || event.Type == "meeting.participant_left") {
		event.Meeting.StartTime = eventTime
	}
	if event.Meeting.EndTime.IsZero() {
		event.Meeting.EndTime = eventTime
	}
	if participant := payload.Object.Participant; participant != nil {
		// user_id distinguishes meeting connections, including anonymous guests.
		event.ParticipantID = participant.UserID
		if event.ParticipantID == "" {
			event.ParticipantID = participant.ID
		}
	}
	return event, event.Validate()
}

// Zoom uses both JSON strings and numbers for meeting IDs across event types.
func (o *EventObject) UnmarshalJSON(data []byte) error {
	type object EventObject
	var value struct {
		*object
		ID json.RawMessage `json:"id"`
	}
	value.object = (*object)(o)
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if len(value.ID) == 0 || string(value.ID) == "null" {
		return nil
	}
	if value.ID[0] == '"' {
		return json.Unmarshal(value.ID, &o.ID)
	}
	var number json.Number
	if err := json.Unmarshal(value.ID, &number); err != nil {
		return err
	}
	if _, err := number.Int64(); err != nil {
		return err
	}
	o.ID = number.String()
	return nil
}
