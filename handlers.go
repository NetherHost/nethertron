package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// getAccountEmailForTicket retrieves the account email for a Discord user
// Returns "Not linked" if no email is found
func getAccountEmailForTicket(discordUserID string) string {
	email, err := getEmailByDiscordID(discordUserID)
	if err != nil {
		log.Printf("%sERROR getting email for Discord ID %s: %v%s", ColorYellow, discordUserID, err, ColorReset)
		return "Not linked"
	}
	if email == "" {
		return "Not linked"
	}
	return email
}

// Ticket command handler
func handleTicketCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Invalid command usage.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	subcommand := options[0].Name

	// For "add" and "remove" subcommands, verify it's a ticket channel and check permissions
	if subcommand == "add" || subcommand == "remove" {
		// Verify this is a ticket channel
		ticketUserID, _, _, err := getTicketByChannel(i.ChannelID)
		if err != nil {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "This command can only be used in a ticket channel.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		// Check if user is the ticket owner or has Manage Events permission
		requesterID := i.Member.User.ID
		isTicketOwner := ticketUserID == requesterID
		hasManageEvents := canManageEventsFromInteraction(s, i)

		if !isTicketOwner && !hasManageEvents {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "You need to be the ticket owner to add/remove users.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		// Handle add subcommand
		if subcommand == "add" {
			if len(options[0].Options) == 0 {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: "Missing 'user' parameter.",
						Flags:   discordgo.MessageFlagsEphemeral,
					},
				})
				return
			}

			targetUser := options[0].Options[0].UserValue(s)
			targetUserID := targetUser.ID

			// Check if user already has access
			channel, err := s.Channel(i.ChannelID)
			if err != nil {
				log.Printf("%sError getting channel: %v%s", ColorRed, err, ColorReset)
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: "An error occurred. Please try again.",
						Flags:   discordgo.MessageFlagsEphemeral,
					},
				})
				return
			}

			// Check if user already has permission overwrite
			hasAccess := false
			for _, overwrite := range channel.PermissionOverwrites {
				if overwrite.ID == targetUserID && overwrite.Type == discordgo.PermissionOverwriteTypeMember {
					// Check if they have view channel permission
					if (overwrite.Allow & discordgo.PermissionViewChannel) != 0 {
						hasAccess = true
					}
					break
				}
			}

			if hasAccess {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: fmt.Sprintf("%s already has access to this ticket.", targetUser.Mention()),
						Flags:   discordgo.MessageFlagsEphemeral,
					},
				})
				return
			}

			// Add user to ticket by setting permissions
			err = s.ChannelPermissionSet(i.ChannelID, targetUserID, discordgo.PermissionOverwriteTypeMember,
				discordgo.PermissionViewChannel|discordgo.PermissionSendMessages|discordgo.PermissionReadMessageHistory,
				0)
			if err != nil {
				log.Printf("%sError adding user to ticket: %v%s", ColorRed, err, ColorReset)
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: "Failed to add user to ticket. Please try again.",
						Flags:   discordgo.MessageFlagsEphemeral,
					},
				})
				return
			}

			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: fmt.Sprintf("✅ %s has been added to this ticket.", targetUser.Mention()),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		// Handle remove subcommand
		if subcommand == "remove" {
			if len(options[0].Options) == 0 {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: "Missing 'user' parameter.",
						Flags:   discordgo.MessageFlagsEphemeral,
					},
				})
				return
			}

			targetUser := options[0].Options[0].UserValue(s)
			targetUserID := targetUser.ID
			requesterID := i.Member.User.ID

			// Prevent users from removing themselves
			if targetUserID == requesterID {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: "You cannot remove yourself from the ticket.",
						Flags:   discordgo.MessageFlagsEphemeral,
					},
				})
				return
			}

			// Get ticket info to check if target is the ticket owner
			ticketUserID, _, _, err := getTicketByChannel(i.ChannelID)
			if err != nil {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: "An error occurred. Please try again.",
						Flags:   discordgo.MessageFlagsEphemeral,
					},
				})
				return
			}

			// Prevent removing the ticket owner
			if targetUserID == ticketUserID {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: "You cannot remove the ticket owner from the ticket.",
						Flags:   discordgo.MessageFlagsEphemeral,
					},
				})
				return
			}

			// Prevent removing users with Administrator permission
			// Check if target user has Administrator permission at guild level
			guild, err := s.Guild(i.GuildID)
			if err == nil {
				// Check if target is guild owner
				if guild.OwnerID == targetUserID {
					s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
						Type: discordgo.InteractionResponseChannelMessageWithSource,
						Data: &discordgo.InteractionResponseData{
							Content: "You cannot remove the server owner from the ticket.",
							Flags:   discordgo.MessageFlagsEphemeral,
						},
					})
					return
				}

				// Get target member to check roles
				targetMember, err := s.GuildMember(i.GuildID, targetUserID)
				if err == nil {
					// Calculate permissions from roles
					var permissions int64
					for _, roleID := range targetMember.Roles {
						for _, role := range guild.Roles {
							if role.ID == roleID {
								permissions |= role.Permissions
								break
							}
						}
					}

					// Check for @everyone role permissions
					for _, role := range guild.Roles {
						if role.ID == i.GuildID {
							permissions |= role.Permissions
							break
						}
					}

					// Check if target has Administrator permission
					if permissions&discordgo.PermissionAdministrator != 0 {
						s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
							Type: discordgo.InteractionResponseChannelMessageWithSource,
							Data: &discordgo.InteractionResponseData{
								Content: "You cannot remove users with Administrator permission from the ticket.",
								Flags:   discordgo.MessageFlagsEphemeral,
							},
						})
						return
					}
				}
			}

			// Check if user has access
			channel, err := s.Channel(i.ChannelID)
			if err != nil {
				log.Printf("%sError getting channel: %v%s", ColorRed, err, ColorReset)
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: "An error occurred. Please try again.",
						Flags:   discordgo.MessageFlagsEphemeral,
					},
				})
				return
			}

			// Check if user has permission overwrite
			hasAccess := false
			for _, overwrite := range channel.PermissionOverwrites {
				if overwrite.ID == targetUserID && overwrite.Type == discordgo.PermissionOverwriteTypeMember {
					if (overwrite.Allow & discordgo.PermissionViewChannel) != 0 {
						hasAccess = true
					}
					break
				}
			}

			if !hasAccess {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: fmt.Sprintf("%s does not have access to this ticket.", targetUser.Mention()),
						Flags:   discordgo.MessageFlagsEphemeral,
					},
				})
				return
			}

			// Remove user from ticket by denying all permissions
			err = s.ChannelPermissionSet(i.ChannelID, targetUserID, discordgo.PermissionOverwriteTypeMember,
				0,
				discordgo.PermissionViewChannel|discordgo.PermissionSendMessages|discordgo.PermissionReadMessageHistory)
			if err != nil {
				log.Printf("%sError removing user from ticket: %v%s", ColorRed, err, ColorReset)
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: "Failed to remove user from ticket. Please try again.",
						Flags:   discordgo.MessageFlagsEphemeral,
					},
				})
				return
			}

			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: fmt.Sprintf("✅ %s has been removed from this ticket.", targetUser.Mention()),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}
	}
}

