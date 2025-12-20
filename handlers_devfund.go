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

	usersMutex.Lock()

	// Find sender user
	var fromUser *User
	for i := range users {
		if strings.EqualFold(users[i].GetUsername(), user.GetUsername()) {
			fromUser = &users[i]
			break
		}
	}
	if fromUser == nil {
		usersMutex.Unlock()
		c.JSON(404, gin.H{"error": "Sender user not found"})
		return
	}

	// Check sender balance
	fromCurrency := fromUser.GetCredits()
	if fromCurrency == 0 {
		usersMutex.Unlock()
		c.JSON(400, gin.H{"error": "Sender user has no currency"})
		return
	}
	fromCurrency = roundVal(fromCurrency)

	if fromCurrency < nAmount {
		usersMutex.Unlock()
		c.JSON(400, gin.H{"error": "Insufficient funds", "required": nAmount, "available": fromCurrency})
		return
	}

	// Deduct from sender (no tax for escrow)
	newBal := roundVal(fromCurrency - nAmount)
	if newBal < 0 { // guard against tiny floating error
		newBal = 0
	}
	
	// Update balance directly while holding lock
	setUserKeyDirect(fromUser, "sys.currency", roundVal(newBal))

	// Add escrow transaction to sender
	now := time.Now().UnixMilli()
	note := strings.TrimSpace(req.Note)
	if note == "" {
		note = "devfund escrow"
	}
	if len(note) > 50 {
		note = note[:50]
	}

	// Add transaction directly while holding lock
	txs := getObjectSlice(*fromUser, "sys.transactions")
	benefits := fromUser.GetSubscriptionBenefits()
	
	tx := map[string]any{
		"note":        note,
		"user":        "devfund-escrow",
		"time":        now,
		"amount":      nAmount,
		"type":        "escrow_out",
		"petition_id": req.PetitionID,
		"new_total":   newBal,
	}
	
	txs = append([]map[string]any{tx}, txs...)
	if len(txs) > benefits.Max_Transaction_History {
		txs = txs[:benefits.Max_Transaction_History]
	}
	setUserKeyDirect(fromUser, "sys.transactions", txs)
	
	usersMutex.Unlock()

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

	usersMutex.Lock()

	// Find recipient user
	var toUser *User
	for i := range users {
		if strings.EqualFold(users[i].GetUsername(), toUsername) {
			toUser = &users[i]
			break
		}
	}
	if toUser == nil {
		usersMutex.Unlock()
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
	setUserKeyDirect(toUser, "sys.currency", newBal)

	// Add transaction to recipient
	now := time.Now().UnixMilli()
	note := strings.TrimSpace(req.Note)
	if note == "" {
		note = "devfund escrow release"
	}
	if len(note) > 50 {
		note = note[:50]
	}

	// Add transaction directly while holding lock
	txs := getObjectSlice(*toUser, "sys.transactions")
	benefits := toUser.GetSubscriptionBenefits()
	
	tx := map[string]any{
		"note":        note,
		"user":        "devfund-escrow",
		"time":        now,
		"amount":      nAmount,
		"type":        "escrow_in",
		"petition_id": req.PetitionID,
		"new_total":   newBal,
	}
	
	txs = append([]map[string]any{tx}, txs...)
	if len(txs) > benefits.Max_Transaction_History {
		txs = txs[:benefits.Max_Transaction_History]
	}
	setUserKeyDirect(toUser, "sys.transactions", txs)

	usersMutex.Unlock()

	go saveUsers()

	c.JSON(200, gin.H{
		"message":     "Escrow release successful",
		"to":          toUser.GetUsername(),
		"amount":      nAmount,
		"petition_id": req.PetitionID,
		"new_balance": newBal,
	})
}
