package main

import (
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
)

func getStatus(c *gin.Context) {
	// Calculate uptime
	uptime := time.Since(startTime).Seconds()

	// Simple load average simulation (since we removed the dependency)
	loadAverage := []float64{0.0, 0.0, 0.0}

	// Count current data
	usersMutex.RLock()
	usersCount := len(users)
	usersMutex.RUnlock()

	postsMutex.RLock()
	postsCount := len(posts)
	postsMutex.RUnlock()

	itemsMutex.RLock()
	itemsCount := len(items)
	itemsMutex.RUnlock()

	keysMutex.RLock()
	keysCount := len(keys)
	keysMutex.RUnlock()

	statusData := gin.H{
		"items":        itemsCount,
		"keys":         keysCount,
		"load_average": loadAverage,
		"posts":        postsCount,
		"status":       "ok",
		"uptime":       uptime,
		"users":        usersCount,
		"version":      "1.0.0",
	}

	c.JSON(200, statusData)
}

func getStringSlice(u User, key string) []string {
	mu := getUserMutex(u.GetUsername())
	mu.Lock()
	defer mu.Unlock()
	if v, ok := u[key]; ok {
		switch s := v.(type) {
		case []string:
			return s
		case []any:
			out := make([]string, 0, len(s))
			for _, val := range s {
				if str, ok := val.(string); ok {
					out = append(out, str)
				}
			}
			return out
		}
	}
	return []string{}
}

func isValidJSON(s string) bool {
	var js any
	return json.Unmarshal([]byte(s), &js) == nil
}