// Admin command handler
func handleAdminCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Invalid command usage.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	// options[0] is the subcommand group "tickets"
	if options[0].Name != "tickets" {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Invalid command usage.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	if len(options[0].Options) == 0 {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Invalid command usage.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	// Get the subcommand name
	var subcommand string
	if len(options[0].Options) > 0 {
		subcommand = options[0].Options[0].Name
	}

	if subcommand == "setup" {
		// Create embed
		embed := &discordgo.MessageEmbed{
			Title:       "Open Support Ticket",
			Description: "Click the **Open Ticket** button below to open a support request. A private channel will be created so you can chat with the Nether Host support team.\n\nYou **must** have your account linked through our website in order to open a ticket. Click the **Link Discord** button below to link it.",
			Color:       0xFF4444,
		}

		// Respond first
		err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Ticket system setup complete!",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})

		if err != nil {
			log.Printf("%sError responding to interaction: %v%s", ColorRed, err, ColorReset)
			return
		}

		// Send the setup message with button using REST API directly to avoid emoji issues
		buttonPayload := map[string]interface{}{
			"embeds": []map[string]interface{}{
				{
					"title":       embed.Title,
					"description": embed.Description,
					"color":       embed.Color,
				},
			},
			"components": []map[string]interface{}{
				{
					"type": 1,
					"components": []map[string]interface{}{
						{
							"type":     2,
							"style":    2, // Secondary button style (gray) - matches reopen button
							"label":    "Open Ticket",
							"custom_id": "open_ticket",
						},
						{
							"type":  2,
							"style": 5, // Link button style
							"label": "Link Account",
							"url":   panelPath("/dashboard"),
						},
					},
				},
			},
		}

		payloadJSON, _ := json.Marshal(buttonPayload)
		url := fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages", i.ChannelID)

		req, _ := http.NewRequest("POST", url, strings.NewReader(string(payloadJSON)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bot "+BotToken)

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("%sError sending setup message via REST: %v%s", ColorRed, err, ColorReset)
			return
		}
		defer resp.Body.Close()

		// Parse response to get message ID
		var msgResp map[string]interface{}
		body, _ := io.ReadAll(resp.Body)
		json.Unmarshal(body, &msgResp)

		if msgID, ok := msgResp["id"].(string); ok {
			saveSetupMessage(i.ChannelID, msgID)
		}

		// Get the response message to save it
		// Wait a moment for the message to be created
		time.Sleep(500 * time.Millisecond)

		// Try to get the message - we'll use the channel's last message
		messages, err := s.ChannelMessages(i.ChannelID, 1, "", "", "")
		if err == nil && len(messages) > 0 {
			saveSetupMessage(i.ChannelID, messages[0].ID)
		}
	} else if subcommand == "require_email" {
		// Get the enabled option value
		if len(options[0].Options[0].Options) == 0 {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "Missing 'enabled' parameter.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		enabled := options[0].Options[0].Options[0].BoolValue()

		// Save the setting
		err := setTicketSetting(i.GuildID, "require_email", enabled)
		if err != nil {
			log.Printf("%sERROR saving ticket setting: %v%s", ColorRed, err, ColorReset)
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "Failed to save setting. Please try again.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		status := "enabled"
		if !enabled {
			status = "disabled"
		}

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("✅ Account email requirement has been **%s**.", status),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		log.Printf("%sTicket setting updated: require_email = %v for guild %s%s", ColorGreen, enabled, i.GuildID, ColorReset)
	} else if subcommand == "blacklist" {
		// Handle blacklist subcommand
		if len(options[0].Options[0].Options) == 0 {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "Missing 'user' parameter.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		targetUserID := options[0].Options[0].Options[0].UserValue(s).ID
		targetUser := options[0].Options[0].Options[0].UserValue(s)

		// Check if user is already blacklisted
		alreadyBlacklisted, err := isBlacklisted(targetUserID)
		if err != nil {
			log.Printf("%sError checking blacklist: %v%s", ColorRed, err, ColorReset)
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "An error occurred while checking the blacklist.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		if alreadyBlacklisted {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: fmt.Sprintf("%s is already blacklisted from opening tickets.", targetUser.Mention()),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		// Add user to blacklist
		err = addToBlacklist(targetUserID)
		if err != nil {
			log.Printf("%sError adding to blacklist: %v%s", ColorRed, err, ColorReset)
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "An error occurred while adding the user to the blacklist.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("%s has been blacklisted from opening tickets.", targetUser.Mention()),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
	} else if subcommand == "set_email" {
		// Handle set_email subcommand
		if len(options[0].Options[0].Options) < 2 {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "Missing required parameters. Usage: /admin tickets set_email [user] [email]",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		targetUserID := options[0].Options[0].Options[0].UserValue(s).ID
		targetUser := options[0].Options[0].Options[0].UserValue(s)
		email := options[0].Options[0].Options[1].StringValue()

		// Basic email validation
		if !strings.Contains(email, "@") || !strings.Contains(email, ".") {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "Invalid email format. Please provide a valid email address.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		// Save the email
		err := saveDiscordAccountEmail(targetUserID, email)
		if err != nil {
			log.Printf("%sError saving email: %v%s", ColorRed, err, ColorReset)
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "An error occurred while setting the email. Please try again.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("✅ Successfully set email for %s to `%s`", targetUser.Mention(), email),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		log.Printf("%sEmail manually set for user %s (%s): %s%s", ColorGreen, targetUser.Username, targetUserID, email, ColorReset)
	} else if subcommand == "ratings_stats" {
		// Check Manage Events permission
		if !canManageEventsFromInteraction(s, i) {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "You don't have permission to use this command.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		// Get rating statistics
		avgRating, count, err := getRatingStats()
		if err != nil {
			log.Printf("%sERROR getting rating stats: %v%s", ColorRed, err, ColorReset)
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "An error occurred while fetching rating statistics. Please try again.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		// Format response
		var message string
		if count == 0 {
			message = "**Ticket Rating Statistics**\n\nNo ratings have been submitted yet."
		} else {
			stars := strings.Repeat("⭐", int(avgRating+0.5)) // Round to nearest integer for star display
			message = fmt.Sprintf("**Ticket Rating Statistics**\n\n**Average Rating:** %.2f/5 %s\n**Total Ratings:** %d", avgRating, stars, count)
		}

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: message,
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
	} else if subcommand == "ratings_show" {
		// Check Manage Events permission
		if !canManageEventsFromInteraction(s, i) {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "You don't have permission to use this command.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		// Handle show command - export all ratings
		handleRatingsShow(s, i)
	}
}

// Handle component interactions (buttons and select menus)
func handleComponentInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	log.Printf("%s=== Component interaction received ===%s", ColorCyan, ColorReset)
	log.Printf("%sInteraction Type: %d%s", ColorCyan, i.Type, ColorReset)
	log.Printf("%sComponent Type: %d%s", ColorCyan, i.MessageComponentData().ComponentType, ColorReset)

	customID := i.MessageComponentData().CustomID
	log.Printf("%sCustom ID: %s%s", ColorCyan, customID, ColorReset)

	// Check if it's a select menu
	if i.MessageComponentData().ComponentType == discordgo.SelectMenuComponent {
		log.Printf("%sThis is a SELECT MENU interaction%s", ColorYellow, ColorReset)
		if len(i.MessageComponentData().Values) > 0 {
			log.Printf("%sSelected value: %s%s", ColorCyan, i.MessageComponentData().Values[0], ColorReset)
		}
	}

	switch customID {
	case "open_ticket":
		log.Printf("%sRouting to open_ticket handler%s", ColorCyan, ColorReset)
		handleOpenTicketButton(s, i)
	case "ticket_category_select":
		log.Printf("%sRouting to ticket_category_select handler%s", ColorCyan, ColorReset)
		handleTicketCategorySelect(s, i)
	case "close_ticket":
		log.Printf("%sRouting to close_ticket handler%s", ColorCyan, ColorReset)
		handleCloseTicketButton(s, i)
	case "delete_ticket":
		log.Printf("%sRouting to delete_ticket handler%s", ColorCyan, ColorReset)
		handleDeleteTicketButton(s, i)
	case "reopen_ticket":
		log.Printf("%sRouting to reopen_ticket handler%s", ColorCyan, ColorReset)
		handleReopenTicketButton(s, i)
	case "transcript_ticket":
		log.Printf("%sRouting to transcript_ticket handler%s", ColorCyan, ColorReset)
		handleTranscriptButton(s, i)
	case "rate_ticket":
		log.Printf("%sRouting to rate_ticket handler%s", ColorCyan, ColorReset)
		handleRateTicketButton(s, i)
	default:
		log.Printf("%sWARNING: Unknown custom ID: %s%s", ColorRed, customID, ColorReset)
		log.Printf("%sAvailable values: %v%s", ColorYellow, i.MessageComponentData().Values, ColorReset)
	}
}

// Handle open ticket button
func handleOpenTicketButton(s *discordgo.Session, i *discordgo.InteractionCreate) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("%sPANIC in handleOpenTicketButton: %v%s", ColorRed, r, ColorReset)
		}
	}()

	log.Printf("%s=== Open ticket button handler START ===%s", ColorCyan, ColorReset)
	log.Printf("%sOpen ticket button clicked by user %s%s", ColorCyan, i.Member.User.ID, ColorReset)

	if db == nil {
		log.Printf("%sERROR: database is nil!%s", ColorRed, ColorReset)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Database error. Please contact support.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	userID := i.Member.User.ID
	log.Printf("%sChecking for existing tickets for user %s...%s", ColorCyan, userID, ColorReset)

	// Check if user is blacklisted
	isBlacklistedUser, err := isBlacklisted(userID)
	if err != nil {
		log.Printf("%sERROR checking blacklist: %v%s", ColorRed, err, ColorReset)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "An error occurred. Please try again.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}
	if isBlacklistedUser {
		log.Printf("%sUser %s is blacklisted, blocking ticket creation%s", ColorYellow, userID, ColorReset)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "You can't open a ticket at this time.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	// Check if user already has an open ticket
	hasTicket, err := hasActiveTicket(userID)
	if err != nil {
		log.Printf("%sERROR checking ticket: %v%s", ColorRed, err, ColorReset)
		respErr := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "An error occurred. Please try again.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		if respErr != nil {
			log.Printf("%sERROR responding to interaction: %v%s", ColorRed, respErr, ColorReset)
		}
		return
	}
	log.Printf("%sTicket check complete. Has ticket: %v%s", ColorCyan, hasTicket, ColorReset)

	if hasTicket {
		log.Printf("%sUser already has a ticket, sending message...%s", ColorYellow, ColorReset)
		channelID, err := getActiveTicketChannelID(userID)
		var message string
		if err != nil {
			message = "You already have an open ticket. Please close it before creating a new one."
		} else {
			message = fmt.Sprintf("You already have an open ticket in <#%s>", channelID)
		}
		respErr := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: message,
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		if respErr != nil {
			log.Printf("%sERROR responding: %v%s", ColorRed, respErr, ColorReset)
		}
		return
	}

	// Check if email is required (based on guild setting)
	requireEmail, err := getTicketSetting(i.GuildID, "require_email")
	if err != nil {
		log.Printf("%sERROR checking ticket setting: %v%s", ColorRed, err, ColorReset)
		// Default to requiring email if there's an error
		requireEmail = true
	}

	// Check if user has their account email linked (only if required)
	if requireEmail {
		email, err := getEmailByDiscordID(userID)
		if err != nil {
			log.Printf("%sERROR checking email: %v%s", ColorRed, err, ColorReset)
		}
		if email == "" {
			log.Printf("%sUser does not have account linked, blocking ticket creation%s", ColorYellow, ColorReset)
			linkURL := panelPath("/dashboard")
			message := fmt.Sprintf("You must link your account before opening a ticket.\n\n[Link your account here](%s)", linkURL)
			respErr := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: message,
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			if respErr != nil {
				log.Printf("%sERROR responding: %v%s", ColorRed, respErr, ColorReset)
			}
			return
		}
	}

	log.Printf("%sNo existing ticket, creating select menu...%s", ColorCyan, ColorReset)

	// Create select menu manually using REST API to avoid emoji issues
	selectMenuPayload := map[string]interface{}{
		"type": 4, // CHANNEL_MESSAGE_WITH_SOURCE
		"data": map[string]interface{}{
			"content": "Please select a category for your ticket:",
			"components": []map[string]interface{}{
				{
					"type": 1, // Action row
					"components": []map[string]interface{}{
						{
							"type":        3, // Select menu type
							"custom_id":   "ticket_category_select",
							"placeholder": "Select a category",
							"options": []map[string]interface{}{
								{
									"label":       "General Support",
									"value":       "general support",
									"description": "General questions and support requests",
								},
								{
									"label":       "Billing Issues",
									"value":       "billing issues",
									"description": "Payment, refunds, and billing questions",
								},
								{
									"label":       "Minecraft Help",
									"value":       "minecraft help",
									"description": "Minecraft server setup and configuration help",
								},
								{
									"label":       "Claim Dedicated IP",
									"value":       "claim dedicated ip",
									"description": "Get your dedicated IP address that you purchased",
								},
								{
									"label":       "Other Support",
									"value":       "other support",
									"description": "Other support requests not covered above",
								},
							},
						},
					},
				},
			},
			"flags": 64, // Ephemeral flag
		},
	}

	log.Printf("%sMarshaling select menu payload...%s", ColorCyan, ColorReset)
	payloadJSON, err := json.Marshal(selectMenuPayload)
	if err != nil {
		log.Printf("%sERROR marshaling payload: %v%s", ColorRed, err, ColorReset)
		// Fallback: try using discordgo's method
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "An error occurred. Please try again.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	log.Printf("%sPayload marshaled, sending to Discord API...%s", ColorCyan, ColorReset)
	url := fmt.Sprintf("https://discord.com/api/v10/interactions/%s/%s/callback", i.Interaction.ID, i.Interaction.Token)
	log.Printf("%sURL: %s%s", ColorCyan, url, ColorReset)

	req, err := http.NewRequest("POST", url, strings.NewReader(string(payloadJSON)))
	if err != nil {
		log.Printf("%sERROR creating HTTP request: %v%s", ColorRed, err, ColorReset)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "An error occurred. Please try again.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+BotToken)

	client := &http.Client{Timeout: 10 * time.Second}
	log.Printf("%sSending HTTP request...%s", ColorCyan, ColorReset)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("%sERROR sending HTTP request: %v%s", ColorRed, err, ColorReset)
		// Fallback response
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "An error occurred. Please try again.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		log.Printf("%sERROR from Discord API: Status %d, Body: %s%s", ColorRed, resp.StatusCode, string(body), ColorReset)
		// Try to send error message to user
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "Failed to show category selection. Please try again.",
		})
	} else {
		log.Printf("%sSelect menu sent successfully (Status: %d)%s", ColorGreen, resp.StatusCode, ColorReset)
	}
}

// Handle ticket category selection
func handleTicketCategorySelect(s *discordgo.Session, i *discordgo.InteractionCreate) {
	log.Printf("%s=== Category selection handler called ===%s", ColorCyan, ColorReset)

	// Respond immediately with deferred response to prevent timeout
	log.Printf("%sSending deferred response...%s", ColorCyan, ColorReset)
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Printf("%sERROR sending deferred response: %v%s", ColorRed, err, ColorReset)
		return
	}
	log.Printf("%sDeferred response sent successfully%s", ColorGreen, ColorReset)

	category := i.MessageComponentData().Values[0]
	log.Printf("%sSelected category: %s%s", ColorCyan, category, ColorReset)

	userID := i.Member.User.ID
	guildID := i.GuildID
	log.Printf("%sUser ID: %s, Guild ID: %s%s", ColorCyan, userID, guildID, ColorReset)

	// Check if user is blacklisted
	isBlacklistedUser, err := isBlacklisted(userID)
	if err != nil {
		log.Printf("%sERROR checking blacklist: %v%s", ColorRed, err, ColorReset)
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "An error occurred. Please try again.",
		})
		return
	}
	if isBlacklistedUser {
		log.Printf("%sUser %s is blacklisted, blocking ticket creation%s", ColorYellow, userID, ColorReset)
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "You can't open a ticket at this time.",
		})
		return
	}

	// Check if user already has an open ticket
	hasTicket, err := hasActiveTicket(userID)
	if err != nil {
		log.Printf("%sERROR checking ticket: %v%s", ColorRed, err, ColorReset)
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "An error occurred. Please try again.",
		})
		return
	}

	if hasTicket {
		log.Printf("%sUser already has a ticket, preventing duplicate ticket creation%s", ColorYellow, ColorReset)
		channelID, err := getActiveTicketChannelID(userID)
		var message string
		if err != nil {
			message = "You already have an open ticket. Please close it before creating a new one."
		} else {
			message = fmt.Sprintf("You already have an open ticket in <#%s>", channelID)
		}
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: message,
		})
		return
	}

	// Check if email is required (based on guild setting)
	requireEmail, err := getTicketSetting(i.GuildID, "require_email")
	if err != nil {
		log.Printf("%sERROR checking ticket setting: %v%s", ColorRed, err, ColorReset)
		// Default to requiring email if there's an error
		requireEmail = true
	}

	// Check if user has their account email linked (only if required)
	if requireEmail {
		email, err := getEmailByDiscordID(userID)
		if err != nil {
			log.Printf("%sERROR checking email: %v%s", ColorRed, err, ColorReset)
		}
		if email == "" {
			log.Printf("%sUser does not have account linked, blocking ticket creation%s", ColorYellow, ColorReset)
			linkURL := panelPath("/dashboard")
			message := fmt.Sprintf("You must link your account before opening a ticket.\n\n[Link your account here](%s)", linkURL)
			s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
				Content: message,
			})
			return
		}
	}

	// Get user's display name
	username := i.Member.User.Username
	if i.Member.Nick != "" {
		username = i.Member.Nick
	}
	log.Printf("%sUsername: %s%s", ColorCyan, username, ColorReset)

	// Create channel name
	channelName := fmt.Sprintf("ticket-%s", strings.ToLower(strings.ReplaceAll(username, " ", "-")))
	log.Printf("%sCreating channel: %s%s", ColorCyan, channelName, ColorReset)

	// Create channel
	channel, err := s.GuildChannelCreateComplex(guildID, discordgo.GuildChannelCreateData{
		Name:     channelName,
		Type:     discordgo.ChannelTypeGuildText,
		ParentID: OpenTicketCategoryID,
		PermissionOverwrites: []*discordgo.PermissionOverwrite{
			{
				ID:    guildID,
				Type:  discordgo.PermissionOverwriteTypeRole,
				Deny:  discordgo.PermissionAll,
				Allow: 0,
			},
			{
				ID:    userID,
				Type:  discordgo.PermissionOverwriteTypeMember,
				Allow: discordgo.PermissionViewChannel | discordgo.PermissionSendMessages | discordgo.PermissionReadMessageHistory,
				Deny:  0,
			},
			{
				ID:    s.State.User.ID,
				Type:  discordgo.PermissionOverwriteTypeMember,
				Allow: discordgo.PermissionAll,
				Deny:  0,
			},
			{
				ID:    StaffRoleID1,
				Type:  discordgo.PermissionOverwriteTypeRole,
				Allow: discordgo.PermissionViewChannel | discordgo.PermissionSendMessages | discordgo.PermissionReadMessageHistory,
				Deny:  0,
			},
			{
				ID:    StaffRoleID2,
				Type:  discordgo.PermissionOverwriteTypeRole,
				Allow: discordgo.PermissionViewChannel | discordgo.PermissionSendMessages | discordgo.PermissionReadMessageHistory,
				Deny:  0,
			},
		},
	})

	if err != nil {
		log.Printf("%sERROR creating channel: %v%s", ColorRed, err, ColorReset)
		// Try to respond with error
		respErr := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Failed to create ticket channel. Please try again.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		if respErr != nil {
			log.Printf("%sERROR responding to interaction (channel creation failed): %v%s", ColorRed, respErr, ColorReset)
		}
		return
	}

	log.Printf("%sChannel created successfully: %s%s", ColorGreen, channel.ID, ColorReset)

	// Save ticket to database
	err = createTicket(userID, channel.ID, category)
	if err != nil {
		log.Printf("%sERROR saving ticket to database: %v%s", ColorRed, err, ColorReset)
		s.ChannelDelete(channel.ID)
		respErr := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Failed to create ticket. Please try again.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		if respErr != nil {
			log.Printf("%sERROR responding to interaction (database save failed): %v%s", ColorRed, respErr, ColorReset)
		}
		return
	}

	log.Printf("%sTicket saved to database%s", ColorGreen, ColorReset)

	// Send follow-up message since we already sent a deferred response
	log.Printf("%sSending follow-up message...%s", ColorCyan, ColorReset)
	_, err = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: fmt.Sprintf("Ticket created! Please check %s", channel.Mention()),
	})

	if err != nil {
		log.Printf("%sERROR sending follow-up message: %v%s", ColorRed, err, ColorReset)
		log.Printf("%sInteraction ID: %s, Token: %s%s", ColorYellow, i.Interaction.ID, i.Interaction.Token[:20]+"...", ColorReset)
	} else {
		log.Printf("%sFollow-up message sent successfully%s", ColorGreen, ColorReset)
	}

	// Send welcome message in ticket channel using REST API to avoid emoji issues
	currentTime := time.Now().Format("January 2, 2006 at 3:04 PM")
	welcomePayload := map[string]interface{}{
		"embeds": []map[string]interface{}{
			{
				"title":       "Thank you for opening a ticket",
				"description": fmt.Sprintf("This is your ticket %s!", i.Member.User.Mention()) + "\n\nOur support team will be with you shortly. In the meantime, please:\n• Describe your issue in detail\n• Share any relevant screenshots\n• Provide error logs if applicable",
				"color":       0xFF4444,
				"fields": []map[string]interface{}{
					{
						"name":   "Account Email",
						"value":  getAccountEmailForTicket(userID),
						"inline": true,
					},
					{
						"name":   "Category",
						"value":  capitalizeWords(category),
						"inline": true,
					},
				},
				"footer": map[string]interface{}{
					"text": currentTime,
				},
			},
		},
		"components": []map[string]interface{}{
			{
				"type": 1,
				"components": []map[string]interface{}{
					{
						"type":     2,
						"style":    4, // Danger button style
						"label":    "Close Ticket",
						"custom_id": "close_ticket",
					},
				},
			},
		},
	}

	payloadJSON, err := json.Marshal(welcomePayload)
	if err != nil {
		log.Printf("%sERROR marshaling welcome payload: %v%s", ColorRed, err, ColorReset)
		return
	}

	log.Printf("%sSending welcome message to channel %s...%s", ColorCyan, channel.ID, ColorReset)
	url := fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages", channel.ID)

	req, err := http.NewRequest("POST", url, strings.NewReader(string(payloadJSON)))
	if err != nil {
		log.Printf("%sERROR creating HTTP request: %v%s", ColorRed, err, ColorReset)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+BotToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("%sERROR sending welcome message HTTP request: %v%s", ColorRed, err, ColorReset)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		log.Printf("%sERROR from Discord API when sending welcome message: Status %d, Body: %s%s", ColorRed, resp.StatusCode, string(body), ColorReset)
	} else {
		log.Printf("%sWelcome message sent successfully to ticket channel%s", ColorGreen, ColorReset)

		// Send a ping message and delete it immediately
		log.Printf("%sSending ping message to user...%s", ColorCyan, ColorReset)
		pingMsg, err := s.ChannelMessageSend(channel.ID, i.Member.User.Mention())
		if err != nil {
			log.Printf("%sERROR sending ping message: %v%s", ColorRed, err, ColorReset)
		} else {
			log.Printf("%sPing message sent, deleting immediately...%s", ColorCyan, ColorReset)
			time.Sleep(100 * time.Millisecond) // Small delay to ensure message is sent
			err = s.ChannelMessageDelete(channel.ID, pingMsg.ID)
			if err != nil {
				log.Printf("%sERROR deleting ping message: %v%s", ColorRed, err, ColorReset)
			} else {
				log.Printf("%sPing message deleted successfully%s", ColorGreen, ColorReset)
			}
		}
	}
}

