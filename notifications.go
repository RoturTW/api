package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func getNotifications(c *gin.Context) {
	user := c.MustGet("user").(*User)

	timePeriod := 1
	if timePeriodStr := c.Query("after"); timePeriodStr != "" {
		if parsed, err := strconv.Atoi(timePeriodStr); err == nil && parsed >= 1 {
			timePeriod = parsed
		} else {
			c.JSON(400, gin.H{"error": "Invalid time period"})
			return
		}
	}

	username := strings.ToLower(user.GetUsername())
	currentTime := time.Now().UnixMilli()
	cutoffTime := currentTime - int64(timePeriod*24*60*60*1000)

	notifications := make([]map[string]any, 0)

	eventsHistoryMutex.RLock()
	userEvents, exists := eventsHistory[username]
	eventsHistoryMutex.RUnlock()

	if exists {
		for _, event := range userEvents {
			if event.Timestamp >= cutoffTime {
				notification := map[string]any{
					"type":      event.Type,
					"id":        event.ID,
					"timestamp": event.Timestamp,
				}
				maps.Copy(notification, event.Data)
				notifications = append(notifications, notification)
			}
		}
	}

	sort.Slice(notifications, func(i, j int) bool {
		return notifications[i]["timestamp"].(int64) > notifications[j]["timestamp"].(int64)
	})

	c.JSON(200, notifications)
}

func makeHTTPRequest(method, url string, payload any, timeout time.Duration, logPrefix string, expectedStatusCode int) bool {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[%s] Error marshaling payload: %v", logPrefix, err)
		return false
	}

	client := &http.Client{Timeout: timeout}

	var resp *http.Response
	switch method {
	case "POST":
		resp, err = client.Post(url, "application/json", bytes.NewBuffer(jsonData))
	case "PATCH":
		req, reqErr := http.NewRequest("PATCH", url, bytes.NewBuffer(jsonData))
		if reqErr != nil {
			log.Printf("[%s] Error creating %s request: %v", logPrefix, method, reqErr)
			return false
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err = client.Do(req)
	default:
		log.Printf("[%s] Unsupported HTTP method: %s", logPrefix, method)
		return false
	}

	if err != nil {
		log.Printf("[%s] Error sending %s request: %v", logPrefix, method, err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode == expectedStatusCode {
		return true
	} else {
		log.Printf("[%s] Request failed with status: %d", logPrefix, resp.StatusCode)
		return false
	}
}

func createEventPayload(eventType string, data any) map[string]any {
	return map[string]any{
		"event_type": eventType,
		"data":       data,
		"from":       "rotur",
	}
}

func broadcastClawEvent(eventType string, data any) bool {
	payload := createEventPayload(eventType, data)
	return makeHTTPRequest("POST", WEBSOCKET_SERVER_URL, payload, 2*time.Second, "WebSocket", 200)
}

func sendPostToDiscord(postData Post) {
	username := postData.User
	if username == "" {
		username = "Unknown User"
	}

	webhookData := map[string]any{
		"username":   username,
		"avatar_url": fmt.Sprintf("https://avatars.rotur.dev/%s", username),
		"content":    postData.Content,
	}

	success := makeHTTPRequest("POST", DISCORD_WEBHOOK_URL, webhookData, 5*time.Second, "Discord", 204)
	if success {
		log.Printf("[Discord] Post %s sent to Discord successfully", postData.ID)
	}
}

func notify(eventType string, data any) bool {
	payload := createEventPayload(eventType, data)
	success := makeHTTPRequest("POST", EVENT_SERVER_URL, payload, 5*time.Second, "Event", 200)
	if success {
		log.Printf("[Event] Event %s sent to Event server successfully", eventType)
	}
	return success
}

// patchUserUpdate makes a PATCH request to /users endpoint for user updates
func patchUserUpdate(username, key string, value any) bool {
	// Find the user's auth key
	usersMutex.RLock()
	var authKey string
	for _, user := range users {
		if strings.EqualFold(user.GetUsername(), username) {
			authKey = user.GetKey()
			break
		}
	}
	usersMutex.RUnlock()

	if authKey == "" {
		log.Printf("[PatchUpdate] User %s not found or has no auth key", username)
		return false
	}

	payload := map[string]any{
		"auth":  authKey,
		"key":   key,
		"value": value,
	}

	envOnce.Do(loadEnvFile)
	ADMIN_TOKEN := os.Getenv("ADMIN_TOKEN")

	// Avatar/banner uploads may take several seconds; allow up to 15s
	success := makeHTTPRequest("PATCH", "http://localhost:5602/users?token="+ADMIN_TOKEN, payload, 15*time.Second, "PatchUpdate", 200)
	if success {
		log.Printf("[PatchUpdate] User %s key %s updated successfully via PATCH", username, key)
		return broadcastUserUpdate(username, key, value)
	}
	return false
}

func broadcastUserUpdate(username, key string, value any) bool {
	mu := getUserMutex(username)
	mu.Lock()
	defer mu.Unlock()
	payload := createEventPayload("user_account_update", map[string]any{
		"username": username,
		"key":      key,
		"value":    value,
		"rotur":    username,
	})

	success := makeHTTPRequest("POST", EVENT_SERVER_URL, payload, 2*time.Second, "UserUpdate", 200)
	return success
}

func addUserEvent(username, eventType string, data map[string]any) Event {
	eventsHistoryMutex.Lock()
	defer eventsHistoryMutex.Unlock()

	switch eventType {
	case "follow":
		go broadcastClawEvent("followers", map[string]any{
			"username":  username,
			"followers": len(data["followers"].([]string)),
		})
	case "reply":
		post_id := data["post_id"].(string)
		post := getPostById(post_id)
		go broadcastClawEvent("update_post", map[string]any{
			"id":   post_id,
			"key":  "replies",
			"data": post.Replies,
		})
	}

	username = strings.ToLower(username)

	if eventsHistory[username] == nil {
		eventsHistory[username] = make([]Event, 0)
	}

	newEvent := Event{
		Type:      eventType,
		Data:      data,
		Timestamp: time.Now().UnixMilli(),
		ID:        generateShortToken(),
	}

	eventsHistory[username] = append(eventsHistory[username], newEvent)

	if len(eventsHistory[username]) > 100 {
		eventsHistory[username] = eventsHistory[username][len(eventsHistory[username])-100:]
	}

	go saveEventsHistory()

	return newEvent
}
