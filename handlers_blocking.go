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

	blocked := user.GetBlocked()
	if slices.Contains(blocked, username) {
		c.JSON(400, gin.H{"error": "User already blocked"})
		return
	}

	user.AddBlocked(username)

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

	blocked := user.GetBlocked()
	index := -1
	for i, b := range blocked {
		if b == username {
			index = i
			break
		}
	}

	if index == -1 {
		c.JSON(404, gin.H{"error": "User not blocked"})
		return
	}

	user.RemoveBlocked(username)

	go saveUsers()

	c.JSON(200, gin.H{"message": "User unblocked"})
}
