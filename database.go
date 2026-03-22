package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func initDatabase() (*sql.DB, error) {
	database, err := sql.Open("sqlite3", DatabaseFile+"?_foreign_keys=1")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %v", err)
	}

	// Create tables
	createTicketsTable := `
	CREATE TABLE IF NOT EXISTS tickets (
		user_id TEXT NOT NULL,
		channel_id TEXT PRIMARY KEY,
		category TEXT NOT NULL,
		status TEXT NOT NULL,
		created_at DATETIME NOT NULL
	);`

	createSetupMessagesTable := `
	CREATE TABLE IF NOT EXISTS setup_messages (
		channel_id TEXT PRIMARY KEY,
		message_id TEXT NOT NULL
	);`

	createDiscordAccountsTable := `
	CREATE TABLE IF NOT EXISTS discord_accounts (
		discord_id TEXT PRIMARY KEY,
		email TEXT NOT NULL,
		updated_at DATETIME NOT NULL
	);`

	createSettingsTable := `
	CREATE TABLE IF NOT EXISTS ticket_settings (
		guild_id TEXT PRIMARY KEY,
		require_email INTEGER NOT NULL DEFAULT 1
	);`

	createBlacklistTable := `
	CREATE TABLE IF NOT EXISTS ticket_blacklist (
		user_id TEXT PRIMARY KEY,
		blacklisted_at DATETIME NOT NULL
	);`

	createRatingsTable := `
	CREATE TABLE IF NOT EXISTS ticket_ratings (
		channel_id TEXT NOT NULL,
		ticket_user_id TEXT NOT NULL,
		rater_user_id TEXT NOT NULL,
		rating INTEGER NOT NULL,
		feedback TEXT,
		rated_at DATETIME NOT NULL,
		PRIMARY KEY (channel_id, rater_user_id)
	);`

	if _, err := database.Exec(createTicketsTable); err != nil {
		return nil, fmt.Errorf("failed to create tickets table: %v", err)
	}

	if _, err := database.Exec(createSetupMessagesTable); err != nil {
		return nil, fmt.Errorf("failed to create setup_messages table: %v", err)
	}

	if _, err := database.Exec(createDiscordAccountsTable); err != nil {
		return nil, fmt.Errorf("failed to create discord_accounts table: %v", err)
	}

	if _, err := database.Exec(createSettingsTable); err != nil {
		return nil, fmt.Errorf("failed to create ticket_settings table: %v", err)
	}

	if _, err := database.Exec(createBlacklistTable); err != nil {
		return nil, fmt.Errorf("failed to create ticket_blacklist table: %v", err)
	}

	if _, err := database.Exec(createRatingsTable); err != nil {
		return nil, fmt.Errorf("failed to create ticket_ratings table: %v", err)
	}

	// Migrate data from JSON if it exists
	jsonFile := "tickets.json"
	if _, err := os.Stat(jsonFile); err == nil {
		log.Printf("%sFound old JSON database, migrating...%s", ColorCyan, ColorReset)
		migrateFromJSON(database, jsonFile)
		log.Printf("%sMigration complete%s", ColorGreen, ColorReset)
	}

	return database, nil
}

func migrateFromJSON(db *sql.DB, jsonFile string) {
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		log.Printf("%sWarning: Could not read JSON file for migration: %v%s", ColorYellow, err, ColorReset)
		return
	}

	var oldDB struct {
		Tickets       []Ticket       `json:"tickets"`
		SetupMessages []SetupMessage `json:"setup_messages"`
	}

	if err := json.Unmarshal(data, &oldDB); err != nil {
		log.Printf("%sWarning: Could not parse JSON file: %v%s", ColorYellow, err, ColorReset)
		return
	}

	// Migrate tickets
	for _, ticket := range oldDB.Tickets {
		_, err := db.Exec(
			"INSERT OR IGNORE INTO tickets (user_id, channel_id, category, status, created_at) VALUES (?, ?, ?, ?, ?)",
			ticket.UserID, ticket.ChannelID, ticket.Category, ticket.Status, ticket.CreatedAt,
		)
		if err != nil {
			log.Printf("%sWarning: Could not migrate ticket %s: %v%s", ColorYellow, ticket.ChannelID, err, ColorReset)
		}
	}

	// Migrate setup messages
	for _, msg := range oldDB.SetupMessages {
		_, err := db.Exec(
			"INSERT OR IGNORE INTO setup_messages (channel_id, message_id) VALUES (?, ?)",
			msg.ChannelID, msg.MessageID,
		)
		if err != nil {
			log.Printf("%sWarning: Could not migrate setup message %s: %v%s", ColorYellow, msg.ChannelID, err, ColorReset)
		}
	}
}

