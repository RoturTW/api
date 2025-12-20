package main

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

const BADGES_FILE_PATH = "./rotur/badges.json"

type JSONBadge struct {
	Name        string   `json:"name"`
	Icon        string   `json:"icon"`
	Description string   `json:"description"`
	Users       []string `json:"users"`
}

var (
	jsonBadges      []JSONBadge
	jsonBadgesMutex sync.RWMutex
)

func loadJSONBadges() error {
	data, err := os.ReadFile(BADGES_FILE_PATH)
	if err != nil {
		return err
	}

	var badges []JSONBadge
	if err := json.Unmarshal(data, &badges); err != nil {
		log.Printf("Error parsing badges.json: %v", err)
		log.Printf("Raw data: %s", string(data))
		return err
	}

	jsonBadgesMutex.Lock()
	jsonBadges = badges
	jsonBadgesMutex.Unlock()

	log.Printf("Successfully loaded %d badges from JSON", len(badges))
	return nil
}

func watchBadgesFile() {
	var lastMtime time.Time
	if stat, err := os.Stat(BADGES_FILE_PATH); err == nil {
		lastMtime = stat.ModTime()
	}

	for {
		time.Sleep(500 * time.Millisecond)
		if stat, err := os.Stat(BADGES_FILE_PATH); err == nil {
			if stat.ModTime().After(lastMtime) {
				time.Sleep(500 * time.Millisecond)
				log.Println("Detected change in badges.json, reloading...")
				if err := loadJSONBadges(); err != nil {
					log.Printf("Error reloading badges: %v", err)
				}
				lastMtime = stat.ModTime()
			}
		}
	}
}

func calculateUserBadges(user *User) []Badge {
	var badges []Badge

	system := user.GetSystem()
	if system != "" {
		system := getStringOrEmpty(system)
		systemsMutex.RLock()
		defer systemsMutex.RUnlock()
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

	if len(user.GetFriends()) >= 10 {
		badges = append(badges, Badge{
			Name:        "friendly",
			Icon:        "w 20 c #ffcc4d dot 0 0 c #ff7892 w 5 dot 5 0 dot -5 0 c #000 w 2.5 cutcircle 0 -1 4 18 60 cutcircle 4 2 2 0 50 cutcircle -4 2 2 0 50",
			Description: "This user has over 10 friends on Rotur",
		})
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
					Icon:        "scale 0.9 c #f33 w 3 cutcircle -4.5 4 5 -3 90 cutcircle 4.5 4 5 3 90 line -8.5 1 0 -9 line 8.5 1 0 -9 w 9 line -4.5 4 0 -1 cont 4.5 4 dot 0 -2.5",
					Description: "This user got married, how cute!",
				})
			}
		}
	}

	subscription := user.GetSubscription()
	if subscription.Tier == "Pro" || subscription.Tier == "Max" {
		badges = append(badges, Badge{
			Name:        "pro",
			Icon:        "scale 1.16 w 3 c #edb210 tri -6 -4 6 -4 0 5 c #ffc50a square 0 -5 6 1 tri -6 -4 -2 -4 -7 4 tri 6 -4 2 -4 7 4 w 2 c #a7213a tri 0 1 -2 -2 2 -2 c #c0365a tri 0 -4 -2 -2 2 -2",
			Description: "This user has a Rotur Pro subscription",
		})
	}

	username := strings.ToLower(user.GetUsername())
	jsonBadgesMutex.RLock()
	for _, jsonBadge := range jsonBadges {
		for _, badgeUser := range jsonBadge.Users {
			if strings.ToLower(badgeUser) == username {
				badges = append(badges, Badge{
					Name:        jsonBadge.Name,
					Icon:        jsonBadge.Icon,
					Description: jsonBadge.Description,
				})
				break
			}
		}
	}
	jsonBadgesMutex.RUnlock()

	return badges
}
