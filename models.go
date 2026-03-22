package main

import (
	"time"
)

// Ticket represents a support ticket
type Ticket struct {
	UserID    string    `json:"user_id"`
	ChannelID string    `json:"channel_id"`
	Category  string    `json:"category"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// SetupMessage represents a ticket setup message
type SetupMessage struct {
	ChannelID string `json:"channel_id"`
	MessageID string `json:"message_id"`
}

// UserCountResponse represents the API response structure.
// The panel should return total users in count and, optionally, concurrent/active users in online_count.
type UserCountResponse struct {
	Success     bool `json:"success"`
	Count       int  `json:"count"`
	OnlineCount *int `json:"online_count,omitempty"`
}

