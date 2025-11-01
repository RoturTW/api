package main

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func renderBioRegex(bio string, profile *User, otherKeys map[string]any) string {
	safeProfile := map[string]string{}
	for k, v := range *profile {
		if k == "key" || k == "password" {
			continue
		}
		switch val := v.(type) {
		case string:
			safeProfile[k] = val
		case int:
			safeProfile[k] = strconv.Itoa(val)
		case float64:
			safeProfile[k] = fmt.Sprintf("%g", val)
		case bool:
			safeProfile[k] = strconv.FormatBool(val)
		}
	}
	for k, v := range otherKeys {
		safeProfile[k] = fmt.Sprintf("%v", v)
	}

	re := regexp.MustCompile(`{{\s*user ([a-zA-Z0-9_.]+)\s*}}`)

	result := re.ReplaceAllStringFunc(bio, func(match string) string {
		key := strings.TrimSpace(re.FindStringSubmatch(match)[1])
		if val, ok := safeProfile[key]; ok {
			return val
		}
		return ""
	})

	return result
}

func getProfile(c *gin.Context) {
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
		if slices.Contains(data.Followers, nameLower) {
			followingCount++
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
				if slices.Contains(data.Followers, authenticatedUsername) {
					following := true
					isFollowing = &following
				}
			}
			if isFollowing == nil {
				notFollowing := false
				isFollowing = &notFollowing
			}

			// Check if the target user is following the authenticated user
			if data, exists := followersData[authenticatedUsername]; exists {
				if slices.Contains(data.Followers, nameLower) {
					followedBy := true
					isFollowedBy = &followedBy
				}
			}
			if isFollowedBy == nil {
				notFollowedBy := false
				isFollowedBy = &notFollowedBy
			}
			followersMutex.RUnlock()
		}
	}

	maxSizeStr := getUserMaxSize(foundUser)
	sub := getSubscription(foundUser)

	// Calculate dynamic badges
	calculatedBadges := calculateUserBadges(*foundUser)

	st, err := loadUserStatus(foundUser.GetUsername())
	if err != nil {
		st = nil
	}

	profileData := map[string]any{
		"username":     foundUser.GetUsername(),
		"pfp":          "https://avatars.rotur.dev/" + foundUser.GetUsername(),
		"followers":    followerCount,
		"following":    followingCount,
		"pronouns":     getStringOrEmpty(foundUser.Get("pronouns")),
		"system":       getStringOrEmpty(foundUser.Get("system")),
		"created":      foundUser.GetCreated(),
		"badges":       calculatedBadges,
		"subscription": sub,
		"theme":        foundUser.Get("theme"),
		"max_size":     maxSizeStr,
		"currency":     foundUser.GetCredits(),
		"index":        userIndex + 1,
		"private":      strings.ToLower(getStringOrEmpty(foundUser.Get("private"))) == "true",
		"status":       st,
	}

	bio := getStringOrEmpty(foundUser.Get("bio"))
	if bio != "" && sub != "Free" {
		bio = renderBioRegex(bio, foundUser, profileData)
	}
	profileData["bio"] = bio

	if foundUser.Get("sys.banner") != nil {
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

func getUserMaxSize(user *User) string {
	maxSizeStr := "5000000"
	if maxSize := user.Get("max_size"); maxSize != nil {
		if maxSizeFloat, ok := maxSize.(float64); ok {
			maxSizeStr = strconv.FormatFloat(maxSizeFloat, 'f', 0, 64)
		} else if maxSizeString, ok := maxSize.(string); ok {
			maxSizeStr = maxSizeString
		}
	}
	return maxSizeStr
}

func getSubscription(user *User) string {
	// Get subscription info
	maxSizeStr := getUserMaxSize(user)

	var sub string
	switch maxSizeStr {
	case "5000000":
		sub = "Free"
	case "15000000":
		sub = "Supporter"
	case "50000000":
		sub = "originDrive"
	case "500000000":
		sub = "originPro"
	case "1000000000":
		sub = "originMax"
	default:
		sub = "Free"
	}

	return sub
}

func getSupporters(c *gin.Context) {
	// anyone who isnt a free user is considered a supporter

	supporters := make([]gin.H, 0)
	usersMutex.RLock()
	for _, user := range users {
		subscriptionName := getSubscription(&user)
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
