package main

import (
	"strings"
)

func calculateUserBadges(user User) []string {
	var badges []string

	system := user.Get("system")
	if system != "" {
		badges = append(badges, getStringOrEmpty(system))
	}

	if currency := user.Get("sys.currency"); currency != nil {
		if currencyFloat, ok := currency.(float64); ok && currencyFloat >= 1000 {
			badges = append(badges, "rich")
		}
	}

	if friends := user.Get("sys.friends"); friends != nil {
		if friendsList, ok := friends.([]interface{}); ok && len(friendsList) >= 10 {
			badges = append(badges, "friendly")
		}
	}

	if discordID := user.Get("discord_id"); discordID != nil && discordID != "" {
		badges = append(badges, "discord")
	}

	if marriage := user.Get("sys.marriage"); marriage != nil {
		if marriageMap, ok := marriage.(map[string]interface{}); ok {
			if status, ok := marriageMap["status"].(string); ok && status == "married" {
				badges = append(badges, "married")
			}
		}
	}

	devTeam := []string{"mist", "rotur", "flufi"}
	username := strings.ToLower(user.GetUsername())
	for _, dev := range devTeam {
		if username == dev {
			badges = append(badges, "rotur")
			break
		}
	}

	return badges
}
