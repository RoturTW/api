package main

import (
	"time"

	"github.com/gin-gonic/gin"
)

func proposeMarriage(c *gin.Context) {
	user := *c.MustGet("user").(*User)

	targetUsername := Username(c.Param("username"))
	if targetUsername == "" {
		c.JSON(400, ErrorResponse{Error: "Target username is required"})
		return
	}

	proposerMarriage := user.GetMarriage()
	if proposerMarriage.Status != "single" {
		c.JSON(400, ErrorResponse{Error: "You are already married or have a pending proposal"})
		return
	}

	foundUsers, err := getAccountsBy("username", targetUsername.String(), 1)
	if err != nil {
		c.JSON(404, ErrorResponse{Error: "Target user not found"})
		return
	}

	targetUser := foundUsers[0]
	targetMarriage := targetUser.GetMarriage()
	if targetMarriage.Status != "single" {
		c.JSON(400, ErrorResponse{Error: "Target user is already married or has a pending proposal"})
		return
	}

	if user.GetId() == targetUser.GetId() {
		c.JSON(400, ErrorResponse{Error: "Cannot propose to yourself"})
		return
	}

	if isUserBlockedBy(user, user.GetId()) {
		c.JSON(400, ErrorResponse{Error: "You cant propose to this user"})
		return
	}

	timestamp := time.Now().UnixMilli()

	user.SetMarriage(Marriage{
		Status:    "proposed",
		Partner:   targetUser.GetId(),
		Timestamp: timestamp,
		Proposer:  user.GetId(),
	})

	targetUser.SetMarriage(Marriage{
		Status:    "proposed",
		Partner:   user.GetId(),
		Timestamp: timestamp,
		Proposer:  user.GetId(),
	})

	go saveUsers()

	c.JSON(200, gin.H{"message": "Marriage proposal sent successfully"})
}

func acceptMarriage(c *gin.Context) {
	user := c.MustGet("user").(*User)

	marriageData := user.GetMarriage()
	if marriageData.Status != "proposed" {
		c.JSON(400, ErrorResponse{Error: "No pending proposal"})
		return
	}

	partnerUsername := marriageData.Partner.User().GetUsername()
	proposerUsername := marriageData.Proposer.User().GetUsername()

	if user.GetUsername().ToLower() != proposerUsername.ToLower() {
		c.JSON(400, ErrorResponse{Error: "Invalid proposal data"})
		return
	}

	if user.GetId() == marriageData.Proposer {
		c.JSON(400, ErrorResponse{Error: "Cannot accept your own proposal"})
		return
	}

	partners, err := getAccountsBy("username", partnerUsername.String(), 1)
	if err != nil {
		c.JSON(404, ErrorResponse{Error: "Partner not found"})
		return
	}
	partner := partners[0]

	// Update marriage status for both users
	timestamp := time.Now().UnixMilli()

	user.SetMarriage(Marriage{
		Status:    "married",
		Partner:   partnerUsername.Id(),
		Timestamp: timestamp,
		Proposer:  proposerUsername.Id(),
	})

	partner.SetMarriage(Marriage{
		Status:    "married",
		Partner:   user.GetId(),
		Timestamp: timestamp,
		Proposer:  proposerUsername.Id(),
	})

	go saveUsers()

	c.JSON(200, gin.H{
		"message": "Marriage accepted successfully",
	})
}

func rejectMarriage(c *gin.Context) {
	user := c.MustGet("user").(*User)

	marriageData := user.GetMarriage()
	if marriageData.Status != "proposed" {
		c.JSON(400, ErrorResponse{Error: "No pending proposal"})
		return
	}

	partnerUsername := marriageData.Partner.User().GetUsername()
	proposerUsername := marriageData.Proposer.User().GetUsername()

	if user.GetId() == marriageData.Proposer {
		c.JSON(400, ErrorResponse{Error: "Invalid proposal data"})
		return
	}

	// Can only reject if you're not the proposer
	if user.GetUsername() == proposerUsername {
		c.JSON(400, gin.H{"error": "Cannot reject your own proposal - use cancel instead"})
		return
	}

	// Find partner
	partners, err := getAccountsBy("username", partnerUsername.String(), 1)

	if err != nil {
		c.JSON(404, ErrorResponse{Error: "Partner not found"})
		return
	}
	partner := partners[0]

	// Remove marriage data entirely for both users
	user.DelKey("sys.marriage")
	partner.DelKey("sys.marriage")

	go saveUsers()

	c.JSON(200, gin.H{"message": "Marriage proposal rejected"})
}

func divorceMarriage(c *gin.Context) {
	user := c.MustGet("user").(*User)

	// Check marriage status
	marriageData := user.GetMarriage()
	if marriageData.Status != "married" {
		c.JSON(400, ErrorResponse{Error: "Not married"})
		return
	}

	partnerUsername := marriageData.Partner.User().GetUsername()
	if partnerUsername == "" {
		c.JSON(400, ErrorResponse{Error: "Invalid marriage data"})
		return
	}

	// Find partner
	partners, err := getAccountsBy("username", partnerUsername.String(), 1)

	if err != nil {
		c.JSON(404, ErrorResponse{Error: "Partner not found"})
		return
	}
	partner := partners[0]

	// Remove marriage data entirely
	user.DelKey("sys.marriage")
	partner.DelKey("sys.marriage")

	go saveUsers()

	c.JSON(200, gin.H{
		"message": "Divorce processed successfully",
	})
}

func cancelMarriage(c *gin.Context) {
	user := c.MustGet("user").(*User)

	// Check marriage status
	marriageData := user.GetMarriage()
	if marriageData.Status != "proposed" {
		c.JSON(400, ErrorResponse{Error: "No pending proposal"})
		return
	}

	partnerUsername := marriageData.Partner.User().GetUsername()

	// Can only cancel if you're the proposer
	if user.GetId() != marriageData.Proposer {
		c.JSON(400, gin.H{"error": "Can only cancel your own proposal"})
		return
	}

	// Find partner
	partners, err := getAccountsBy("username", partnerUsername.String(), 1)

	if err != nil {
		c.JSON(404, gin.H{"error": "Partner not found"})
		return
	}
	partner := partners[0]

	// Remove marriage data entirely for both users
	user.DelKey("sys.marriage")
	partner.DelKey("sys.marriage")

	go saveUsers()

	c.JSON(200, gin.H{"message": "Marriage proposal cancelled"})
}

func getMarriageStatus(c *gin.Context) {
	user := c.MustGet("user").(*User)

	marriageData := user.GetMarriage()
	if marriageData.Status == "single" {
		c.JSON(200, gin.H{
			"status":    "single",
			"partner":   "",
			"timestamp": 0,
			"proposer":  "",
		})
		return
	}

	c.JSON(200, marriageData.ToNet())
}
