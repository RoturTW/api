package main

import (
	"strings"

	"github.com/gin-gonic/gin"
)

func getBlocking(c *gin.Context) {
	user := c.MustGet("user").(*User)

	usersMutex.RLock()
	defer usersMutex.RUnlock()

	if user.Has("sys.blocked") {
		c.JSON(200, user.Get("sys.blocked"))
		return
	}

	c.JSON(200, []string{})
}

func blockUser(c *gin.Context) {
	user := c.MustGet("user").(*User)

	username := c.Param("username")
	if username == "" {
		c.JSON(400, gin.H{"error": "Username is required"})
		return
	}

	usernameLower := strings.ToLower(username)

	if !user.Has("sys.blocked") {
		user.Set("sys.blocked", []string{})
	}

	blocked := user.Get("sys.blocked").([]string)
	for i, blockedUser := range blocked {
		if blockedUser == usernameLower {
			blocked = append(blocked[:i], blocked[i+1:]...)
			break
		}
	}
	blocked = append(blocked, usernameLower)

	go broadcastUserUpdate(username, "sys.blocked", blocked)
	go saveUsers()

	c.JSON(200, gin.H{"message": "User blocked"})
}

func unblockUser(c *gin.Context) {
	user := c.MustGet("user").(*User)

	username := c.Param("username")
	if username == "" {
		c.JSON(400, gin.H{"error": "Username is required"})
		return
	}

	usernameLower := strings.ToLower(username)

	if !user.Has("sys.blocked") {
		user.Set("sys.blocked", []string{})
	}

	blocked := user.Get("sys.blocked").([]string)
	for i, blockedUser := range blocked {
		if blockedUser == usernameLower {
			blocked = append(blocked[:i], blocked[i+1:]...)
			break
		}
	}

	go broadcastUserUpdate(username, "sys.blocked", blocked)
	go saveUsers()

	c.JSON(200, gin.H{"message": "User unblocked"})
}
