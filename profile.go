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

type profileResp struct {
	Username     Username       `json:"username"`
	DiscordID    string         `json:"discord_id"`
	Avatar       string         `json:"pfp"`
	Banned       bool           `json:"sys.banned"`
	Private      bool           `json:"private"`
	Bio          string         `json:"bio"`
	Banner       string         `json:"banner"`
	Followers    int            `json:"followers"`
	Following    int            `json:"following"`
	Pronouns     string         `json:"pronouns"`
	System       string         `json:"system"`
	Created      int64          `json:"created"`
	Badges       []Badge        `json:"badges"`
	Subscription string         `json:"subscription"`
	MaxSize      string         `json:"max_size"`
	Currency     float64        `json:"currency"`
	Index        int            `json:"index"`
	PrivateData  any            `json:"private_data,omitempty"`
	Balance      any            `json:"balance,omitempty"`
	Blocked      []string       `json:"blocked,omitempty"`
	Posts        []NetPost      `json:"posts,omitempty"`
	Status       *UserStatus    `json:"status,omitempty"`
	Theme        map[string]any `json:"theme,omitempty"`
	Followed     bool           `json:"followed,omitempty"`
	FollowsMe    bool           `json:"follows_me,omitempty"`
	MarriedTo    Username       `json:"married_to,omitempty"`
	Id           UserId         `json:"id"`
}

func renderBioRegex(bio string, profile *User, otherKeys profileResp) string {
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
	safeProfile["bio"] = otherKeys.Bio
	safeProfile["followers"] = fmt.Sprint(otherKeys.Followers)
	safeProfile["following"] = fmt.Sprint(otherKeys.Following)

	re := regexp.MustCompile(`{{\s*([a-zA-Z0-9_]+)(?:\s+([^}]+?))?\s*}}`)

	result := re.ReplaceAllStringFunc(bio, func(match string) string {
		sub := re.FindStringSubmatch(match)
		if len(sub) < 2 {
			return ""
		}

		prefix := sub[1]
		key := ""
		if len(sub) >= 3 {
			key = sub[2]
		}

		switch prefix {
		case "user":
			if val, ok := safeProfile[key]; ok {
				return val
			}
			return ""
		case "time":
			tier := profile.GetSubscription().Tier
			if !hasTierOrHigher(tier, "drive") {
				return "{{ Error, time only available to Drive users }}"
			}

			format := strings.TrimSpace(key)
			if format == "" {
				format = "15:04"
			}
			format = normalizeUserTimeLayout(format)

			tzStr := getStringOrEmpty(profile.Get("timezone"))
			offsetHours, ok := parseUTCOffsetHours(tzStr)
			if !ok {
				offsetHours = 0
			}

			loc := time.FixedZone(fmt.Sprintf("UTC%+d", offsetHours), offsetHours*60*60)
			return time.Now().UTC().In(loc).Format(format)
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
		case "flex":
			if key == "economy%" {
				currencyData := getUserCreditData()

				total := 0.0
				for _, amount := range currencyData {
					total += amount
				}

				if total == 0 {
					return "0%"
				}

				current := getFloatOrDefault(safeProfile["sys.currency"], 0.0)
				return fmt.Sprintf("%.2f%%", (current/total)*100)
			}

		}
		return ""
	})
	return result
}

