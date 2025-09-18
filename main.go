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
	r.Use(globalRateLimitMiddleware())

	r.GET("/post", createPost)
	r.GET("/reply", replyToPost)
	r.GET("/follow", followUser)
	r.GET("/unfollow", unfollowUser)
	r.GET("/followers", rateLimit("profile"), getFollowers)
	r.GET("/following", rateLimit("profile"), getFollowing)
	r.GET("/notifications", rateLimit("default"), getNotifications)
	r.GET("/profile", getProfile)
	r.GET("/feed", rateLimit("default"), getFeed)
	r.GET("/following_feed", rateLimit("default"), getFollowingFeed)
	r.GET("/delete", deletePost)
	r.GET("/rate", ratePost)
	r.GET("/repost", repost)
	r.GET("/pin_post", pinPost)
	r.GET("/unpin_post", unpinPost)
	r.GET("/top_posts", getTopPosts)

	r.GET("/stats/economy", rateLimit("default"), getEconomyStats)
	r.GET("/stats/users", rateLimit("default"), getUserStats)
	r.GET("/stats/rich", rateLimit("default"), getRichList)
	r.GET("/stats/aura", rateLimit("default"), getAuraStats)
	r.GET("/stats/systems", rateLimit("default"), getSystemStats)
	r.GET("/stats/followers", rateLimit("default"), getFollowersStats)

	r.GET("/search_posts", rateLimit("search"), searchPosts)

	r.GET("/get_user", rateLimit("profile"), getUser)
	r.GET("/get_user_new", rateLimit("profile"), getUser)

	r.GET("/systems", getSystems)
	r.GET("/reload_systems", reloadSystemsEndpoint)

	r.GET("/generate_validator", generateValidator)
	r.GET("/validate", validateToken)

	r.GET("/status", getStatus)

	// Items endpoints
	r.GET("/items/transfer/:name", transferItem)
	r.GET("/items/buy/:name", buyItem)
	r.GET("/items/stop_selling/:name", stopSellingItem)
	r.GET("/items/set_price/:name", setItemPrice)
	r.GET("/items/create", createItem)
	r.GET("/items/get/:name", getItem)
	r.GET("/items/delete/:name", deleteItem)
	r.GET("/items/list/:username", listItems)
	r.GET("/items/update/:name", updateItem)
	r.GET("/items/sell/:name", sellItem)
	r.GET("/items/selling", getSellingItems)
	r.GET("/items/admin_add/:id", adminAddUserToItem)

	// Keys endpoints
	r.GET("/keys/create", createKey)
	r.GET("/keys/mine", getMyKeys)
	r.GET("/keys/check/:username", checkKey)
	r.GET("/keys/revoke/:id", revokeKey)
	r.GET("/keys/delete/:id", deleteKey)
	r.GET("/keys/update/:id", updateKey)
	r.GET("/keys/name/:id", setKeyName)
	r.GET("/keys/get/:id", getKey)
	r.GET("/keys/admin_add/:id", adminAddUserToKey)
	r.GET("/keys/admin_remove/:id", adminRemoveUserFromKey)
	r.GET("/keys/buy/:id", buyKey)
	r.GET("/keys/cancel/:id", cancelKey)
	r.GET("/keys/debug_subscriptions", debugSubscriptionsEndpoint)

	// Admin endpoints
	r.GET("/admin/get_user_by", getUserBy)
	r.POST("/admin/update_user", updateUserAdmin)
	r.POST("/admin/delete_user", deleteUserAdmin)

	// Users endpoints
	r.POST("/create_user", registerUser)
	r.DELETE("/users/:username", deleteUser)
	r.PATCH("/users", updateUser)
	r.DELETE("/users", deleteUserKey)

	r.POST("/me/update", updateUser)
	r.DELETE("/me/delete", deleteUserKey)
	r.GET("/me", getUser)
	r.POST("/me/refresh_token", refreshToken)
	r.POST("/me/transfer", transferCredits)
	r.POST("/me/gamble", gambleCredits)

	r.POST("/friends/request/:username", sendFriendRequest)
	r.POST("/friends/accept/:username", acceptFriendRequest)
	r.POST("/friends/reject/:username", rejectFriendRequest)
	r.POST("/friends/remove/:username", removeFriend)
	r.GET("/friends", getFriends)

	// Marriage endpoints
	r.POST("/marriage/propose/:username", proposeMarriage)
	r.POST("/marriage/accept", acceptMarriage)
	r.POST("/marriage/reject", rejectMarriage)
	r.POST("/marriage/cancel", cancelMarriage)
	r.POST("/marriage/divorce", divorceMarriage)
	r.GET("/marriage/status", getMarriageStatus)

	r.GET("/link/code", getLinkCode)
	r.POST("/link/code", linkCodeToAccount)
	r.GET("/link/status", getLinkStatus)
	r.GET("/link/user", getLinkedUser)

	r.GET("/status/update", statusUpdate)
	r.GET("/status/clear", statusClear)
	r.GET("/status/get", statusGet)

	r.GET("/claim_daily", claimDaily)
	r.GET("/supporters", getSupporters)

	// Badges endpoints
	r.GET("/badges", getBadges)

	// DevFund endpoints
	r.POST("/devfund/escrow_transfer", escrowTransfer)
	r.POST("/devfund/escrow_release", escrowRelease)

	// Other endpoints
	r.POST("/accept_tos", acceptTos)

	log.Println("Claw server starting on port 5602...")
	if err := r.Run("0.0.0.0:5602"); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
