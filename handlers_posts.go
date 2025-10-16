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

var lockedKeys = []string{"username", "last_login", "max_size", "key", "created", "system", "id"}

type ValidatorInfo struct {
	Value     string
	Timestamp int64
}

var validatorInfos = make(map[string]ValidatorInfo)

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
	chars := 100
	if doesUserOwnKey(user.GetUsername(), "bd6249d2b87796a25c30b1f1722f784f") {
		chars = 300
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
		if len(attachmentStr) > 500 {
			c.JSON(400, gin.H{"error": "Attachment URL exceeds 500 character limit"})
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
		User:        user.GetUsername(),
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
			broadcastNewPost(newPost)
			sendPostToDiscord(newPost)
		}()
	}

	c.JSON(201, newPost)
}

func replyToPost(c *gin.Context) {
	// Rate limiting check
	rateLimitKey := getRateLimitKey(c)
	isAllowed, remaining, resetTime := applyRateLimit(rateLimitKey, "reply")

	if !isAllowed {
		c.Header("X-RateLimit-Limit", strconv.Itoa(rateLimits["reply"].Count))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))
		c.Header("X-RateLimit-Reset", strconv.FormatFloat(resetTime, 'f', 0, 64))
		c.JSON(429, gin.H{"error": "Rate limit exceeded. Try again later."})
		return
	}

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
	if len(content) > 100 {
		c.JSON(400, gin.H{"error": "Content exceeds 100 character limit"})
		return
	}

	// Check for derogatory language
	if containsDerogatory(content) {
		c.JSON(400, gin.H{"error": "Reply contains prohibited language"})
		return
	}

	postsMutex.Lock()
	var targetPost *Post
	for i := range posts {
		if posts[i].ID == postID {
			targetPost = &posts[i]
			break
		}
	}

	if targetPost == nil {
		postsMutex.Unlock()
		c.JSON(404, gin.H{"error": "Post not found"})
		return
	}

	if targetPost.Replies == nil {
		targetPost.Replies = make([]Reply, 0)
	}

	newReply := Reply{
		ID:        generateToken(),
		Content:   content,
		User:      strings.ToLower(user.GetUsername()),
		Timestamp: time.Now().UnixMilli(),
	}

	targetPost.Replies = append(targetPost.Replies, newReply)
	postsMutex.Unlock()

	go savePosts()

	go func() {
		broadcastEvent("update_post", map[string]any{
			"id":   postID,
			"key":  "replies",
			"data": targetPost.Replies,
		})
	}()

	addUserEvent(targetPost.User, "reply", map[string]any{
		"post_id":  postID,
		"reply_id": newReply.ID,
		"user":     strings.ToLower(user.GetUsername()),
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
			if strings.ToLower(user.GetUsername()) != "mist" && !strings.EqualFold(user.GetUsername(), posts[i].User) {
				postsMutex.Unlock()
				c.JSON(403, gin.H{"error": "You cannot delete this post"})
				return
			}
		} else {
			newPosts = append(newPosts, posts[i])
		}
	}

	if postToDelete == nil {
		postsMutex.Unlock()
		c.JSON(404, gin.H{"error": "Post not found"})
		return
	}

	wasPublic := !postToDelete.ProfileOnly
	posts = newPosts
	postsMutex.Unlock()

	go savePosts()

	// Broadcast deletion event for public posts
	if wasPublic {
		go broadcastEvent("delete_post", map[string]any{"id": postID})
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

	postsMutex.Lock()
	var targetPost *Post
	for i := range posts {
		if posts[i].ID == postID {
			targetPost = &posts[i]
			break
		}
	}

	if targetPost == nil {
		postsMutex.Unlock()
		c.JSON(404, gin.H{"error": "Post not found"})
		return
	}

	// Ensure the 'likes' array exists
	if targetPost.Likes == nil {
		targetPost.Likes = make([]string, 0)
	}

	username := strings.ToLower(user.GetUsername())

	// Like or unlike based on the rating
	if rating == 1 {
		// Check if user already liked
		alreadyLiked := false
		for _, liker := range targetPost.Likes {
			if liker == username {
				alreadyLiked = true
				break
			}
		}
		if !alreadyLiked {
			targetPost.Likes = append(targetPost.Likes, username)
		}
	} else {
		// Remove like
		newLikes := make([]string, 0)
		for _, liker := range targetPost.Likes {
			if liker != username {
				newLikes = append(newLikes, liker)
			}
		}
		targetPost.Likes = newLikes
	}

	postsMutex.Unlock()

	go savePosts()

	// Broadcast rating update for public posts
	if !targetPost.ProfileOnly {
		go broadcastEvent("update_post", map[string]any{
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
	postsMutex.Lock()
	var originalPost *Post
	for i := range posts {
		if posts[i].ID == postID {
			originalPost = &posts[i]
			break
		}
	}

	if originalPost == nil {
		postsMutex.Unlock()
		c.JSON(404, gin.H{"error": "Original post not found"})
		return
	}

	// Check if the original post is profile-only
	if originalPost.ProfileOnly {
		postsMutex.Unlock()
		c.JSON(403, gin.H{"error": "Cannot repost a profile-only post"})
		return
	}

	if originalPost.IsRepost {
		postsMutex.Unlock()
		c.JSON(403, gin.H{"error": "Cannot repost a repost"})
		return
	}

	// Create the repost
	newRepost := Post{
		ID:        generateToken(),
		User:      user.GetUsername(),
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

	postsMutex.Lock()
	var targetPost *Post
	for i := range posts {
		if posts[i].ID == postID {
			targetPost = &posts[i]
			break
		}
	}

	if targetPost == nil {
		postsMutex.Unlock()
		c.JSON(404, gin.H{"error": "Post not found"})
		return
	}

	// Check if the user is the owner of the post
	if !strings.EqualFold(targetPost.User, user.GetUsername()) {
		postsMutex.Unlock()
		c.JSON(403, gin.H{"error": "You can only pin your own posts"})
		return
	}

	targetPost.Pinned = true
	postsMutex.Unlock()

	go savePosts()

	// Broadcast pin update for public posts
	if !targetPost.ProfileOnly {
		go broadcastEvent("update_post", map[string]any{
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

	postsMutex.Lock()
	var targetPost *Post
	for i := range posts {
		if posts[i].ID == postID {
			targetPost = &posts[i]
			break
		}
	}

	if targetPost == nil {
		postsMutex.Unlock()
		c.JSON(404, gin.H{"error": "Post not found"})
		return
	}

	// Check if the user is the owner of the post
	if !strings.EqualFold(targetPost.User, user.GetUsername()) {
		postsMutex.Unlock()
		c.JSON(403, gin.H{"error": "You can only unpin your own posts"})
		return
	}

	targetPost.Pinned = false
	postsMutex.Unlock()

	go savePosts()

	// Broadcast unpin update for public posts
	if !targetPost.ProfileOnly {
		go broadcastEvent("update_post", map[string]any{
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
	}
	if limit > 50 {
		limit = 50
	}

	queryLower := strings.ToLower(query)

	// Search posts by content
	searchResults := make([]Post, 0)
	postsMutex.RLock()
	for _, post := range posts {
		if strings.Contains(strings.ToLower(post.Content), queryLower) {
			searchResults = append(searchResults, post)
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
	}
	if limit > 100 {
		limit = 100
	}

	timePeriodStr := c.DefaultQuery("time_period", "24")
	timePeriod, err := strconv.Atoi(timePeriodStr)
	if err != nil || timePeriod <= 0 {
		timePeriod = 24
	}

	// Filter posts within the time period, excluding profile-only posts
	currentTime := time.Now().Unix()

	postsWithinPeriod := make([]Post, 0)
	postsMutex.RLock()
	for _, post := range posts {
		postTime := post.Timestamp / 1000 // Convert milliseconds to seconds
		if (currentTime-postTime) <= int64(timePeriod*3600) && !post.ProfileOnly {
			postsWithinPeriod = append(postsWithinPeriod, post)
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
	}
	if limit > 100 {
		limit = 100
	}

	offsetStr := c.DefaultQuery("offset", "0")
	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	postsMutex.RLock()
	// Filter out profile-only posts
	publicPosts := make([]Post, 0)
	for _, post := range posts {
		if !post.ProfileOnly {
			publicPosts = append(publicPosts, post)
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

	username := strings.ToLower(user.GetUsername())

	// Get the list of users the authenticated user is following
	following := make([]string, 0)
	followersMutex.RLock()
	for targetUser, data := range followersData {
		for _, follower := range data.Followers {
			if follower == username {
				following = append(following, targetUser)
				break
			}
		}
	}
	followersMutex.RUnlock()

	// Get posts from followed users (excluding profile-only posts from other users)
	postsMutex.RLock()
	followingPosts := make([]Post, 0)
	for _, post := range posts {
		isFollowed := false
		for _, followedUser := range following {
			if post.User == followedUser {
				isFollowed = true
				break
			}
		}
		if isFollowed && !(post.ProfileOnly && post.User != username) {
			followingPosts = append(followingPosts, post)
		}
	}
	postsMutex.RUnlock()

	// Sort by timestamp (newest first)
	sort.Slice(followingPosts, func(i, j int) bool {
		return followingPosts[i].Timestamp > followingPosts[j].Timestamp
	})

	// Apply limit - get last 'limit' posts and reverse
	var result = make([]Post, 0)
	if len(followingPosts) > limit {
		result = followingPosts[len(followingPosts)-limit:]
	} else {
		result = followingPosts
	}

	c.JSON(200, result)
}