func getProfile(c *gin.Context) {
	nameRaw := c.Query("username")
	if nameRaw == "" {
		nameRaw = c.Query("name")
	}

	discord_id := c.Query("discord_id")
	if nameRaw == "" && discord_id == "" {
		c.JSON(400, gin.H{"error": "Name or Discord ID is required"})
		return
	}

	authKey := c.Query("auth")
	includePosts := c.DefaultQuery("include_posts", "1") == "1"

	// Convert the name to lowercase for case-insensitive comparison
	name := Username(nameRaw)
	nameLower := name.ToLower()
	// Find user with case-insensitive matching
	var foundUser *User
	if discord_id != "" {
		foundUsers, err := getAccountsBy("discord_id", discord_id, 1)
		if err != nil {
			c.JSON(404, gin.H{"error": "User not found"})
			return
		}
		foundUser = foundUsers[0]
		name = foundUser.GetUsername()
		nameLower = name.ToLower()
	} else {
		fmt.Println("Searching for", name.String())
		foundUsers, err := getAccountsBy("username", name.String(), 1)
		fmt.Println("Found", len(foundUsers), "users")
		if err != nil {
			fmt.Println("Error", err.Error())
			c.JSON(404, gin.H{"error": "User not found"})
			return
		}
		foundUser = foundUsers[0]
	}
	userIndex := getIdxOfAccountBy("username", nameLower.String())

	userId := foundUser.GetId()

	if foundUser.IsBanned() {
		c.JSON(200, profileResp{
			Username:  foundUser.GetUsername(),
			Badges:    []Badge{},
			Currency:  0,
			MaxSize:   "0",
			Avatar:    "https://avatars.rotur.dev/.banned_user",
			Bio:       "This user is banned.",
			Banner:    "https://avatars.rotur.dev/.banners/.banned_user",
			Index:     0,
			Followers: 0,
			Following: 0,
			Posts:     []NetPost{},
			Created:   time.Now().UnixMilli(),
			Private:   true,
			Banned:    true,
			Id:        "",
		})
		return
	}
	// Get user posts
	pinnedPosts := make([]NetPost, 0)
	regularPosts := make([]NetPost, 0)

	if includePosts {
		postsMutex.RLock()
		for _, post := range posts {
			if post.User == userId {
				if post.Pinned {
					pinnedPosts = append(pinnedPosts, post.ToNet())
				} else {
					regularPosts = append(regularPosts, post.ToNet())
				}
			}
		}
		postsMutex.RUnlock()
	}

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
	if data, exists := followersData[userId]; exists {
		followerCount = len(data.Followers)
	}

	// Get following count
	followingCount := 0
	for _, data := range followersData {
		if slices.Contains(data.Followers, userId) {
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
			authenticatedId := authenticatedUser.GetId()

			// Check if the authenticated user is following the target user
			followersMutex.RLock()
			if data, exists := followersData[userId]; exists {
				if slices.Contains(data.Followers, authenticatedId) {
					following := true
					isFollowing = &following
				}
			}
			if isFollowing == nil {
				notFollowing := false
				isFollowing = &notFollowing
			}

			// Check if the target user is following the authenticated user
			if data, exists := followersData[authenticatedId]; exists {
				if slices.Contains(data.Followers, userId) {
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

	maxSizeStr := foundUser.GetMaxSize()
	sub := foundUser.GetSubscription().Tier

	// Calculate dynamic badges
	calculatedBadges := calculateUserBadges(foundUser)

	st, err := loadUserStatus(foundUser.GetUsername())
	if err != nil {
		st = nil
	}

	profileData := profileResp{
		Username:     foundUser.GetUsername(),
		Avatar:       "https://avatars.rotur.dev/" + foundUser.GetUsername().String(),
		Followers:    followerCount,
		Following:    followingCount,
		Pronouns:     getStringOrEmpty(foundUser.Get("pronouns")),
		System:       foundUser.GetSystem(),
		Created:      foundUser.GetCreated(),
		Badges:       calculatedBadges,
		Subscription: sub,
		Theme:        foundUser.GetTheme(),
		MaxSize:      maxSizeStr,
		Currency:     foundUser.GetCredits(),
		Index:        userIndex + 1,
		Private:      foundUser.IsPrivate(),
		Status:       st,
		Id:           foundUser.GetId(),
	}

	benefits := foundUser.GetSubscriptionBenefits()
	bio := getStringOrEmpty(foundUser.Get("bio"))
	if bio != "" && benefits.Has_Bio_templating {
		bio = renderBioRegex(bio, foundUser, profileData)
	}

	if len(bio) > benefits.Bio_Length {
		bio = bio[:benefits.Bio_Length]
	}
	profileData.Bio = bio

	if foundUser.Get("sys.banner") != nil || foundUser.Get("banner") != nil {
		profileData.Banner = "https://avatars.rotur.dev/.banners/" + foundUser.GetUsername().String()
	}

	if includePosts {
		// Combine pinned and regular posts (pinned first, then regular, both in reverse chronological order)
		allUserPosts := make([]NetPost, 0, len(pinnedPosts)+len(regularPosts))

		for i := 0; i < len(pinnedPosts); i++ {
			allUserPosts = append(allUserPosts, pinnedPosts[i])
		}
		for i := 0; i < len(regularPosts); i++ {
			allUserPosts = append(allUserPosts, regularPosts[i])
		}

		profileData.Posts = allUserPosts
	}

	// Add follow relationship info if available
	if authKey != "" && isFollowing != nil {
		profileData.Followed = *isFollowing
		profileData.FollowsMe = *isFollowedBy
	}

	if marriage := foundUser.GetMarriage(); marriage.Status == "married" {
		profileData.MarriedTo = getUserById(marriage.Partner).GetUsername()
	}

	c.JSON(200, profileData)
}

func getExists(c *gin.Context) {
	type resp struct {
		Exists bool `json:"exists"`
	}

	username := Username(c.Query("username")).ToLower()
	if username == "" {
		c.JSON(400, gin.H{"error": "Username is required"})
		return
	}

	usersMutex.RLock()
	defer usersMutex.RUnlock()

	for _, user := range users {
		if user.GetUsername().ToLower() == username {
			c.JSON(200, resp{Exists: true})
			return
		}
	}
	c.JSON(200, resp{Exists: false})
}

func getSupporters(c *gin.Context) {
	// anyone who isnt a free user is considered a supporter

	type resp struct {
		Username     Username `json:"username"`
		Subscription string   `json:"subscription"`
	}

	supporters := make([]resp, 0)
	usersMutex.RLock()
	defer usersMutex.RUnlock()
	for _, user := range users {
		subscriptionName := user.GetSubscription().Tier
		if subscriptionName == "Free" {
			continue
		}
		supporters = append(supporters, resp{
			Username:     user.GetUsername(),
			Subscription: subscriptionName,
		})
	}
	c.JSON(200, supporters)
}
