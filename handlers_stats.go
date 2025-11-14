package main

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func getEconomyStats(c *gin.Context) {
	usersMutex.RLock()
	defer usersMutex.RUnlock()

	currencies := make([]float64, 0)
	for _, user := range users {
		userCredits := user.GetCredits()
		if userCredits < 0 {
			continue
		}
		if user.Get("sys.banned") != nil || user.Get("private") == true {
			continue
		}
		currencies = append(currencies, userCredits)
	}

	// Skip calculation if no currency data is available
	if len(currencies) == 0 {
		c.JSON(404, gin.H{
			"error": "No currency data available",
		})
		return
	}

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
	loadEnvFile()
	pence_per_1000, err := strconv.Atoi(os.Getenv("PENCE_PER_1000"))
	if err != nil {
		pence_per_1000 = 2
	}
	penceRate := roundVal((float64(1000*pence_per_1000)/total)*100) / 100      // pence per credit
	centsRate := roundVal((float64(1000*1.31*pence_per_1000)/total)*100) / 100 // cents per credit

	c.JSON(200, gin.H{
		"average":  average,
		"total":    total,
		"variance": variance,
		"currency_comparison": gin.H{
			"pence": fmt.Sprintf("%.2fp / credit", penceRate),
			"cents": fmt.Sprintf("%.2f¢ / credit", centsRate),
		},
	})
}

func getUserStats(c *gin.Context) {
	usersMutex.RLock()
	defer usersMutex.RUnlock()

	stats := gin.H{
		"total_users":  len(users),
		"banned_users": 0,
		"active_users": 0,
	}

	for _, user := range users {
		if user.Get("sys.banned") != nil {
			stats["banned_users"] = stats["banned_users"].(int) + 1
		} else {
			stats["active_users"] = stats["active_users"].(int) + 1
		}
	}

	c.JSON(200, stats)
}

func getRichList(c *gin.Context) {
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
	isAdmin := authenticateAdmin(c)
	usersMutex.RLock()
	defer usersMutex.RUnlock()

	richList := make([]gin.H, 0)
	for _, user := range users {
		// Safely check banned or private status without forcing a type assertion.
		isBanned := user.Get("sys.banned") != nil
		isPrivate := false
		if p := user.Get("private"); p != nil {
			switch v := p.(type) {
			case bool:
				isPrivate = v
			case string:
				isPrivate = strings.ToLower(v) == "true"
			}
		}
		if (isBanned || isPrivate) && !isAdmin {
			continue
		}
		currency := user.GetCredits()
		if currency <= 0 {
			continue
		}
		richList = append(richList, gin.H{
			"username": user.Get("username"),
			"wealth":   currency,
		})
	}

	// Sort richList by wealth in descending order
	sort.Slice(richList, func(i, j int) bool {
		return richList[i]["wealth"].(float64) > richList[j]["wealth"].(float64)
	})
	if len(richList) > max {
		richList = richList[:max]
	}

	c.JSON(200, richList)
}

func getAuraStats(c *gin.Context) {
	usersMutex.RLock()
	defer usersMutex.RUnlock()

	auraStats := make(map[string]int)
	for _, user := range users {
		if user.Get("sys.banned") != nil || user.Get("private") == true {
			continue
		}
		if aura := user.Get("sys.aura"); aura != nil {
			switch v := aura.(type) {
			case string:
				auraStats[v]++
			}
		}
	}

	if len(auraStats) == 0 {
		c.JSON(404, gin.H{
			"error": "No aura data available",
		})
		return
	}

	c.JSON(200, auraStats)
}

func getSystemStats(c *gin.Context) {
	usersMutex.RLock()
	defer usersMutex.RUnlock()

	systems := make(map[string]int)
	for _, user := range users {
		if user.Get("sys.banned") != nil || user.Get("private") == true {
			continue
		}
		if system := user.Get("system"); system != nil {
			switch v := system.(type) {
			case string:
				systems[v]++
			}
		}
	}

	if len(systems) == 0 {
		c.JSON(404, gin.H{
			"error": "No system data available",
		})
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

	followersMutex.RLock()
	usersMutex.RLock()
	defer followersMutex.RUnlock()
	defer usersMutex.RUnlock()

	type followerStats struct {
		Username      string `json:"username"`
		FollowerCount int    `json:"follower_count"`
	}

	followersList := make([]followerStats, 0)

	// Create a map to check if users are banned or private
	userStatusMap := make(map[string]bool) // true = valid user
	for _, user := range users {
		if user.Get("sys.banned") == nil && user.Get("private") != true {
			if username := user.Get("username"); username != nil {
				userStatusMap[strings.ToLower(username.(string))] = true
			}
		}
	}

	// Iterate through followersData to get follower counts
	for username, data := range followersData {
		// Only include users who are not banned and not private
		if userStatusMap[username] {
			followersList = append(followersList, followerStats{
				Username:      username,
				FollowerCount: len(data.Followers),
			})
		}
	}

	// Sort followersList by follower count in descending order
	sort.Slice(followersList, func(i, j int) bool {
		return followersList[i].FollowerCount > followersList[j].FollowerCount
	})

	if len(followersList) > max {
		followersList = followersList[:max]
	}

	c.JSON(200, followersList)
}
