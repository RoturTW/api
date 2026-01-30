package main

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

func getEconomyStats(c *gin.Context) {
	currencies := getUserCreditData()

	// Calculate stats
	count := len(currencies)
	total := 0.0
	for _, currency := range currencies {
		total += currency
	}
	average := total / float64(count)

	// Calculate variance
	variance := 0.0
	for _, currency := range currencies {
		variance += math.Pow(currency-average, 2)
	}
	variance = variance / float64(count)

	// Currency conversion rates
	c.JSON(200, gin.H{
		"average":  average,
		"total":    total,
		"variance": variance,
		"currency_comparison": gin.H{
			"pence": fmt.Sprintf("%.2fp / credit", creditsToPence(1)),
			"cents": fmt.Sprintf("%.2f¢ / credit", creditsToCents(1)),
		},
	})
}

func getUserCreditData() []float64 {
	usersMutex.RLock()
	defer usersMutex.RUnlock()

	currencies := make([]float64, 0)
	for _, user := range users {
		userCredits := user.GetCredits()
		if userCredits < 0 {
			continue
		}
		currencies = append(currencies, userCredits)
	}

	// Skip calculation if no currency data is available
	if len(currencies) == 0 {
		return []float64{}
	}

	return currencies
}

func getPencePerCredit() float64 {
	pencePer1000, err := strconv.Atoi(os.Getenv("PENCE_PER_1000"))
	if err != nil {
		pencePer1000 = 6
	}
	return float64(pencePer1000)
}

func creditsToPence(credits float64) float64 {
	pencePerCredit := getPencePerCredit()
	currencies := getUserCreditData()
	total := 0.0
	for _, currency := range currencies {
		total += currency
	}
	penceRate := roundVal((float64(1000*pencePerCredit)/total)*100) / 100 // pence per credit
	return penceRate * credits
}

func creditsToCents(credits float64) float64 {
	pencePerCredit := getPencePerCredit()
	currencies := getUserCreditData()
	total := 0.0
	for _, currency := range currencies {
		total += currency
	}
	centsRate := roundVal((float64(1000*1.31*pencePerCredit)/total)*100) / 100 // cents per credit
	return centsRate * credits
}

func getUserStats(c *gin.Context) {
	usersMutex.RLock()
	defer usersMutex.RUnlock()

	stats := map[string]int{
		"total_users":  len(users),
		"banned_users": 0,
		"active_users": 0,
	}

	for _, user := range users {
		if user.IsBanned() {
			stats["banned_users"] += 1
		} else {
			stats["active_users"] += 1
		}
	}

	c.JSON(200, stats)
}

func findSinceMonth(txs []map[string]any, cutoff int64) int {
	return sort.Search(len(txs), func(i int) bool {
		t, _ := txs[i]["time"].(float64)
		return int64(t) >= cutoff
	})
}

func getMostGained(c *gin.Context) {
	usersMutex.RLock()
	defer usersMutex.RUnlock()

	max := c.Query("max")
	if max == "" {
		max = "10"
	}
	maxInt, err := strconv.Atoi(max)
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid max parameter"})
		return
	}
	if maxInt > 10 {
		maxInt = 10
	}

	monthAgo := time.Now().AddDate(0, -1, 0).UnixMilli()

	type result struct {
		User   Username `json:"user"`
		Earned float64  `json:"earned"`
	}

	leaderboard := make([]result, 0, len(users))

	for _, user := range users {
		if user.IsBanned() || user.IsPrivate() {
			continue
		}

		txs := user.GetTransactions()
		if len(txs) == 0 {
			continue
		}

		var earned float64

		start := findSinceMonth(txs, monthAgo)
		for _, tx := range txs[start:] {
			t, ok := tx["time"].(float64)
			if !ok || int64(t) < monthAgo {
				continue
			}

			amt, ok := tx["amount"].(float64)
			if !ok || amt <= 0 {
				continue
			}

			typ, _ := tx["type"].(string)
			switch typ {
			case "in", "key_sale", "tax":
				earned += amt
			case "out":
				earned -= amt
			}
		}

		if earned > 0 {
			leaderboard = append(leaderboard, result{
				User:   user.GetUsername(),
				Earned: earned,
			})
		}
	}

	sort.Slice(leaderboard, func(i, j int) bool {
		return leaderboard[i].Earned > leaderboard[j].Earned
	})

	if len(leaderboard) > maxInt {
		leaderboard = leaderboard[:maxInt]
	}

	c.JSON(200, leaderboard)
}

func getSystemStats(c *gin.Context) {
	usersMutex.RLock()
	defer usersMutex.RUnlock()

	systems := make(map[string]int)
	for _, user := range users {
		if user.IsBanned() || user.IsPrivate() {
			continue
		}
		systems[user.GetSystem()]++
	}

	if len(systems) == 0 {
		c.JSON(404, gin.H{"error": "No system data available"})
		return
	}

	c.JSON(200, systems)
}

func getFollowersStats(c *gin.Context) {
	maxNum := c.Query("max")
	if maxNum == "" {
		maxNum = "10"
	}
	max, err := strconv.Atoi(maxNum)
	if err != nil || max <= 0 {
		max = 10
	}
	if max > 100 {
		max = 100
	}

	type followerStats struct {
		Username      Username `json:"username"`
		FollowerCount int      `json:"follower_count"`
	}

	followersList := make([]followerStats, 0, max*2)

	userStatusMap := make(map[Username]bool)
	usersMutex.RLock()
	for _, user := range users {
		if user.IsBanned() || user.IsPrivate() {
			continue
		}
		username := user.GetUsername()
		userStatusMap[username.ToLower()] = true
	}
	usersMutex.RUnlock()

	followersMutex.RLock()
	for userId, data := range followersData {
		username := userId.User().GetUsername()
		if userStatusMap[username.ToLower()] {
			followersList = append(followersList, followerStats{
				Username:      username,
				FollowerCount: len(data.Followers),
			})
		}
	}
	followersMutex.RUnlock()

	sort.Slice(followersList, func(i, j int) bool {
		return followersList[i].FollowerCount > followersList[j].FollowerCount
	})
	if len(followersList) > max {
		followersList = followersList[:max]
	}

	c.JSON(200, followersList)
}
