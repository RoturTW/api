package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

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

	re := regexp.MustCompile(`{{\s*([a-zA-Z0-9_]+)\s+([:\/?&\-a-zA-Z0-9_.]+)\s*}}`)

	result := re.ReplaceAllStringFunc(bio, func(match string) string {
		sub := re.FindStringSubmatch(match)
		if len(sub) != 3 {
			return ""
		}

		prefix := sub[1]
		key := sub[2]

		switch prefix {
		case "user":
			if val, ok := safeProfile[key]; ok {
				return val
			}
			return ""
		case "url":
			tier := profile.GetSubscription().Tier
			if !hasTierOrHigher(tier, "Pro") {
				return "{{ Error, url only available to Pro users }}"
			}
			client := &http.Client{
				Timeout: 3 * time.Second,
			}

			resp, err := client.Get("https://proxy.mistium.com?url=" + url.QueryEscape(key))
			if err != nil {
				return ""
			}
			defer resp.Body.Close()

			limited := io.LimitReader(resp.Body, 1000)

			body, err := io.ReadAll(limited)
			if err != nil {
				return ""
			}

			if len(body) > 1000 {
				body = body[:1000]
			}

			return string(body)
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
		name = foundUser.GetUsername()
		nameLower = strings.ToLower(name)
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

	banned := foundUser.Get("sys.banned")
	if banned == "true" || banned == true {
		foundUser = &User{
			"username":   ".banned_user",
			"private":    true,
			"sys.banned": true,
		}
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
	sub := foundUser.GetSubscription().Tier

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

	benefits := foundUser.GetSubscriptionBenefits()
	bio := getStringOrEmpty(foundUser.Get("bio"))
	if bio != "" && benefits.Has_Bio_templating {
		bio = renderBioRegex(bio, foundUser, profileData)
	}

	if len(bio) > benefits.Bio_Length {
		bio = bio[:benefits.Bio_Length]
	}
	profileData["bio"] = bio

	if foundUser.Get("sys.banner") != nil || foundUser.Get("banner") != nil {
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
			status, statusExists := marriageMap["status"]
			if statusExists && status == "married" {
				partner, partnerExists := marriageMap["partner"]
				if partnerExists && partner != "" {
					profileData["married_to"] = partner
				}
			}
		}
	}

	c.JSON(200, profileData)
}

func getUserMaxSize(user *User) string {
	amt := strconv.Itoa(user.GetSubscriptionBenefits().FileSystem_Size)
	user.Set("max_size", amt)
	return amt
}

func getSupporters(c *gin.Context) {
	// anyone who isnt a free user is considered a supporter

	supporters := make([]map[string]string, 0)
	usersMutex.RLock()
	defer usersMutex.RUnlock()
	for _, user := range users {
		subscriptionName := user.GetSubscription().Tier
		if subscriptionName == "Free" {
			continue
		}
		supporters = append(supporters, map[string]string{
			"username":     user.GetUsername(),
			"subscription": subscriptionName,
		})
	}
	c.JSON(200, supporters)
}
