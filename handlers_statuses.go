package main

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Status data model written to disk per user
type UserStatus struct {
	Type     string            `json:"type"`              // "simple" or "activity"
	Content  *string           `json:"content,omitempty"` // for simple sentence
	Activity *StatusActivity   `json:"activity,omitempty"`
	Created  int64             `json:"created"`            // unix ms
	Expires  int64             `json:"expires"`            // unix ms
	Metadata map[string]string `json:"metadata,omitempty"` // reserved for future
}

type StatusActivity struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Image       *string `json:"image,omitempty"`
}

const statusBasePath = "/Users/admin/Documents/rotur/userdata" // base directory containing per-user folders
const statusTTL = 24 * time.Hour
const statusCleanupInterval = time.Hour

// build path to a user's status.json
func userStatusPath(username Username) string {
	name := username.ToLower().String()
	return filepath.Join(statusBasePath, name, "status.json")
}

func loadUserStatus(username Username) (*UserStatus, error) {
	path := userStatusPath(username)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New("not_found")
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, errors.New("empty")
	}
	var st UserStatus
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func saveUserStatus(username Username, st *UserStatus) error {
	dir := filepath.Dir(userStatusPath(username))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := userStatusPath(username) + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, userStatusPath(username))
}

func deleteUserStatus(username Username) {
	os.Remove(userStatusPath(username))
}

// GET /status/clear?auth=KEY
func statusClear(c *gin.Context) {
	user := c.MustGet("user").(*User)

	deleteUserStatus(user.GetUsername())
	go broadcastUserUpdate(user.GetUsername(), "status", nil)
	c.JSON(200, gin.H{"message": "status cleared"})
}

// GET /status/get?name=username
func statusGet(c *gin.Context) {
	name := Username(c.Query("name"))
	if name == "" {
		c.JSON(400, gin.H{"error": "name parameter missing"})
		return
	}

	st, err := loadUserStatus(name)
	if err != nil {
		c.JSON(404, gin.H{"error": "no status"})
		return
	}

	now := time.Now().UnixMilli()
	if now >= st.Expires { // expired
		deleteUserStatus(name)
		c.JSON(404, gin.H{"error": "no status"})
		return
	}

	remaining := st.Expires - now
	c.JSON(200, gin.H{
		"username":          name.ToLower(),
		"status":            st,
		"time_remaining_ms": remaining,
	})
}

// GET /status/update?auth=KEY&content=... OR activity params
// activity_name= & activity_desc= & activity_image=
func statusUpdate(c *gin.Context) {
	user := c.MustGet("user").(*User)

	content := strings.TrimSpace(c.Query("content"))
	actName := strings.TrimSpace(c.Query("activity_name"))
	actDesc := strings.TrimSpace(c.Query("activity_desc"))
	actImage := strings.TrimSpace(c.Query("activity_image"))

	if content == "" && actName == "" {
		c.JSON(400, gin.H{"error": "either content or activity_name required"})
		return
	}
	if content != "" && actName != "" {
		c.JSON(400, gin.H{"error": "cannot set both simple content and activity simultaneously"})
		return
	}

	if content != "" {
		if len(content) > 250 {
			c.JSON(400, gin.H{"error": "content length exceeds 250 characters"})
			return
		}
		if containsDerogatory(content) {
			c.JSON(400, gin.H{"error": "content contains prohibited language"})
			return
		}
	}

	var activity *StatusActivity
	if actName != "" {
		if len(actName) > 100 {
			c.JSON(400, gin.H{"error": "activity_name exceeds 100 characters"})
			return
		}
		if actDesc != "" && len(actDesc) > 500 {
			c.JSON(400, gin.H{"error": "activity_desc exceeds 500 characters"})
			return
		}
		if actImage != "" && !(strings.HasPrefix(actImage, "http://") || strings.HasPrefix(actImage, "https://")) {
			c.JSON(400, gin.H{"error": "activity_image must be a valid URL"})
			return
		}
		if actImage != "" && len(actImage) > 500 {
			c.JSON(400, gin.H{"error": "activity_image exceeds 500 characters"})
			return
		}
		// Basic banned domain check reuse (optional)
		if actImage != "" && isFromBannedDomain(actImage) {
			c.JSON(400, gin.H{"error": "activity_image from prohibited domain"})
			return
		}
		activity = &StatusActivity{Name: actName}
		if actDesc != "" {
			activity.Description = &actDesc
		}
		if actImage != "" {
			activity.Image = &actImage
		}
	}

	now := time.Now().UnixMilli()
	st := &UserStatus{
		Type: func() string {
			if activity != nil {
				return "activity"
			}
			return "simple"
		}(),
		Created: now,
		Expires: now + int64(statusTTL/time.Millisecond),
	}
	if content != "" {
		st.Content = &content
	}
	if activity != nil {
		st.Activity = activity
	}

	if err := saveUserStatus(user.GetUsername(), st); err != nil {
		c.JSON(500, gin.H{"error": "failed to save status"})
		return
	}

	remaining := st.Expires - now
	c.JSON(200, gin.H{"message": "status updated", "status": st, "time_remaining_ms": remaining})
}

func cleanExpiredStatuses() {
	ticker := time.NewTicker(statusCleanupInterval)
	defer ticker.Stop()

	runStatusCleanup()

	for range ticker.C {
		runStatusCleanup()
	}
}

func runStatusCleanup() {
	entries, err := os.ReadDir(statusBasePath)
	if err != nil {
		return
	}
	now := time.Now().UnixMilli()
	removed := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		username := Username(e.Name())
		st, err := loadUserStatus(username)
		if err != nil {
			continue
		}
		if now >= st.Expires {
			deleteUserStatus(username)
			go broadcastUserUpdate(username, "status", nil)
			removed++
		}
	}
	if removed > 0 {
		log.Printf("Status cleanup: removed %d expired statuses", removed)
	}
}
