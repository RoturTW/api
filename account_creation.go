package main

import (
	"crypto/sha256"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/google/uuid"
)

type AccountCreateInput struct {
	Username string
	Password string
	Email    string
	System   System

	Provider      string
	RequestIP     string
	RequestOrigin string
	ExtraSys      map[string]any
}

func createAccount(in AccountCreateInput) (User, error) {
	usernameLower := strings.ToLower(in.Username)
	if usernameLower == "" {
		return nil, fmt.Errorf("username is required")
	}
	if in.Email == "" {
		return nil, fmt.Errorf("email is required")
	}
	if in.System.Name == "" {
		return nil, fmt.Errorf("system is required")
	}

	usersMutex.Lock()
	defer usersMutex.Unlock()

	for _, user := range users {
		if strings.EqualFold(user.GetUsername(), usernameLower) {
			return nil, fmt.Errorf("username already in use")
		}
		if strings.EqualFold(user.GetEmail(), in.Email) {
			return nil, fmt.Errorf("email already in use")
		}
	}

	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(in.RequestIP)))
	provider := in.Provider
	if provider == "" {
		provider = "unknown"
	}
	origin := in.RequestOrigin
	if origin == "" {
		origin = "unknown"
	}
	go sendDiscordWebhook([]map[string]any{
		{
			"title": "New Account Registered",
			"description": fmt.Sprintf("**Username:** %s\n**Email:** %s\n**System:** %s\n**Provider:** %s\n**IP:** %s\n**Host:** %s",
				in.Username, in.Email, in.System.Name, provider, hash, origin),
			"color":     0x57cdac,
			"timestamp": time.Now().Format(time.RFC3339),
		},
	})

	newUser := User{
		"username":         in.Username,
		"pfp":              "https://avatars.rotur.dev/" + usernameLower,
		"password":         in.Password,
		"email":            in.Email,
		"key":              generateAccountToken(),
		"system":           in.System.Name,
		"max_size":         5000000,
		"sys.last_login":   time.Now().UnixMilli(),
		"sys.total_logins": 0,
		"sys.friends":      []string{},
		"sys.requests":     []string{},
		"sys.links":        []map[string]any{},
		"sys.currency":     float64(0),
		"sys.transactions": []any{},
		"sys.items":        []any{},
		"sys.badges":       []string{},
		"sys.purchases":    []any{},
		"private":          false,
		"sys.id":           uuid.New().String(),
		"theme": map[string]any{
			"primary":    "#222",
			"secondary":  "#555",
			"tertiary":   "#777",
			"text":       "#fff",
			"background": "#050505",
			"accent":     "#57cdac",
		},
		"onboot": []string{
			"Origin/(A) System/System Apps/originWM.osl",
			"Origin/(A) System/System Apps/Desktop.osl",
			"Origin/(A) System/Docks/Dock.osl",
			"Origin/(A) System/System Apps/Quick_Settings.osl",
		},
		"created":          time.Now().UnixMilli(),
		"wallpaper":        in.System.Wallpaper,
		"sys.tos_accepted": false,
	}

	if in.ExtraSys != nil {
		maps.Copy(newUser, in.ExtraSys)
	}

	users = append(users, newUser)
	go saveUsers()
	return newUser, nil
}
