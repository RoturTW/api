package main

import (
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
)

func followUser(c *gin.Context) {
	user := c.MustGet("user").(*User)

	targetUsername := c.Query("username")
	if targetUsername == "" {
		targetUsername = c.Query("name")
	}
	if targetUsername == "" {
		c.JSON(400, gin.H{"error": "Target username is required"})
		return
	}

	targetUsername = strings.ToLower(targetUsername)
	currentUsername := strings.ToLower(user.GetUsername())

	// Check if the target user exists
	idx := getIdxOfAccountBy("username", targetUsername)
	if idx == -1 {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}
	if isUserBlockedBy(users[idx], currentUsername) {
		c.JSON(400, gin.H{"error": "You cant follow this user"})
		return
	}

	if currentUsername == targetUsername {
		c.JSON(400, gin.H{"error": "You cannot follow yourself"})
		return
	}

	followersMutex.Lock()
	defer followersMutex.Unlock()

	// Ensure target user has an entry in followers data
	if _, exists := followersData[targetUsername]; !exists {
		followersData[targetUsername] = FollowerData{Followers: make([]string, 0)}
	}

	// Check if already following
	if slices.Contains(followersData[targetUsername].Followers, currentUsername) {
		c.JSON(400, gin.H{"error": "You are already following " + targetUsername})
		return
	}

	// Add to followers list
	data := followersData[targetUsername]
	data.Followers = append(data.Followers, currentUsername)
	followersData[targetUsername] = data

	go saveFollowers()

	addUserEvent(targetUsername, "follow", map[string]any{
		"followers": data.Followers,
	})

	c.JSON(200, gin.H{"message": "You are now following " + targetUsername})
}

func unfollowUser(c *gin.Context) {
	user := c.MustGet("user").(*User)

	targetUsername := c.Query("username")
	if targetUsername == "" {
		targetUsername = c.Query("name")
	}
	if targetUsername == "" {
		c.JSON(400, gin.H{"error": "Target username is required"})
		return
	}

	targetUsername = strings.ToLower(targetUsername)
	currentUsername := strings.ToLower(user.GetUsername())

	followersMutex.Lock()
	defer followersMutex.Unlock()

	data, exists := followersData[targetUsername]
	if !exists || len(data.Followers) == 0 {
		c.JSON(400, gin.H{"error": "You are not following this user"})
		return
	}

	// Remove from followers list
	newFollowers := make([]string, 0)
	found := false
	for _, follower := range data.Followers {
		if follower != currentUsername {
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
	followersData[targetUsername] = data

	go saveFollowers()

	go broadcastClawEvent("followers", map[string]any{
		"username":  targetUsername,
		"followers": len(data.Followers),
	})

	// Remove follow notification from target user's events history
	shouldSave := false
	eventsHistoryMutex.Lock()
	if userEvents, exists := eventsHistory[targetUsername]; exists {
		newEvents := make([]Event, 0)
		for _, event := range userEvents {
			if !(event.Type == "follow" &&
				event.Data["follower"] != nil &&
				strings.ToLower(event.Data["follower"].(string)) == currentUsername) {
				newEvents = append(newEvents, event)
			}
		}
		eventsHistory[targetUsername] = newEvents
		shouldSave = true
	}
	eventsHistoryMutex.Unlock()
	if shouldSave {
		go saveEventsHistory()
	}

	c.JSON(200, gin.H{"message": "You have unfollowed " + targetUsername})
}

func getFollowing(c *gin.Context) {
	name := c.Query("name")
	if name == "" {
		name = c.Query("username")
	}
	if name == "" {
		c.JSON(400, gin.H{"error": "Username is required"})
		return
	}
	name = strings.ToLower(name)

	// Check if the user exists
	idx := getIdxOfAccountBy("username", name)
	if idx == -1 {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}

	followersMutex.RLock()
	defer followersMutex.RUnlock()

	following := make([]string, 0)

	// Iterate through all followersData to find who this user is following
	for targetUser, data := range followersData {
		for _, follower := range data.Followers {
			if strings.ToLower(follower) == name {
				// This user (name) is following targetUser
				for _, user := range users {
					if strings.ToLower(user.GetUsername()) == targetUser {
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
	name := c.Query("name")
	if name == "" {
		name = c.Query("username")
	}
	if name == "" {
		c.JSON(400, gin.H{"error": "Username is required"})
		return
	}
	name = strings.ToLower(name)

	// Check if the user exists
	idx := getIdxOfAccountBy("username", name)
	if idx == -1 {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}

	followersMutex.RLock()
	defer followersMutex.RUnlock()

	followers := make([]string, 0)

	if data, exists := followersData[name]; exists {
		for _, follower := range data.Followers {
			for _, user := range users {
				if strings.EqualFold(user.GetUsername(), follower) {
					followers = append(followers, user.GetUsername())
					break
				}
			}
		}
	}

	c.JSON(200, gin.H{"followers": followers})
}
