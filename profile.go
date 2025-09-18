package main

import (
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func getProfile(c *gin.Context) {
	// Rate limiting check
	rateLimitKey := getRateLimitKey(c)
	isAllowed, remaining, resetTime := applyRateLimit(rateLimitKey, "profile")

	if !isAllowed {
		c.Header("X-RateLimit-Limit", strconv.Itoa(rateLimits["profile"].Count))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))
		c.Header("X-RateLimit-Reset", strconv.FormatFloat(resetTime, 'f', 0, 64))
		c.JSON(429, gin.H{"error": "Rate limit exceeded. Try again later."})
		return
	}

	name := c.Query("username")
	if name == "" {
		name = c.Query("name")
	}
	discord_id := c.Query("discord_id")
	if name == "" && discord_id == "" {
		c.JSON(400, gin.H{"error": "Name or Discord ID is required"})
		return
	}

	authKey := c.Query("auth")
	includePosts := c.DefaultQuery("include_posts", "1") == "1"

	// Convert the name to lowercase for case-insensitive comparison
	nameLower := strings.ToLower(name)

	// Find user with case-insensitive matching
	usersMutex.RLock()
	var foundUser *User
	var userIndex int
	if discord_id != "" {
		for i, user := range users {
			if user.Get("discord_id") == discord_id {
				foundUser = &user
				userIndex = i
				break
			}
		}
	} else {
		for i, user := range users {
			if strings.ToLower(user.GetUsername()) == nameLower {
				foundUser = &user
				userIndex = i
				break
			}
		}
	}
	usersMutex.RUnlock()

	if foundUser == nil {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}

	// Get user posts
	postsMutex.RLock()
	pinnedPosts := make([]Post, 0)
	regularPosts := make([]Post, 0)

	for _, post := range posts {
		if strings.ToLower(post.User) == nameLower {
			if post.Pinned {
				pinnedPosts = append(pinnedPosts, post)
			} else {
				regularPosts = append(regularPosts, post)
			}
		}
	}
	postsMutex.RUnlock()

	// Sort posts by timestamp (newest first)
	sort.Slice(pinnedPosts, func(i, j int) bool {
		return pinnedPosts[i].Timestamp > pinnedPosts[j].Timestamp
	})
	sort.Slice(regularPosts, func(i, j int) bool {
		return regularPosts[i].Timestamp > regularPosts[j].Timestamp
	})

	// Get follower count
	followersMutex.RLock()
	followerCount := 0
	if data, exists := followersData[nameLower]; exists {
		followerCount = len(data.Followers)
	}

	// Get following count
	followingCount := 0
	for _, data := range followersData {
		for _, follower := range data.Followers {
			if follower == nameLower {
				followingCount++
				break
			}
		}
	}
	followersMutex.RUnlock()

	// Initialize follow relationship info
	var isFollowing *bool
	var isFollowedBy *bool

	if authKey != "" {
		// Authenticate the user with the provided auth key
		authenticatedUser := authenticateWithKey(authKey)
		if authenticatedUser != nil {
			authenticatedUsername := strings.ToLower(authenticatedUser.GetUsername())

			// Check if the authenticated user is following the target user
			followersMutex.RLock()
			if data, exists := followersData[nameLower]; exists {
				for _, follower := range data.Followers {
					if follower == authenticatedUsername {
						following := true
						isFollowing = &following
						break
					}
				}
			}
			if isFollowing == nil {
				notFollowing := false
				isFollowing = &notFollowing
			}

			// Check if the target user is following the authenticated user
			if data, exists := followersData[authenticatedUsername]; exists {
				for _, follower := range data.Followers {
					if follower == nameLower {
						followedBy := true
						isFollowedBy = &followedBy
						break
					}
				}
			}
			if isFollowedBy == nil {
				notFollowedBy := false
				isFollowedBy = &notFollowedBy
			}
			followersMutex.RUnlock()
		}
	}

	// Get subscription info
	maxSizeStr := "5000000"
	if maxSize := foundUser.Get("max_size"); maxSize != nil {
		if maxSizeFloat, ok := maxSize.(float64); ok {
			maxSizeStr = strconv.FormatFloat(maxSizeFloat, 'f', 0, 64)
		} else if maxSizeString, ok := maxSize.(string); ok {
			maxSizeStr = maxSizeString
		}
	}

	var sub string
	switch maxSizeStr {
	case "5000000":
		sub = "Free"
	case "10000000":
		sub = "Supporter"
	case "50000000":
		sub = "originDrive"
	case "100000000":
		sub = "originPro"
	case "1000000000":
		sub = "originMax"
	default:
		sub = "Free"
	}

	// Calculate dynamic badges
	calculatedBadges := calculateUserBadges(*foundUser)

	st, err := loadUserStatus(foundUser.GetUsername())
	if err != nil {
		st = nil
	}

	// Get marriage status if applicable
	var marriageStatus map[string]any
	if marriage := foundUser.Get("sys.marriage"); marriage != nil {
		if marriageMap, ok := marriage.(map[string]any); ok {
			marriageStatus = marriageMap
		}
	}

	profileData := gin.H{
		"username":     foundUser.GetUsername(),
		"pfp":          "https://avatars.rotur.dev/" + foundUser.GetUsername(),
		"followers":    followerCount,
		"following":    followingCount,
		"bio":          getStringOrEmpty(foundUser.Get("bio")),
		"pronouns":     getStringOrEmpty(foundUser.Get("pronouns")),
		"system":       getStringOrEmpty(foundUser.Get("system")),
		"created":      foundUser.GetCreated(),
		"badges":       calculatedBadges,
		"marriage":     marriageStatus,
		"subscription": sub,
		"theme":        foundUser.Get("theme"),
		"max_size":     maxSizeStr,
		"currency":     foundUser.Get("sys.currency"),
		"index":        userIndex + 1,
		"private":      strings.ToLower(getStringOrEmpty(foundUser.Get("private"))) == "true",
		"status":       st,
	}
	if foundUser.Get("banner") != nil {
		profileData["banner"] = "https://avatars.rotur.dev/.banners/" + foundUser.GetUsername()
	}

	if includePosts {
		// Combine pinned and regular posts (pinned first, then regular, both in reverse chronological order)
		allUserPosts := make([]Post, 0, len(pinnedPosts)+len(regularPosts))

		for i := 0; i < len(pinnedPosts); i++ {
			allUserPosts = append(allUserPosts, pinnedPosts[i])
		}
		for i := 0; i < len(regularPosts); i++ {
			allUserPosts = append(allUserPosts, regularPosts[i])
		}

		profileData["posts"] = allUserPosts
	}

	// Add follow relationship info if available
	if authKey != "" && isFollowing != nil {
		profileData["followed"] = *isFollowing
		profileData["follows_me"] = *isFollowedBy
	}

	if marriage := foundUser.Get("sys.marriage"); marriage != nil {
		if marriageMap, ok := marriage.(map[string]any); ok {
			if partner, ok := marriageMap["partner"].(string); ok && partner != "" {
				profileData["married_to"] = partner
			}
		}
	}

	c.JSON(200, profileData)
}

func getSupporters(c *gin.Context) {
	// anyone who isnt a free user is considered a supporter

	supporters := make([]gin.H, 0)
	usersMutex.RLock()
	for _, user := range users {
		maxSizeStr := "5000000"
		if maxSize := user.Get("max_size"); maxSize != nil {
			if maxSizeFloat, ok := maxSize.(float64); ok {
				maxSizeStr = strconv.FormatFloat(maxSizeFloat, 'f', 0, 64)
			} else if maxSizeString, ok := maxSize.(string); ok {
				maxSizeStr = maxSizeString
			}
		}

		subscriptionName := "Free"
		switch maxSizeStr {
		case "10000000":
			subscriptionName = "Supporter"
		case "50000000":
			subscriptionName = "originDrive"
		case "100000000":
			subscriptionName = "originPro"
		case "1000000000":
			subscriptionName = "Admin"
		}

		if subscriptionName == "Free" {
			continue
		}
		supporters = append(supporters, gin.H{
			"username":     user.GetUsername(),
			"subscription": subscriptionName,
		})
	}
	usersMutex.RUnlock()
	c.JSON(200, supporters)
}
