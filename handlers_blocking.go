package main

import (
	"slices"

	"github.com/gin-gonic/gin"
)

func getBlocking(c *gin.Context) {
	user := c.MustGet("user").(*User)

	c.JSON(200, user.GetBlockedUsers())
}

func blockUser(c *gin.Context) {
	user := c.MustGet("user").(*User)
	userId := Username(c.Param("username")).Id()

	if userId == "" {
		c.JSON(400, gin.H{"error": "Username is required"})
		return
	}

	if !accountExists(userId) {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}

	if user.GetId() == userId {
		c.JSON(400, gin.H{"error": "Cannot block yourself"})
		return
	}

	blocked := user.GetBlocked()
	if slices.Contains(blocked, userId) {
		c.JSON(400, gin.H{"error": "User already blocked"})
		return
	}

	user.AddBlocked(userId)

	go saveUsers()

	c.JSON(200, gin.H{"message": "User blocked"})
}

func unblockUser(c *gin.Context) {
	user := c.MustGet("user").(*User)
	userId := Username(c.Param("username")).Id()

	if userId == "" {
		c.JSON(400, gin.H{"error": "Username is required"})
		return
	}

	blocked := user.GetBlocked()
	index := -1
	for i, b := range blocked {
		if b == userId {
			index = i
			break
		}
	}

	if index == -1 {
		c.JSON(404, gin.H{"error": "User not blocked"})
		return
	}

	user.RemoveBlocked(userId)

	go saveUsers()

	c.JSON(200, gin.H{"message": "User unblocked"})
}
