package main

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func setStandingAdmin(c *gin.Context) {
	type Request struct {
		Username string        `json:"username"`
		Level    StandingLevel `json:"level"`
		Reason   string        `json:"reason"`
	}

	var req Request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request body"})
		return
	}

	if req.Level == "" {
		c.JSON(400, gin.H{"error": "standing level is required"})
		return
	}

	if req.Reason == "" {
		c.JSON(400, gin.H{"error": "reason is required"})
		return
	}

	userId := getIdByUsername(Username(req.Username))
	if userId == "" {
		c.JSON(404, gin.H{"error": "user not found"})
		return
	}

	usersMutex.RLock()
	user := getUserById(userId)
	usersMutex.RUnlock()

	if len(user) == 0 {
		c.JSON(404, gin.H{"error": "user not found"})
		return
	}

	adminId := c.GetHeader("X-Admin-ID")
	if adminId == "" {
		adminId = "unknown"
	}

	user.SetStanding(req.Level, req.Reason, UserId(adminId))

	usersMutex.Lock()
	saveUsers()
	usersMutex.Unlock()

	c.JSON(200, gin.H{
		"success":  true,
		"username": req.Username,
		"standing": user.GetStanding(),
	})
}

func getStandingHistoryAdmin(c *gin.Context) {
	type Request struct {
		Username string `json:"username"`
	}

	var req Request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request body"})
		return
	}

	if req.Username == "" {
		c.JSON(400, gin.H{"error": "username is required"})
		return
	}

	userId := getIdByUsername(Username(req.Username))
	if userId == "" {
		c.JSON(404, gin.H{"error": "user not found"})
		return
	}

	usersMutex.RLock()
	user := getUserById(userId)
	usersMutex.RUnlock()

	if len(user) == 0 {
		c.JSON(404, gin.H{"error": "user not found"})
		return
	}

	history := user.GetStandingHistory()

	c.JSON(200, gin.H{
		"username": req.Username,
		"standing": user.GetStanding(),
		"history":  history,
	})
}

func recoverStandingAdmin(c *gin.Context) {
	type Request struct {
		Username string `json:"username"`
		Reason   string `json:"reason"`
	}

	var req Request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request body"})
		return
	}

	if req.Username == "" {
		c.JSON(400, gin.H{"error": "username is required"})
		return
	}

	if req.Reason == "" {
		c.JSON(400, gin.H{"error": "reason is required"})
		return
	}

	userId := getIdByUsername(Username(req.Username))
	if userId == "" {
		c.JSON(404, gin.H{"error": "user not found"})
		return
	}

	usersMutex.RLock()
	user := getUserById(userId)
	usersMutex.RUnlock()

	if len(user) == 0 {
		c.JSON(404, gin.H{"error": "user not found"})
		return
	}

	current := user.GetStanding()
	if current == StandingGood {
		c.JSON(400, gin.H{"error": "user is already in good standing"})
		return
	}

	adminId := c.GetHeader("X-Admin-ID")
	if adminId == "" {
		adminId = "unknown"
	}

	var newLevel StandingLevel
	switch current {
	case StandingSuspended:
		newLevel = StandingWarning
	case StandingWarning:
		newLevel = StandingGood
	case StandingBanned:
		newLevel = StandingWarning
	default:
		newLevel = StandingGood
	}

	user.SetStanding(newLevel, req.Reason, UserId(adminId))

	usersMutex.Lock()
	saveUsers()
	usersMutex.Unlock()

	c.JSON(200, gin.H{
		"success":           true,
		"username":          req.Username,
		"previous_standing": current,
		"new_standing":      newLevel,
	})
}

func startStandingRecoveryChecker() {
	go func() {
		for {
			time.Sleep(5 * time.Minute)

			usersMutex.Lock()
			updated := false
			now := time.Now().Unix()

			for i := range users {
				user := &users[i]
				recoverAt := user.GetStandingRecoverAt()

				if recoverAt > 0 && recoverAt < now {
					standing := user.GetStanding()
					var newLevel StandingLevel

					switch standing {
					case StandingWarning:
						newLevel = StandingGood
					case StandingSuspended:
						newLevel = StandingWarning
					default:
						continue
					}

					user.SetStanding(newLevel, "Automatic recovery", UserId("system"))
					updated = true
				}
			}

			if updated {
				saveUsers()
			}
			usersMutex.Unlock()
		}
	}()
}

func GetUserStanding(c *gin.Context) {
	username := c.Query("username")
	if username == "" {
		c.JSON(400, gin.H{"error": "username is required"})
		return
	}

	userId := getIdByUsername(Username(username))
	if userId == "" {
		c.JSON(404, gin.H{"error": "user not found"})
		return
	}

	usersMutex.RLock()
	user := getUserById(userId)
	usersMutex.RUnlock()

	if len(user) == 0 {
		c.JSON(404, gin.H{"error": "user not found"})
		return
	}

	recoverAt := user.GetStandingRecoverAt()

	c.JSON(http.StatusOK, gin.H{
		"username":   username,
		"standing":   user.GetStanding(),
		"recover_at": recoverAt,
		"history":    user.GetStandingHistory(),
	})
}
