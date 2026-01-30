package main

import (
	"log"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

var postLimits = map[string]int{
	"content_length":         300,
	"content_length_premium": 600,
	"attachment_length":      200,
}

var lockedKeys = []string{"last_login", "max_size", "key", "created", "system", "discord_id"}

func getLimits(c *gin.Context) {
	c.JSON(200, postLimits)
}

func createPost(c *gin.Context) {
	rateLimitKey := getRateLimitKey(c)
	isAllowed, remaining, resetTime := applyRateLimit(rateLimitKey, "post")

	if !isAllowed {
		c.Header("X-RateLimit-Limit", strconv.Itoa(rateLimits["post"].Count))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))
		c.Header("X-RateLimit-Reset", strconv.FormatFloat(resetTime, 'f', 0, 64))
		c.JSON(429, gin.H{"error": "Rate limit exceeded. Try again later."})
		return
	}

	user := c.MustGet("user").(*User)

	content := c.Query("content")
	if content == "" {
		c.JSON(400, gin.H{"error": "Content is required"})
		return
	}

	osParam := c.Query("os")
	if osParam != "" {
		systems := getAllSystems()
		keys := make([]string, len(systems))
		i := 0
		for k := range systems {
			keys[i] = k
			i++
		}
		isValid := slices.Contains(keys, osParam)
		if !isValid {
			c.JSON(400, gin.H{"error": "OS is invalid"})
			return
		}
	}

	// claw key id, this is not a security issue
	chars := postLimits["content_length"]
	if doesUserOwnKey(user.GetUsername(), "bd6249d2b87796a25c30b1f1722f784f") {
		chars = postLimits["content_length_premium"]
	}

	// Check content length
	if len(content) > chars {
		c.JSON(400, gin.H{"error": "Content exceeds " + strconv.Itoa(chars) + " character limit"})
		return
	}

	// Check for derogatory language
	if containsDerogatory(content) {
		c.JSON(400, gin.H{"error": "Post contains prohibited language"})
		return
	}

	// Get attachment if available
	var attachment *string
	if attachmentStr := c.Query("attachment"); attachmentStr != "" {
		if len(attachmentStr) > postLimits["attachment_length"] {
			c.JSON(400, gin.H{"error": "Attachment URL exceeds " + strconv.Itoa(postLimits["attachment_length"]) + " character limit"})
			return
		}

		if !strings.HasPrefix(attachmentStr, "http://") && !strings.HasPrefix(attachmentStr, "https://") {
			c.JSON(400, gin.H{"error": "Attachment must be a valid URL"})
			return
		}

		// Check if attachment is from a banned domain
		if isFromBannedDomain(attachmentStr) {
			c.JSON(400, gin.H{"error": "Attachment from prohibited website"})
			return
		}

		// Verify MIME type
		allowedMimeTypes := []string{"image/png", "image/jpeg", "image/gif"}
		if !isValidMimeType(attachmentStr, allowedMimeTypes) {
			c.JSON(400, gin.H{"error": "Attachment must be a valid image (PNG, JPEG, GIF)"})
			return
		}

		attachment = &attachmentStr
	}

	// Check if post is profile-only
	profileOnly := c.Query("profile_only") == "1"

	// Create new post
	newPost := Post{
		ID:          generateToken(),
		Content:     content,
		User:        user.GetId(),
		Timestamp:   time.Now().UnixMilli(),
		Attachment:  attachment,
		ProfileOnly: profileOnly,
	}

	if osParam != "" {
		newPost.OS = &osParam
	}

	postsMutex.Lock()
	posts = append(posts, newPost)
	postsMutex.Unlock()

	go savePosts()

	// Send the new post to the WebSocket server only if it's not profile-only
	if !profileOnly {
		go func() {
			broadcastClawEvent("new_post", newPost)
			sendPostToDiscord(newPost)
		}()
	}

	c.JSON(201, newPost)
}

func getPostById(id string) *Post {
	postsMutex.Lock()
	defer postsMutex.Unlock()

	var targetPost *Post = nil
	for i := range posts {
		if posts[i].ID == id {
			targetPost = &posts[i]
			break
		}
	}

	return targetPost
}

