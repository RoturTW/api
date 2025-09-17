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
	authKey := c.Query("auth")
	if authKey == "" {
		c.JSON(403, gin.H{"error": "auth key is required"})
		return
	}

	user := authenticateWithKey(authKey)
	if user == nil {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		return
	}

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
	defer usersMutex.Unlock()

	// Find sender user
	var fromUser *User
	for i := range users {
		if strings.EqualFold(users[i].GetUsername(), user.GetUsername()) {
			fromUser = &users[i]
			break
		}
	}
	if fromUser == nil {
		c.JSON(404, gin.H{"error": "Sender user not found"})
		return
	}

	// Check sender balance
	amtAny := fromUser.Get("sys.currency")
	if amtAny == nil {
		c.JSON(400, gin.H{"error": "Sender user has no currency"})
		return
	}

	var fromCurrency float64
	switch v := amtAny.(type) {
	case int:
		fromCurrency = float64(v)
	case int64:
		fromCurrency = float64(v)
	case float32:
		fromCurrency = float64(v)
	case float64:
		fromCurrency = v
	default:
		c.JSON(400, gin.H{"error": "Invalid currency amount"})
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
	fromUser.Set("sys.currency", newBal)

	// Add escrow transaction to sender
	now := time.Now().UnixMilli()
	note := strings.TrimSpace(req.Note)
	if note == "" {
		note = "devfund escrow"
	}
	if len(note) > 50 {
		note = note[:50]
	}

	// Helper to add transaction
	addTransaction := func(u *User, tx map[string]interface{}) {
		raw := (*u)["sys.transactions"]
		var txs []map[string]interface{}

		switch v := raw.(type) {
		case nil:
			txs = make([]map[string]interface{}, 0)
		case []interface{}:
			for _, item := range v {
				if m, ok := item.(map[string]interface{}); ok {
					txs = append(txs, m)
				}
			}
		case []map[string]interface{}:
			txs = v
		default:
			txs = make([]map[string]interface{}, 0)
		}

		txs = append([]map[string]interface{}{tx}, txs...)
		if len(txs) > 20 {
			txs = txs[:20]
		}
		(*u)["sys.transactions"] = txs
	}

	addTransaction(fromUser, map[string]interface{}{
		"note":        note,
		"user":        "devfund-escrow",
		"time":        now,
		"amount":      nAmount,
		"type":        "escrow_out",
		"petition_id": req.PetitionID,
	})

	go saveUsers()
	go broadcastUserUpdate(fromUser.GetUsername(), "sys.currency", fromUser.Get("sys.currency"))
	go broadcastUserUpdate(fromUser.GetUsername(), "sys.transactions", fromUser.Get("sys.transactions"))

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
	authKey := c.Query("auth")
	if authKey == "" {
		c.JSON(403, gin.H{"error": "auth key is required"})
		return
	}

	user := authenticateWithKey(authKey)
	if user == nil {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		return
	}

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
	defer usersMutex.Unlock()

	// Find recipient user
	var toUser *User
	for i := range users {
		if strings.EqualFold(users[i].GetUsername(), toUsername) {
			toUser = &users[i]
			break
		}
	}
	if toUser == nil {
		c.JSON(404, gin.H{"error": "Recipient user not found"})
		return
	}

	// Get recipient balance
	toAny := toUser.Get("sys.currency")
	if toAny == nil {
		toUser.Set("sys.currency", float64(0))
		toAny = float64(0)
	}

	var toCurrency float64
	switch v := toAny.(type) {
	case int:
		toCurrency = float64(v)
	case int64:
		toCurrency = float64(v)
	case float32:
		toCurrency = float64(v)
	case float64:
		toCurrency = v
	default:
		toCurrency = 0
	}
	toCurrency = roundVal(toCurrency)

	// Add credits to recipient
	newBal := roundVal(toCurrency + nAmount)
	toUser.Set("sys.currency", newBal)

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
	addTransaction := func(u *User, tx map[string]interface{}) {
		raw := (*u)["sys.transactions"]
		var txs []map[string]interface{}

		switch v := raw.(type) {
		case nil:
			txs = make([]map[string]interface{}, 0)
		case []interface{}:
			for _, item := range v {
				if m, ok := item.(map[string]interface{}); ok {
					txs = append(txs, m)
				}
			}
		case []map[string]interface{}:
			txs = v
		default:
			txs = make([]map[string]interface{}, 0)
		}

		txs = append([]map[string]interface{}{tx}, txs...)
		if len(txs) > 20 {
			txs = txs[:20]
		}
		(*u)["sys.transactions"] = txs
	}

	addTransaction(toUser, map[string]interface{}{
		"note":        note,
		"user":        "devfund-escrow",
		"time":        now,
		"amount":      nAmount,
		"type":        "escrow_in",
		"petition_id": req.PetitionID,
	})

	go saveUsers()
	go broadcastUserUpdate(toUser.GetUsername(), "sys.currency", toUser.Get("sys.currency"))
	go broadcastUserUpdate(toUser.GetUsername(), "sys.transactions", toUser.Get("sys.transactions"))

	c.JSON(200, gin.H{
		"message":     "Escrow release successful",
		"to":          toUser.GetUsername(),
		"amount":      nAmount,
		"petition_id": req.PetitionID,
		"new_balance": newBal,
	})
}
