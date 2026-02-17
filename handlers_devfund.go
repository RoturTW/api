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

	// Check sender balance
	fromCurrency := user.GetCredits()
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

	user.SetBalance(newBal)

	// Add escrow transaction to sender
	now := time.Now().UnixMilli()
	note := strings.TrimSpace(req.Note)
	if note == "" {
		note = "devfund escrow"
	}
	if len(note) > 50 {
		note = note[:50]
	}

	user.addTransaction(Transaction{
		Note:       note,
		User:       Username("rotur").Id(),
		Timestamp:  now,
		Amount:     nAmount,
		Type:       "escrow_out",
		PetitionId: req.PetitionID,
		NewTotal:   newBal,
	})

	go saveUsers()

	c.JSON(200, gin.H{
		"message":     "Escrow transfer successful",
		"from":        user.GetUsername(),
		"amount":      nAmount,
		"petition_id": req.PetitionID,
		"new_balance": newBal,
	})
}

// escrowRelease - Release escrow credits to developer (admin only)
func escrowRelease(c *gin.Context) {
	user := c.MustGet("user").(*User)

	// Only allow mist (admin) to release escrow
	if user.GetUsername().ToLower() != "mist" {
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

	toUsername := Username(req.ToUsername)

	toUser, err := getAccountByUsername(toUsername)
	if err != nil {
		c.JSON(404, gin.H{"error": "Recipient user not found"})
		return
	}

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
	toUser.addTransaction(Transaction{
		Note:       note,
		User:       Username("rotur").Id(),
		Timestamp:  now,
		Amount:     nAmount,
		Type:       "escrow_in",
		PetitionId: req.PetitionID,
		NewTotal:   newBal,
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
