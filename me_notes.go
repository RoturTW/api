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

	usersMutex.Lock()
	notes := user.GetNotes()

	notes[username] = noteContent

	user.Set("sys.notes", notes)
	usersMutex.Unlock()

	saveUsers()

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

	usersMutex.Lock()
	notes := user.GetNotes()
	delete(notes, username)
	user.Set("sys.notes", notes)
	usersMutex.Unlock()

	saveUsers()

	c.JSON(200, gin.H{"success": true})
}
