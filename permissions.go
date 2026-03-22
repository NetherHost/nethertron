package main

import (
	"github.com/bwmarrin/discordgo"
)

// hasPermission checks if a user has admin permissions or a specific role
// Returns true if user has Administrator permission OR has the specified role
func hasPermission(s *discordgo.Session, guildID, userID, requiredRoleID string) (bool, error) {
	// Check if user has Administrator permission
	perms, err := s.UserChannelPermissions(userID, guildID)
	if err != nil {
		return false, err
	}

	// Check for Administrator permission
	if perms&discordgo.PermissionAdministrator != 0 {
		return true, nil
	}

	// Get guild member to check roles
	member, err := s.GuildMember(guildID, userID)
	if err != nil {
		return false, err
	}

	// Check if user has the required role
	for _, roleID := range member.Roles {
		if roleID == requiredRoleID {
			return true, nil
		}
	}

	return false, nil
}

// hasPermissionFromInteraction is a convenience function that checks permissions from an interaction
func hasPermissionFromInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, requiredRoleID string) bool {
	// Check Administrator permission first
	perms, err := s.UserChannelPermissions(i.Member.User.ID, i.GuildID)
	if err == nil && (perms&discordgo.PermissionAdministrator != 0) {
		return true
	}

	// Check for required role
	for _, roleID := range i.Member.Roles {
		if roleID == requiredRoleID {
			return true
		}
	}

	return false
}

// isAdmin checks if a user has Administrator permission
// Returns true only if user has Administrator permission
func isAdmin(s *discordgo.Session, guildID, userID string) bool {
	perms, err := s.UserChannelPermissions(userID, guildID)
	if err != nil {
		return false
	}
	return perms&discordgo.PermissionAdministrator != 0
}

// isAdminFromInteraction is a convenience function that checks admin permission from an interaction
// Returns true only if user has Administrator permission
func isAdminFromInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) bool {
	perms, err := s.UserChannelPermissions(i.Member.User.ID, i.GuildID)
	if err != nil {
		return false
	}
	return perms&discordgo.PermissionAdministrator != 0
}

// canManageEvents checks if a user has Manage Events permission at the guild level
// Returns true only if user has Manage Events permission
func canManageEvents(s *discordgo.Session, guildID, userID string) bool {
	// Get guild to access roles
	guild, err := s.Guild(guildID)
	if err != nil {
		return false
	}

	// Get member to check their roles
	member, err := s.GuildMember(guildID, userID)
	if err != nil {
		return false
	}

	// Calculate permissions from roles
	var permissions int64

	// Check if user is the guild owner
	if guild.OwnerID == userID {
		return true // Guild owner has all permissions
	}

	// Calculate permissions from roles
	for _, roleID := range member.Roles {
		for _, role := range guild.Roles {
			if role.ID == roleID {
				permissions |= role.Permissions
				break
			}
		}
	}

	// Check for @everyone role permissions
	for _, role := range guild.Roles {
		if role.ID == guildID {
			permissions |= role.Permissions
			break
		}
	}

	return permissions&discordgo.PermissionManageEvents != 0
}

// canManageEventsFromInteraction is a convenience function that checks Manage Events permission from an interaction
// Returns true only if user has Manage Events permission
func canManageEventsFromInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) bool {
	// Get guild to access roles
	guild, err := s.Guild(i.GuildID)
	if err != nil {
		// Fallback: try using channel permissions if guild fetch fails
		if i.ChannelID != "" {
			perms, err := s.UserChannelPermissions(i.Member.User.ID, i.ChannelID)
			if err == nil {
				return perms&discordgo.PermissionManageEvents != 0
			}
		}
		return false
	}

	// Check if user is the guild owner
	if guild.OwnerID == i.Member.User.ID {
		return true // Guild owner has all permissions
	}

	// Calculate permissions from member's roles
	var permissions int64

	// Calculate permissions from roles
	for _, roleID := range i.Member.Roles {
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

	return permissions&discordgo.PermissionManageEvents != 0
}

