package main

import (
	"time"

	"github.com/gin-gonic/gin"
)

const (
	GiftTaxPercent      = 0.01
	GiftMaxExpiryDays   = 90
	GiftMaxExpiryHours  = GiftMaxExpiryDays * 24
	GiftMaxExpiryMillis = int64(GiftMaxExpiryHours) * 60 * 60 * 1000
)

func createGift(c *gin.Context) {
	user := c.MustGet("user").(*User)

	var req struct {
		Amount       float64 `json:"amount"`
		Note         string  `json:"note"`
		ExpiresInHrs int     `json:"expires_in_hrs"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request payload"})
		return
	}

	nAmount, ok := normalizeEscrowAmount(req.Amount)
	if !ok {
		c.JSON(400, gin.H{"error": "Minimum amount is 0.01"})
		return
	}

	if nAmount <= 0 {
		c.JSON(400, gin.H{"error": "Amount must be greater than 0"})
		return
	}

	taxAmount := roundVal(nAmount * GiftTaxPercent)
	totalDeduction := roundVal(nAmount + taxAmount)

	userCredits := user.GetCredits()
	if userCredits < totalDeduction {
		c.JSON(400, gin.H{"error": "Insufficient funds", "required": totalDeduction, "available": userCredits})
		return
	}

	note := trimAndCapNote(req.Note, 50)

	var expiresAt int64 = 0
	if req.ExpiresInHrs > 0 {
		if req.ExpiresInHrs > GiftMaxExpiryHours {
			c.JSON(400, gin.H{"error": "Maximum expiration is 90 days", "max_hours": GiftMaxExpiryHours})
			return
		}
		expiresAt = time.Now().UnixMilli() + int64(req.ExpiresInHrs)*60*60*1000
	}

	giftId := generateToken()
	giftCode := generateGiftCode()

	now := time.Now().UnixMilli()

	gift := Gift{
		Id:        giftId,
		Code:      giftCode,
		Amount:    nAmount,
		Note:      note,
		CreatorId: user.GetId(),
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}

	newBal := roundVal(userCredits - totalDeduction)
	user.SetBalance(newBal)

	user.addTransaction(Transaction{
		Note:      note,
		User:      UserId(""),
		Amount:    totalDeduction,
		Type:      "gift_create",
		Timestamp: now,
		NewTotal:  newBal,
		GiftId:    giftId,
		GiftCode:  giftCode,
	})

	giftsMutex.Lock()
	gifts = append(gifts, gift)
	giftsMutex.Unlock()

	go saveGifts()
	go saveUsers()

	c.JSON(200, gin.H{
		"message":    "Gift created successfully",
		"id":         giftId,
		"code":       giftCode,
		"amount":     nAmount,
		"tax":        taxAmount,
		"total_paid": totalDeduction,
		"expires_at": expiresAt,
		"claim_url":  "https://rotur.dev/gift/" + giftCode,
	})
}

func getGift(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		c.JSON(400, gin.H{"error": "Gift code is required"})
		return
	}

	gift, exists := getGiftByCode(code)
	if !exists {
		c.JSON(404, gin.H{"error": "Gift not found"})
		return
	}

	if !gift.CanBeClaimed() {
		if gift.ClaimedAt != nil {
			c.JSON(410, gin.H{"error": "This gift has already been claimed"})
			return
		}
		if gift.CancelledAt != nil {
			c.JSON(410, gin.H{"error": "This gift has been cancelled"})
			return
		}
		if gift.IsExpired() {
			c.JSON(410, gin.H{"error": "This gift has expired"})
			return
		}
	}

	c.JSON(200, gin.H{"gift": gift.ToPublic()})
}

func claimGift(c *gin.Context) {
	user := c.MustGet("user").(*User)

	code := c.Param("code")
	if code == "" {
		c.JSON(400, gin.H{"error": "Gift code is required"})
		return
	}

	giftsMutex.Lock()
	defer giftsMutex.Unlock()

	giftIdx := -1
	for i := range gifts {
		if gifts[i].Code == code {
			giftIdx = i
			break
		}
	}

	if giftIdx == -1 {
		c.JSON(404, gin.H{"error": "Gift not found"})
		return
	}

	gift := &gifts[giftIdx]

	if gift.CreatorId == user.GetId() {
		c.JSON(400, gin.H{"error": "You cannot claim your own gift"})
		return
	}

	if !gift.CanBeClaimed() {
		if gift.ClaimedAt != nil {
			c.JSON(400, gin.H{"error": "This gift has already been claimed"})
			return
		}
		if gift.CancelledAt != nil {
			c.JSON(400, gin.H{"error": "This gift has been cancelled"})
			return
		}
		if gift.IsExpired() {
			c.JSON(400, gin.H{"error": "This gift has expired"})
			return
		}
	}

	userCredits := user.GetCredits()
	newBal := roundVal(userCredits + gift.Amount)
	user.SetBalance(newBal)

	now := time.Now().UnixMilli()
	claimedBy := user.GetId()

	user.addTransaction(Transaction{
		Note:      gift.Note,
		User:      gift.CreatorId,
		Amount:    gift.Amount,
		Type:      "gift_claim",
		Timestamp: now,
		NewTotal:  newBal,
		GiftId:    gift.Id,
		GiftCode:  gift.Code,
	})

	creator := getUserById(gift.CreatorId)
	if len(creator) > 0 {
		creator.addTransaction(Transaction{
			Note:      "Gift claimed by " + string(user.GetUsername()),
			User:      user.GetId(),
			Amount:    gift.Amount,
			Type:      "gift_claimed",
			Timestamp: now,
			NewTotal:  creator.GetCredits(),
			GiftId:    gift.Id,
			GiftCode:  gift.Code,
		})
	}

	gift.ClaimedAt = &now
	gift.ClaimedBy = &claimedBy

	go saveGifts()
	go saveUsers()

	c.JSON(200, gin.H{
		"message":     "Gift claimed successfully",
		"amount":      gift.Amount,
		"new_balance": newBal,
	})
}

func cancelGift(c *gin.Context) {
	user := c.MustGet("user").(*User)

	giftId := c.Param("id")
	if giftId == "" {
		c.JSON(400, gin.H{"error": "Gift ID is required"})
		return
	}

	giftsMutex.Lock()
	defer giftsMutex.Unlock()

	giftIdx := -1
	for i := range gifts {
		if gifts[i].Id == giftId {
			giftIdx = i
			break
		}
	}

	if giftIdx == -1 {
		c.JSON(404, gin.H{"error": "Gift not found"})
		return
	}

	gift := &gifts[giftIdx]

	if gift.CreatorId != user.GetId() {
		c.JSON(403, gin.H{"error": "You can only cancel your own gifts"})
		return
	}

	if !gift.CanBeCancelled() {
		if gift.ClaimedAt != nil {
			c.JSON(400, gin.H{"error": "This gift has already been claimed"})
			return
		}
		if gift.CancelledAt != nil {
			c.JSON(400, gin.H{"error": "This gift has already been cancelled"})
			return
		}
		if gift.IsExpired() {
			c.JSON(400, gin.H{"error": "This gift has expired"})
			return
		}
	}

	userCredits := user.GetCredits()
	newBal := roundVal(userCredits + gift.Amount)
	user.SetBalance(newBal)

	now := time.Now().UnixMilli()

	user.addTransaction(Transaction{
		Note:      "Gift cancelled: " + gift.Code,
		User:      UserId(""),
		Amount:    gift.Amount,
		Type:      "gift_refund",
		Timestamp: now,
		NewTotal:  newBal,
		GiftId:    gift.Id,
		GiftCode:  gift.Code,
	})

	gift.CancelledAt = &now

	go saveGifts()
	go saveUsers()

	c.JSON(200, gin.H{
		"message":     "Gift cancelled successfully",
		"refunded":    gift.Amount,
		"new_balance": newBal,
	})
}

func getMyGifts(c *gin.Context) {
	user := c.MustGet("user").(*User)

	creatorGifts := getGiftsByCreator(user.GetId())

	netGifts := make([]GiftNet, 0, len(creatorGifts))
	for _, gift := range creatorGifts {
		netGifts = append(netGifts, gift.ToNet())
	}

	c.JSON(200, gin.H{
		"gifts": netGifts,
		"count": len(netGifts),
	})
}

func trimAndCapNote(note string, maxLen int) string {
	note = trim(note)
	runes := []rune(note)
	if len(runes) > maxLen {
		runes = runes[:maxLen]
	}
	return string(runes)
}

func trim(s string) string {
	return trimSpace(s)
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
