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

	foundUsers, err := getAccountsBy("username", targetLower.String(), 1)
	if err != nil {
		c.JSON(404, gin.H{"error": "Account Does Not Exist"})
		return
	}
	target := foundUsers[0]
	if isUserBlockedBy(target, senderId) {
		c.JSON(400, gin.H{"error": "You cant send friend requests to this user"})
		return
	}

	for sender.IsFriend(targetUsername) {
		c.JSON(400, gin.H{"error": "Already Friends"})
		return
	}
	// if we find the sender in the target's friends list,
	// we add them automatically because they arent friends with each other
	if target.IsFriend(senderName) {
		sender.AddFriend(targetUsername)
		c.JSON(400, gin.H{"error": "Already Friends"})
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

	foundUsers, err := getAccountsBy("username", requesterName.String(), 1)
	if err != nil {
		c.JSON(404, gin.H{"error": "Account Does Not Exist"})
		return
	}

	requester := foundUsers[0]
	found := current.RemoveRequest(requesterName)
	if !found {
		c.JSON(400, gin.H{"error": "No Pending Request"})
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

	foundUsers, err := getAccountsBy("username", otherName.String(), 1)
	if err != nil {
		c.JSON(404, gin.H{"error": "Account Does Not Exist"})
		return
	}
	other := foundUsers[0]

	if !current.IsFriend(otherName) {
		c.JSON(400, gin.H{"error": "Not Friends"})
		return
	}

	current.RemoveFriend(otherName)
	other.RemoveFriend(currentName)

	go saveUsers()

	c.JSON(200, gin.H{"message": "Friend removed"})
}

// GET /friends
func getFriends(c *gin.Context) {
	user := c.MustGet("user").(*User)

	c.JSON(200, gin.H{"friends": user.GetFriendUsers()})
}
