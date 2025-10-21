package main

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func proposeMarriage(c *gin.Context) {
	user := c.MustGet("user").(*User)

	targetUsername := c.Param("username")
	if targetUsername == "" {
		c.JSON(400, gin.H{"error": "Target username is required"})
		return
	}

	proposerIndex := getIdxOfAccountBy("username", user.GetUsername())

	if proposerIndex == -1 {
		c.JSON(404, gin.H{"error": "Proposer not found"})
		return
	}

	targetIndex := getIdxOfAccountBy("username", targetUsername)

	if targetIndex == -1 {
		c.JSON(404, gin.H{"error": "Target user not found"})
		return
	}

	proposerMarriage := users[proposerIndex].Get("sys.marriage")
	if proposerMarriage != nil {
		if marriageMap, ok := proposerMarriage.(map[string]any); ok {
			if status, exists := marriageMap["status"]; exists && status != "single" {
				c.JSON(400, gin.H{"error": "You are already married or have a pending proposal"})
				return
			}
		}
	}

	targetMarriage := users[targetIndex].Get("sys.marriage")
	if targetMarriage != nil {
		if marriageMap, ok := targetMarriage.(map[string]any); ok {
			if status, exists := marriageMap["status"]; exists && status != "single" {
				c.JSON(400, gin.H{"error": "Target user is already married or has a pending proposal"})
				return
			}
		}
	}

	if user.GetUsername() == targetUsername {
		c.JSON(400, gin.H{"error": "Cannot propose to yourself"})
		return
	}

	if isUserBlockedBy(users[proposerIndex], user.GetUsername()) {
		c.JSON(400, gin.H{"error": "You cant propose to this user"})
		return
	}

	timestamp := time.Now().UnixMilli()

	users[proposerIndex]["sys.marriage"] = map[string]any{
		"status":    "proposed",
		"partner":   targetUsername,
		"timestamp": timestamp,
		"proposer":  user.GetUsername(),
	}

	users[targetIndex]["sys.marriage"] = map[string]any{
		"status":    "proposed",
		"partner":   user.GetUsername(),
		"timestamp": timestamp,
		"proposer":  user.GetUsername(),
	}

	go saveUsers()

	c.JSON(200, gin.H{"message": "Marriage proposal sent successfully"})
}

func acceptMarriage(c *gin.Context) {
	user := c.MustGet("user").(*User)

	userIndex := getIdxOfAccountBy("username", user.GetUsername())

	if userIndex == -1 {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}

	marriageData := users[userIndex].Get("sys.marriage")
	if marriageData == nil {
		c.JSON(400, gin.H{"error": "No pending proposal"})
		return
	}

	marriageMap, ok := marriageData.(map[string]any)
	if !ok {
		c.JSON(400, gin.H{"error": "No pending proposal"})
		return
	}

	status, statusExists := marriageMap["status"]
	if !statusExists || status != "proposed" {
		c.JSON(400, gin.H{"error": "No pending proposal"})
		return
	}

	partnerUsername, partnerExists := marriageMap["partner"].(string)
	proposerUsername, proposerExists := marriageMap["proposer"].(string)

	if !partnerExists || !proposerExists {
		c.JSON(400, gin.H{"error": "Invalid proposal data"})
		return
	}

	if user.GetUsername() == proposerUsername {
		c.JSON(400, gin.H{"error": "Cannot accept your own proposal"})
		return
	}

	partnerIndex := getIdxOfAccountBy("username", partnerUsername)

	if partnerIndex == -1 {
		c.JSON(404, gin.H{"error": "Partner not found"})
		return
	}

	// Update marriage status for both users
	timestamp := time.Now().UnixMilli()

	users[userIndex]["sys.marriage"] = map[string]any{
		"status":    "married",
		"partner":   partnerUsername,
		"timestamp": timestamp,
		"proposer":  proposerUsername,
	}

	users[partnerIndex]["sys.marriage"] = map[string]any{
		"status":    "married",
		"partner":   user.GetUsername(),
		"timestamp": timestamp,
		"proposer":  proposerUsername,
	}

	go saveUsers()

	c.JSON(200, gin.H{
		"message": "Marriage accepted successfully",
	})
}

