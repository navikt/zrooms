package models

import (
	"time"
)

// MeetingStatus represents the current status of a meeting
type MeetingStatus int

const (
	MeetingStatusCreated MeetingStatus = iota
	MeetingStatusUpdated
	MeetingStatusStarted
	MeetingStatusEnded
)

// String returns the string representation of a meeting status
func (s MeetingStatus) String() string {
	return [...]string{"created", "updated", "started", "ended"}[s]
}

// Participant represents a user participating in a meeting
type Participant struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	JoinTime  time.Time `json:"join_time,omitempty"`
	LeaveTime time.Time `json:"leave_time,omitempty"`
}

// Meeting represents a Zoom meeting
type Meeting struct {
	ID           string        `json:"id"`
	Topic        string        `json:"topic"`
	StartTime    time.Time     `json:"start_time"`
	EndTime      time.Time     `json:"end_time,omitempty"`
	Duration     int           `json:"duration"` // in minutes
	Status       MeetingStatus `json:"status"`
	Host         Participant   `json:"host"`
	Participants []Participant `json:"participants"`
}

// AddParticipant adds a participant to the meeting
func (m *Meeting) AddParticipant(participant Participant) {
	// Set join time if not already set
	if participant.JoinTime.IsZero() {
		participant.JoinTime = time.Now()
	}

	m.Participants = append(m.Participants, participant)
}

// RemoveParticipant removes a participant from the meeting by ID
// Returns true if participant was found and removed, false otherwise
func (m *Meeting) RemoveParticipant(participantID string) bool {
	for i, p := range m.Participants {
		if p.ID == participantID {
			// Remove participant by swapping with the last element and truncating
			p.LeaveTime = time.Now()

			// If the participant is not the last one in the slice
			if i < len(m.Participants)-1 {
				m.Participants[i] = m.Participants[len(m.Participants)-1]
			}

			m.Participants = m.Participants[:len(m.Participants)-1]
			return true
		}
	}
	return false
}