func replyToPost(c *gin.Context) {
	// Rate limiting check

	user := c.MustGet("user").(*User)

	postID := c.Query("id")
	if postID == "" {
		c.JSON(400, gin.H{"error": "Post ID is required"})
		return
	}

	content := c.Query("content")
	if content == "" {
		c.JSON(400, gin.H{"error": "Content is required"})
		return
	}

	// Check content length
	postLimit := postLimits["content_length"]
	if doesUserOwnKey(user.GetUsername(), "bd6249d2b87796a25c30b1f1722f784f") {
		postLimit = postLimits["content_length_premium"]
	}

	if len(content) > postLimit {
		c.JSON(400, gin.H{"error": "Content exceeds " + strconv.Itoa(postLimit) + " character limit"})
		return
	}

	// Check for derogatory language
	if containsDerogatory(content) {
		c.JSON(400, gin.H{"error": "Reply contains prohibited language"})
		return
	}

	var targetPost *Post = getPostById(postID)

	if targetPost == nil {
		c.JSON(404, gin.H{"error": "Post not found"})
		return
	}

	idx := getIdxOfAccountBy("username", targetPost.User.String())
	targetUser, err := getUserByIdx(idx)
	if isUserBlockedBy(*targetUser, user.GetId()) || err != nil {
		c.JSON(400, gin.H{"error": "You cant reply to this post"})
		return
	}

	if targetPost.Replies == nil {
		targetPost.Replies = make([]Reply, 0)
	}

	newReply := Reply{
		ID:        generateToken(),
		Content:   content,
		User:      user.GetId(),
		Timestamp: time.Now().UnixMilli(),
	}

	targetPost.Replies = append(targetPost.Replies, newReply)

	go savePosts()

	addUserEvent(targetPost.User, "reply", map[string]any{
		"post_id":  postID,
		"reply_id": newReply.ID,
		"user":     user.GetUsername(),
		"content":  content,
	})

	c.JSON(201, newReply)
}

func deletePost(c *gin.Context) {
	user := c.MustGet("user").(*User)

	postID := c.Query("id")
	if postID == "" {
		c.JSON(400, gin.H{"error": "Post ID is required"})
		return
	}

	postsMutex.Lock()
	var postToDelete *Post
	newPosts := make([]Post, 0)

	for i := range posts { // use index to safely take address
		if posts[i].ID == postID {
			postToDelete = &posts[i]
			// Check if the user can delete this post
			if user.GetUsername().ToLower() != "mist" && user.GetId() != posts[i].User {
				postsMutex.Unlock()
				c.JSON(403, ErrorResponse{Error: "You cannot delete this post"})
				return
			}
		} else {
			newPosts = append(newPosts, posts[i])
		}
	}

	if postToDelete == nil {
		postsMutex.Unlock()
		c.JSON(404, ErrorResponse{Error: "Post not found"})
		return
	}

	wasPublic := !postToDelete.ProfileOnly
	posts = newPosts
	postsMutex.Unlock()

	go savePosts()

	// Broadcast deletion event for public posts
	if wasPublic {
		go broadcastClawEvent("delete_post", map[string]any{"id": postID})
	}

	c.JSON(200, gin.H{"message": "Post deleted successfully"})
}

func ratePost(c *gin.Context) {
	user := c.MustGet("user").(*User)

	postID := c.Query("id")
	if postID == "" {
		c.JSON(400, gin.H{"error": "Post ID is required"})
		return
	}

	ratingStr := c.Query("rating")
	if ratingStr == "" {
		c.JSON(400, gin.H{"error": "Rating is required"})
		return
	}

	rating, err := strconv.Atoi(ratingStr)
	if err != nil || (rating != 0 && rating != 1) {
		c.JSON(400, gin.H{"error": "Rating must be 1 (like) or 0 (unlike)"})
		return
	}

	var targetPost *Post = getPostById(postID)

	if targetPost == nil {
		c.JSON(404, gin.H{"error": "Post not found"})
		return
	}

	if rating == 1 {
		idx := getIdxOfAccountBy("username", targetPost.User.String())
		if idx == -1 {
			c.JSON(400, gin.H{"error": "User does not exist"})
			return
		}
		targetUser, err := getUserByIdx(idx)
		if isUserBlockedBy(*targetUser, user.GetId()) || err != nil {
			c.JSON(400, gin.H{"error": "You cant like this post"})
			return
		}
	}

	// Ensure the 'likes' array exists
	if targetPost.Likes == nil {
		targetPost.Likes = make([]UserId, 0)
	}

	userId := user.GetId()

	// Like or unlike based on the rating
	if rating == 1 {
		// Check if user already liked
		alreadyLiked := slices.Contains(targetPost.Likes, userId)
		if !alreadyLiked {
			targetPost.Likes = append(targetPost.Likes, userId)
		}
	} else {
		// Remove like
		newLikes := make([]UserId, 0)
		for _, liker := range targetPost.Likes {
			if liker != userId {
				newLikes = append(newLikes, liker)
			}
		}
		targetPost.Likes = newLikes
	}

	go savePosts()

	// Broadcast rating update for public posts
	if !targetPost.ProfileOnly {
		go broadcastClawEvent("update_post", map[string]any{
			"id":   postID,
			"key":  "likes",
			"data": targetPost.Likes,
		})
	}

	c.JSON(200, gin.H{
		"message": "Post rated successfully",
		"likes":   targetPost.Likes,
	})
}