func rejectMarriage(c *gin.Context) {
	user := c.MustGet("user").(*User)

	// Find user
	userIndex := getIdxOfAccountBy("username", user.GetUsername())

	if userIndex == -1 {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}

	// Check marriage status
	marriageData := users[userIndex].Get("sys.marriage")
	if marriageData == nil {
		c.JSON(400, gin.H{"error": "No pending proposal"})
		return
	}

	marriageMap, ok := marriageData.(map[string]any)
	if !ok {
		c.JSON(400, gin.H{"error": "No pending proposal"})
		return
	}

	status, statusExists := marriageMap["status"]
	if !statusExists || status != "proposed" {
		c.JSON(400, gin.H{"error": "No pending proposal"})
		return
	}

	partnerUsername, partnerExists := marriageMap["partner"].(string)
	proposerUsername, proposerExists := marriageMap["proposer"].(string)

	if !partnerExists || !proposerExists {
		c.JSON(400, gin.H{"error": "Invalid proposal data"})
		return
	}

	// Can only reject if you're not the proposer
	if user.GetUsername() == proposerUsername {
		c.JSON(400, gin.H{"error": "Cannot reject your own proposal - use cancel instead"})
		return
	}

	// Find partner
	partnerIndex := getIdxOfAccountBy("username", partnerUsername)

	if partnerIndex == -1 {
		c.JSON(404, gin.H{"error": "Partner not found"})
		return
	}

	// Remove marriage data entirely for both users
	users[userIndex].DelKey("sys.marriage")
	users[partnerIndex].DelKey("sys.marriage")

	go saveUsers()

	c.JSON(200, gin.H{"message": "Marriage proposal rejected"})
}

func divorceMarriage(c *gin.Context) {
	user := c.MustGet("user").(*User)

	// Find user
	userIndex := getIdxOfAccountBy("username", user.GetUsername())

	if userIndex == -1 {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}

	// Check marriage status
	marriageData := users[userIndex].Get("sys.marriage")
	if marriageData == nil {
		c.JSON(400, gin.H{"error": "Not married"})
		return
	}

	marriageMap, ok := marriageData.(map[string]any)
	if !ok {
		c.JSON(400, gin.H{"error": "Not married"})
		return
	}

	status, statusExists := marriageMap["status"]
	if !statusExists || status != "married" {
		c.JSON(400, gin.H{"error": "Not married"})
		return
	}

	partnerUsername, partnerExists := marriageMap["partner"].(string)
	if !partnerExists {
		c.JSON(400, gin.H{"error": "Invalid marriage data"})
		return
	}

	// Find partner
	partnerIndex := getIdxOfAccountBy("username", partnerUsername)

	if partnerIndex == -1 {
		c.JSON(404, gin.H{"error": "Partner not found"})
		return
	}

	// Remove marriage data entirely
	users[userIndex].DelKey("sys.marriage")
	users[partnerIndex].DelKey("sys.marriage")

	go saveUsers()

	c.JSON(200, gin.H{
		"message": "Divorce processed successfully",
	})
}

func cancelMarriage(c *gin.Context) {
	user := c.MustGet("user").(*User)

	// Find user
	userIndex := getIdxOfAccountBy("username", user.GetUsername())

	if userIndex == -1 {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}

	// Check marriage status
	marriageData := users[userIndex].Get("sys.marriage")
	if marriageData == nil {
		c.JSON(400, gin.H{"error": "No pending proposal"})
		return
	}

	marriageMap, ok := marriageData.(map[string]interface{})
	if !ok {
		c.JSON(400, gin.H{"error": "No pending proposal"})
		return
	}

	status, statusExists := marriageMap["status"]
	if !statusExists || status != "proposed" {
		c.JSON(400, gin.H{"error": "No pending proposal"})
		return
	}

	partnerUsername, partnerExists := marriageMap["partner"].(string)
	proposerUsername, proposerExists := marriageMap["proposer"].(string)

	if !partnerExists || !proposerExists {
		c.JSON(400, gin.H{"error": "Invalid proposal data"})
		return
	}

	// Can only cancel if you're the proposer
	if !strings.EqualFold(user.GetUsername(), proposerUsername) {
		c.JSON(400, gin.H{"error": "Can only cancel your own proposal"})
		return
	}

	// Find partner
	partnerIndex := getIdxOfAccountBy("username", partnerUsername)

	if partnerIndex == -1 {
		c.JSON(404, gin.H{"error": "Partner not found"})
		return
	}

	// Remove marriage data entirely for both users
	users[userIndex].DelKey("sys.marriage")
	users[partnerIndex].DelKey("sys.marriage")

	go saveUsers()

	c.JSON(200, gin.H{"message": "Marriage proposal cancelled"})
}

func getMarriageStatus(c *gin.Context) {
	user := c.MustGet("user").(*User)

	// Find user
	userIdx := getIdxOfAccountBy("username", user.GetUsername())
	if userIdx == -1 {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}
	u := users[userIdx]

	marriageData := u.Get("sys.marriage")
	if marriageData == nil {
		c.JSON(200, gin.H{
			"status":    "single",
			"partner":   "",
			"timestamp": 0,
			"proposer":  "",
		})
		return
	}

	if marriageMap, ok := marriageData.(map[string]any); ok {
		c.JSON(200, marriageMap)
	} else {
		c.JSON(200, gin.H{
			"status":    "single",
			"partner":   "",
			"timestamp": 0,
			"proposer":  "",
		})
	}
}
