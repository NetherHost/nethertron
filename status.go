package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"time"
)

// fetchUserCount gets the user count from the API
func fetchUserCount() (*UserCountResponse, error) {
	req, err := http.NewRequest("GET", panelPath("/api/v3/users/count"), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-API-Key", APIKey)

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var userCount UserCountResponse
	err = json.Unmarshal(body, &userCount)
	if err != nil {
		return nil, err
	}

	return &userCount, nil
}

// updateBotStatus updates the bot's status with the given message
func updateBotStatus(status string) {
	err := discordSession.UpdateWatchStatus(0, status)
	if err != nil {
		log.Printf("%sError updating status: %v%s", ColorRed, err, ColorReset)
	}
}

// calculateVagueCount calculates the vague count string from total user count
// Rounds to the nearest 1,000 (no decimal), and adds a + sign
func calculateVagueCount(count int) string {
	rounded := int(math.Round(float64(count)/1000.0) * 1000)
	formatted := formatVagueTotalUsers(rounded)
	return formatted + "+"
}

// formatVagueTotalUsers formats a number as a plain integer with 'k' (no decimal), e.g. 123k
func formatVagueTotalUsers(count int) string {
	if count >= 1000000 {
		// This branch is for completeness (not likely for your use case)
		return fmt.Sprintf("%dM", count/1000000)
	} else if count >= 1000 {
		return fmt.Sprintf("%dk", count/1000)
	}
	return fmt.Sprintf("%d", count)
}

// formatOnlineCountForStatus rounds to nearest 100, and returns 100.4k style, always one decimal
func formatOnlineCountForStatus(count int) string {
	rounded := int(math.Round(float64(count)/100.0) * 100)
	if rounded >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(rounded)/1000000.0)
	} else if rounded >= 1000 {
		return fmt.Sprintf("%.1fk", float64(rounded)/1000.0)
	}
	return fmt.Sprintf("%d", rounded)
}

// cycleStatusPeriodically cycles between the two status messages every few seconds
func cycleStatusPeriodically() {
	// Fetch data immediately
	userCount, err := fetchUserCount()
	if err != nil {
		log.Printf("%sError fetching user count: %v%s", ColorRed, err, ColorReset)
		return
	}

	if !userCount.Success || userCount.Count == 0 {
		log.Printf("%sWarning: Invalid user count response%s", ColorYellow, ColorReset)
		return
	}

	totalUsers := userCount.Count
	vagueCount := calculateVagueCount(totalUsers)

	if userCount.OnlineCount == nil {
		log.Printf("%sStatus: response has no online_count; only the total line will show. Add online_count to GET /api/v3/users/count to alternate active users.%s\n",
			ColorYellow, ColorReset)
	}

	// Start with total users status
	showTotal := true
	updateBotStatus(fmt.Sprintf("🔥 Serving %s users", vagueCount))

	// Alternate only when the API supplies online_count (same endpoint, next poll updates the value)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	refreshTicker := time.NewTicker(5 * time.Minute)
	defer refreshTicker.Stop()

	for {
		select {
		case <-ticker.C:
			if userCount.OnlineCount == nil {
				continue
			}
			if showTotal {
				updateBotStatus(fmt.Sprintf("🔥 Serving %s users", vagueCount))
			} else {
				onlineStr := formatOnlineCountForStatus(*userCount.OnlineCount)
				updateBotStatus(fmt.Sprintf("🔥 %s active users", onlineStr))
			}
			showTotal = !showTotal

		case <-refreshTicker.C:
			newUserCount, err := fetchUserCount()
			if err != nil {
				log.Printf("%sError fetching user count: %v%s", ColorRed, err, ColorReset)
				continue
			}
			if newUserCount.Success && newUserCount.Count > 0 {
				userCount = newUserCount
				totalUsers = userCount.Count
				vagueCount = calculateVagueCount(totalUsers)
				fmt.Printf("%s✓ Refreshed user count data%s\n", ColorBlue, ColorReset)
			}
		}
	}
}

// updateStatusPeriodically updates the bot status every 5 minutes
func updateStatusPeriodically() {
	// Start cycling status
	go cycleStatusPeriodically()
}
