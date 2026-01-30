package main

import (
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

func authenticateWithKey(key string) *User {
	usersMutex.RLock()
	defer usersMutex.RUnlock()

	for _, user := range users {
		if user.GetKey() == key {
			return &user
		}
	}
	return nil
}

func doesUserOwnKey(username Username, key string) bool {
	keyOwnershipCacheMutex.Lock()
	defer keyOwnershipCacheMutex.Unlock()

	userId := username.Id()

	keysMutex.RLock()
	defer keysMutex.RUnlock()

	for _, userKey := range keys {
		if userKey.Key == key {
			if _, exists := userKey.Users[userId]; exists {
				return true
			}
			break
		}
	}

	return false
}

func getKeyNextBilling(username Username, key string) int64 {
	keyOwnershipCacheMutex.Lock()
	defer keyOwnershipCacheMutex.Unlock()

	userId := username.Id()

	var success bool = keysMutex.TryRLock()
	if success {
		defer keysMutex.RUnlock()
	}

	for _, userKey := range keys {
		if userKey.Key == key {
			if _, exists := userKey.Users[userId]; exists {
				nextBilling := userKey.Users[userId].NextBilling
				if nextBilling == nil {
					return 0
				}
				switch v := nextBilling.(type) {
				case float64:
					return int64(v)
				case int64:
					return v
				case int:
					return int64(v)
				default:
					return 0
				}
			}
			break
		}
	}

	// If the key doesn't exist, return 0 to indicate that the user doesn't have a subscription
	return 0
}

func isAdmin(c *gin.Context) bool {
	envOnce.Do(loadEnvFile)
	ADMIN_TOKEN := os.Getenv("ADMIN_TOKEN")
	if ADMIN_TOKEN == "" {
		return false
	}

	authHeader := c.GetHeader("Authorization")
	var adminToken string
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		adminToken = authHeader[7:]
	} else if authHeader != "" {
		adminToken = authHeader
	}

	return adminToken == ADMIN_TOKEN
}

func authenticateAdmin(c *gin.Context) bool {
	envOnce.Do(loadEnvFile)
	ADMIN_TOKEN := os.Getenv("ADMIN_TOKEN")
	if ADMIN_TOKEN == "" {
		c.JSON(500, gin.H{"error": "ADMIN_TOKEN environment variable not set"})
		return false
	}

	authHeader := c.GetHeader("Authorization")
	var adminToken string
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		adminToken = authHeader[7:]
	} else if authHeader != "" {
		adminToken = authHeader
	}

	if adminToken != ADMIN_TOKEN {
		c.JSON(403, gin.H{"error": "Invalid admin authentication"})
		return false
	}

	return true
}
