package main

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"
)

func main() {
	// Ensure environment variables are loaded before any handlers/config usage
	envOnce.Do(loadEnvFile)
	// (Re)load config in case env was changed externally
	loadConfigFromEnv()

	// Initialize start time
	startTime = time.Now()

	// Load initial data
	loadBannedWords()
	loadUsers()
	loadFollowers()
	loadPosts()
	loadItems()
	loadKeys()
	loadSystems()
	loadEventsHistory()

	StartAutoUpdater(5 * time.Minute)

	go cleanRateLimitStorage()
	go checkSubscriptions()
	go watchUsersFile()
	go cleanExpiredStatuses()

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	r.Use(corsMiddleware())

	// Posts endpoints
	r.GET("/post", requiresAuth, createPost)
	r.GET("/reply", requiresAuth, replyToPost)
	r.GET("/follow", requiresAuth, followUser)
	r.GET("/unfollow", unfollowUser)
	r.GET("/followers", rateLimit("profile"), getFollowers)
	r.GET("/following", rateLimit("profile"), getFollowing)
	r.GET("/notifications", rateLimit("default"), requiresAuth, getNotifications)
	r.GET("/profile", rateLimit("profile"), getProfile)
	r.GET("/feed", rateLimit("default"), getFeed)
	r.GET("/following_feed", rateLimit("default"), requiresAuth, getFollowingFeed)
	r.GET("/delete", requiresAuth, deletePost)
	r.GET("/rate", requiresAuth, ratePost)
	r.GET("/repost", requiresAuth, repost)
	r.GET("/pin_post", requiresAuth, pinPost)
	r.GET("/unpin_post", requiresAuth, unpinPost)
	r.GET("/top_posts", getTopPosts)
	r.GET("/search_posts", rateLimit("search"), searchPosts)

	// Stats endpoints
	stats := r.Group("/stats")
	{
		stats.GET("/economy", rateLimit("default"), getEconomyStats)
		stats.GET("/users", rateLimit("default"), getUserStats)
		stats.GET("/rich", rateLimit("default"), getRichList)
		stats.GET("/systems", rateLimit("default"), getSystemStats)
		stats.GET("/followers", rateLimit("default"), getFollowersStats)
	}

	// Systems endpoints
	r.GET("/systems", getSystems)
	r.GET("/reload_systems", requiresAuth, reloadSystemsEndpoint)

	// Validator endpoints
	r.GET("/generate_validator", requiresAuth, generateValidator)
	r.GET("/validate", validateToken)

	// Items endpoints
	items := r.Group("/items")
	{
		items.GET("/create", requiresAuth, createItem)
		items.GET("/get/:name", getItem)
		items.GET("/list/:username", listItems)
		items.GET("/selling", getSellingItems)

		items.GET("/buy/:name", requiresAuth, buyItem)
		items.GET("/transfer/:name", requiresAuth, transferItem)
		items.GET("/sell/:name", requiresAuth, sellItem)
		items.GET("/stop_selling/:name", requiresAuth, stopSellingItem)
		items.GET("/set_price/:name", requiresAuth, setItemPrice)

		items.GET("/update/:name", requiresAuth, updateItem)
		items.GET("/delete/:name", requiresAuth, deleteItem)
		items.GET("/admin_add/:id", requiresAuth, adminAddUserToItem)
	}

	// Keys endpoints
	keys := r.Group("/keys")
	{
		keys.GET("/create", requiresAuth, createKey)
		keys.GET("/mine", requiresAuth, getMyKeys)
		keys.GET("/get/:id", getKey)
		keys.GET("/check/:username", checkKey)
		keys.GET("/name/:id", requiresAuth, setKeyName)
		keys.GET("/update/:id", requiresAuth, updateKey)
		keys.GET("/revoke/:id", requiresAuth, revokeKey)
		keys.GET("/delete/:id", requiresAuth, deleteKey)
		keys.GET("/admin_add/:id", requiresAuth, adminAddUserToKey)
		keys.GET("/admin_remove/:id", requiresAuth, adminRemoveUserFromKey)
		keys.GET("/buy/:id", requiresAuth, buyKey)
		keys.GET("/cancel/:id", requiresAuth, cancelKey)
		keys.GET("/debug_subscriptions", requiresAuth, debugSubscriptionsEndpoint)
	}

	// Admin endpoints
	admin := r.Group("/admin")
	{
		admin.GET("/tos_update", tosUpdate)
		admin.GET("/get_user_by", getUserBy)
		admin.POST("/update_user", updateUserAdmin)
		admin.POST("/delete_user", deleteUserAdmin)
	}

	// Users endpoints
	r.GET("/me", rateLimit("profile"), getUser)
	r.GET("/get_user", rateLimit("profile"), getUser)
	r.GET("/get_user_new", rateLimit("profile"), getUser)

	r.POST("/create_user", registerUser)

	me := r.Group("/me")
	{
		me.POST("/update", updateUser)
		me.POST("/refresh_token", requiresAuth, refreshToken)
		me.POST("/transfer", requiresAuth, transferCredits)
		me.POST("/gamble", requiresAuth, gambleCredits)
		me.DELETE("/me/delete", deleteUserKey)
	}
	r.POST("/accept_tos", requiresAuth, acceptTos)

	r.PATCH("/users", updateUser)

	r.DELETE("/users", deleteUserKey)
	r.DELETE("/users/:username", requiresAuth, deleteUser)

	// Friends endpoints
	friends := r.Group("/friends")
	{
		friends.GET("", requiresAuth, getFriends)
		friends.POST("/request/:username", requiresAuth, sendFriendRequest)
		friends.POST("/accept/:username", requiresAuth, acceptFriendRequest)
		friends.POST("/reject/:username", requiresAuth, rejectFriendRequest)
		friends.POST("/remove/:username", requiresAuth, removeFriend)
	}

	// Marriage endpoints
	marriage := r.Group("/marriage")
	{
		marriage.GET("/status", requiresAuth, getMarriageStatus)
		marriage.POST("/propose/:username", requiresAuth, proposeMarriage)
		marriage.POST("/accept", requiresAuth, acceptMarriage)
		marriage.POST("/reject", requiresAuth, rejectMarriage)
		marriage.POST("/cancel", requiresAuth, cancelMarriage)
		marriage.POST("/divorce", requiresAuth, divorceMarriage)
	}

	// Linking endpoints
	link := r.Group("/link")
	{
		link.GET("/code", getLinkCode)
		link.GET("/status", getLinkStatus)
		link.GET("/user", getLinkedUser)

		link.POST("/code", requiresAuth, linkCodeToAccount)
	}

	// Status endpoints
	status := r.Group("/status")
	{
		status.GET("/update", requiresAuth, statusUpdate)
		status.GET("/clear", requiresAuth, statusClear)
		status.GET("/get", statusGet)
	}

	// DevFund endpoints
	devfund := r.Group("/devfund")
	{
		devfund.POST("/escrow_transfer", requiresAuth, escrowTransfer)
		devfund.POST("/escrow_release", requiresAuth, escrowRelease)
	}

	// Other endpoints
	r.GET("/claim_daily", requiresAuth, claimDaily)
	r.GET("/supporters", getSupporters)
	r.GET("/badges", requiresAuth, getBadges)
	r.GET("/ai", rateLimit("ai"), requiresAuth, handleAI)
	r.GET("/status", getStatus)

	log.Println("Claw server starting on port 5602...")
	if err := r.Run("0.0.0.0:5602"); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