func hasActiveTicket(userID string) (bool, error) {
	log.Printf("%sChecking database for active tickets...%s", ColorCyan, ColorReset)
	dbMutex.RLock()
	defer dbMutex.RUnlock()

	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM tickets WHERE user_id = ? AND status = ?", userID, "open").Scan(&count)
	if err != nil {
		log.Printf("%sERROR checking active tickets: %v%s", ColorRed, err, ColorReset)
		return false, err
	}

	if count > 0 {
		log.Printf("%sFound active ticket for user%s", ColorYellow, ColorReset)
		return true, nil
	}
	log.Printf("%sNo active ticket found%s", ColorCyan, ColorReset)
	return false, nil
}

func getActiveTicketChannelID(userID string) (string, error) {
	dbMutex.RLock()
	defer dbMutex.RUnlock()

	var channelID string
	err := db.QueryRow("SELECT channel_id FROM tickets WHERE user_id = ? AND status = ? LIMIT 1", userID, "open").Scan(&channelID)
	if err != nil {
		return "", fmt.Errorf("no active ticket found")
	}
	return channelID, nil
}

func hasOtherActiveTicket(userID string, excludeChannelID string) (bool, string, error) {
	dbMutex.RLock()
	defer dbMutex.RUnlock()

	var channelID string
	err := db.QueryRow("SELECT channel_id FROM tickets WHERE user_id = ? AND status = ? AND channel_id != ? LIMIT 1",
		userID, "open", excludeChannelID).Scan(&channelID)
	if err == sql.ErrNoRows {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	return true, channelID, nil
}

func createTicket(userID, channelID, category string) error {
	log.Printf("%screateTicket called: user=%s, channel=%s, category=%s%s", ColorCyan, userID, channelID, category, ColorReset)
	log.Printf("%sAcquiring database lock...%s", ColorCyan, ColorReset)
	dbMutex.Lock()
	defer dbMutex.Unlock()
	log.Printf("%sDatabase lock acquired%s", ColorCyan, ColorReset)

	_, err := db.Exec(
		"INSERT INTO tickets (user_id, channel_id, category, status, created_at) VALUES (?, ?, ?, ?, ?)",
		userID, channelID, category, "open", time.Now(),
	)
	if err != nil {
		log.Printf("%sERROR creating ticket: %v%s", ColorRed, err, ColorReset)
		return err
	}
	log.Printf("%sTicket created successfully%s", ColorGreen, ColorReset)
	return nil
}

func getTicketByChannel(channelID string) (string, string, string, error) {
	dbMutex.RLock()
	defer dbMutex.RUnlock()

	var userID, category, status string
	err := db.QueryRow("SELECT user_id, category, status FROM tickets WHERE channel_id = ?", channelID).
		Scan(&userID, &category, &status)
	if err != nil {
		return "", "", "", fmt.Errorf("ticket not found")
	}
	return userID, category, status, nil
}

func updateTicketStatus(channelID, status string) error {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	result, err := db.Exec("UPDATE tickets SET status = ? WHERE channel_id = ?", status, channelID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return fmt.Errorf("ticket not found")
	}
	return nil
}

func deleteTicket(channelID string) error {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	result, err := db.Exec("DELETE FROM tickets WHERE channel_id = ?", channelID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return fmt.Errorf("ticket not found")
	}
	return nil
}

func saveSetupMessage(channelID, messageID string) error {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	_, err := db.Exec(
		"INSERT OR REPLACE INTO setup_messages (channel_id, message_id) VALUES (?, ?)",
		channelID, messageID,
	)
	return err
}

// Save or update Discord account email mapping
func saveDiscordAccountEmail(discordID, email string) error {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	_, err := db.Exec(
		"INSERT OR REPLACE INTO discord_accounts (discord_id, email, updated_at) VALUES (?, ?, ?)",
		discordID, email, time.Now(),
	)
	return err
}

// Get email by Discord ID
func getEmailByDiscordID(discordID string) (string, error) {
	dbMutex.RLock()
	defer dbMutex.RUnlock()

	var email string
	err := db.QueryRow("SELECT email FROM discord_accounts WHERE discord_id = ?", discordID).Scan(&email)
	if err == sql.ErrNoRows {
		return "", nil // Return empty string if not found, not an error
	}
	if err != nil {
		return "", err
	}
	return email, nil
}

// Delete Discord account email mapping
func deleteDiscordAccountEmail(discordID string) error {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	_, err := db.Exec("DELETE FROM discord_accounts WHERE discord_id = ?", discordID)
	return err
}

// Get ticket setting for a guild
func getTicketSetting(guildID string, settingName string) (bool, error) {
	dbMutex.RLock()
	defer dbMutex.RUnlock()

	var value int
	err := db.QueryRow("SELECT "+settingName+" FROM ticket_settings WHERE guild_id = ?", guildID).Scan(&value)
	if err == sql.ErrNoRows {
		// Return default value (require_email defaults to true)
		if settingName == "require_email" {
			return true, nil
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value == 1, nil
}

// Set ticket setting for a guild
func setTicketSetting(guildID string, settingName string, value bool) error {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	intValue := 0
	if value {
		intValue = 1
	}

	_, err := db.Exec(
		"INSERT OR REPLACE INTO ticket_settings (guild_id, "+settingName+") VALUES (?, ?)",
		guildID, intValue,
	)
	return err
}

// Add user to blacklist
func addToBlacklist(userID string) error {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	_, err := db.Exec(
		"INSERT OR REPLACE INTO ticket_blacklist (user_id, blacklisted_at) VALUES (?, ?)",
		userID, time.Now(),
	)
	return err
}

// Remove user from blacklist
func removeFromBlacklist(userID string) error {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	_, err := db.Exec("DELETE FROM ticket_blacklist WHERE user_id = ?", userID)
	return err
}

// Check if user is blacklisted
func isBlacklisted(userID string) (bool, error) {
	dbMutex.RLock()
	defer dbMutex.RUnlock()

	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM ticket_blacklist WHERE user_id = ?", userID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// Save ticket rating
func saveTicketRating(channelID, ticketUserID, raterUserID string, rating int, feedback string) error {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	_, err := db.Exec(
		"INSERT OR REPLACE INTO ticket_ratings (channel_id, ticket_user_id, rater_user_id, rating, feedback, rated_at) VALUES (?, ?, ?, ?, ?, ?)",
		channelID, ticketUserID, raterUserID, rating, feedback, time.Now(),
	)
	return err
}

// Check if user has already rated a ticket
func hasRatedTicket(channelID, userID string) (bool, error) {
	dbMutex.RLock()
	defer dbMutex.RUnlock()

	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM ticket_ratings WHERE channel_id = ? AND rater_user_id = ?", channelID, userID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// Get average rating and count
func getRatingStats() (float64, int, error) {
	dbMutex.RLock()
	defer dbMutex.RUnlock()

	var avgRating sql.NullFloat64
	var count int

	err := db.QueryRow("SELECT AVG(rating), COUNT(*) FROM ticket_ratings").Scan(&avgRating, &count)
	if err != nil {
		return 0, 0, err
	}

	avg := 0.0
	if avgRating.Valid {
		avg = avgRating.Float64
	}

	return avg, count, nil
}

// Rating represents a ticket rating
type Rating struct {
	ChannelID   string
	TicketUserID string
	RaterUserID string
	Rating      int
	Feedback    string
	RatedAt     time.Time
}

// GetAllRatings retrieves all ticket ratings from the database
func getAllRatings() ([]Rating, error) {
	dbMutex.RLock()
	defer dbMutex.RUnlock()

	rows, err := db.Query("SELECT channel_id, ticket_user_id, rater_user_id, rating, feedback, rated_at FROM ticket_ratings ORDER BY rated_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ratings []Rating
	for rows.Next() {
		var r Rating
		var feedback sql.NullString
		err := rows.Scan(&r.ChannelID, &r.TicketUserID, &r.RaterUserID, &r.Rating, &feedback, &r.RatedAt)
		if err != nil {
			return nil, err
		}
		if feedback.Valid {
			r.Feedback = feedback.String
		}
		ratings = append(ratings, r)
	}

	return ratings, rows.Err()
}