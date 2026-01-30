package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type ValidatorInfo struct {
	Value     string
	Timestamp int64
}

var validatorInfos = make(map[Username]ValidatorInfo)
var validatorMutex sync.RWMutex

func generateValidator(c *gin.Context) {
	authKey := c.Query("auth")
	user := c.MustGet("user").(*User)
	key := c.Query("key")
	if key == "" {
		c.JSON(400, gin.H{"error": "key is required"})
		return
	}

	// Hash the key with sha256
	hasher := sha256.New()
	timestamp := int64(time.Now().Unix())
	roundedTimestamp := timestamp / 300 * 300
	hasher.Write([]byte(key + authKey + fmt.Sprintf("%d", roundedTimestamp)))
	hashedKey := hex.EncodeToString(hasher.Sum(nil))

	// Store the validator and timestamp for this user
	validatorMutex.Lock()
	defer validatorMutex.Unlock()
	validatorInfos[user.GetUsername()] = ValidatorInfo{
		Value:     hashedKey,
		Timestamp: timestamp,
	}

	c.JSON(200, gin.H{
		"validator": user.GetUsername().String() + "," + hashedKey,
	})
}

func validateToken(c *gin.Context) {
	validator := c.Query("v")
	if validator == "" {
		c.JSON(400, gin.H{"error": "Validator is required"})
		return
	}

	key := c.Query("key")
	if key == "" {
		c.JSON(400, gin.H{"error": "Key is required"})
		return
	}

	// Strip any whitespace from the validator
	validator = strings.TrimSpace(validator)

	parts := strings.SplitN(validator, ",", 2)
	if len(parts) != 2 {
		c.JSON(400, gin.H{"error": "Invalid validator format"})
		return
	}

	username := Username(parts[0])
	encryptedData := parts[1]

	// Find the user in the users list
	foundUsers, err := getAccountsBy("username", username.String(), 1)
	if err != nil {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}
	foundUser := foundUsers[0]
	// Get the user's key (token)
	userKey := foundUser.GetKey()
	if userKey == "" {
		c.JSON(400, gin.H{"error": "User has no token"})
		return
	}

	// Check if validator matches latest and is not expired
	validatorMutex.RLock()
	info, ok := validatorInfos[username]
	validatorMutex.RUnlock()
	if !ok || info.Value != encryptedData || time.Now().Unix()-info.Timestamp > 300 {
		c.JSON(200, gin.H{"valid": false, "error": "Validator expired or invalid"})
		return
	}

	// Hash the key with sha256 and check equality
	hasher := sha256.New()
	timestamp := info.Timestamp / 300 * 300
	hasher.Write([]byte(key + userKey + fmt.Sprintf("%d", timestamp)))
	hashedKey := hex.EncodeToString(hasher.Sum(nil))

	if hashedKey == encryptedData {
		c.JSON(200, gin.H{"valid": true})
	} else {
		c.JSON(200, gin.H{"valid": false})
	}
}
