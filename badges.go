package main

import (
	"slices"
	"strings"
)

func calculateUserBadges(user User) []Badge {
	var badges []Badge

	system := user.Get("system")
	if system != "" {
		system := getStringOrEmpty(system)
		systemData, ok := systems[system]
		if ok {
			if systemData.Owner.Name != "" {
				badges = append(badges, Badge{
					Name:        systemData.Name,
					Icon:        getStringOrEmpty(systemData.Icon),
					Description: "This account was created on " + systemData.Name,
				})
			}
		}
	}

	currency := user.GetCredits()
	if currency >= 1000 {
		badges = append(badges, Badge{
			Name:        "rich",
			Icon:        "c #DAF0F2 w 3 line 3 5 -3 5 cont -6 2 cont 0 -5 cont 6 2 cont 3 5 w 9.5 dot 0 0",
			Description: "This user has over 1k Rotur Credits",
		})
	}

	if friends := user.Get("sys.friends"); friends != nil {
		if friendsList, ok := friends.([]any); ok && len(friendsList) >= 10 {
			badges = append(badges, Badge{
				Name:        "friendly",
				Icon:        "w 20 c #ffcc4d dot 0 0 c #ff7892 w 5 dot 5 0 dot -5 0 c #000 w 2.5 cutcircle 0 -1 4 18 60 cutcircle 4 2 2 0 50 cutcircle -4 2 2 0 50",
				Description: "This user has over 10 friends on Rotur",
			})
		}
	}

	if discordID := user.Get("discord_id"); discordID != nil && discordID != "" {
		badges = append(badges, Badge{
			Name:        "discord",
			Icon:        "c #5865f2 w 20 dot 0 0 c #fff w 3 line 3 2 5 -2 cont 3 -3 cont 2 -2 cont -2 -2 cont -3 -3 cont -5 -2 cont -3 2.5 cont -2 2.5 cont -1 1.7 cont 1 1.7 cont 2 2.5 cont 3 2.5 cont 5 -2 w 4.5 line -2 0 2 0 c #5865f2 w 2.5 dot 2 -1 dot -2 -1",
			Description: "This user linked their Discord account to Rotur",
		})
	}

	if marriage := user.Get("sys.marriage"); marriage != nil {
		if marriageMap, ok := marriage.(map[string]any); ok {
			if status, ok := marriageMap["status"].(string); ok && status == "married" {
				badges = append(badges, Badge{
					Name:        "married",
					Icon:        "c #f33 w 3 cutcircle -4.5 4 5 -3 90 cutcircle 4.5 4 5 3 90 line -8.5 1 0 -9 line 8.5 1 0 -9 w 9 line -4.5 4 0 -1 cont 4.5 4 dot 0 -2.5",
					Description: "This user got married, how cute!",
				})
			}
		}
	}

	devTeam := []string{"mist", "flufi", "iris", "mikedev", "b1j2754"}
	username := strings.ToLower(user.GetUsername())
	if slices.Contains(devTeam, username) {
		badges = append(badges, Badge{
			Name:        "dev",
			Icon:        "c #3f2f3c w 22 dot 0 0 c #000 w 19 dot 0 0 c #fff w 1 ellipse 0 0 9 0.45 100 ellipse 0 0 9 0.45 160 ellipse 0 0 9 0.45 220",
			Description: "This user is part of the Rotur dev team",
		})
	}

	subscription := user.GetSubscription()
	if subscription.Tier == "Pro" || subscription.Tier == "Max" {
		badges = append(badges, Badge{
			Name:        "pro",
			Icon:        "c #8A2BE2 w 22 dot 0 0 c #4B0082 w 19 dot 0 0 c #FFF w 1 ellipse 0 0 9 0.45 100 ellipse 0 0 9 0.45 160 ellipse 0 0 9 0.45 220",
			Description: "This user has a Rotur Pro subscription",
		})
	}

	return badges
}
