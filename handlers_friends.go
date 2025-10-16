package main

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// POST /friends/request/:username
func sendFriendRequest(c *gin.Context) {
	sender := c.MustGet("user").(*User)

	targetUsername := c.Param("username")
	if targetUsername == "" {
		c.JSON(400, gin.H{"error": "Username cannot be empty"})
		return
	}

	senderName := strings.ToLower(sender.GetUsername())
	targetLower := strings.ToLower(targetUsername)

	if senderName == targetLower {
		c.JSON(400, gin.H{"error": "You Need Other Friends"})
		return
	}

	usersMutex.Lock()

	idx := getIdxOfAccountBy("username", targetLower)
	if idx == -1 {
		usersMutex.Unlock()
		c.JSON(404, gin.H{"error": "Account Does Not Exist"})
		return
	}
	var target User = users[idx]

	senderFriends := sender.GetFriends()
	targetFriends := target.GetFriends()
	targetRequests := target.GetRequests()

	for _, f := range senderFriends {
		if strings.ToLower(f) == targetLower {
			usersMutex.Unlock()
			c.JSON(400, gin.H{"error": "Already Friends"})
			return
		}
	}
	for _, f := range targetFriends {
		if strings.ToLower(f) == senderName {
			usersMutex.Unlock()
			c.JSON(400, gin.H{"error": "Already Friends"})
			return
		}
	}
	for _, r := range targetRequests {
		if strings.ToLower(r) == senderName {
			usersMutex.Unlock()
			c.JSON(400, gin.H{"error": "Already Requested"})
			return
		}
	}

	targetRequests = append(targetRequests, senderName)
	target.SetRequests(targetRequests)
	sender.SetFriends(senderFriends)

	usersMutex.Unlock()
	go saveUsers()

	c.JSON(200, gin.H{"message": "Friend request sent successfully"})
}

// POST /friends/accept/:username  (username = original requester)
func acceptFriendRequest(c *gin.Context) {
	current := c.MustGet("user").(*User)
	requesterName := strings.ToLower(c.Param("username"))
	if requesterName == "" {
		c.JSON(400, gin.H{"error": "Username cannot be empty"})
		return
	}

	currentName := strings.ToLower(current.GetUsername())
	if currentName == requesterName {
		c.JSON(400, gin.H{"error": "Invalid Operation"})
		return
	}

	idx := getIdxOfAccountBy("username", requesterName)
	if idx == -1 {
		c.JSON(404, gin.H{"error": "Account Does Not Exist"})
		return
	}
	var requester User = users[idx]
	usersMutex.Lock()

	currentRequests := current.GetRequests()
	found := false
	newRequests := make([]string, 0, len(currentRequests))
	for _, r := range currentRequests {
		if strings.ToLower(r) == requesterName {
			found = true
			continue
		}
		newRequests = append(newRequests, r)
	}
	if !found {
		usersMutex.Unlock()
		c.JSON(400, gin.H{"error": "No Pending Request"})
		return
	}

	currentFriends := current.GetFriends()
	alreadyFriends := false
	for _, f := range currentFriends {
		if strings.ToLower(f) == requesterName {
			alreadyFriends = true
			break
		}
	}
	if !alreadyFriends {
		currentFriends = append(currentFriends, requesterName)
	}

	requesterFriends := requester.GetFriends()
	requesterAlreadyHas := false
	for _, f := range requesterFriends {
		if strings.ToLower(f) == currentName {
			requesterAlreadyHas = true
			break
		}
	}
	if !requesterAlreadyHas {
		requesterFriends = append(requesterFriends, currentName)
	}

	usersMutex.Unlock()

	current.SetRequests(newRequests)
	current.SetFriends(currentFriends)
	requester.SetFriends(requesterFriends)

	go saveUsers()

	c.JSON(200, gin.H{"message": "Friend request accepted"})
}

// POST /friends/reject/:username
func rejectFriendRequest(c *gin.Context) {
	current := c.MustGet("user").(*User)
	requesterName := strings.ToLower(c.Param("username"))
	if requesterName == "" {
		c.JSON(400, gin.H{"error": "Username cannot be empty"})
		return
	}

	usersMutex.Lock()

	currentRequests := getStringSlice(*current, "sys.requests")
	found := false
	newRequests := make([]string, 0, len(currentRequests))
	for _, r := range currentRequests {
		if strings.ToLower(r) == requesterName {
			found = true
			continue
		}
		newRequests = append(newRequests, r)
	}
	if !found {
		usersMutex.Unlock()
		c.JSON(400, gin.H{"error": "No Pending Request"})
		return
	}

	setStringSlice(*current, "sys.requests", newRequests)

	usersMutex.Unlock()
	go saveUsers()

	// Broadcast the user account update for the current user
	go broadcastUserUpdate(strings.ToLower(current.GetUsername()), "sys.requests", newRequests)

	c.JSON(200, gin.H{"message": "Friend request rejected"})
}

// POST /friends/remove/:username
func removeFriend(c *gin.Context) {
	current := c.MustGet("user").(*User)
	otherName := strings.ToLower(c.Param("username"))
	if otherName == "" {
		c.JSON(400, gin.H{"error": "Username cannot be empty"})
		return
	}

	currentName := strings.ToLower(current.GetUsername())
	if currentName == otherName {
		c.JSON(400, gin.H{"error": "Cannot Remove Yourself"})
		return
	}

	idx := getIdxOfAccountBy("username", otherName)
	if idx == -1 {
		c.JSON(404, gin.H{"error": "Account Does Not Exist"})
		return
	}
	var other User = users[idx]

	currentFriends := current.GetFriends()
	otherFriends := other.GetFriends()

	isFriend := false
	for _, f := range currentFriends {
		if strings.ToLower(f) == otherName {
			isFriend = true
			break
		}
	}
	if !isFriend {
		c.JSON(400, gin.H{"error": "Not Friends"})
		return
	}

	newCurrentFriends := make([]string, 0, len(currentFriends))
	for _, f := range currentFriends {
		if strings.ToLower(f) != otherName {
			newCurrentFriends = append(newCurrentFriends, f)
		}
	}
	newOtherFriends := make([]string, 0, len(otherFriends))
	for _, f := range otherFriends {
		if strings.ToLower(f) != currentName {
			newOtherFriends = append(newOtherFriends, f)
		}
	}

	current.Set("sys.friends", newCurrentFriends)
	other.Set("sys.friends", newOtherFriends)

	go saveUsers()

	c.JSON(200, gin.H{"message": "Friend removed"})
}

// GET /friends
func getFriends(c *gin.Context) {
	user := c.MustGet("user").(*User)

	usersMutex.RLock()
	defer usersMutex.RUnlock()

	friends := user.GetFriends()
	c.JSON(200, gin.H{"friends": friends})
}
