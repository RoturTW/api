package main

import (
	"crypto/md5"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

var usedCodesMutex sync.RWMutex
var usedCodes = make(map[string]string)
var counter int64

func generateUniqueLinkCode() string {
	for {
		counter++
		timestamp := time.Now().UnixNano()
		hash := md5.Sum([]byte(fmt.Sprintf("%d-%d", timestamp, counter)))
		code := strings.ToUpper(fmt.Sprintf("%x", hash)[:6])

		usedCodesMutex.Lock()
		defer usedCodesMutex.Unlock()
		if _, exists := usedCodes[code]; !exists {
			usedCodes[code] = ""
			return code
		}

		time.Sleep(time.Nanosecond)
	}
}

func getLinkCode(c *gin.Context) {
	code := generateUniqueLinkCode()
	c.JSON(200, gin.H{"code": code})
}

func linkCodeToAccount(c *gin.Context) {
	code := c.Query("code")

	usedCodesMutex.Lock()
	defer usedCodesMutex.Unlock()
	if _, exists := usedCodes[code]; exists {
		user := c.MustGet("user").(*User)
		usedCodes[code] = user.GetKey()
		c.JSON(200, "Linked Successfully")
		return
	}
	c.JSON(404, gin.H{"error": "No auth code found"})
}

func getLinkStatus(c *gin.Context) {
	code := c.Query("code")
	usedCodesMutex.RLock()
	defer usedCodesMutex.RUnlock()
	if val, exists := usedCodes[code]; exists && val != "" {
		c.JSON(200, gin.H{"status": "linked"})
	} else {
		c.JSON(404, gin.H{"status": "not found"})
	}
}

func getLinkedUser(c *gin.Context) {
	code := c.Query("code")
	usedCodesMutex.RLock()
	defer usedCodesMutex.RUnlock()
	if val, exists := usedCodes[code]; exists && val != "" {
		c.JSON(200, gin.H{"linked": true, "token": val})
		delete(usedCodes, code)
	} else {
		c.JSON(404, gin.H{"linked": false, "token": ""})
	}
}