func repost(c *gin.Context) {
	user := c.MustGet("user").(*User)

	postID := c.Query("id")
	if postID == "" {
		c.JSON(400, gin.H{"error": "Post ID is required"})
		return
	}

	// Find the original post by ID
	var originalPost *Post = getPostById(postID)

	if originalPost == nil {
		c.JSON(404, gin.H{"error": "Original post not found"})
		return
	}

	idx := getIdxOfAccountBy("username", originalPost.User.String())
	originalUser, err := getUserByIdx(idx)
	if isUserBlockedBy(*originalUser, user.GetId()) || err != nil {
		c.JSON(400, gin.H{"error": "You cant repost this post"})
		return
	}

	// Check if the original post is profile-only
	if originalPost.ProfileOnly {
		c.JSON(403, gin.H{"error": "Cannot repost a profile-only post"})
		return
	}

	if originalPost.IsRepost {
		c.JSON(403, gin.H{"error": "Cannot repost a repost"})
		return
	}

	// Create the repost
	newRepost := Post{
		ID:        generateToken(),
		User:      user.GetId(),
		Timestamp: time.Now().UnixMilli(),
		IsRepost:  true,
		OriginalPost: &Post{
			ID:         originalPost.ID,
			Content:    originalPost.Content,
			User:       originalPost.User,
			Timestamp:  originalPost.Timestamp,
			Attachment: originalPost.Attachment,
		},
		ProfileOnly: true, // Reposts are profile-only as per Python implementation
	}

	if originalPost.OS != nil {
		newRepost.OriginalPost.OS = originalPost.OS
	}

	postsMutex.Lock()
	posts = append(posts, newRepost)
	postsMutex.Unlock()

	go savePosts()

	addUserEvent(originalPost.User, "repost", map[string]any{
		"repost_id":        newRepost.ID,
		"user":             user.GetUsername(),
		"original_post_id": originalPost.ID,
	})

	c.JSON(201, newRepost)
}

func pinPost(c *gin.Context) {
	user := c.MustGet("user").(*User)

	postID := c.Query("id")
	if postID == "" {
		c.JSON(400, gin.H{"error": "Post ID is required"})
		return
	}

	var targetPost *Post = getPostById(postID)

	if targetPost == nil {
		c.JSON(404, gin.H{"error": "Post not found"})
		return
	}

	// Check if the user is the owner of the post
	if targetPost.User != user.GetId() {
		c.JSON(403, gin.H{"error": "You can only pin your own posts"})
		return
	}

	postsMutex.Lock()
	targetPost.Pinned = true
	postsMutex.Unlock()

	go savePosts()

	// Broadcast pin update for public posts
	if !targetPost.ProfileOnly {
		go broadcastClawEvent("update_post", map[string]any{
			"id":   postID,
			"key":  "pinned",
			"data": true,
		})
	}

	c.JSON(200, gin.H{"message": "Post pinned successfully"})
}

func unpinPost(c *gin.Context) {
	user := c.MustGet("user").(*User)

	postID := c.Query("id")
	if postID == "" {
		c.JSON(400, gin.H{"error": "Post ID is required"})
		return
	}

	var targetPost *Post = getPostById(postID)

	if targetPost == nil {
		c.JSON(404, gin.H{"error": "Post not found"})
		return
	}

	// Check if the user is the owner of the post
	if targetPost.User != user.GetId() {
		c.JSON(403, gin.H{"error": "You can only unpin your own posts"})
		return
	}

	postsMutex.Lock()
	targetPost.Pinned = false
	postsMutex.Unlock()

	go savePosts()

	// Broadcast unpin update for public posts
	if !targetPost.ProfileOnly {
		go broadcastClawEvent("update_post", map[string]any{
			"id":   postID,
			"key":  "pinned",
			"data": false,
		})
	}

	c.JSON(200, gin.H{"message": "Post unpinned successfully"})
}

func searchPosts(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		c.JSON(400, gin.H{"error": "Search query is required"})
		return
	}

	limitStr := c.DefaultQuery("limit", "20")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 20
	} else {
		limit = clamp(limit, 1, 20)
	}

	queryLower := strings.ToLower(query)

	// Search posts by content
	searchResults := make([]NetPost, 0)
	postsMutex.RLock()
	for _, post := range posts {
		if strings.Contains(strings.ToLower(post.Content), queryLower) {
			searchResults = append(searchResults, post.ToNet())
			if len(searchResults) >= limit {
				break
			}
		}
	}
	postsMutex.RUnlock()

	// Reverse to show newest first
	for i := len(searchResults)/2 - 1; i >= 0; i-- {
		opp := len(searchResults) - 1 - i
		searchResults[i], searchResults[opp] = searchResults[opp], searchResults[i]
	}

	c.JSON(200, searchResults)
}