// Handle close ticket button
func handleCloseTicketButton(s *discordgo.Session, i *discordgo.InteractionCreate) {
	channelID := i.ChannelID
	userID := i.Member.User.ID

	// Get ticket info
	ticketUserID, _, status, err := getTicketByChannel(channelID)
	if err != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Ticket not found.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	// Check if ticket is already closed
	if status == "closed" {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "This ticket is already closed.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	// Check if user owns the ticket or is staff
	if ticketUserID != userID {
		// Check if user has admin/manage channels permission
		perms, err := s.UserChannelPermissions(userID, channelID)
		if err != nil || (perms&discordgo.PermissionManageChannels) == 0 {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "You don't have permission to close this ticket.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}
	}

	// Update ticket status
	err = updateTicketStatus(channelID, "closed")
	if err != nil {
		log.Printf("%sError updating ticket status: %v%s", ColorRed, err, ColorReset)
	}

	// Move channel to closed category
	_, err = s.ChannelEdit(channelID, &discordgo.ChannelEdit{
		ParentID: ClosedTicketCategoryID,
	})
	if err != nil {
		log.Printf("%sError moving channel: %v%s", ColorRed, err, ColorReset)
	}

	// Remove send messages permission for the user
	channel, err := s.Channel(channelID)
	if err == nil {
		overwrites := channel.PermissionOverwrites
		for _, overwrite := range overwrites {
			if overwrite.ID == ticketUserID && overwrite.Type == discordgo.PermissionOverwriteTypeMember {
				err = s.ChannelPermissionSet(channelID, ticketUserID, discordgo.PermissionOverwriteTypeMember,
					overwrite.Allow&^(discordgo.PermissionSendMessages),
					overwrite.Deny|discordgo.PermissionSendMessages)
				break
			}
		}
	}

	// Get the user who closed the ticket (use proper mention)
	closerMention := i.Member.User.Mention()

	// Respond to interaction (acknowledge without showing anything)
	// Type 7 = DEFERRED_UPDATE_MESSAGE (acknowledges component interaction without showing anything)
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: 7,
	})

	// Send closed message with embed and buttons using REST API
	closedPayload := map[string]interface{}{
		"embeds": []map[string]interface{}{
			{
				"title":       "Ticket Closed",
				"description": fmt.Sprintf("This ticket has been closed by %s. Below, there are some actions that the ticket owner or a staff member can perform.", closerMention),
				"color":       0xFF4444,
				"fields": []map[string]interface{}{
					{
						"name":   "Delete Ticket",
						"value":  "A staff member can delete if it is no longer being used. This will delete the channel and all of its messages.",
						"inline": true,
					},
					{
						"name":   "Reopen Ticket",
						"value":  "You or a staff member can re-open this ticket if you have more questions.",
						"inline": true,
					},
					{
						"name":   "\u200b",
						"value":  "\u200b",
						"inline": true,
					},
					{
						"name":   "Get Transcript",
						"value":  "You can get a transcript of the ticket to share with others. This will be available for 1 hour.",
						"inline": true,
					},
					{
						"name":   "Rate your Experience",
						"value":  "Help us improve by rating your experience with this ticket. You can rate from 1 to 5 stars and optionally provide feedback.",
						"inline": true,
					},
					{
						"name":   "\u200b",
						"value":  "\u200b",
						"inline": true,
					},
				},
			},
		},
		"components": []map[string]interface{}{
			{
				"type": 1,
				"components": []map[string]interface{}{
					{
						"type":     2,
						"style":    4,
						"label":    "Delete Ticket",
						"custom_id": "delete_ticket",
					},
					{
						"type":     2,
						"style":    2,
						"label":    "Reopen Ticket",
						"custom_id": "reopen_ticket",
					},
					{
						"type":     2,
						"style":    2,
						"label":    "Get Transcript",
						"custom_id": "transcript_ticket",
					},
					{
						"type":     2,
						"style":    3,
						"label":    "Rate your Experience",
						"custom_id": "rate_ticket",
					},
				},
			},
		},
	}

	payloadJSON, err := json.Marshal(closedPayload)
	if err != nil {
		log.Printf("%sERROR marshaling closed payload: %v%s", ColorRed, err, ColorReset)
		return
	}

	url := fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages", channelID)
	req, err := http.NewRequest("POST", url, strings.NewReader(string(payloadJSON)))
	if err != nil {
		log.Printf("%sERROR creating HTTP request: %v%s", ColorRed, err, ColorReset)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+BotToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("%sERROR sending closed message: %v%s", ColorRed, err, ColorReset)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("%sERROR from Discord API when sending closed message: Status %d, Body: %s%s", ColorRed, resp.StatusCode, string(body), ColorReset)
	} else {
		log.Printf("%sClosed message with buttons sent successfully%s", ColorGreen, ColorReset)
	}
}

