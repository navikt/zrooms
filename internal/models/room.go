package models

// Room represents a physical meeting room
type Room struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Capacity         int    `json:"capacity"`
	Location         string `json:"location"`
	CurrentMeetingID string `json:"current_meeting_id,omitempty"`
}

// IsAvailable returns true if the room has no active meeting
func (r *Room) IsAvailable() bool {
	return r.CurrentMeetingID == ""
}
