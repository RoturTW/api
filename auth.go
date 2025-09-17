package main

import (
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

func authenticateWithKey(key string) *User {
	usersMutex.RLock()
	defer usersMutex.RUnlock()

	for i := range users {
		if users[i].GetKey() == key {
			return &users[i]
		}
	}
	return nil
}

func doesUserOwnKey(username string, key string) bool {
	keyOwnershipCacheMutex.Lock()
	defer keyOwnershipCacheMutex.Unlock()

	username = strings.ToLower(username)

	keysMutex.RLock()
	defer keysMutex.RUnlock()

	for _, userKey := range keys {
		if userKey.Key == key {
			if _, exists := userKey.Users[username]; exists {
				return true
			}
			break
		}
	}

	return false
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
