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

const validatorWindowSeconds = int64(300)

type ValidatorInfo struct {
	Value     string
	Timestamp int64
}

var validatorInfos = make(map[UserId][]ValidatorInfo)
var validatorMutex sync.RWMutex

func windowStart(t int64) int64 {
	return t / validatorWindowSeconds * validatorWindowSeconds
}

func windowEnd(t int64) int64 {
	return windowStart(t) + validatorWindowSeconds
}

func hashValidator(key, authKey string, roundedTs int64) string {
	hasher := sha256.New()
	hasher.Write([]byte(key + authKey + fmt.Sprintf("%d", roundedTs)))
	return hex.EncodeToString(hasher.Sum(nil))
}

func pruneExpired(id UserId) {
	now := time.Now().Unix()
	infos := validatorInfos[id]
	valid := infos[:0]
	for _, info := range infos {
		if now < windowEnd(info.Timestamp) {
			valid = append(valid, info)
		}
	}
	if len(valid) == 0 {
		delete(validatorInfos, id)
	} else {
		validatorInfos[id] = valid
	}
}

func StartValidatorCleanup() {
	go func() {
		ticker := time.NewTicker(time.Duration(validatorWindowSeconds) * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			validatorMutex.Lock()
			for id := range validatorInfos {
				pruneExpired(id)
			}
			validatorMutex.Unlock()
		}
	}()
}

func generateValidator(c *gin.Context) {
	authKey := c.Query("auth")
	user := c.MustGet("user").(*User)
	key := c.Query("key")
	if key == "" {
		c.JSON(400, gin.H{"error": "key is required"})
		return
	}

	id := user.GetId()
	timestamp := time.Now().Unix()
	hashedKey := hashValidator(key, authKey, windowStart(timestamp))

	validatorMutex.Lock()
	pruneExpired(id)
	validatorInfos[id] = append(validatorInfos[id], ValidatorInfo{
		Value:     hashedKey,
		Timestamp: timestamp,
	})
	validatorMutex.Unlock()

	c.JSON(200, gin.H{
		"validator": string(id) + "," + hashedKey,
	})
}

func validateToken(c *gin.Context) {
	validator := strings.TrimSpace(c.Query("v"))
	if validator == "" {
		c.JSON(400, gin.H{"error": "Validator is required"})
		return
	}

	key := c.Query("key")
	if key == "" {
		c.JSON(400, gin.H{"error": "Key is required"})
		return
	}

	parts := strings.SplitN(validator, ",", 2)
	if len(parts) != 2 {
		c.JSON(400, gin.H{"error": "Invalid validator format"})
		return
	}

	userId := UserId(parts[0])
	encryptedData := parts[1]

	idToUserMutex.RLock()
	foundUser, ok := idToUser[userId]
	idToUserMutex.RUnlock()
	if !ok {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}

	userKey := foundUser.GetKey()
	if userKey == "" {
		c.JSON(400, gin.H{"error": "User has no token"})
		return
	}

	now := time.Now().Unix()

	validatorMutex.RLock()
	infos := validatorInfos[userId]
	var matched *ValidatorInfo
	for i := range infos {
		info := &infos[i]
		if info.Value == encryptedData && now < windowEnd(info.Timestamp) {
			matched = info
			break
		}
	}
	validatorMutex.RUnlock()

	if matched == nil {
		c.JSON(200, gin.H{"valid": false, "error": "Validator expired or not found"})
		return
	}

	expected := hashValidator(key, userKey, windowStart(matched.Timestamp))
	if expected != encryptedData {
		c.JSON(200, gin.H{"valid": false, "error": "Invalid validator"})
		return
	}

	c.JSON(200, gin.H{
		"valid":    true,
		"username": foundUser.GetUsername(),
		"id":       foundUser.GetId(),
	})
}
