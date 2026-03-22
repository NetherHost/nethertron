package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"
)

// API request/response structures
type DiscordAccountUpdateRequest struct {
	DiscordID string `json:"discord_id"`
	Email     string `json:"email"`
	Servers   int    `json:"servers"` // Number of active servers
	Action    string `json:"action"`  // "link" or "unlink"
}

type APIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// Start API server to receive updates from Laravel
func startAPIServer() {
	http.HandleFunc("/api/discord/account", handleDiscordAccountUpdate)
	http.HandleFunc("/health", handleHealthCheck)

	port := APIServerPort
	address := "0.0.0.0:" + port
	log.Printf("%sStarting API server on %s (accessible via localhost:%s, 127.0.0.1:%s, and all network interfaces)...%s\n", 
		ColorCyan, address, port, port, ColorReset)
	
	go func() {
		if err := http.ListenAndServe(address, nil); err != nil {
			log.Printf("%sERROR starting API server: %v%s\n", ColorRed, err, ColorReset)
		}
	}()
	
	// Give the server a moment to start, then log success
	go func() {
		time.Sleep(100 * time.Millisecond)
		log.Printf("%s✓ API server started and listening on %s%s\n", ColorGreen, address, ColorReset)
		log.Printf("%s  - Try: curl http://localhost:%s/health%s\n", ColorCyan, port, ColorReset)
	}()
}

// getKeyPreview returns a preview of the API key (first 8 chars + "...") for logging
func getKeyPreview(key string) string {
	if key == "" {
		return "(empty)"
	}
	if len(key) <= 12 {
		return key
	}
	return key[:8] + "..."
}

// getGuildID gets the guild ID from the Discord session state
// Returns the first guild ID the bot is in, or the constant GuildID as fallback
func getGuildID() string {
	if discordSession == nil {
		return GuildID // Fallback to constant
	}

	// Try to get guilds from session state
	guilds := discordSession.State.Guilds
	if len(guilds) > 0 {
		// Return the first guild ID (most bots are only in one guild)
		guildID := guilds[0].ID
		log.Printf("%sUsing guild ID from session state: %s%s\n", ColorCyan, guildID, ColorReset)
		return guildID
	}

	// Fallback to constant if state is empty
	log.Printf("%sSession state empty, using constant guild ID: %s%s\n", ColorCyan, GuildID, ColorReset)
	return GuildID
}

// Handle Discord account update from Laravel
func handleDiscordAccountUpdate(w http.ResponseWriter, r *http.Request) {
	// Log all incoming requests for debugging
	log.Printf("%sIncoming API request: %s %s from %s%s\n", ColorCyan, r.Method, r.URL.Path, r.RemoteAddr, ColorReset)
	
	// Only allow POST requests
	if r.Method != http.MethodPost {
		log.Printf("%sMethod not allowed: %s (expected POST)%s\n", ColorYellow, r.Method, ColorReset)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify API key
	apiKey := r.Header.Get("X-API-Key")
	if apiKey != APIKey {
		log.Printf("%sUnauthorized API request from %s (API key mismatch - provided: %s, expected: %s)%s\n", 
			ColorRed, r.RemoteAddr, 
			getKeyPreview(apiKey), getKeyPreview(APIKey), 
			ColorReset)
		respondJSON(w, http.StatusUnauthorized, APIResponse{
			Success: false,
			Message: "Invalid API key",
		})
		return
	}
	log.Printf("%sAPI key verified successfully%s\n", ColorGreen, ColorReset)

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("%sERROR reading request body: %v%s\n", ColorRed, err, ColorReset)
		respondJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "Failed to read request body",
		})
		return
	}
	defer r.Body.Close()

	// Parse JSON
	var req DiscordAccountUpdateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Printf("%sERROR parsing JSON: %v%s\n", ColorRed, err, ColorReset)
		respondJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "Invalid JSON format",
		})
		return
	}

	// Validate required fields
	if req.DiscordID == "" {
		respondJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "discord_id is required",
		})
		return
	}

	// Handle link or unlink action
	if req.Action == "unlink" {
		// Delete the mapping
		if err := deleteDiscordAccountEmail(req.DiscordID); err != nil {
			log.Printf("%sERROR deleting Discord account: %v%s\n", ColorRed, err, ColorReset)
			respondJSON(w, http.StatusInternalServerError, APIResponse{
				Success: false,
				Message: "Failed to unlink Discord account",
			})
			return
		}
		log.Printf("%sDiscord account unlinked: %s%s\n", ColorGreen, req.DiscordID, ColorReset)
		respondJSON(w, http.StatusOK, APIResponse{
			Success: true,
			Message: "Discord account unlinked successfully",
		})
		return
	}

	// Default to "link" action
	if req.Email == "" {
		respondJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "email is required for link action",
		})
		return
	}

	// Save or update the mapping
	if err := saveDiscordAccountEmail(req.DiscordID, req.Email); err != nil {
		log.Printf("%sERROR saving Discord account: %v%s\n", ColorRed, err, ColorReset)
		respondJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Failed to link Discord account",
		})
		return
	}

	log.Printf("%sDiscord account linked: %s -> %s (servers: %d)%s\n", ColorGreen, req.DiscordID, req.Email, req.Servers, ColorReset)

	// If user has active servers, add the active servers role
	if req.Servers > 0 && discordSession != nil {
		// Try to get the guild ID from session state, or use the constant
		guildID := getGuildID()
		if guildID == "" {
			log.Printf("%sWARNING: Could not determine guild ID%s\n", ColorYellow, ColorReset)
		} else {
			err := discordSession.GuildMemberRoleAdd(guildID, req.DiscordID, ActiveServersRoleID)
			if err != nil {
				log.Printf("%sWARNING: Failed to add active servers role to user %s in guild %s: %v%s\n", ColorYellow, req.DiscordID, guildID, err, ColorReset)
				// Don't fail the request if role assignment fails
			} else {
				log.Printf("%s✓ Added active servers role to user %s (has %d active servers)%s\n", ColorGreen, req.DiscordID, req.Servers, ColorReset)
			}
		}
	} else if req.Servers > 0 {
		log.Printf("%sWARNING: Cannot add role - Discord session not available%s\n", ColorYellow, ColorReset)
	}

	respondJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Discord account linked successfully",
	})
}

// Handle health check endpoint
func handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	log.Printf("%sHealth check request from %s%s\n", ColorCyan, r.RemoteAddr, ColorReset)
	respondJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "API server is running",
	})
}

// Helper function to send JSON response
func respondJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
