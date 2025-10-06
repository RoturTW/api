package main

import (
	"slices"
	"strings"
)

func calculateUserBadges(user User) []string {
	var badges []string

	system := user.Get("system")
	if system != "" {
		badges = append(badges, getStringOrEmpty(system))
	}

	currency := user.GetCredits()
	if currency >= 1000 {
		badges = append(badges, "rich")
	}

	if friends := user.Get("sys.friends"); friends != nil {
		if friendsList, ok := friends.([]any); ok && len(friendsList) >= 10 {
			badges = append(badges, "friendly")
		}
	}

	if discordID := user.Get("discord_id"); discordID != nil && discordID != "" {
		badges = append(badges, "discord")
	}

	if marriage := user.Get("sys.marriage"); marriage != nil {
		if marriageMap, ok := marriage.(map[string]any); ok {
			if status, ok := marriageMap["status"].(string); ok && status == "married" {
				badges = append(badges, "married")
			}
		}
	}

	devTeam := []string{"mist", "rotur", "flufi"}
	username := strings.ToLower(user.GetUsername())
	if slices.Contains(devTeam, username) {
		badges = append(badges, "rotur")
	}

	return badges
}