// Handle delete ticket button
func handleDeleteTicketButton(s *discordgo.Session, i *discordgo.InteractionCreate) {
	channelID := i.ChannelID

	// Check if user has one of the staff roles
	hasStaffRole := false
	for _, memberRoleID := range i.Member.Roles {
		for _, setupRoleID := range TicketSetupRoleIDs {
			if memberRoleID == setupRoleID {
				hasStaffRole = true
				break
			}
		}
		if hasStaffRole {
			break
		}
	}

	if !hasStaffRole {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "You don't have permission to delete tickets. Only staff members can delete tickets.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	// Delete ticket from database
	err := deleteTicket(channelID)
	if err != nil {
		log.Printf("%sError deleting ticket: %v%s", ColorRed, err, ColorReset)
	}

	// Delete channel
	_, err = s.ChannelDelete(channelID)
	if err != nil {
		log.Printf("%sError deleting channel: %v%s", ColorRed, err, ColorReset)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Failed to delete ticket channel.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Ticket deleted.",
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

// Handle reopen ticket button
func handleReopenTicketButton(s *discordgo.Session, i *discordgo.InteractionCreate) {
	channelID := i.ChannelID
	userID := i.Member.User.ID

	// Get ticket info
	ticketUserID, _, status, err := getTicketByChannel(channelID)
	if err != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Ticket not found.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	// Check if user owns the ticket or is staff
	if ticketUserID != userID {
		// Check if user has admin/manage channels permission
		perms, err := s.UserChannelPermissions(userID, channelID)
		if err != nil || (perms&discordgo.PermissionManageChannels) == 0 {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "You don't have permission to reopen this ticket.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}
	}

	if status == "open" {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Ticket is already open.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	// Check if ticket owner already has another active ticket
	hasOtherTicket, otherChannelID, err := hasOtherActiveTicket(ticketUserID, channelID)
	if err != nil {
		log.Printf("%sERROR checking for other active tickets: %v%s", ColorRed, err, ColorReset)
	} else if hasOtherTicket {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("You already have an open ticket in <#%s>. Please close it before reopening this one.", otherChannelID),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	// Update ticket status
	err = updateTicketStatus(channelID, "open")
	if err != nil {
		log.Printf("%sError updating ticket status: %v%s", ColorRed, err, ColorReset)
	}

	// Move channel back to open category
	_, err = s.ChannelEdit(channelID, &discordgo.ChannelEdit{
		ParentID: OpenTicketCategoryID,
	})
	if err != nil {
		log.Printf("%sError moving channel: %v%s", ColorRed, err, ColorReset)
	}

	// Restore send messages permission for the user
	channel, err := s.Channel(channelID)
	if err == nil {
		overwrites := channel.PermissionOverwrites
		for _, overwrite := range overwrites {
			if overwrite.ID == ticketUserID && overwrite.Type == discordgo.PermissionOverwriteTypeMember {
				err = s.ChannelPermissionSet(channelID, ticketUserID, discordgo.PermissionOverwriteTypeMember,
					overwrite.Allow|discordgo.PermissionSendMessages,
					overwrite.Deny&^(discordgo.PermissionSendMessages))
				break
			}
		}
	}

	// Find and edit the "Ticket Closed" message to show it's reopened
	messages, err := s.ChannelMessages(channelID, 50, "", "", "")
	if err == nil {
		for _, msg := range messages {
			// Check if message has embeds and if it's the "Ticket Closed" message
			if len(msg.Embeds) > 0 && msg.Embeds[0].Title == "Ticket Closed" {
				// Get the user who reopened the ticket
				reopenerMention := i.Member.User.Mention()

				// Edit the message to show it's reopened and remove buttons
				editPayload := map[string]interface{}{
					"embeds": []map[string]interface{}{
						{
							"title":       "Ticket Reopened",
							"description": fmt.Sprintf("This ticket has been reopened by %s", reopenerMention),
							"color":       0xFF4444,
						},
					},
					"components": []interface{}{}, // Empty components array
				}

				editJSON, _ := json.Marshal(editPayload)
				editURL := fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages/%s", channelID, msg.ID)
				editReq, _ := http.NewRequest("PATCH", editURL, strings.NewReader(string(editJSON)))
				editReq.Header.Set("Content-Type", "application/json")
				editReq.Header.Set("Authorization", "Bot "+BotToken)

				client := &http.Client{Timeout: 10 * time.Second}
				editResp, err := client.Do(editReq)
				if err == nil {
					editResp.Body.Close()
					log.Printf("%sEdited closed message to show reopened%s", ColorGreen, ColorReset)
				}
				break
			}
		}
	}

	// Respond to interaction immediately (no thinking message)
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "",
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

// Handle transcript button
func handleTranscriptButton(s *discordgo.Session, i *discordgo.InteractionCreate) {
	channelID := i.ChannelID

	// Get ticket info to verify it's a ticket and get owner username
	ticketUserID, category, _, err := getTicketByChannel(channelID)
	if err != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "This command can only be used in a ticket channel.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	// Get ticket owner's username for filename
	ticketOwner, err := s.User(ticketUserID)
	if err != nil {
		log.Printf("%sERROR getting ticket owner: %v%s", ColorRed, err, ColorReset)
		// Fallback to channel ID if we can't get username
		ticketOwner = &discordgo.User{Username: "unknown"}
	}

	// Respond with deferred response since this might take a while
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})

	// Fetch all messages from the channel
	var allMessages []*discordgo.Message
	var lastMessageID string

	for {
		messages, err := s.ChannelMessages(channelID, 100, lastMessageID, "", "")
		if err != nil {
			log.Printf("%sERROR fetching messages: %v%s", ColorRed, err, ColorReset)
			s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
				Content: "Failed to fetch messages. Please try again.",
			})
			return
		}

		if len(messages) == 0 {
			break
		}

		allMessages = append(allMessages, messages...)
		lastMessageID = messages[len(messages)-1].ID

		if len(messages) < 100 {
			break
		}
	}

	// Reverse messages to get chronological order (oldest first)
	for i, j := 0, len(allMessages)-1; i < j; i, j = i+1, j-1 {
		allMessages[i], allMessages[j] = allMessages[j], allMessages[i]
	}

	// Get channel info for header
	channel, err := s.Channel(channelID)
	if err != nil {
		log.Printf("%sERROR getting channel info: %v%s", ColorRed, err, ColorReset)
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "Failed to get channel information. Please try again.",
		})
		return
	}

	// Format transcript
	var transcript strings.Builder
	transcript.WriteString("=" + strings.Repeat("=", 50) + "\n")
	transcript.WriteString(fmt.Sprintf("TICKET TRANSCRIPT\n"))
	transcript.WriteString(fmt.Sprintf("Channel: %s\n", channel.Name))
	transcript.WriteString(fmt.Sprintf("Category: %s\n", capitalizeWords(category)))
	transcript.WriteString(fmt.Sprintf("Generated: %s\n", time.Now().Format("January 2, 2006 at 3:04 PM MST")))
	transcript.WriteString("=" + strings.Repeat("=", 50) + "\n\n")

	for _, msg := range allMessages {
		// Skip bot messages that are system messages
		if msg.Author == nil {
			continue
		}

		// Format timestamp
		timestamp := msg.Timestamp
		dateTime := timestamp.Format("2006-01-02 15:04:05 MST")

		// Get username
		username := msg.Author.Username
		if msg.Member != nil && msg.Member.Nick != "" {
			username = msg.Member.Nick
		}

		// Format message content
		content := msg.Content
		if content == "" {
			// Handle embeds or attachments
			if len(msg.Embeds) > 0 {
				content = "[Embed]"
			} else if len(msg.Attachments) > 0 {
				content = fmt.Sprintf("[Attachment: %s]", msg.Attachments[0].Filename)
			} else {
				content = "[System Message]"
			}
		}

		// Write formatted message
		transcript.WriteString(fmt.Sprintf("[%s] %s: %s\n", dateTime, username, content))

		// Add attachments if any
		if len(msg.Attachments) > 0 {
			for _, att := range msg.Attachments {
				transcript.WriteString(fmt.Sprintf("  └─ Attachment: %s (%s)\n", att.Filename, att.URL))
			}
		}
	}

	transcript.WriteString("\n" + "=" + strings.Repeat("=", 50) + "\n")
	transcript.WriteString("End of transcript\n")

	// Upload to file hosting service
	transcriptContent := transcript.String()
	// Use shorter filename: ticket-USERNAME.txt
	filename := fmt.Sprintf("ticket-%s.txt", strings.ToLower(strings.ReplaceAll(ticketOwner.Username, " ", "-")))
	
	// Try 0x0.st first
	fileURL, err := uploadTo0x0(transcriptContent, filename)
	if err != nil {
		log.Printf("%s0x0.st upload failed, trying alternative: %v%s", ColorYellow, err, ColorReset)
		// Try alternative: tmpfiles.org
		fileURL, err = uploadToTmpFiles(transcriptContent, filename)
		if err != nil {
			log.Printf("%sERROR uploading transcript to all services. Please try again later.%s", ColorRed, ColorReset)
			return
		}
	}

	// Send the link to the user
	s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: fmt.Sprintf("[Download Transcript](%s)", fileURL),
	})
}

