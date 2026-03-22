package main

import (
	"fmt"
	"os"
	"strings"
)

// Secrets and external URLs — set via environment (see README and .env.example).
var (
	BotToken     string
	APIKey       string
	PanelBaseURL string
)

// loadSecretsAndURLs reads required configuration from the environment.
func loadSecretsAndURLs() error {
	BotToken = strings.TrimSpace(os.Getenv("DISCORD_BOT_TOKEN"))
	if BotToken == "" {
		return fmt.Errorf("DISCORD_BOT_TOKEN is required")
	}
	APIKey = strings.TrimSpace(os.Getenv("DISCORD_BOT_API_KEY"))
	if APIKey == "" {
		return fmt.Errorf("DISCORD_BOT_API_KEY is required (shared secret for the HTTP API, e.g. from your Laravel app)")
	}
	PanelBaseURL = strings.TrimRight(strings.TrimSpace(os.Getenv("PANEL_BASE_URL")), "/")
	if PanelBaseURL == "" {
		return fmt.Errorf("PANEL_BASE_URL is required (e.g. https://panel.example.com, no trailing slash)")
	}
	return nil
}

// panelPath joins the configured panel origin with a path that starts with "/".
func panelPath(path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return PanelBaseURL + path
}

// Ticket system constants — replace with your Discord server's IDs before running.
var (
	TicketSetupRoleIDs = []string{"000000000000000001", "000000000000000002"}
)

const (
	OpenTicketCategoryID   = "000000000000000003"
	ClosedTicketCategoryID = "000000000000000004"
	DatabaseFile           = "tickets.db"
	APIServerPort          = "8080" // Port for the API server to receive updates from Laravel
	AutoRoleID             = "000000000000000005" // Role to assign to new members
	LinkAccountEmojiID     = ""                   // Custom emoji ID for link account icon (set this to your emoji ID)
	RatingChannelID        = "000000000000000006" // Channel ID for rating notifications
	StaffRoleID1           = "000000000000000007" // First staff role ID
	StaffRoleID2           = "000000000000000008" // Second staff role ID
	GuildID                = "000000000000000009" // Discord server/guild ID
	ActiveServersRoleID    = "000000000000000010" // Role to assign to users with active servers
)

// ANSI color codes for terminal output
const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
	ColorCyan   = "\033[36m"
)
