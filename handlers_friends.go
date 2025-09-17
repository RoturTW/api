package main

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// POST /friends/request/:username
func sendFriendRequest(c *gin.Context) {
	auth := c.Query("auth")
	sender := authenticateWithKey(auth)
	if sender == nil {
		c.JSON(401, gin.H{"error": "Unauthorized"})
		return
	}

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

	var target *User
	for i := range users {
		if strings.ToLower(users[i].GetUsername()) == targetLower {
			target = &users[i]
			break
		}
	}
	if target == nil {
		usersMutex.Unlock()
		c.JSON(404, gin.H{"error": "Account Does Not Exist"})
		return
	}

	senderFriends := getStringSlice(*sender, "sys.friends")
	targetFriends := getStringSlice(*target, "sys.friends")
	targetRequests := getStringSlice(*target, "sys.requests")

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
	setStringSlice(*target, "sys.requests", targetRequests)
	setStringSlice(*sender, "sys.friends", senderFriends)

	usersMutex.Unlock()
	go saveUsers() // async to avoid holding lock during disk IO

	// Broadcast the user account update for the target user
	go broadcastUserUpdate(targetLower, "sys.requests", targetRequests)

	c.JSON(200, gin.H{"message": "Friend request sent successfully"})
}

// POST /friends/accept/:username  (username = original requester)
func acceptFriendRequest(c *gin.Context) {
	auth := c.Query("auth")
	current := authenticateWithKey(auth)
	if current == nil {
		c.JSON(401, gin.H{"error": "Unauthorized"})
		return
	}
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

	var requester *User
	for i := range users {
		if strings.ToLower(users[i].GetUsername()) == requesterName {
			requester = &users[i]
			break
		}
	}
	if requester == nil {
		c.JSON(404, gin.H{"error": "Account Does Not Exist"})
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

	currentFriends := getStringSlice(*current, "sys.friends")
	requesterFriends := getStringSlice(*requester, "sys.friends")

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

	setStringSlice(*current, "sys.requests", newRequests)
	setStringSlice(*current, "sys.friends", currentFriends)
	setStringSlice(*requester, "sys.friends", requesterFriends)

	usersMutex.Unlock()
	go saveUsers()

	// Broadcast user account updates for both users
	go broadcastUserUpdate(currentName, "sys.requests", newRequests)
	go broadcastUserUpdate(currentName, "sys.friends", currentFriends)
	go broadcastUserUpdate(requesterName, "sys.friends", requesterFriends)

	c.JSON(200, gin.H{"message": "Friend request accepted"})
}

// POST /friends/reject/:username
func rejectFriendRequest(c *gin.Context) {
	auth := c.Query("auth")
	current := authenticateWithKey(auth)
	if current == nil {
		c.JSON(401, gin.H{"error": "Unauthorized"})
		return
	}
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
	auth := c.Query("auth")
	current := authenticateWithKey(auth)
	if current == nil {
		c.JSON(401, gin.H{"error": "Unauthorized"})
		return
	}
	otherName := strings.ToLower(c.Param("username"))
	if otherName == "" {
		c.JSON(400, gin.H{"error": "Username cannot be empty"})
		return
	}

	currentName := strings.ToLower(current.GetUsername())
	if currentName == otherName {
		c.JSON(400, gin.H{"error": "Invalid Operation"})
		return
	}

	usersMutex.Lock()

	var other *User
	for i := range users {
		if strings.ToLower(users[i].GetUsername()) == otherName {
			other = &users[i]
			break
		}
	}
	if other == nil {
		usersMutex.Unlock()
		c.JSON(404, gin.H{"error": "Account Does Not Exist"})
		return
	}

	currentFriends := getStringSlice(*current, "sys.friends")
	otherFriends := getStringSlice(*other, "sys.friends")

	isFriend := false
	for _, f := range currentFriends {
		if strings.ToLower(f) == otherName {
			isFriend = true
			break
		}
	}
	if !isFriend {
		usersMutex.Unlock()
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

	setStringSlice(*current, "sys.friends", newCurrentFriends)
	setStringSlice(*other, "sys.friends", newOtherFriends)

	usersMutex.Unlock()
	go saveUsers()

	// Broadcast user account updates for both users
	go broadcastUserUpdate(currentName, "sys.friends", newCurrentFriends)
	go broadcastUserUpdate(otherName, "sys.friends", newOtherFriends)

	c.JSON(200, gin.H{"message": "Friend removed"})
}

// GET /friends
func getFriends(c *gin.Context) {
	auth := c.Query("auth")
	user := authenticateWithKey(auth)
	if user == nil {
		c.JSON(401, gin.H{"error": "Unauthorized"})
		return
	}

	usersMutex.RLock()
	defer usersMutex.RUnlock()

	friends := getStringSlice(*user, "sys.friends")
	c.JSON(200, gin.H{"friends": friends})
}
