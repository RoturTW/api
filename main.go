package main

import (
	"fmt"
	"log"

	"github.com/gin-gonic/gin"
)

func main() {
	// Ensure environment variables are loaded before any handlers/config usage
	envOnce.Do(loadEnvFile)
	// (Re)load config in case env was changed externally
	loadConfigFromEnv()

	// Load initial data
	loadBannedWords()
	loadUsers()
	loadGroupData()
	loadFollowers()
	loadPosts()
	loadItems()
	loadKeys()
	loadSystems()
	loadEventsHistory()
	loadGifts()
	loadCosmeticsCatalog()
	buildSubTokenIndex()
	// doAfter(reconnectFriends, nil, time.Second*20)

	if err := loadJSONBadges(); err != nil {
		log.Printf("Warning: Failed to load badges.json: %v", err)
	}

	fmt.Println("Completed loading data")

	go cleanRateLimitStorage()
	go checkSubscriptions()
	go watchUsersFile()
	go watchBadgesFile()
	go cleanExpiredGifts()
	go cleanExpiredSubTokens()
	// go enactInactivityTax()
	go startStandingRecoveryChecker()

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	r.Use(corsMiddleware())

	// Posts endpoints
	r.GET("/post", rateLimit("default"), requiresAuth, requirePermission(PermCreatePost), requireStanding(StandingGood), createPost)
	r.GET("/limits", getLimits)
	r.GET("/reply", rateLimit("default"), requiresAuth, requirePermission(PermReplyPost), requireStanding(StandingGood), replyToPost)
	r.GET("/follow", rateLimit("follow"), requiresAuth, requirePermission(PermFollow), requireStanding(StandingWarning), followUser)
	r.GET("/unfollow", rateLimit("follow"), requiresAuth, requirePermission(PermUnfollow), unfollowUser)
	r.GET("/followers", rateLimit("profile"), getFollowers)
	r.GET("/following", rateLimit("profile"), getFollowing)
	r.GET("/notifications", rateLimit("default"), requiresAuth, requirePermission(PermViewNotifications), getNotifications)
	r.GET("/profile", rateLimit("profile"), getProfile)
	r.GET("/exists", rateLimit("profile"), getExists)
	r.GET("/feed", rateLimit("default"), getFeed)
	r.GET("/following_feed", rateLimit("default"), requiresAuth, requirePermission(PermViewPosts), getFollowingFeed)
	r.GET("/delete", requiresAuth, requirePermission(PermDeletePost), deletePost)
	r.GET("/rate", requiresAuth, requirePermission(PermLikePost), ratePost)
	r.GET("/repost", rateLimit("default"), requiresAuth, requirePermission(PermRepost), requireStanding(StandingGood), repost)
	r.GET("/pin_post", requiresAuth, requirePermission(PermManagePosts), pinPost)
	r.GET("/unpin_post", requiresAuth, requirePermission(PermManagePosts), unpinPost)
	r.GET("/top_posts", rateLimit("search"), getTopPosts)
	r.GET("/search_posts", rateLimit("search"), searchPosts)

	// Stats endpoints
	stats := r.Group("/stats")
	{
		stats.GET("/economy", rateLimit("default"), getEconomyStats)
		stats.GET("/users", rateLimit("default"), getUserStats)
		// stats.GET("/rich", rateLimit("default"), getRichList)
		stats.GET("/most_gained", rateLimit("default"), getMostGained)
		stats.GET("/systems", rateLimit("default"), getSystemStats)
		stats.GET("/followers", rateLimit("default"), getFollowersStats)
	}

	// Systems endpoints
	r.GET("/systems", getSystems)
	r.GET("/system/users", requiresAuth, requirePermission(PermViewGroups), getSystemUsers)
	r.GET("/reload_systems", requiresAuth, requirePermission(PermManageSettings), reloadSystemsEndpoint)
	r.POST("/update_system", requiresAuth, requirePermission(PermManageSettings), updateSystem)

	// Validator endpoints
	r.GET("/generate_validator", requiresAuth, requirePermission(PermGenerateValidator), generateValidator)
	r.GET("/validate", validateToken)

	// Items endpoints
	items := r.Group("/items")
	{
		items.GET("/create", requiresAuth, requirePermission(PermManageItems), requireStanding(StandingGood), createItem)
		items.GET("/get/:name", getItem)
		items.GET("/list/:username", listItems)
		items.GET("/selling", getSellingItems)

		items.GET("/buy/:name", requiresAuth, requirePermission(PermBuyItems), requireStanding(StandingWarning), buyItem)
		items.GET("/transfer/:name", requiresAuth, requirePermission(PermManageItems), requireStanding(StandingGood), transferItem)
		items.GET("/sell/:name", requiresAuth, requirePermission(PermSellItems), requireStanding(StandingGood), sellItem)
		items.GET("/stop_selling/:name", requiresAuth, requirePermission(PermSellItems), stopSellingItem)
		items.GET("/set_price/:name", requiresAuth, requirePermission(PermManageItems), setItemPrice)

		items.GET("/update/:name", requiresAuth, requirePermission(PermManageItems), updateItem)
		items.GET("/delete/:name", requiresAuth, requirePermission(PermManageItems), deleteItem)
		items.GET("/admin_add/:id", requiresAuth, requirePermission(PermManageItems), adminAddUserToItem)
	}

	// Keys endpoints
	keys := r.Group("/keys")
	{
		keys.GET("/create", requiresAuth, requirePermission(PermManageKeys), createKey)
		keys.GET("/mine", requiresAuth, requirePermission(PermViewKeys), getMyKeys)
		keys.GET("/get/:id", getKey)
		keys.GET("/check/:username", checkKey)
		keys.GET("/name/:id", requiresAuth, requirePermission(PermManageKeys), setKeyName)
		keys.GET("/update/:id", requiresAuth, requirePermission(PermManageKeys), updateKey)
		keys.GET("/revoke/:id", requiresAuth, requirePermission(PermManageKeys), revokeKey)
		keys.GET("/delete/:id", requiresAuth, requirePermission(PermManageKeys), deleteKey)
		keys.GET("/admin_add/:id", requiresAuth, requirePermission(PermManageKeys), adminAddUserToKey)
		keys.GET("/admin_remove/:id", requiresAuth, requirePermission(PermManageKeys), adminRemoveUserFromKey)
		keys.GET("/buy/:id", requiresAuth, requirePermission(PermManageKeys), buyKey)
		keys.GET("/cancel/:id", requiresAuth, requirePermission(PermManageKeys), cancelKey)
		keys.GET("/debug_subscriptions", requiresAuth, requirePermission(PermViewKeys), debugSubscriptionsEndpoint)
	}

	// Admin endpoints
	admin := r.Group("/admin")
	{
		admin.GET("/tos_update", tosUpdate)
		admin.GET("/get_user_by", getUserBy)
		admin.POST("/update_user", updateUserAdmin)
		admin.POST("/delete_user", deleteUserAdmin)
		admin.POST("/ban_user", banUserAdmin)
		admin.POST("/transfer_credits", transferCreditsAdmin)
		admin.POST("/kofi", handleKofiTransaction)
		admin.POST("/set_sub", setSubscription)
		admin.POST("/set_standing", setStandingAdmin)
		admin.POST("/get_standing_history", getStandingHistoryAdmin)
		admin.POST("/recover_standing", recoverStandingAdmin)
	}

	// Standing endpoints
	r.GET("/get_standing", GetUserStanding)

	// Users endpoints
	r.GET("/me", rateLimit("profile"), getUser)
	r.GET("/get_user", rateLimit("profile"), getUser)
	r.GET("/get_user_new", rateLimit("profile"), getUser)

	r.GET("/check_auth", requiresAuth, requirePermission(PermViewProfile), checkAuth)

	r.POST("/create_user", rateLimit("register"), registerUser)
	auth := r.Group("/auth")
	{
		auth.POST("/rotur", rateLimit("register"), registerUser)
		auth.POST("/google", rateLimit("profile"), handleUserGoogle)
	}

	me := r.Group("/me")
	{
		me.POST("/update", updateUser)
		me.POST("/refresh_token", requiresAuth, requireMainToken(), refreshToken)
		me.POST("/transfer", requiresAuth, requirePermission(PermTransferCredits), transferCredits)
		me.POST("/gamble", requiresAuth, requirePermission(PermManageCredits), gambleCredits)
		me.DELETE("/delete", requiresAuth, requirePermission(PermDeleteAccount), deleteUserKey)

		me.GET("/blocked", requiresAuth, requirePermission(PermViewBlocked), getBlocking)
		me.POST("/block/:username", requiresAuth, requirePermission(PermManageBlocked), blockUser)
		me.POST("/unblock/:username", requiresAuth, requirePermission(PermManageBlocked), unblockUser)

		// notes endpoints, get not needed, stored in user["sys.notes"]
		me.POST("/note/:username", requiresAuth, requirePermission(PermManageProfile), requireTier("Plus"), noteUser)
		me.DELETE("/note/:username", requiresAuth, requirePermission(PermManageProfile), requireTier("Plus"), deleteNote)
		me.GET("/able", requiresAuth, getTokenAbilities)
	}

	groups := r.Group("/groups")
	{
		groups.GET("/mine", requiresAuth, requirePermission(PermViewGroups), getMyGroups)
		groups.GET("/search", requiresAuth, requirePermission(PermViewGroups), searchGroups)
		groups.POST("/create", requiresAuth, requirePermission(PermManageGroups), requireStanding(StandingGood), createGroup)
		groups.POST("/:grouptag/rep", requiresAuth, requirePermission(PermManageSettings), representGroup)
		groups.POST("/:grouptag/disrep", requiresAuth, requirePermission(PermManageSettings), disrepresentGroup)
		groups.POST("/:grouptag/report", requiresAuth, requirePermission(PermViewGroups), reportGroup)
		groups.POST("/:grouptag/join", requiresAuth, requirePermission(PermJoinGroup), joinGroup)
		groups.POST("/:grouptag/leave", requiresAuth, requirePermission(PermLeaveGroup), leaveGroup)
		groups.GET("/:grouptag", requiresAuth, requirePermission(PermViewGroups), getGroup)
		groups.PATCH("/:grouptag", requiresAuth, requirePermission(PermManageGroups), updateGroup)
		groups.DELETE("/:grouptag", requiresAuth, requirePermission(PermManageGroups), deleteGroup)

		groups.GET("/:grouptag/announcements", getAnnouncements)
		groups.POST("/:grouptag/announcements", requiresAuth, requirePermission(PermManageGroups), createAnnouncement)
		groups.DELETE("/:grouptag/announcements/:announcementid", requiresAuth, requirePermission(PermManageGroups), deleteAnnouncement)
		groups.POST("/:grouptag/announcements/mute", requiresAuth, requirePermission(PermManageGroups), toggleAnnouncementMute)

		groups.GET("/:grouptag/events", requiresAuth, requirePermission(PermViewGroups), getEvents)
		groups.POST("/:grouptag/events", requiresAuth, requirePermission(PermManageGroups), createEvent)

		groups.GET("/:grouptag/tips", requiresAuth, requirePermission(PermViewGroups), getTips)
		groups.POST("/:grouptag/tips", requiresAuth, requirePermission(PermManageCredits), sendTip)

		groups.GET("/:grouptag/roles", requiresAuth, requirePermission(PermViewGroups), getRoles)
		groups.POST("/:grouptag/roles", requiresAuth, requirePermission(PermManageGroups), createRole)
		groups.PATCH("/:grouptag/roles/:roleid", requiresAuth, requirePermission(PermManageGroups), updateRole)
		groups.DELETE("/:grouptag/roles/:roleid", requiresAuth, requirePermission(PermManageGroups), deleteRole)

		groups.GET("/:grouptag/members/:userid/roles", requiresAuth, requirePermission(PermViewGroups), getUserRoles)
		groups.GET("/:grouptag/members/:userid/permissions", requiresAuth, requirePermission(PermViewGroups), getUserPermissions)
		groups.GET("/:grouptag/members/:userid/benefits", requiresAuth, requirePermission(PermViewGroups), getUserBenefits)
		groups.POST("/:grouptag/members/:userid/roles/:roleid", requiresAuth, requirePermission(PermManageGroups), assignRole)
		groups.DELETE("/:grouptag/members/:userid/roles/:roleid", requiresAuth, requirePermission(PermManageGroups), removeRole)
	}
	r.POST("/accept_tos", requiresAuth, requirePermission(PermManageSettings), acceptTos)

	r.PATCH("/users", updateUser)
	r.DELETE("/users", deleteUserKey)
	r.DELETE("/users/:username", requiresAuth, requirePermission(PermDeleteAccount), deleteUser)

	files := r.Group("/files")
	{
		files.GET("", requiresAuth, requirePermission(PermViewFiles), getFileByUUID)
		files.POST("", requiresAuth, requirePermission(PermManageFiles), updateFiles)
		files.DELETE("", requiresAuth, requirePermission(PermDeleteFiles), deleteAllUserFiles)

		files.GET("/usage", requiresAuth, requirePermission(PermViewFiles), getUserFileSize)
		files.GET("/index", requiresAuth, requirePermission(PermViewFiles), getFilesIndex)
		files.GET("/entries", requiresAuth, requirePermission(PermViewFiles), getFilesAll)

		files.POST("/by-uuid", requiresAuth, requirePermission(PermViewFiles), getFilesByUUIDs)
		files.GET("/by-uuid", requiresAuth, requirePermission(PermViewFiles), getFileByUUID)
		files.GET("/by-path/*path", requiresAuth, requirePermission(PermViewFiles), getFileByPath)

		files.POST("/stats", requiresAuth, requirePermission(PermViewFiles), getFileSizes)
		files.GET("/path-index", requiresAuth, requirePermission(PermViewFiles), getPathIndex)
	}

	r.GET("/read-files", requiresAuth, requirePermission(PermViewFiles), getFilesAll)
	r.GET("/read-file", requiresAuth, requirePermission(PermViewFiles), getFileByUUID)
	r.GET("/read-index", requiresAuth, requirePermission(PermViewFiles), getFilesIndex)

	// Friends endpoints
	friends := r.Group("/friends")
	{
		friends.GET("", requiresAuth, requirePermission(PermViewFriends), getFriends)
		friends.POST("/request/:username", requiresAuth, requirePermission(PermSendFriendReq), requireStanding(StandingGood), sendFriendRequest)
		friends.POST("/accept/:username", requiresAuth, requirePermission(PermAcceptFriend), acceptFriendRequest)
		friends.POST("/reject/:username", requiresAuth, requirePermission(PermAcceptFriend), rejectFriendRequest)
		friends.POST("/remove/:username", requiresAuth, requirePermission(PermRemoveFriend), removeFriend)
	}

	// Linking endpoints
	link := r.Group("/link")
	{
		link.GET("/code", getLinkCode)
		link.GET("/status", getLinkStatus)
		link.GET("/user", getLinkedUser)

		link.POST("/code", requiresAuth, requirePermission(PermManageSettings), linkCodeToAccount)
	}

	// Services endpoints
	// services := r.Group("/services")
	{
		// for future integrations
	}

	r.GET("/ws", statusWSHandler)

	status := r.Group("/status")
	{
		status.GET("/ws", statusWSHandler)
		status.GET("/get", statusGetHTTP)
	}

	// DevFund endpoints
	devfund := r.Group("/devfund")
	{
		devfund.POST("/escrow_transfer", requiresAuth, requirePermission(PermTransferCredits), escrowTransfer)
		devfund.POST("/escrow_release", requiresAuth, requirePermission(PermManageCredits), escrowRelease)
	}

	tokens := r.Group("/tokens")
	{
		tokens.GET("/permissions", listPermissions)
		tokens.GET("", requiresAuth, requirePermission(PermManageTokens), listSubTokens)
		tokens.GET("/active", requiresAuth, requirePermission(PermManageTokens), listActiveSubTokens)
		tokens.POST("/create", requiresAuth, requireMainToken(), createSubToken)
		tokens.GET("/:id", requiresAuth, requirePermission(PermManageTokens), getSubToken)
		tokens.GET("/:id/activity", requiresAuth, requirePermission(PermManageTokens), getSubTokenActivity)
		tokens.PATCH("/:id", requiresAuth, requireMainToken(), updateSubToken)
		tokens.POST("/:id/rename", requiresAuth, requireMainToken(), renameSubToken)
		tokens.POST("/:id/revoke", requiresAuth, requireMainToken(), revokeSubToken)
		tokens.DELETE("/:id", requiresAuth, requireMainToken(), deleteSubToken)
	}

	// Gifts endpoints
	gifts := r.Group("/gifts")
	{
		gifts.POST("/create", rateLimit("default"), requiresAuth, requirePermission(PermCreateGift), requireStanding(StandingGood), createGift)
		gifts.GET("/:code", getGift)
		gifts.POST("/claim/:code", rateLimit("default"), requiresAuth, requirePermission(PermClaimGift), requireStanding(StandingWarning), claimGift)
		gifts.POST("/cancel/:id", requiresAuth, requirePermission(PermCancelGift), cancelGift)
		gifts.GET("/mine", requiresAuth, requirePermission(PermViewGifts), getMyGifts)
	}

	notify := r.Group("/notify")
	{
		notify.GET("/vapid", rateLimit("notify"), getVAPIDKeys)
		notify.POST("/register", rateLimit("notify"), requiresAuth, requirePermission(PermManageSettings), registerForNotifications)
		notify.GET("/check", requiresAuth, requirePermission(PermViewNotifications), checkNotifyRegistration)
		notify.GET("/endpoints", requiresAuth, requirePermission(PermViewNotifications), getNotifyEndpoints)
		notify.DELETE("/device/:device_id", requiresAuth, requirePermission(PermManageSettings), deleteNotifyDevice)
		notify.GET("/allowed", requiresAuth, requirePermission(PermViewNotifications), getNotifyAllowedSenders)
		notify.POST("/allowed/:username", requiresAuth, requirePermission(PermManageSettings), addNotifyAllowedSender)
		notify.DELETE("/allowed/:username", requiresAuth, requirePermission(PermManageSettings), removeNotifyAllowedSender)
		notify.GET("/log", rateLimit("notify"), requiresAuth, requirePermission(PermViewNotifications), getNotifyLogHandler)
		notify.POST("/:username", rateLimit("notify"), requiresAuth, requirePermission(PermSendNotifications), notifyUser)
		notify.POST("/", rateLimit("notify"), requiresAuth, requirePermission(PermSendNotifications), notifyManyUsers)
		notify.GET("/:source/users", rateLimit("notify"), requiresAuth, requirePermission(PermViewNotifications), getNotifiableUsers)
	}

	// Cosmetics endpoints
	cosmetics := r.Group("/cosmetics")
	{
		cosmetics.GET("/shop", rateLimit("default"), getShop)
		cosmetics.GET("/items/:id", rateLimit("default"), getCosmeticDetail)
		cosmetics.GET("/mine", requiresAuth, requirePermission(PermViewProfile), getMyCosmetics)
		cosmetics.POST("/purchase/:id", rateLimit("default"), requiresAuth, requirePermission(PermBuyItems), requireStanding(StandingWarning), purchaseCosmetic)
		cosmetics.POST("/equip/:id", requiresAuth, requirePermission(PermManageProfile), equipCosmetic)
		cosmetics.POST("/unequip", requiresAuth, requirePermission(PermManageProfile), unequipCosmetic)
		cosmetics.GET("/overlays/*filepath", rateLimit("default"), serveOverlayAsset)
		cosmetics.GET("/admin/list", rateLimit("default"), adminListCosmetics)
		cosmetics.POST("/admin/create", rateLimit("default"), adminCreateCosmetic)
		cosmetics.PATCH("/admin/update/:id", rateLimit("default"), adminUpdateCosmetic)
		cosmetics.DELETE("/admin/delete/:id", rateLimit("default"), adminDeleteCosmetic)
	}

	// Other endpoints
	r.GET("/claim_daily", rateLimit("default"), requiresAuth, requirePermission(PermClaimDaily), requireStanding(StandingGood), claimDaily)
	r.GET("/claim_time", rateLimit("default"), requiresAuth, requirePermission(PermClaimDaily), timeUntilNextClaim)
	r.GET("/supporters", rateLimit("default"), getSupporters)
	r.GET("/badges", rateLimit("default"), requiresAuth, requirePermission(PermViewProfile), getBadges)
	r.GET("/ai", rateLimit("ai"), requiresAuth, requirePermission(PermViewPosts), handleAI)
	r.GET("/status", rateLimit("default"), getStatus)

	go func() {
		avatars := gin.Default()

		avatars.Use(corsMiddleware())

		avatars.GET("/:username", avatarHandler)
		avatars.HEAD("/:username", avatarHandler)
		avatars.GET("/.overlay/:username", overlayHandler)
		avatars.HEAD("/.overlay/:username", overlayHandler)
		avatars.GET("/.banners/:username", bannerHandler)
		avatars.HEAD("/.banners/:username", bannerHandler)
		avatars.POST("/rotur-upload-pfp", uploadPfpHandler)
		avatars.POST("/rotur-upload-banner", uploadBannerHandler)

		avatars.POST("/reload-overlays", requiresAuth, requirePermission(PermManageSettings), reloadOverlays)

		log.Println("Avatar server starting on port 5604...")
		if err := avatars.Run("0.0.0.0:5604"); err != nil {
			log.Fatalf("Failed to start avatar server: %v", err)
		}
	}()

	log.Println("Claw server starting on port 5602...")
	if err := r.Run("0.0.0.0:5602"); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
