package main

import "github.com/gin-gonic/gin"

func noteUser(c *gin.Context) {
	username := c.Param("username")
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

	if len(noteContent) > 300 {
		c.JSON(400, gin.H{"error": "Note content is too long"})
		return
	}

	// No need for usersMutex - GetNotes and Set use getUserMutex
	notes := user.GetNotes()

	notes[username] = noteContent

	user.Set("sys.notes", notes)

	go saveUsers()

	c.JSON(200, gin.H{"success": true})
}

func deleteNote(c *gin.Context) {
	username := c.Param("username")
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

	// GetNotes and Set use getUserMutex for safe concurrent access
	notes := user.GetNotes()
	// Create a new map to avoid modifying the returned map directly
	updatedNotes := make(map[string]string, len(notes))
	for k, v := range notes {
		if k != username {
			updatedNotes[k] = v
		}
	}
	user.Set("sys.notes", updatedNotes)

	go saveUsers()

	c.JSON(200, gin.H{"success": true})
}