// Upload text content to 0x0.st file hosting service
func uploadTo0x0(content string, filename string) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// Add file field - 0x0.st expects the field name to be "file"
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		log.Printf("%sERROR creating form file: %v%s", ColorRed, err, ColorReset)
		return "", err
	}
	_, err = part.Write([]byte(content))
	if err != nil {
		log.Printf("%sERROR writing file content: %v%s", ColorRed, err, ColorReset)
		return "", err
	}

	// Close the writer before creating the request
	contentType := writer.FormDataContentType()
	err = writer.Close()
	if err != nil {
		log.Printf("%sERROR closing multipart writer: %v%s", ColorRed, err, ColorReset)
		return "", err
	}

	// Create request
	req, err := http.NewRequest("POST", "https://0x0.st", &body)
	if err != nil {
		log.Printf("%sERROR creating HTTP request: %v%s", ColorRed, err, ColorReset)
		return "", err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "Discord-Bot/1.0")

	// Send request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("%sERROR sending HTTP request: %v%s", ColorRed, err, ColorReset)
		return "", err
	}
	defer resp.Body.Close()

	// Read response body
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("%sERROR reading response body: %v%s", ColorRed, err, ColorReset)
		return "", err
	}

	if resp.StatusCode != 200 {
		log.Printf("%sUpload failed with status %d: %s%s", ColorRed, resp.StatusCode, string(responseBody), ColorReset)
		return "", fmt.Errorf("upload failed with status %d: %s", resp.StatusCode, string(responseBody))
	}

	// 0x0.st returns the URL on a single line, trim whitespace
	url := strings.TrimSpace(string(responseBody))
	if url == "" {
		return "", fmt.Errorf("empty response from upload service")
	}
	
	log.Printf("%s✓ Transcript uploaded successfully: %s%s", ColorGreen, url, ColorReset)
	return url, nil
}

