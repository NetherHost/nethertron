package main

import (
	"fmt"
	"log"

	"github.com/bwmarrin/discordgo"
)

// Register slash commands with Discord
func registerCommands(s *discordgo.Session) {
	commands := []*discordgo.ApplicationCommand{
		{
			Name:        "ping",
			Description: "Check if the bot is responsive",
		},
		{
			Name:        "ticket",
			Description: "Ticket system commands",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "add",
					Description: "Add a user to this ticket",
					Options: []*discordgo.ApplicationCommandOption{
						{
							Type:        discordgo.ApplicationCommandOptionUser,
							Name:        "user",
							Description: "The user to add to the ticket",
							Required:    true,
						},
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "remove",
					Description: "Remove a user from this ticket",
					Options: []*discordgo.ApplicationCommandOption{
						{
							Type:        discordgo.ApplicationCommandOptionUser,
							Name:        "user",
							Description: "The user to remove from the ticket",
							Required:    true,
						},
					},
				},
			},
		},
		{
			Name:        "services",
			Description: "View your services",
		},
		{
			Name:        "admin",
			Description: "Administrative commands",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionSubCommandGroup,
					Name:        "tickets",
					Description: "Ticket system administration",
					Options: []*discordgo.ApplicationCommandOption{
						{
							Type:        discordgo.ApplicationCommandOptionSubCommand,
							Name:        "setup",
							Description: "Set up the ticket system in this channel",
						},
						{
							Type:        discordgo.ApplicationCommandOptionSubCommand,
							Name:        "require_email",
							Description: "Toggle whether users must link their account before opening tickets",
							Options: []*discordgo.ApplicationCommandOption{
								{
									Type:        discordgo.ApplicationCommandOptionBoolean,
									Name:        "enabled",
									Description: "Whether to require account linking (true/false)",
									Required:    true,
								},
							},
						},
						{
							Type:        discordgo.ApplicationCommandOptionSubCommand,
							Name:        "blacklist",
							Description: "Blacklist a user from opening tickets",
							Options: []*discordgo.ApplicationCommandOption{
								{
									Type:        discordgo.ApplicationCommandOptionUser,
									Name:        "user",
									Description: "The user to blacklist",
									Required:    true,
								},
							},
						},
						{
							Type:        discordgo.ApplicationCommandOptionSubCommand,
							Name:        "set_email",
							Description: "Manually set a user's email address",
							Options: []*discordgo.ApplicationCommandOption{
								{
									Type:        discordgo.ApplicationCommandOptionUser,
									Name:        "user",
									Description: "The user to set the email for",
									Required:    true,
								},
								{
									Type:        discordgo.ApplicationCommandOptionString,
									Name:        "email",
									Description: "The email address to set",
									Required:    true,
								},
							},
						},
						{
							Type:        discordgo.ApplicationCommandOptionSubCommand,
							Name:        "ratings_stats",
							Description: "View ticket rating statistics",
						},
						{
							Type:        discordgo.ApplicationCommandOptionSubCommand,
							Name:        "ratings_show",
							Description: "Export all ratings to a text file",
						},
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "register_all",
					Description: "Assign the auto-role to all members who don't have it",
				},
			},
			// Restrict to users with Manage Events permission
			DefaultMemberPermissions: func() *int64 {
				perm := int64(discordgo.PermissionManageEvents)
				return &perm
			}(),
		},
	}

	// Get all guilds (servers) the bot is in
	guilds, err := s.UserGuilds(100, "", "")
	if err != nil {
		log.Printf("%sError getting guilds: %v%s", ColorRed, err, ColorReset)
		return
	}

	// Register commands for each guild
	for _, guild := range guilds {
		// First, delete all existing commands to clear cache
		existingCommands, err := s.ApplicationCommands(s.State.User.ID, guild.ID)
		if err == nil {
			for _, cmd := range existingCommands {
				err := s.ApplicationCommandDelete(s.State.User.ID, guild.ID, cmd.ID)
				if err != nil {
					log.Printf("%s✗ Cannot delete old command '%v' in guild '%v': %v%s", ColorYellow, cmd.Name, guild.Name, err, ColorReset)
				} else {
					fmt.Printf("%s✓ Deleted old command '%s%s%s' in guild '%s%s%s'%s\n", ColorYellow, ColorCyan, cmd.Name, ColorReset, ColorCyan, guild.Name, ColorReset, ColorReset)
				}
			}
		}

		// Then register new commands
		for _, command := range commands {
			_, err := s.ApplicationCommandCreate(s.State.User.ID, guild.ID, command)
			if err != nil {
				log.Printf("%s✗ Cannot create command '%v' in guild '%v': %v%s", ColorRed, command.Name, guild.Name, err, ColorReset)
			} else {
				fmt.Printf("%s✓ Registered command '%s%s%s' in guild '%s%s%s'%s\n", ColorGreen, ColorCyan, command.Name, ColorReset, ColorCyan, guild.Name, ColorReset, ColorReset)
			}
		}
	}
}

// This function will be called when a slash command interaction is created
func interactionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	log.Printf("%s=== Interaction received ===%s", ColorBlue, ColorReset)
	log.Printf("%sInteraction Type: %d%s", ColorBlue, i.Type, ColorReset)

	// Handle button and select menu interactions
	if i.Type == discordgo.InteractionMessageComponent {
		log.Printf("%sDetected as MessageComponent interaction%s", ColorBlue, ColorReset)
		handleComponentInteraction(s, i)
		return
	}

	// Handle modal submissions
	if i.Type == discordgo.InteractionModalSubmit {
		log.Printf("%sDetected as ModalSubmit interaction%s", ColorBlue, ColorReset)
		handleModalSubmit(s, i)
		return
	}

	log.Printf("%sNot a MessageComponent, checking for slash command...%s", ColorBlue, ColorReset)

	// Handle slash commands
	switch i.ApplicationCommandData().Name {
	case "ping":
		// Get the WebSocket latency (round-trip time to Discord)
		wsLatency := s.HeartbeatLatency()
		
		// Respond with latency information
		var content string
		if wsLatency > 0 {
			content = fmt.Sprintf("Pong! 🏓\n**Latency:** %d ms", wsLatency.Milliseconds())
		} else {
			content = "Pong! 🏓\n**Latency:** Calculating..."
		}
		
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: content,
			},
		})

	case "ticket":
		handleTicketCommand(s, i)
	case "services":
		handleServicesCommand(s, i)
	case "admin":
		handleAdminCommand(s, i)
	}
}