func getTopPosts(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "50")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 50
	} else {
		limit = clamp(limit, 1, 50)
	}

	timePeriodStr := c.DefaultQuery("time_period", "24")
	timePeriod, err := strconv.Atoi(timePeriodStr)
	if err != nil || timePeriod <= 0 {
		timePeriod = 24
	}

	// Filter posts within the time period, excluding profile-only posts
	currentTime := time.Now().Unix()

	postsWithinPeriod := make([]NetPost, 0)
	postsMutex.RLock()
	for _, post := range posts {
		postTime := post.Timestamp / 1000 // Convert milliseconds to seconds
		if (currentTime-postTime) <= int64(timePeriod*3600) && !post.ProfileOnly {
			postsWithinPeriod = append(postsWithinPeriod, post.ToNet())
		}
	}
	postsMutex.RUnlock()

	// Sort posts by likes count
	sort.Slice(postsWithinPeriod, func(i, j int) bool {
		return len(postsWithinPeriod[i].Likes) > len(postsWithinPeriod[j].Likes)
	})

	// Apply limit
	if len(postsWithinPeriod) > limit {
		postsWithinPeriod = postsWithinPeriod[:limit]
	}

	c.JSON(200, postsWithinPeriod)
}

var envOnce sync.Once

func loadEnvFile() {
	// Prefer workspace root .env (one directory up) then fall back to local .env.
	// Root file now holds authoritative configuration; local .env may override selectively if present.
	parent := "../.env"
	local := ".env"
	if _, err := os.Stat(parent); err == nil {
		if err := godotenv.Overload(parent); err != nil {
			log.Printf("[env] failed to load parent .env: %v", err)
		} else {
			log.Printf("[env] loaded parent .env (%s)", parent)
		}
	} else {
		log.Printf("[env] parent .env not found (%s): %v", parent, err)
	}
	if _, err := os.Stat(local); err == nil {
		if err := godotenv.Overload(local); err != nil {
			log.Printf("[env] failed to load local .env overrides: %v", err)
		} else {
			log.Printf("[env] loaded local .env overrides (%s)", local)
		}
	}
	// Reload config variables after populating environment
	loadConfigFromEnv()
}

func getFeed(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "100")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 100
	} else {
		limit = clamp(limit, 1, 100)
	}

	offsetStr := c.DefaultQuery("offset", "0")
	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	postsMutex.RLock()
	// Filter out profile-only posts
	publicPosts := make([]NetPost, 0)
	for _, post := range posts {
		if !post.ProfileOnly {
			publicPosts = append(publicPosts, post.ToNet())
		}
	}
	postsMutex.RUnlock()

	// Sort by timestamp (newest first)
	sort.Slice(publicPosts, func(i, j int) bool {
		return publicPosts[i].Timestamp > publicPosts[j].Timestamp
	})

	// Apply offset and limit to get the newest posts
	start := offset
	end := offset + limit
	if start > len(publicPosts) {
		start = len(publicPosts)
	}
	if end > len(publicPosts) {
		end = len(publicPosts)
	}
	result := publicPosts[start:end]

	c.JSON(200, result)
}

func getFollowingFeed(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "100")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 100
	}
	if limit > 100 {
		limit = 100
	}

	user := c.MustGet("user").(*User)

	userId := user.GetId()

	// Get the list of users the authenticated user is following
	following := make([]UserId, 0)
	followersMutex.RLock()
	for targetUser, data := range followersData {
		if slices.Contains(data.Followers, userId) {
			following = append(following, targetUser)
		}
	}
	followersMutex.RUnlock()

	// Get posts from followed users (excluding profile-only posts from other users)
	postsMutex.RLock()
	followingPosts := make([]NetPost, 0)
	for _, post := range posts {
		isFollowed := slices.Contains(following, post.User)
		if isFollowed && !(post.ProfileOnly && post.User != userId) {
			followingPosts = append(followingPosts, post.ToNet())
		}
	}
	postsMutex.RUnlock()

	// Sort by timestamp (newest first)
	sort.Slice(followingPosts, func(i, j int) bool {
		return followingPosts[i].Timestamp > followingPosts[j].Timestamp
	})

	// Apply limit - get last 'limit' posts and reverse
	var result = make([]NetPost, 0)
	if len(followingPosts) > limit {
		result = followingPosts[len(followingPosts)-limit:]
	} else {
		result = followingPosts
	}

	c.JSON(200, result)
}