// Upload text content to tmpfiles.org as fallback
func uploadToTmpFiles(content string, filename string) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// Add file field
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %v", err)
	}
	_, err = part.Write([]byte(content))
	if err != nil {
		return "", fmt.Errorf("failed to write file content: %v", err)
	}

	contentType := writer.FormDataContentType()
	writer.Close()

	// Create request
	req, err := http.NewRequest("POST", "https://tmpfiles.org/api/v1/upload", &body)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)

	// Send request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("network error: %v", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %v", err)
	}

	if resp.StatusCode != 200 {
		// Try to parse error response
		responseStr := string(responseBody)
		if len(responseStr) > 200 {
			responseStr = responseStr[:200] + "..."
		}
		return "", fmt.Errorf("upload failed with status %d. Response: %s", resp.StatusCode, responseStr)
	}

	// Parse JSON response from tmpfiles.org
	var result struct {
		Status string `json:"status"`
		Data   struct {
			URL string `json:"url"`
		} `json:"data"`
		Error string `json:"error"`
	}
	
	err = json.Unmarshal(responseBody, &result)
	if err != nil {
		// If JSON parsing fails, show what we got
		responseStr := string(responseBody)
		if len(responseStr) > 200 {
			responseStr = responseStr[:200] + "..."
		}
		return "", fmt.Errorf("invalid JSON response (got HTML?): %s", responseStr)
	}

	if result.Status != "success" || result.Data.URL == "" {
		return "", fmt.Errorf("upload failed: status=%s, error=%s", result.Status, result.Error)
	}

	// tmpfiles.org returns a URL - ensure it has /dl/ for direct download
	url := result.Data.URL
	// If URL doesn't have /dl/, we need to extract the file ID and construct it
	// The API returns something like https://tmpfiles.org/xxxxx/filename.txt
	// We need https://tmpfiles.org/dl/xxxxx/filename.txt
	if !strings.Contains(url, "/dl/") {
		// Extract the path after tmpfiles.org
		parts := strings.Split(url, "tmpfiles.org/")
		if len(parts) > 1 {
			url = "https://tmpfiles.org/dl/" + parts[1]
		}
	}

	log.Printf("%s✓ Transcript uploaded to tmpfiles.org: %s%s", ColorGreen, url, ColorReset)
	return url, nil
}

