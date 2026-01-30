package main

import "github.com/gin-gonic/gin"

func noteUser(c *gin.Context) {
	username := Username(c.Param("username"))
	if username == "" {
		c.JSON(400, gin.H{"error": "Username is required"})
		return
	}

	authKey := c.Query("auth")
	if authKey == "" {
		c.JSON(400, gin.H{"error": "Authentication key is required"})
		return
	}

	noteContent := c.Query("note")
	if noteContent == "" {
		c.JSON(400, gin.H{"error": "Note content is required"})
		return
	}

	user := authenticateWithKey(authKey)
	if user == nil {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		return
	}

	err := user.SetNote(username, noteContent)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	go saveUsers()

	c.JSON(200, gin.H{"success": true})
}

func deleteNote(c *gin.Context) {
	username := Username(c.Param("username"))
	if username == "" {
		c.JSON(400, gin.H{"error": "Username is required"})
		return
	}

	authKey := c.Query("auth")
	if authKey == "" {
		c.JSON(400, gin.H{"error": "Authentication key is required"})
		return
	}

	user := authenticateWithKey(authKey)
	if user == nil {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		return
	}

	user.RemoveNote(username)

	go saveUsers()

	c.JSON(200, gin.H{"success": true})
}
