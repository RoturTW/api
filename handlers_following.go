package main

import (
	"slices"

	"github.com/gin-gonic/gin"
)

func followUser(c *gin.Context) {
	user := c.MustGet("user").(*User)

	targetUsername := Username(c.Query("username"))
	if targetUsername == "" {
		targetUsername = Username(c.Query("name"))
	}
	if targetUsername == "" {
		c.JSON(400, gin.H{"error": "Target username is required"})
		return
	}
	targetId := targetUsername.Id()
	currentId := user.GetId()

	targetUsername = targetUsername.ToLower()

	// Check if the target user exists
	if !accountExists(targetId) {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}
	idx := getIdxOfAccountBy("username", targetUsername.String())
	if isUserBlockedBy(users[idx], currentId) {
		c.JSON(400, gin.H{"error": "You cant follow this user"})
		return
	}

	if targetId == currentId {
		c.JSON(400, gin.H{"error": "You cannot follow yourself"})
		return
	}

	followersMutex.Lock()
	defer followersMutex.Unlock()

	// Ensure target user has an entry in followers data
	if _, exists := followersData[targetId]; !exists {
		followersData[targetId] = FollowerData{Followers: make([]UserId, 0)}
	}

	// Check if already following
	if slices.Contains(followersData[targetId].Followers, currentId) {
		c.JSON(400, gin.H{"error": "You are already following " + targetUsername})
		return
	}

	// Add to followers list
	data := followersData[targetId]
	data.Followers = append(data.Followers, currentId)
	followersData[targetId] = data

	go saveFollowers()

	addUserEvent(targetId, "follow", map[string]any{
		"followers": data.Followers,
	})

	c.JSON(200, gin.H{"message": "You are now following " + targetUsername})
}

func unfollowUser(c *gin.Context) {
	user := c.MustGet("user").(*User)

	targetUsername := Username(c.Query("username"))
	if targetUsername == "" {
		targetUsername = Username(c.Query("name"))
	}
	targetId := targetUsername.Id()
	if !accountExists(targetId) {
		c.JSON(400, gin.H{"error": "Target username is required"})
		return
	}
	currentId := user.GetId()

	targetUsername = targetUsername.ToLower()

	followersMutex.Lock()
	defer followersMutex.Unlock()

	data, exists := followersData[targetId]
	if !exists || len(data.Followers) == 0 {
		c.JSON(400, gin.H{"error": "You are not following this user"})
		return
	}

	// Remove from followers list
	newFollowers := make([]UserId, 0)
	found := false
	for _, follower := range data.Followers {
		if follower != currentId {
			newFollowers = append(newFollowers, follower)
		} else {
			found = true
		}
	}

	if !found {
		c.JSON(400, gin.H{"error": "You are not following this user"})
		return
	}

	data.Followers = newFollowers
	followersData[targetId] = data

	go saveFollowers()

	go broadcastClawEvent("followers", map[string]any{
		"username":  targetUsername,
		"followers": len(data.Followers),
	})

	// Remove follow notification from target user's events history
	shouldSave := false
	eventsHistoryMutex.Lock()
	if userEvents, exists := eventsHistory[targetId]; exists {
		newEvents := make([]Event, 0)
		for _, event := range userEvents {
			if !(event.Type == "follow" &&
				event.Data["follower"] != nil &&
				event.Data["follower"].(UserId) == currentId) {
				newEvents = append(newEvents, event)
			}
		}
		eventsHistory[targetId] = newEvents
		shouldSave = true
	}
	eventsHistoryMutex.Unlock()
	if shouldSave {
		go saveEventsHistory()
	}

	c.JSON(200, gin.H{"message": "You have unfollowed " + targetUsername})
}

func getFollowing(c *gin.Context) {
	name := Username(c.Query("name"))
	if name == "" {
		name = Username(c.Query("username"))
	}
	if name == "" {
		c.JSON(400, gin.H{"error": "Username is required"})
		return
	}

	targetId := name.Id()
	// Check if the user exists
	if !accountExists(targetId) {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}

	followersMutex.RLock()
	defer followersMutex.RUnlock()

	following := make([]Username, 0)

	// Iterate through all followersData to find who this user is following
	for _, data := range followersData {
		for _, follower := range data.Followers {
			if follower == targetId {
				// This user (name) is following targetUser
				for _, user := range users {
					if user.GetId() == targetId {
						following = append(following, user.GetUsername())
						break
					}
				}
				break
			}
		}
	}

	c.JSON(200, gin.H{"following": following})
}

func getFollowers(c *gin.Context) {
	name := Username(c.Query("name"))
	if name == "" {
		name = Username(c.Query("username"))
	}
	if name == "" {
		c.JSON(400, gin.H{"error": "Username is required"})
		return
	}

	// Check if the user exists
	targetId := name.Id()
	if !accountExists(targetId) {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}

	followersMutex.RLock()
	defer followersMutex.RUnlock()

	followers := make([]Username, 0)

	if data, exists := followersData[targetId]; exists {
		for _, follower := range data.Followers {
			for _, user := range users {
				if user.GetId() == follower {
					followers = append(followers, user.GetUsername())
					break
				}
			}
		}
	}

	c.JSON(200, gin.H{"followers": followers})
}