// Handle services command
func handleServicesCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	userID := i.Member.User.ID

	// Check if user has their account linked
	email, err := getEmailByDiscordID(userID)
	if err != nil {
		log.Printf("%sERROR checking email: %v%s", ColorRed, err, ColorReset)
	}
	if email == "" {
		linkURL := panelPath("/dashboard")
		message := fmt.Sprintf("You must link your account before viewing services.\n\n[Link your account here](%s)", linkURL)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: message,
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	// Acknowledge the interaction immediately to prevent timeout
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})

	// Make API call to get services (URL encode the email for path segment)
	encodedEmail := url.PathEscape(email)
	apiURL := fmt.Sprintf("%s/api/v3/users/%s/services", PanelBaseURL, encodedEmail)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		log.Printf("%sERROR creating API request: %v%s", ColorRed, err, ColorReset)
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "Failed to fetch services. Please try again later.",
			Flags:   discordgo.MessageFlagsEphemeral,
		})
		return
	}

	req.Header.Set("X-API-Key", APIKey)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("%sERROR calling services API: %v%s", ColorRed, err, ColorReset)
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "Failed to fetch services. Please try again later.",
			Flags:   discordgo.MessageFlagsEphemeral,
		})
		return
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("%sERROR reading API response: %v%s", ColorRed, err, ColorReset)
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "Failed to fetch services. Please try again later.",
			Flags:   discordgo.MessageFlagsEphemeral,
		})
		return
	}

	// Parse API response
	var apiResponse struct {
		Success bool `json:"success"`
		Message string `json:"message"`
		Services []struct {
			ID           int     `json:"id"`
			Price        string  `json:"price"` // API returns price as string
			Status       string  `json:"status"`
			Name         string  `json:"name"`
			DueDate      *string `json:"due_date"`
			CancelledAt  *string `json:"cancelled_at"`
		} `json:"services"`
	}

	err = json.Unmarshal(responseBody, &apiResponse)
	if err != nil {
		log.Printf("%sERROR parsing API response: %v%s", ColorRed, err, ColorReset)
		log.Printf("%sResponse body: %s%s", ColorYellow, string(responseBody), ColorReset)
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "Failed to parse services data. Please try again later.",
			Flags:   discordgo.MessageFlagsEphemeral,
		})
		return
	}

	if !apiResponse.Success {
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: fmt.Sprintf("Error: %s", apiResponse.Message),
			Flags:   discordgo.MessageFlagsEphemeral,
		})
		return
	}

	// Build embed
	var embed *discordgo.MessageEmbed
	if len(apiResponse.Services) == 0 {
		embed = &discordgo.MessageEmbed{
			Title:       "Your Services",
			Description: "You don't have any services yet.",
			Color:       0xFF4444,
		}
	} else {
		var fields []*discordgo.MessageEmbedField
		for idx, service := range apiResponse.Services {
			// Format price (parse string to float for display)
			var priceStr string
			if price, err := strconv.ParseFloat(service.Price, 64); err == nil {
				priceStr = fmt.Sprintf("$%.2f", price)
			} else {
				priceStr = fmt.Sprintf("$%s", service.Price) // Fallback to raw string
			}
			
			// Format due date
			dueDateStr := "N/A"
			if service.DueDate != nil && *service.DueDate != "" {
				if t, err := time.Parse("2006-01-02 15:04:05", *service.DueDate); err == nil {
					dueDateStr = t.Format("January 2, 2006")
				} else if t, err := time.Parse(time.RFC3339, *service.DueDate); err == nil {
					dueDateStr = t.Format("January 2, 2006")
				} else {
					dueDateStr = *service.DueDate
				}
			}

			// Capitalize first letter of status
			statusDisplay := capitalizeWords(service.Status)
			
			// Build service info
			value := fmt.Sprintf("**Price:** %s\n**Status:** %s\n**Due Date:** %s", 
				priceStr, statusDisplay, dueDateStr)
			
			// Add cancelled info if applicable
			if service.CancelledAt != nil && *service.CancelledAt != "" {
				var cancelledStr string
				if t, err := time.Parse("2006-01-02 15:04:05", *service.CancelledAt); err == nil {
					cancelledStr = t.Format("January 2, 2006")
				} else if t, err := time.Parse(time.RFC3339, *service.CancelledAt); err == nil {
					cancelledStr = t.Format("January 2, 2006")
				} else {
					cancelledStr = *service.CancelledAt
				}
				value += fmt.Sprintf("\n**Cancelled:** %s", cancelledStr)
			}

			fields = append(fields, &discordgo.MessageEmbedField{
				Name:   fmt.Sprintf("%d. %s", idx+1, service.Name),
				Value:  value,
				Inline: false,
			})
		}

		embed = &discordgo.MessageEmbed{
			Title:       fmt.Sprintf("Your Services (%d)", len(apiResponse.Services)),
			Description: "Here are your services:",
			Fields:     fields,
			Color:      0xFF4444,
		}
	}

	// Build buttons for each service using REST API format (max 5 buttons per row, Discord limit)
	var components []map[string]interface{}
	if len(apiResponse.Services) > 0 {
		var buttonRow []map[string]interface{}
		for idx, service := range apiResponse.Services {
			// Discord allows max 5 buttons per row
			if idx > 0 && idx%5 == 0 {
				components = append(components, map[string]interface{}{
					"type":       1, // ActionRow
					"components": buttonRow,
				})
				buttonRow = []map[string]interface{}{}
			}
			
			// Truncate service name if too long (Discord button label limit is 80 chars)
			buttonLabel := service.Name
			if len(buttonLabel) > 80 {
				buttonLabel = buttonLabel[:77] + "..."
			}
			
			buttonRow = append(buttonRow, map[string]interface{}{
				"type":  2, // Button
				"style": 5, // Link button
				"label": buttonLabel,
				"url":   panelPath(fmt.Sprintf("/services/%d", service.ID)),
			})
		}
		
		// Add remaining buttons
		if len(buttonRow) > 0 {
			components = append(components, map[string]interface{}{
				"type":       1, // ActionRow
				"components": buttonRow,
			})
		}
	}

	// Build followup message using REST API to avoid emoji issues
	followupURL := fmt.Sprintf("https://discord.com/api/v10/webhooks/%s/%s", s.State.User.ID, i.Interaction.Token)
	
	// Convert embed fields to map format
	var embedFields []map[string]interface{}
	if embed.Fields != nil {
		for _, field := range embed.Fields {
			embedFields = append(embedFields, map[string]interface{}{
				"name":   field.Name,
				"value":  field.Value,
				"inline": field.Inline,
			})
		}
	}
	
	payload := map[string]interface{}{
		"embeds": []map[string]interface{}{
			{
				"title":       embed.Title,
				"description": embed.Description,
				"color":       embed.Color,
				"fields":      embedFields,
			},
		},
		"flags": 64, // Ephemeral
	}
	
	if len(components) > 0 {
		payload["components"] = components
	}
	
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		log.Printf("%sERROR marshaling followup payload: %v%s", ColorRed, err, ColorReset)
		return
	}
	
	req, err = http.NewRequest("POST", followupURL, strings.NewReader(string(payloadJSON)))
	if err != nil {
		log.Printf("%sERROR creating followup request: %v%s", ColorRed, err, ColorReset)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	
	client = &http.Client{Timeout: 10 * time.Second}
	resp, err = client.Do(req)
	if err != nil {
		log.Printf("%sERROR sending followup message: %v%s", ColorRed, err, ColorReset)
		return
	}
	defer resp.Body.Close()
	
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("%sERROR from Discord API when sending followup: Status %d, Body: %s%s", ColorRed, resp.StatusCode, string(body), ColorReset)
	}
}

// Handle guild member join event - assign auto-role
func guildMemberAdd(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
	log.Printf("%sUser %s (ID: %s) joined guild %s%s", ColorCyan, m.User.Username, m.User.ID, m.GuildID, ColorReset)

	// Assign the auto-role to the new member
	err := s.GuildMemberRoleAdd(m.GuildID, m.User.ID, AutoRoleID)
	if err != nil {
		log.Printf("%sERROR assigning auto-role to user %s: %v%s", ColorRed, m.User.ID, err, ColorReset)
	} else {
		log.Printf("%s✓ Assigned auto-role to user %s%s", ColorGreen, m.User.Username, ColorReset)
	}
}

// Handle register_all command - assign auto-role to all members who don't have it
func handleRegisterAll(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Check Manage Events permission
	if !canManageEventsFromInteraction(s, i) {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "You don't have permission to use this command.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	// Respond with deferred response since this might take a while
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})

	guildID := i.GuildID
	assignedCount := 0
	skippedCount := 0
	errorCount := 0

	// Fetch all members from the guild
	// Note: This requires the bot to have the GuildMembers intent and privileged gateway intents
	members, err := s.GuildMembers(guildID, "", 1000)
	if err != nil {
		log.Printf("%sERROR fetching guild members: %v%s", ColorRed, err, ColorReset)
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: fmt.Sprintf("Failed to fetch guild members: %v", err),
		})
		return
	}

	log.Printf("%sProcessing %d members for auto-role assignment...%s", ColorCyan, len(members), ColorReset)

	// Process each member
	for _, member := range members {
		// Skip bots
		if member.User.Bot {
			skippedCount++
			continue
		}

		// Check if member already has the role
		hasRole := false
		for _, roleID := range member.Roles {
			if roleID == AutoRoleID {
				hasRole = true
				break
			}
		}

		if hasRole {
			skippedCount++
			continue
		}

		// Assign the role
		err := s.GuildMemberRoleAdd(guildID, member.User.ID, AutoRoleID)
		if err != nil {
			log.Printf("%sERROR assigning auto-role to user %s: %v%s", ColorRed, member.User.ID, err, ColorReset)
			errorCount++
		} else {
			assignedCount++
			log.Printf("%s✓ Assigned auto-role to %s%s", ColorGreen, member.User.Username, ColorReset)
		}

		// Small delay to avoid rate limits
		time.Sleep(100 * time.Millisecond)
	}

	// Send summary
	message := fmt.Sprintf("**Auto-Role Assignment Complete**\n\n✅ **Assigned:** %d members\n⏭️ **Skipped:** %d members (already had role or bots)\n❌ **Errors:** %d", assignedCount, skippedCount, errorCount)
	s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: message,
	})

	log.Printf("%sAuto-role assignment complete: %d assigned, %d skipped, %d errors%s", ColorGreen, assignedCount, skippedCount, errorCount, ColorReset)
}

