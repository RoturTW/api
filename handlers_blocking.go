package main

import (
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
)

func getBlocking(c *gin.Context) {
	user := c.MustGet("user").(*User)

	c.JSON(200, user.GetBlocked())
}

func blockUser(c *gin.Context) {
	user := c.MustGet("user").(*User)
	username := strings.ToLower(c.Param("username"))

	if username == "" {
		c.JSON(400, gin.H{"error": "Username is required"})
		return
	}

	if !accountExists(username) {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}

	if strings.EqualFold(user.GetUsername(), username) {
		c.JSON(400, gin.H{"error": "Cannot block yourself"})
		return
	}

	// Acquire user mutex to protect the read-modify-write operation
	mu := getUserMutex(user.GetUsername())
	mu.Lock()
	blocked := getStringSliceDirect(*user, "sys.blocked")
	if slices.Contains(blocked, username) {
		mu.Unlock()
		c.JSON(400, gin.H{"error": "User already blocked"})
		return
	}

	blocked = append(blocked, username)
	setUserKeyDirectInternal(*user, "sys.blocked", blocked)
	mu.Unlock()

	go broadcastUserUpdate(user.GetUsername(), "sys.blocked", blocked)
	go saveUsers()

	c.JSON(200, gin.H{"message": "User blocked"})
}

func unblockUser(c *gin.Context) {
	user := c.MustGet("user").(*User)
	username := strings.ToLower(c.Param("username"))

	if username == "" {
		c.JSON(400, gin.H{"error": "Username is required"})
		return
	}

	// Acquire user mutex to protect the read-modify-write operation
	mu := getUserMutex(user.GetUsername())
	mu.Lock()
	blocked := getStringSliceDirect(*user, "sys.blocked")
	index := -1
	for i, b := range blocked {
		if b == username {
			index = i
			break
		}
	}

	if index == -1 {
		mu.Unlock()
		c.JSON(404, gin.H{"error": "User not blocked"})
		return
	}

	blocked = append(blocked[:index], blocked[index+1:]...)
	setUserKeyDirectInternal(*user, "sys.blocked", blocked)
	mu.Unlock()

	go broadcastUserUpdate(user.GetUsername(), "sys.blocked", blocked)
	go saveUsers()

	c.JSON(200, gin.H{"message": "User unblocked"})
}
