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
	r.GET("/profile", getProfile)
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
	r.GET("/stats/economy", rateLimit("default"), getEconomyStats)
	r.GET("/stats/users", rateLimit("default"), getUserStats)
	r.GET("/stats/rich", rateLimit("default"), getRichList)
	r.GET("/stats/systems", rateLimit("default"), getSystemStats)
	r.GET("/stats/followers", rateLimit("default"), getFollowersStats)
	r.GET("/status", getStatus)

	// Systems endpoints
	r.GET("/systems", getSystems)
	r.GET("/reload_systems", requiresAuth, reloadSystemsEndpoint)

	// Validator endpoints
	r.GET("/generate_validator", generateValidator)
	r.GET("/validate", validateToken)

	// Items endpoints
	r.GET("/items/create", requiresAuth, createItem)
	r.GET("/items/get/:name", getItem)
	r.GET("/items/list/:username", listItems)
	r.GET("/items/selling", getSellingItems)

	r.GET("/items/buy/:name", requiresAuth, buyItem)
	r.GET("/items/transfer/:name", requiresAuth, transferItem)
	r.GET("/items/sell/:name", requiresAuth, sellItem)
	r.GET("/items/stop_selling/:name", requiresAuth, stopSellingItem)
	r.GET("/items/set_price/:name", requiresAuth, setItemPrice)

	r.GET("/items/update/:name", requiresAuth, updateItem)
	r.GET("/items/delete/:name", requiresAuth, deleteItem)
	r.GET("/items/admin_add/:id", requiresAuth, adminAddUserToItem)

	// Keys endpoints
	r.GET("/keys/create", requiresAuth, createKey)
	r.GET("/keys/mine", requiresAuth, getMyKeys)
	r.GET("/keys/get/:id", getKey)
	r.GET("/keys/check/:username", checkKey)
	r.GET("/keys/name/:id", requiresAuth, setKeyName)
	r.GET("/keys/update/:id", requiresAuth, updateKey)
	r.GET("/keys/revoke/:id", requiresAuth, revokeKey)
	r.GET("/keys/delete/:id", requiresAuth, deleteKey)
	r.GET("/keys/admin_add/:id", requiresAuth, adminAddUserToKey)
	r.GET("/keys/admin_remove/:id", requiresAuth, adminRemoveUserFromKey)
	r.GET("/keys/buy/:id", requiresAuth, buyKey)
	r.GET("/keys/cancel/:id", requiresAuth, cancelKey)
	r.GET("/keys/debug_subscriptions", requiresAuth, debugSubscriptionsEndpoint)

	// Admin endpoints
	r.GET("/admin/tos_update", tosUpdate)
	r.GET("/admin/get_user_by", getUserBy)

	r.POST("/admin/update_user", updateUserAdmin)
	r.POST("/admin/delete_user", deleteUserAdmin)

	// Users endpoints
	r.GET("/me", rateLimit("profile"), getUser)
	r.GET("/get_user", rateLimit("profile"), getUser)
	r.GET("/get_user_new", rateLimit("profile"), getUser)

	r.POST("/create_user", registerUser)
	r.POST("/me/update", updateUser)
	r.POST("/me/refresh_token", requiresAuth, refreshToken)
	r.POST("/me/transfer", requiresAuth, transferCredits)
	r.POST("/me/gamble", requiresAuth, gambleCredits)
	r.POST("/accept_tos", requiresAuth, acceptTos)

	r.PATCH("/users", updateUser)

	r.DELETE("/users", deleteUserKey)
	r.DELETE("/me/delete", deleteUserKey)
	r.DELETE("/users/:username", requiresAuth, deleteUser)

	// Friends endpoints
	r.GET("/friends", requiresAuth, getFriends)

	r.POST("/friends/request/:username", requiresAuth, sendFriendRequest)
	r.POST("/friends/accept/:username", requiresAuth, acceptFriendRequest)
	r.POST("/friends/reject/:username", requiresAuth, rejectFriendRequest)
	r.POST("/friends/remove/:username", requiresAuth, removeFriend)

	// Marriage endpoints
	r.GET("/marriage/status", requiresAuth, getMarriageStatus)

	r.POST("/marriage/propose/:username", requiresAuth, proposeMarriage)
	r.POST("/marriage/accept", requiresAuth, acceptMarriage)
	r.POST("/marriage/reject", requiresAuth, rejectMarriage)
	r.POST("/marriage/cancel", requiresAuth, cancelMarriage)
	r.POST("/marriage/divorce", requiresAuth, divorceMarriage)

	// Linking endpoints
	r.GET("/link/code", getLinkCode)
	r.GET("/link/status", getLinkStatus)
	r.GET("/link/user", getLinkedUser)

	r.POST("/link/code", requiresAuth, linkCodeToAccount)

	// Status endpoints
	r.GET("/status/update", requiresAuth, statusUpdate)
	r.GET("/status/clear", requiresAuth, statusClear)
	r.GET("/status/get", statusGet)

	// DevFund endpoints
	r.POST("/devfund/escrow_transfer", requiresAuth, escrowTransfer)
	r.POST("/devfund/escrow_release", requiresAuth, escrowRelease)

	// Other endpoints
	r.GET("/claim_daily", requiresAuth, claimDaily)
	r.GET("/supporters", getSupporters)
	r.GET("/badges", requiresAuth, getBadges)
	r.GET("/ai", rateLimit("ai"), requiresAuth, handleAI)

	log.Println("Claw server starting on port 5602...")
	if err := r.Run("0.0.0.0:5602"); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