// Handle rate ticket button - shows modal for rating
func handleRateTicketButton(s *discordgo.Session, i *discordgo.InteractionCreate) {
	channelID := i.ChannelID
	userID := i.Member.User.ID

	// Get ticket info to verify it's a ticket
	_, _, status, err := getTicketByChannel(channelID)
	if err != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "This command can only be used in a ticket channel.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	// Check if ticket is closed
	if status != "closed" {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "You can only rate closed tickets.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	// Check if user has already rated this ticket
	hasRated, err := hasRatedTicket(channelID, userID)
	if err != nil {
		log.Printf("%sERROR checking rating: %v%s", ColorRed, err, ColorReset)
	}
	if hasRated {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "You have already rated this ticket.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	// Show modal with rating form
	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "rate_ticket_modal:" + channelID,
			Title:    "Rate your Experience",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "rating_stars",
							Label:       "Rating (1-5 stars)",
							Style:       discordgo.TextInputShort,
							Placeholder: "Enter a number from 1 to 5",
							Required:    true,
							MaxLength:   1,
							MinLength:   1,
						},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "rating_feedback",
							Label:       "Feedback (Optional)",
							Style:       discordgo.TextInputParagraph,
							Placeholder: "Tell us why you gave this rating...",
							Required:    false,
							MaxLength:   1000,
						},
					},
				},
			},
		},
	})

	if err != nil {
		log.Printf("%sERROR showing rating modal: %v%s", ColorRed, err, ColorReset)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Failed to open rating form. Please try again.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
	}
}

// Handle modal submit for rating
func handleModalSubmit(s *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.ModalSubmitData().CustomID

	// Check if it's a rating modal
	if strings.HasPrefix(customID, "rate_ticket_modal:") {
		channelID := strings.TrimPrefix(customID, "rate_ticket_modal:")
		userID := i.Member.User.ID

		// Get rating from modal data
		var ratingStr string
		var feedback string

		for _, component := range i.ModalSubmitData().Components {
			switch row := component.(type) {
			case *discordgo.ActionsRow:
				for _, input := range row.Components {
					switch textInput := input.(type) {
					case *discordgo.TextInput:
						if textInput.CustomID == "rating_stars" {
							ratingStr = textInput.Value
						} else if textInput.CustomID == "rating_feedback" {
							feedback = textInput.Value
						}
					}
				}
			}
		}

		// Validate rating
		rating, err := strconv.Atoi(ratingStr)
		if err != nil || rating < 1 || rating > 5 {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "Invalid rating. Please enter a number between 1 and 5.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		// Get ticket info
		ticketUserID, _, _, err := getTicketByChannel(channelID)
		if err != nil {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "Ticket not found.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		// Save rating to database
		err = saveTicketRating(channelID, ticketUserID, userID, rating, feedback)
		if err != nil {
			log.Printf("%sERROR saving rating: %v%s", ColorRed, err, ColorReset)
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "Failed to save rating. Please try again.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		// Respond with success message
		stars := strings.Repeat("⭐", rating)
		message := fmt.Sprintf("Thank you for your feedback! You rated this ticket **%d/5** %s", rating, stars)
		if feedback != "" {
			message += fmt.Sprintf("\n\n**Your feedback:** %s", feedback)
		}

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: message,
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})

		// Get user info for notification
		raterUser, err := s.User(userID)
		raterName := "Unknown User"
		if err == nil {
			raterName = raterUser.Username
		}

		// Get channel info for ticket link
		channel, err := s.Channel(channelID)
		channelMention := channelID
		if err == nil {
			channelMention = channel.Mention()
		}

		// Send notification to rating channel
		notificationStars := strings.Repeat("⭐", rating)
		notificationMessage := fmt.Sprintf("**New Ticket Rating**\n\n**User:** %s (%s)\n**Ticket:** %s\n**Rating:** %d/5 %s", raterName, i.Member.User.Mention(), channelMention, rating, notificationStars)
		if feedback != "" {
			notificationMessage += fmt.Sprintf("\n\n**Feedback:**\n%s", feedback)
		}

		_, err = s.ChannelMessageSend(RatingChannelID, notificationMessage)
		if err != nil {
			log.Printf("%sERROR sending rating notification: %v%s", ColorRed, err, ColorReset)
		} else {
			log.Printf("%sRating notification sent to channel%s", ColorGreen, ColorReset)
		}

		log.Printf("%sRating saved: Channel=%s, User=%s, Rating=%d/5%s", ColorGreen, channelID, userID, rating, ColorReset)
	}
}

// Handle ratings show command - export all ratings to a text file
func handleRatingsShow(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Respond with deferred response since this might take a while
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})

	// Get all ratings from database
	ratings, err := getAllRatings()
	if err != nil {
		log.Printf("%sERROR getting ratings: %v%s", ColorRed, err, ColorReset)
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "Failed to fetch ratings. Please try again.",
		})
		return
	}

	if len(ratings) == 0 {
		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "No ratings have been submitted yet.",
		})
		return
	}

	// Build the ratings report
	var report strings.Builder
	report.WriteString("Ticket Ratings Report\n")
	report.WriteString("====================\n\n")
	report.WriteString(fmt.Sprintf("Generated: %s\n", time.Now().Format("2006-01-02 15:04:05 UTC")))
	report.WriteString(fmt.Sprintf("Total Ratings: %d\n\n", len(ratings)))
	report.WriteString(strings.Repeat("=", 80) + "\n\n")

	// Process each rating
	for idx, rating := range ratings {
		// Get user info
		raterUser, err := s.User(rating.RaterUserID)
		raterName := "Unknown User"
		if err == nil {
			raterName = raterUser.Username
		}

		// Get ticket owner info
		ticketOwner, err := s.User(rating.TicketUserID)
		ticketOwnerName := "Unknown User"
		if err == nil {
			ticketOwnerName = ticketOwner.Username
		}

		// Get channel info
		channel, err := s.Channel(rating.ChannelID)
		channelName := rating.ChannelID
		if err == nil {
			channelName = channel.Name
		}

		// Format rating entry
		stars := strings.Repeat("⭐", rating.Rating)
		report.WriteString(fmt.Sprintf("Rating #%d\n", idx+1))
		report.WriteString(fmt.Sprintf("  Rater: %s (ID: %s)\n", raterName, rating.RaterUserID))
		report.WriteString(fmt.Sprintf("  Ticket Owner: %s (ID: %s)\n", ticketOwnerName, rating.TicketUserID))
		report.WriteString(fmt.Sprintf("  Ticket Channel: %s (ID: %s)\n", channelName, rating.ChannelID))
		report.WriteString(fmt.Sprintf("  Rating: %d/5 %s\n", rating.Rating, stars))
		report.WriteString(fmt.Sprintf("  Date: %s\n", rating.RatedAt.Format("2006-01-02 15:04:05 UTC")))
		if rating.Feedback != "" {
			report.WriteString(fmt.Sprintf("  Feedback: %s\n", rating.Feedback))
		}
		report.WriteString("\n" + strings.Repeat("-", 80) + "\n\n")
	}

	report.WriteString("End of report\n")

	// Upload to file hosting service
	reportContent := report.String()
	filename := fmt.Sprintf("ticket-ratings-%s.txt", time.Now().Format("2006-01-02"))

	// Try 0x0.st first
	fileURL, err := uploadTo0x0(reportContent, filename)
	if err != nil {
		log.Printf("%s0x0.st upload failed, trying alternative: %v%s", ColorYellow, err, ColorReset)
		// Try alternative: tmpfiles.org
		fileURL, err = uploadToTmpFiles(reportContent, filename)
		if err != nil {
			log.Printf("%sERROR uploading ratings report to all services. Please try again later.%s", ColorRed, ColorReset)
			s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
				Content: "Failed to upload ratings report. Please try again later.",
			})
			return
		}
	}

	// Send the link to the user
	s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: fmt.Sprintf("[Download Ratings Report](%s)", fileURL),
	})
}

