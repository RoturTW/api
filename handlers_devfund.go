package main

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// helper to normalize and validate monetary amounts (minimum 0.01, 2 decimal places)
func normalizeEscrowAmount(raw float64) (float64, bool) {
	amt := roundVal(raw)
	if amt < 0.01 {
		return 0, false
	}
	return amt, true
}

// escrowTransfer - Transfer credits to escrow (no tax for internal transfers)
func escrowTransfer(c *gin.Context) {
	user := c.MustGet("user").(*User)

	var req struct {
		Amount     float64 `json:"amount"`
		PetitionID string  `json:"petition_id"`
		Note       string  `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request payload"})
		return
	}

	// Normalize & validate amount
	nAmount, ok := normalizeEscrowAmount(req.Amount)
	if !ok {
		c.JSON(400, gin.H{"error": "Minimum amount is 0.01"})
		return
	}

	if req.PetitionID == "" {
		c.JSON(400, gin.H{"error": "Petition ID is required"})
		return
	}

	// Find sender user
	foundUsers, err := getAccountsBy("username", user.GetUsername(), 1)
	if err != nil {
		c.JSON(404, gin.H{"error": "Sender user not found"})
		return
	}
	fromUser := foundUsers[0]

	// Check sender balance
	fromCurrency := fromUser.GetCredits()
	if fromCurrency == 0 {
		c.JSON(400, gin.H{"error": "Sender user has no currency"})
		return
	}
	fromCurrency = roundVal(fromCurrency)

	if fromCurrency < nAmount {
		c.JSON(400, gin.H{"error": "Insufficient funds", "required": nAmount, "available": fromCurrency})
		return
	}

	// Deduct from sender (no tax for escrow)
	newBal := roundVal(fromCurrency - nAmount)
	if newBal < 0 { // guard against tiny floating error
		newBal = 0
	}

	fromUser.SetBalance(newBal)

	// Add escrow transaction to sender
	now := time.Now().UnixMilli()
	note := strings.TrimSpace(req.Note)
	if note == "" {
		note = "devfund escrow"
	}
	if len(note) > 50 {
		note = note[:50]
	}

	fromUser.addTransaction(map[string]any{
		"note":        note,
		"user":        "devfund-escrow",
		"time":        now,
		"amount":      nAmount,
		"type":        "escrow_out",
		"petition_id": req.PetitionID,
		"new_total":   newBal,
	})

	go saveUsers()

	c.JSON(200, gin.H{
		"message":     "Escrow transfer successful",
		"from":        fromUser.GetUsername(),
		"amount":      nAmount,
		"petition_id": req.PetitionID,
		"new_balance": newBal,
	})
}

// escrowRelease - Release escrow credits to developer (admin only)
func escrowRelease(c *gin.Context) {
	user := c.MustGet("user").(*User)

	// Only allow mist (admin) to release escrow
	if strings.ToLower(user.GetUsername()) != "mist" {
		c.JSON(403, gin.H{"error": "Admin access required"})
		return
	}

	var req struct {
		Amount     float64 `json:"amount"`
		ToUsername string  `json:"to_username"`
		PetitionID string  `json:"petition_id"`
		Note       string  `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request payload"})
		return
	}

	// Normalize & validate amount
	nAmount, ok := normalizeEscrowAmount(req.Amount)
	if !ok {
		c.JSON(400, gin.H{"error": "Minimum amount is 0.01"})
		return
	}

	if req.ToUsername == "" {
		c.JSON(400, gin.H{"error": "Recipient username is required"})
		return
	}

	if req.PetitionID == "" {
		c.JSON(400, gin.H{"error": "Petition ID is required"})
		return
	}

	toUsername := strings.ToLower(req.ToUsername)

	toUsers, err := getAccountsBy("username", toUsername, 1)
	if err != nil {
		c.JSON(404, gin.H{"error": "Recipient user not found"})
		return
	}
	toUser := toUsers[0]

	// Get recipient balance
	toCurrency := toUser.GetCredits()
	if toCurrency == 0 {
		toCurrency = float64(0)
	}

	// Add credits to recipient
	newBal := roundVal(toCurrency + nAmount)
	toUser.SetBalance(newBal)

	// Add transaction to recipient
	now := time.Now().UnixMilli()
	note := strings.TrimSpace(req.Note)
	if note == "" {
		note = "devfund escrow release"
	}
	if len(note) > 50 {
		note = note[:50]
	}

	// Helper to add transaction
	toUser.addTransaction(map[string]any{
		"note":        note,
		"user":        "devfund-escrow",
		"time":        now,
		"amount":      nAmount,
		"type":        "escrow_in",
		"petition_id": req.PetitionID,
		"new_total":   newBal,
	})

	go saveUsers()

	c.JSON(200, gin.H{
		"message":     "Escrow release successful",
		"to":          toUser.GetUsername(),
		"amount":      nAmount,
		"petition_id": req.PetitionID,
		"new_balance": newBal,
	})
}
