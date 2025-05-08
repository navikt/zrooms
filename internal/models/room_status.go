package models

import "time"

// RoomStatus represents the current status of a room for display purposes
type RoomStatus struct {
	RoomID           string    `json:"room_id"`
	RoomName         string    `json:"room_name"`
	Available        bool      `json:"available"`
	CurrentMeetingID string    `json:"current_meeting_id,omitempty"`
	MeetingTopic     string    `json:"meeting_topic,omitempty"`
	ParticipantCount int       `json:"participant_count,omitempty"`
	MeetingStartTime time.Time `json:"meeting_start_time,omitempty"`
}
