package main

import (
	"github.com/gin-gonic/gin"
)

// POST /friends/request/:username
func sendFriendRequest(c *gin.Context) {
	sender := c.MustGet("user").(*User)

	targetUsername := Username(c.Param("username"))
	if targetUsername == "" {
		c.JSON(400, gin.H{"error": "Username cannot be empty"})
		return
	}

	senderName := sender.GetUsername().ToLower()
	senderId := sender.GetId()
	targetLower := targetUsername.ToLower()

	if senderName == targetLower {
		c.JSON(400, gin.H{"error": "You need other friends"})
		return
	}

	target, err := getAccountByUsername(targetLower)
	if err != nil {
		c.JSON(404, gin.H{"error": "Account Does Not Exist"})
		return
	}
	if isUserBlockedBy(target, senderId) {
		c.JSON(400, gin.H{"error": "You cant send friend requests to this user"})
		return
	}

	if sender.IsFriend(targetLower) {
		c.JSON(400, gin.H{"error": "Already Friends"})
		return
	}
	// target already considers sender a friend (one-sided) — auto-accept both ways
	if target.IsFriend(senderName) {
		sender.AddFriend(targetLower)
		sender.RemoveRequest(targetLower)
		go saveUsers()
		c.JSON(200, gin.H{"message": "Friend request accepted automatically"})
		return
	}
	if target.HasRequest(senderName) {
		c.JSON(400, gin.H{"error": "Already Requested"})
		return
	}

	target.AddRequest(senderName)

	go saveUsers()

	c.JSON(200, gin.H{"message": "Friend request sent successfully"})
}

// POST /friends/accept/:username  (username = original requester)
func acceptFriendRequest(c *gin.Context) {
	current := c.MustGet("user").(*User)
	requesterName := Username(c.Param("username")).ToLower()
	if requesterName == "" {
		c.JSON(400, gin.H{"error": "Username cannot be empty"})
		return
	}

	currentName := current.GetUsername().ToLower()
	if currentName == requesterName {
		c.JSON(400, gin.H{"error": "Invalid Operation"})
		return
	}

	requester, err := getAccountByUsername(requesterName)
	if err != nil {
		c.JSON(404, gin.H{"error": "Account Does Not Exist"})
		return
	}

	found := current.RemoveRequest(requesterName)
	if !found {
		c.JSON(400, gin.H{"error": "No Pending Request"})
		return
	}

	if current.IsFriend(requesterName) {
		// stale request — already friends, just discard it
		go saveUsers()
		c.JSON(200, gin.H{"message": "Friend request accepted"})
		return
	}

	current.AddFriend(requesterName)
	requester.AddFriend(currentName)

	go saveUsers()

	c.JSON(200, gin.H{"message": "Friend request accepted"})
}

// POST /friends/reject/:username
func rejectFriendRequest(c *gin.Context) {
	current := c.MustGet("user").(*User)
	requesterName := Username(c.Param("username")).ToLower()
	if requesterName == "" {
		c.JSON(400, gin.H{"error": "Username cannot be empty"})
		return
	}

	found := current.RemoveRequest(requesterName)
	if !found {
		c.JSON(400, gin.H{"error": "No Pending Request"})
		return
	}

	go saveUsers()

	c.JSON(200, gin.H{"message": "Friend request rejected"})
}

// POST /friends/remove/:username
func removeFriend(c *gin.Context) {
	current := c.MustGet("user").(*User)
	otherName := Username(c.Param("username")).ToLower()
	if otherName == "" {
		c.JSON(400, gin.H{"error": "Username cannot be empty"})
		return
	}

	currentName := current.GetUsername().ToLower()
	if currentName == otherName {
		c.JSON(400, gin.H{"error": "Cannot Remove Yourself"})
		return
	}

	other, err := getAccountByUsername(otherName)
	if err != nil {
		c.JSON(404, gin.H{"error": "Account Does Not Exist"})
		return
	}

	// clean up friends and pending requests on both sides
	removedCurrent := current.RemoveFriend(otherName)
	current.RemoveRequest(otherName)
	other.RemoveFriend(currentName)
	other.RemoveRequest(currentName)

	if !removedCurrent {
		c.JSON(400, gin.H{"error": "Not Friends"})
		return
	}

	go saveUsers()

	c.JSON(200, gin.H{"message": "Friend removed"})
}

// GET /friends
func getFriends(c *gin.Context) {
	user := c.MustGet("user").(*User)

	c.JSON(200, gin.H{"friends": user.GetFriendUsers()})
}
