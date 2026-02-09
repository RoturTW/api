package main

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

func getAccountsBy(key string, value string, max int) ([]User, error) {
	usersMutex.RLock()
	defer usersMutex.RUnlock()

	var matches []User
	if key == "username" {
		valueLower := Username(value).ToLower()
		for _, user := range users {
			if user.GetUsername().ToLower() == valueLower {
				matches = append(matches, user)
				if max != -1 && len(matches) >= max {
					return matches, nil
				}
			}
		}
	} else {
		for _, user := range users {
			if fmt.Sprintf("%v", user.Get(key)) == value {
				matches = append(matches, user)
				if max != -1 && len(matches) >= max {
					return matches, nil
				}
			}
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("account not found for %s=%q", key, value)
	}
	return matches, nil
}

func findAccountByLogin(username string, password string) (User, error) {
	usersMutex.RLock()
	defer usersMutex.RUnlock()

	name := Username(username).ToLower()
	for _, user := range users {
		if user.GetUsername().ToLower() == name && user.GetPassword() == password {
			return user, nil
		}
	}

	return nil, fmt.Errorf("account not found for login")
}

func getIdxOfAccountBy(key string, value string) int {
	usersMutex.RLock()
	defer usersMutex.RUnlock()

	if key == "username" {
		valueLower := Username(value).ToLower()
		for i, user := range users {
			if user.GetUsername().ToLower() == valueLower {
				return i
			}
		}
		return -1
	}

	for i, user := range users {
		if user.Get(key) == value {
			return i
		}
	}
	return -1
}

// helper function to update user keys
func setAccountKey(username Username, key string, value any) error {

	i := getIdxOfAccountBy("username", username.String())

	if i != -1 {
		usersMutex.Lock()
		defer usersMutex.Unlock()

		users[i].Set(key, value)
		return nil
	}
	return fmt.Errorf("user not found: %s", username)
}

func getUserBy(c *gin.Context) {
	if !authenticateAdmin(c) {
		return
	}

	key := c.Query("key")
	if key == "" {
		c.JSON(400, gin.H{"error": "Key is required"})
		return
	}

	value := c.Query("value")
	if value == "" {
		var body struct {
			Value string `json:"value"`
		}
		_ = c.ShouldBindJSON(&body)
		value = body.Value
	}
	if value == "" {
		c.JSON(400, gin.H{"error": "Value is required"})
		return
	}

	foundUsers, err := getAccountsBy(key, value, 1)
	if err != nil {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}

	copy := copyUser(foundUsers[0])
	delete(copy, "password")

	c.JSON(200, userToNet(copy))
}

func getUser(c *gin.Context) {
	authKey := c.Query("auth")

	var foundUser User

	if authKey != "" {
		foundUsers, _ := getAccountsBy("key", authKey, 1)
		if foundUsers != nil {
			foundUser = foundUsers[0]
			if foundUser.GetKey() != authKey {
				c.JSON(403, gin.H{"error": "Invalid authentication credentials"})
				return
			}
		}
	}

	username := c.Query("username")
	password := c.Query("password")

	if username != "" && password != "" && foundUser == nil {
		var err error = nil
		foundUser, err = findAccountByLogin(username, password)
		if err != nil && foundUser != nil {
			addLogin(c, foundUser, "Failed login")
			c.JSON(403, gin.H{"error": "Invalid authentication credentials"})
			return
		}
	}

	if foundUser != nil {
		usersMutex.Lock()
		defer usersMutex.Unlock()

		if foundUser.IsBanned() {
			c.JSON(403, gin.H{
				"error":    "User is banned",
				"username": foundUser.GetUsername(),
			})
		}
		if foundUser.Get("sys.tos_accepted") != true {
			// early return - TOS not accepted
			c.JSON(403, gin.H{
				"error":            "Terms-Of-Service are not accepted or outdated",
				"username":         foundUser.GetUsername(),
				"token":            foundUser.GetKey(),
				"sys.tos_accepted": false,
			})
			return
		}

		ip := c.ClientIP()
		blocked_ips := foundUser.GetBlockedIps()
		if slices.Contains(blocked_ips, ip) {
			addLogin(c, foundUser, "Blocked ip attempted login")
			c.JSON(403, gin.H{"error": "Unable to login to this account"})
			return
		}

		now := time.Now().UnixMilli()
		foundUser.Set("sys.last_login", now)
		foundUser.Set("sys.total_logins", foundUser.GetInt("sys.total_logins")+1)
		foundUser.Set("sys.badges", calculateUserBadges(foundUser))

		header := c.GetHeader("CF-IPCountry")
		if header == "T1" {
			// block tor
			addLogin(c, foundUser, "Tor login attempted")
			c.JSON(403, gin.H{"error": "Tor is not allowed"})
			return
		}

		addLogin(c, foundUser, "Successful Login")
		foundUser.SetSubscription(foundUser.GetSubscription())

		go saveUsers()
		c.JSON(200, userToNet(foundUser))
		return
	}

	c.JSON(403, gin.H{"error": "Invalid authentication credentials"})
}

func userToNet(user User) User {
	userCopy := copyUser(user)
	userCopy["sys.friends"] = user.GetFriendUsers()
	userCopy["sys.requests"] = user.GetRequestedUsers()
	if user.GetMarriage().Status != "single" {
		userCopy["sys.marriage"] = user.GetMarriage().ToNet()
	}
	transactions := user.GetTransactions()
	netTransactions := make([]TransactionNet, len(transactions))
	for i, transaction := range transactions {
		netTransactions[i] = transaction.ToNet()
	}
	userCopy["sys.transactions"] = netTransactions

	delete(userCopy, "password")
	return userCopy
}

func checkAuth(c *gin.Context) {
	auth := c.Query("auth")
	if auth == "" {
		c.JSON(400, gin.H{"error": "auth is required"})
		return
	}

	usersMutex.RLock()
	defer usersMutex.RUnlock()

	for _, user := range users {
		if user.GetKey() == auth {
			c.JSON(200, gin.H{"auth": true, "username": user.GetUsername()})
			return
		}
	}
	c.JSON(200, gin.H{"auth": false, "username": ""})
}

func addLogin(c *gin.Context, user User, message string) {
	if user == nil {
		return
	}
	logins := user.GetLogins()
	ip := c.ClientIP()
	hostname := c.GetHeader("Origin")
	userAgent := c.Request.UserAgent()
	device_type := "Unknown"
	if c.GetHeader("Sec-CH-UA-Mobile") == "?1" {
		device_type = "Mobile"
	} else {
		device_type = "Desktop"
	}

	logins = append(logins, Login{
		Origin:      hostname,
		UserAgent:   userAgent,
		IP_hmac:     hmacIp(ip),
		Country:     c.GetHeader("CF-IPCountry"),
		Timestamp:   time.Now().UnixMilli(),
		Device_type: device_type,
		Message:     message,
	})
	maxLogins := user.GetSubscriptionBenefits().Max_Login_History
	if n := len(logins); n > maxLogins {
		logins = logins[n-maxLogins:]
	}
	user.Set("sys.logins", logins)
}

func generateAccountToken() string {
	currentTimestamp := time.Now().UnixMilli()

	randomNumber := rand.Int63n(999999999999999) + 1

	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	randomString1000 := make([]byte, 1000)
	for i := range randomString1000 {
		randomString1000[i] = charset[rand.Intn(len(charset))]
	}

	hasher := md5.New()
	hasher.Write(randomString1000)
	md5Hash := hex.EncodeToString(hasher.Sum(nil))

	randomString128 := make([]byte, 128)
	for i := range randomString128 {
		randomString128[i] = charset[rand.Intn(len(charset))]
	}

	finalToken := fmt.Sprintf("%d_%d_%s_%s", currentTimestamp, randomNumber, md5Hash, string(randomString128))

	return finalToken
}

func refreshToken(c *gin.Context) {
	user := c.MustGet("user").(*User)

	newToken := generateAccountToken()

	usersMutex.Lock()
	defer usersMutex.Unlock()

	user.Set("key", newToken)
	go saveUsers()

	c.JSON(200, gin.H{"token": newToken})
}

func registerUser(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Email    string `json:"email"`
		System   string `json:"system"`
		Captcha  string `json:"captcha"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request body"})
		return
	}

	ip := c.ClientIP()
	from_url := c.GetHeader("referer")
	if from_url == "" {
		from_url = c.GetHeader("origin")
		if from_url == "" {
			from_url = "unknown"
		}
	}

	if isBannedIp(ip) {
		randomResponses := []string{
			"so sad, stay mad",
			"L bozo",
			"L",
			":3",
			"damn so close that time",
			"awwww",
			"ur gay :3 (and gay people are awesome)",
			"Take a shower",
			"even a toddler could do this better",
		}
		c.JSON(403, gin.H{"error": randomResponses[rand.Intn(len(randomResponses))]})
		return
	}

	if !verifyHCaptcha(req.Captcha) {
		c.JSON(400, gin.H{"error": "hCaptcha verification failed"})
		return
	}

	username := Username(req.Username)
	password := req.Password
	email := req.Email
	system := req.System

	if username == "" || password == "" {
		c.JSON(400, gin.H{"error": "Username and password are required"})
		return
	}

	usernameLower := username.ToLower()
	if ok, msg := ValidateUsername(username); !ok {
		c.JSON(400, gin.H{"error": msg})
		return
	}

	if IsIpInBannedList(ip) {
		c.JSON(400, gin.H{"error": "IP address is banned"})
		return
	}

	usersMutex.Lock()
	for _, user := range users {
		if user.GetUsername().ToLower() == usernameLower {
			c.JSON(400, gin.H{"error": "Username already in use"})
			usersMutex.Unlock()
			return
		}
		if strings.EqualFold(user.GetEmail(), email) {
			c.JSON(400, gin.H{"error": "Email already in use"})
			usersMutex.Unlock()
			return
		}
	}
	usersMutex.Unlock()

	if ok, msg := ValidatePasswordHash(password); !ok {
		c.JSON(400, gin.H{"error": msg})
		return
	}

	// Validate username against systems with detailed feedback
	isValid, errorMessage, matchedSystem := validateSystem(system)
	if !isValid {
		c.JSON(400, gin.H{"error": errorMessage})
		return
	}

	newUser, err := createAccount(AccountCreateInput{
		Username:      username,
		Password:      password,
		Email:         email,
		System:        matchedSystem,
		Provider:      "rotur",
		RequestIP:     ip,
		RequestOrigin: from_url,
	})
	if err != nil {
		if strings.Contains(err.Error(), "username already") {
			c.JSON(400, gin.H{"error": "Username already in use"})
			return
		}
		if strings.Contains(err.Error(), "email already") {
			c.JSON(400, gin.H{"error": "Email already in use"})
			return
		}
		c.JSON(500, gin.H{"error": "Failed to create account"})
		return
	}
	userCopy := copyUser(newUser)
	delete(userCopy, "password")
	c.JSON(201, userCopy)
}

func findUserSize(username Username) int {
	totalSize := 0
	usersMutex.RLock()
	defer usersMutex.RUnlock()
	for _, u := range users {
		if u.GetUsername().ToLower() == username {
			for k, v := range u {
				if strings.HasPrefix(k, "sys.") {
					continue
				}
				switch v := v.(type) {
				case string:
					totalSize += len(v)
				case []string:
					for _, item := range v {
						totalSize += len(item)
					}
				case []any:
					for _, item := range v {
						if strItem, ok := item.(string); ok {
							totalSize += len(strItem)
						}
					}
				case map[string]any:
					for mk, mv := range v {
						if strMv, ok := mv.(string); ok {
							totalSize += len(strMv)
						}
						if strMk := strings.ToLower(mk); strMk != "username" && strMk != "sys.password" {
							totalSize += len(strMk)
						}
					}
				default:
					// Handle other types if necessary
					totalSize += 100 // Arbitrary size for unknown types
				}
			}
		}
	}
	return totalSize
}

func uploadUserImage(imageType, imageData, token string) (*http.Response, error) {
	// Avatar/banner uploads can be slow; allow extra time to avoid spurious 500s
	client := &http.Client{Timeout: 20 * time.Second}
	var url string
	switch imageType {
	case "banner":
		url = "https://avatars.rotur.dev/rotur-upload-banner?ADMIN_TOKEN=" + os.Getenv("ADMIN_TOKEN")
	case "pfp":
		url = "https://avatars.rotur.dev/rotur-upload-pfp?ADMIN_TOKEN=" + os.Getenv("ADMIN_TOKEN")
	default:
		return nil, fmt.Errorf("invalid image type")
	}
	payload := fmt.Sprintf(`{"image":"%s","token":"%s"}`, imageData, token)
	req, err := http.NewRequest("POST", url, strings.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return resp, nil
}

func canUpdateUsernameUnsafe(username string) (bool, string) {
	if username == "" {
		return false, "Invalid username"
	}
	name := Username(username)
	ok, msg := ValidateUsername(name)
	if !ok {
		return false, msg
	}
	usernameLower := name.ToLower()
	for _, user := range users {
		if user.GetUsername().ToLower() == usernameLower {
			return false, "Username already in use"
		}
	}

	return true, "Can update username"
}

func updateUsername(userId UserId, oldUsername, newUsername Username) {
	usernameLower := oldUsername.ToLower()
	newUsernameLower := newUsername.ToLower()

	if usernameLower == newUsernameLower {
		return
	}

	fs.RenameUserFileSystem(usernameLower, newUsernameLower)
	renameUserAvatar(oldUsername, newUsername)
}

func updateUser(c *gin.Context) {
	var req struct {
		Auth  string `json:"auth"`
		Key   string `json:"key"`
		Value any    `json:"value"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request body"})
		return
	}
	authKey := req.Auth
	key := req.Key
	if key == "" {
		c.JSON(400, gin.H{"error": "Key is required"})
		return
	}
	value := req.Value
	if value == nil {
		c.JSON(400, gin.H{"error": "Value is required"})
		return
	}
	stringValue := fmt.Sprintf("%v", value)

	if authKey == "" {
		c.JSON(403, gin.H{"error": "auth key is required"})
		return
	}

	user := authenticateWithKey(authKey)
	if user == nil {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		return
	}

	username := user.GetUsername()

	if key == "banner" {
		// Allow both data URIs and normal URLs
		var imageData string
		if strings.HasPrefix(stringValue, "data:") {
			imageData = stringValue
		} else {
			c.JSON(400, gin.H{"error": "Banner must be a valid data URI"})
			return
		}
		userIndex := getIdxOfAccountBy("username", username.String())
		if userIndex == -1 {
			c.JSON(403, gin.H{"error": "User not found"})
			return
		}
		benefits := user.GetSubscriptionBenefits()
		freeAndGifUploads := benefits.Has_Free_Banner_Uploads
		if strings.Contains(imageData, "data:image/gif;base64,") {
			if !benefits.Has_Animated_Banner {
				c.JSON(400, gin.H{"error": "GIFs are only available to Pro users"})
				return
			}
		}
		var currencyFloat float64 = user.GetCredits()
		if currencyFloat < 10 && !freeAndGifUploads {
			c.JSON(403, gin.H{"error": "Not enough credits to set banner (10 required)"})
			return
		}
		resp, err := uploadUserImage("banner", imageData, user.GetKey())
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to upload banner"})
			return
		}
		if resp.StatusCode != 200 {
			statusCode := resp.StatusCode
			c.JSON(statusCode, gin.H{"error": "Banner upload failed"})
			return
		}
		if !freeAndGifUploads {
			user.SetBalance(currencyFloat - 10)
		}
		go doAfter(func(data any) {
			user.Set("sys.banner", "https://avatars.rotur.dev/.banners/"+user.GetUsername())
			go saveUsers()
		}, nil, time.Second*2)
		c.JSON(200, gin.H{"message": "Banner uploaded successfully"})
		return
	}

	if key == "pfp" {
		// Allow both data URIs and normal URLs
		var imageData string
		if strings.HasPrefix(stringValue, "data:") {
			imageData = stringValue
		} else {
			c.JSON(400, gin.H{"error": "Profile picture must be a valid data URI"})
			return
		}
		if strings.Contains(imageData, "data:image/gif;base64,") {
			benefits := user.GetSubscriptionBenefits()
			if !benefits.Has_Animated_Pfp {
				c.JSON(400, gin.H{"error": "GIFs are only available to Pro users"})
				return
			}
		}

		resp, err := uploadUserImage("pfp", imageData, user.GetKey())
		if err != nil {
			c.JSON(500, gin.H{"error": err})
			return
		}
		if resp.StatusCode != 200 {
			statusCode := resp.StatusCode
			c.JSON(statusCode, gin.H{"error": "Failed to upload profile picture"})
			return
		}
		go doAfter(func(data any) {
			broadcastUserUpdate(user.GetUsername(), "pfp", "https://avatars.rotur.dev/"+user.GetUsername())
			go saveUsers()
		}, nil, time.Second*2)
		c.JSON(200, gin.H{"message": "Profile picture uploaded successfully"})
		return
	}

	// Check for admin privileges - try Authorization header first, then query param
	var admin bool
	if c.GetHeader("Authorization") != "" {
		admin = isAdmin(c)
	} else {
		// Fall back to query param method
		envOnce.Do(loadEnvFile)
		ADMIN_TOKEN := os.Getenv("ADMIN_TOKEN")
		adminToken := c.Query("token")
		admin = adminToken != "" && ADMIN_TOKEN != "" && adminToken == ADMIN_TOKEN
	}

	totalSize := findUserSize(username)
	if totalSize+len(fmt.Sprintf("%v", value)) > 25000 {
		c.JSON(400, gin.H{"error": "Total account size exceeds 25000 bytes"})
		return
	}

	if key == "sys.id" {
		c.JSON(400, gin.H{"error": "Cannot update sys.id"})
		return
	}

	if key == "email" {
		usersMutex.RLock()
		for _, user := range users {
			if strings.EqualFold(user.GetEmail(), stringValue) {
				c.JSON(400, gin.H{"error": "Email already in use"})
				usersMutex.RUnlock()
				return
			}
		}
		usersMutex.RUnlock()
	}

	if key == "bio" {
		length := len(stringValue)
		bio_length := user.GetSubscriptionBenefits().Bio_Length
		if length > bio_length {
			c.JSON(400, gin.H{"error": "Bio length exceeds " + strconv.Itoa(bio_length) + " characters"})
			return
		}
	}

	if key == "system" {
		// switch your account's system
		systems := getAllSystems()
		for _, system := range systems {
			if system.Name == stringValue {
				user.Set("system", system.Name)
				c.JSON(200, gin.H{"message": "Successfully switched system to " + system.Name})
				return
			}
		}
		c.JSON(404, gin.H{"error": "System not found"})
		return
	}

	if strings.HasPrefix(key, "sys.") && !admin {
		c.JSON(400, gin.H{"error": "System keys cannot be modified directly"})
		return
	}
	if len(stringValue) > 1000 {
		c.JSON(400, gin.H{"error": "Value length exceeds 1000 characters"})
		return
	}
	if len(key) > 20 {
		c.JSON(400, gin.H{"error": "Key length exceeds 20 characters"})
		return
	}
	if key == "username" {
		usersMutex.RLock()
		ok, msg := canUpdateUsernameUnsafe(getStringOrEmpty(value))
		usersMutex.RUnlock()
		if !ok {
			c.JSON(400, gin.H{"error": msg})
			return
		}
		updateUsername(user.GetId(), user.GetUsername(), Username(getStringOrEmpty(value)))
	}
	if slices.Contains(lockedKeys, key) {
		c.JSON(400, gin.H{"error": fmt.Sprintf("Key '%s' cannot be updated", key)})
		return
	}

	if err := setAccountKey(username, key, value); err != nil {
		c.JSON(404, gin.H{"error": err.Error()})
		return
	}

	go saveUsers()

	c.JSON(200, gin.H{
		"message":  "User key updated successfully",
		"username": username,
		"key":      key,
		"value":    value,
	})
}

func updateUserAdmin(c *gin.Context) {
	if !authenticateAdmin(c) {
		return
	}

	// Parse request body - expects user_data object from Python client
	var userData map[string]any
	if err := c.ShouldBindJSON(&userData); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request body"})
		return
	}

	// Check if this is a typed operation (update/remove)
	operationType, hasType := userData["type"].(string)
	if hasType {
		// Handle typed operations from Python client
		username, ok := userData["username"].(string)
		if !ok || username == "" {
			c.JSON(400, gin.H{"error": "username is required"})
			return
		}

		switch operationType {
		case "update":
			key, hasKey := userData["key"].(string)
			value, hasValue := userData["value"]
			if !hasKey || !hasValue {
				c.JSON(400, gin.H{"error": "key and value are required for update operation"})
				return
			}

			// Find the user by username
			userIndex := getIdxOfAccountBy("username", username)

			if userIndex == -1 {
				c.JSON(404, gin.H{"error": "User not found"})
				return
			}

			// Validate key and value constraints
			if !strings.HasPrefix(key, "sys.") {
				if len(key) > 50 {
					c.JSON(400, gin.H{"error": fmt.Sprintf("Key '%s' length exceeds 50 characters", key)})
					return
				}
			}

			// Ensure sys.currency stays a float64 when updated via admin endpoint
			usersMutex.RLock()
			user := users[userIndex]
			usersMutex.RUnlock()
			switch key {
			case "username":
				username := Username(getStringOrEmpty(value))
				if ok, msg := canUpdateUsernameUnsafe(username.String()); !ok {
					c.JSON(400, gin.H{"error": msg})
					return
				}
				oldUsername := user.GetUsername()
				updateUsername(user.GetId(), oldUsername, username)
				user.Set("username", username.String())
			case "sys.currency":
				user.SetBalance(value)
			default:
				user.Set(key, value)
			}

			go saveUsers()

			c.JSON(200, gin.H{
				"message":  "User updated successfully",
				"username": username,
				"key":      key,
				"value":    value,
			})
			return

		case "remove":
			key, hasKey := userData["key"].(string)
			if !hasKey || key == "" {
				c.JSON(400, gin.H{"error": "key is required for remove operation"})
				return
			}

			// Find the user by username
			userIndex := getIdxOfAccountBy("username", username)

			if userIndex == -1 {
				c.JSON(404, gin.H{"error": "User not found"})
				return
			}

			if strings.HasPrefix(key, "sys.") {
				c.JSON(400, gin.H{"error": "System keys cannot be deleted"})
				return
			}

			if slices.Contains(lockedKeys, key) {
				c.JSON(400, gin.H{"error": fmt.Sprintf("Key '%s' cannot be deleted", key)})
				return
			}

			usersMutex.Lock()
			users[userIndex].DelKey(key)
			usersMutex.Unlock()

			go saveUsers()

			c.JSON(200, gin.H{
				"message":  "User key deleted successfully",
				"username": username,
				"key":      key,
			})
			return

		default:
			c.JSON(400, gin.H{"error": fmt.Sprintf("Invalid operation type '%s'. Must be 'update' or 'remove'", operationType)})
			return
		}
	}

	// If no type specified, return error
	c.JSON(400, gin.H{"error": "type parameter is required. Must be 'update' or 'remove'"})
}

func gambleCredits(c *gin.Context) {
	c.JSON(400, gin.H{"error": "This endpoint is no longer available"})
}

func deleteUserKey(c *gin.Context) {
	var req struct {
		Auth string `json:"auth"`
		Key  string `json:"key"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request body"})
		return
	}
	authKey := req.Auth
	key := req.Key
	if authKey == "" {
		c.JSON(403, gin.H{"error": "auth key is required"})
		return
	}

	user := authenticateWithKey(authKey)
	if user == nil {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		return
	}

	username := user.GetUsername()
	if username == "" {
		c.JSON(403, gin.H{"error": "User not found"})
		return
	}

	if key == "" {
		c.JSON(400, gin.H{"error": "Key is required"})
		return
	}

	if strings.HasPrefix(key, "sys.") {
		c.JSON(400, gin.H{"error": "System keys cannot be deleted"})
		return
	}

	if slices.Contains(lockedKeys, key) {
		c.JSON(400, gin.H{"error": fmt.Sprintf("Key '%s' cannot be deleted", key)})
		return
	}

	if key == "username" {
		c.JSON(400, gin.H{"error": "Cannot delete username key"})
		return
	}

	user.DelKey(key)

	go saveUsers()

	c.JSON(204, gin.H{"message": "User key deleted successfully", "username": username, "key": key})
}

// PerformCreditTransfer performs a credit transfer between two users.
// Handles tax, transaction logging, and safety rules.
// Returns an error if the transfer cannot be completed.
func PerformCreditTransfer(fromUsername, toUsername Username, amount float64, note string) error {
	const totalTax = 0.0
	const taxRecipientShare = 0.25

	// normalize + validate amount
	nAmount, ok := normalizeEscrowAmount(amount)
	if !ok {
		return fmt.Errorf("minimum amount is 0.01")
	}

	fromUsers, err := getAccountsBy("username", fromUsername.String(), 1)
	if err != nil {
		return fmt.Errorf("sender user not found")
	}
	fromUser := fromUsers[0]

	toUsers, err := getAccountsBy("username", toUsername.String(), 1)
	if err != nil {
		return fmt.Errorf("recipient user not found")
	}
	toUser := toUsers[0]

	if fromUser.GetUsername().ToLower() == toUser.GetUsername().ToLower() {
		return fmt.Errorf("cannot send credits to yourself")
	}

	fromCurrency := roundVal(fromUser.GetCredits())
	if fromUsername != "rotur" {
		if fromCurrency < (nAmount + totalTax) {
			return fmt.Errorf("insufficient funds (required: %.2f, available: %.2f)", nAmount+totalTax, fromCurrency)
		}
	}

	toCurrency := roundVal(toUser.GetCredits())

	now := time.Now().UnixMilli()

	// Helper: clean note
	mkNote := func(base string) string {
		n := strings.TrimSpace(base)
		if n == "" {
			n = "transfer"
		}
		if len(n) > 50 {
			n = n[:50]
		}
		return n
	}
	note = mkNote(note)

	// Send credits when rotur is the sender
	if fromUsername == "rotur" {
		taxRecipient := Username("mist")
		fromSystem := toUser.GetSystem()
		systemsMutex.RLock()
		if sys, ok := systems[fromSystem]; ok {
			taxRecipient = sys.Owner.Name
		}
		systemsMutex.RUnlock()

		// Apply tax to taxRecipient if exists
		if idx := getIdxOfAccountBy("username", taxRecipient.String()); taxRecipient != toUser.GetUsername() && idx != -1 {
			taxUser, _ := getUserByIdx(idx)
			newBalance := roundVal(taxUser.GetCredits() + taxRecipientShare)
			taxUser.SetBalance(newBalance)
			taxUser.addTransaction(Transaction{
				Note:      "Daily credit",
				User:      toUser.GetId(),
				Timestamp: now,
				Amount:    taxRecipientShare,
				Type:      "tax",
				NewTotal:  newBalance,
			})
		}
	}
	// Update balances
	if fromUser.GetUsername() != "rotur" {
		fromUser.SetBalance(roundVal(fromCurrency - (nAmount + totalTax)))
	}
	if toUser.GetUsername() != "rotur" {
		toUser.SetBalance(roundVal(toCurrency + nAmount))
	}

	// Log transactions
	fromUser.addTransaction(Transaction{
		Note:     note,
		User:     toUser.GetId(),
		Amount:   nAmount + totalTax,
		Type:     "out",
		NewTotal: fromCurrency - nAmount - totalTax,
	})
	toUser.addTransaction(Transaction{
		Note:     note,
		User:     fromUser.GetId(),
		Amount:   nAmount,
		Type:     "in",
		NewTotal: toCurrency + nAmount,
	})

	go saveUsers()

	return nil
}

func transferCredits(c *gin.Context) {
	user := c.MustGet("user").(*User)

	var req struct {
		To     string `json:"to"`
		Amount any    `json:"amount"`
		Note   string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request payload"})
		return
	}
	amt := fmt.Sprintf("%v", req.Amount)

	if amt == "" {
		c.JSON(400, gin.H{"error": "Amount must be provided"})
		return
	}
	var nAmount float64
	var err error
	if after, ok := strings.CutPrefix(amt, "£"); ok {
		// convert to GBP
		nAmount, err = strconv.ParseFloat(after, 64)
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid amount"})
			return
		}
		creditsPerPound := creditsToPence(1) * 100
		nAmount = nAmount / creditsPerPound
	} else {
		nAmount, err = strconv.ParseFloat(amt, 64)
		if err != nil {
			c.JSON(400, gin.H{"error": "Invalid amount"})
			return
		}
	}
	nAmount = math.Round(nAmount*100) / 100 // round to 2 decimal places
	if nAmount < 0.01 {
		c.JSON(400, gin.H{"error": "Minimum amount is 0.01"})
		return
	}

	toUsername := Username(req.To).ToLower()
	if toUsername == "" {
		c.JSON(400, gin.H{"error": "Recipient username and amount must be provided"})
		return
	}
	if toUsername == user.GetUsername().ToLower() {
		c.JSON(400, gin.H{"error": "Cannot send credits to yourself"})
		return
	}

	err = PerformCreditTransfer(user.GetUsername(), toUsername, nAmount, req.Note)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "Transfer successful", "from": user.GetUsername(), "to": toUsername, "amount": nAmount, "debited": nAmount})
}

func deleteUser(c *gin.Context) {
	user := c.MustGet("user").(*User)

	username := Username(c.Param("username"))
	if username == "" {
		c.JSON(400, gin.H{"error": "Username is required"})
		return
	}

	usernameLower := username.ToLower()
	requester := user.GetUsername().ToLower()
	if requester != "mist" && requester != usernameLower {
		c.JSON(403, gin.H{"error": "Insufficient permissions to delete this user"})
		return
	}

	if err := performUserDeletion(username, false, false); err != nil {
		c.JSON(404, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "User deleted successfully"})
}

func deleteUserAdmin(c *gin.Context) {
	if !authenticateAdmin(c) {
		return
	}

	var req struct {
		Username string `json:"username"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request body"})
		return
	}

	username := Username(req.Username)
	if username == "" {
		c.JSON(400, gin.H{"error": "Username is required"})
		return
	}

	if err := performUserDeletion(username, true, false); err != nil {
		c.JSON(404, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "User deleted successfully"})
}

func banUserAdmin(c *gin.Context) {
	if !authenticateAdmin(c) {
		return
	}

	var req struct {
		Username Username `json:"username"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request body"})
		return
	}

	username := req.Username
	if username == "" {
		c.JSON(400, gin.H{"error": "Username is required"})
		return
	}

	if err := performUserDeletion(username, true, true); err != nil {
		c.JSON(404, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "User banned successfully"})
}

func transferCreditsAdmin(c *gin.Context) {
	if !authenticateAdmin(c) {
		return
	}

	toUsername := Username(c.Query("to"))
	amountStr := c.Query("amount")
	fromUsername := Username(c.Query("from"))
	note := c.Query("note")

	amountNum, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	err = PerformCreditTransfer(fromUsername, toUsername, amountNum, note)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"message": "Transfer successful", "from": fromUsername, "to": toUsername, "amount": amountNum, "debited": amountNum})
}

func removeUserDirectory(path string) error {
	return os.RemoveAll(path)
}

func reconnectFriends(_ any) {
	usersMutex.Lock()
	defer usersMutex.Unlock()

	friendMap := make(map[UserId][]UserId, len(users))
	for i := range users {
		u := users[i]
		friends := u.GetFriends()

		valid := make([]UserId, 0, len(friends))
		for _, f := range friends {
			if friendUser := f.User(); friendUser != nil {
				valid = append(valid, f)
			}
		}
		friendMap[u.GetId()] = valid
	}

	for uId, friends := range friendMap {
		for _, f := range friends {
			if !accountExists(f) {
				continue
			}

			friendList := friendMap[f]
			if !slices.Contains(friendList, uId) {
				friendMap[f] = append(friendList, uId)
			}
		}
	}

	changed := false

	for uId, finalList := range friendMap {
		u := getUserById(uId)
		old := u.GetFriends()

		if !slices.Equal(old, finalList) {
			u.SetFriends(finalList)
			changed = true
		}
	}

	if changed {
		go saveUsers()
		log.Printf("Reconnected %d friends", len(friendMap))
	}
}

func performUserDeletion(username Username, isAdmin bool, ban bool) error {
	usernameLower := username.ToLower()

	idx := getIdxOfAccountBy("username", usernameLower.String())
	if idx == -1 {
		return fmt.Errorf("user not found")
	}

	logPrefix := "Deleting user"
	if isAdmin {
		logPrefix = "Admin deleting user"
	}
	log.Printf("%s %s", logPrefix, usernameLower)

	if ban {
		usersMutex.Lock()
		// set as banned
		users[idx] = User{
			"username":   username,
			"email":      users[idx].GetEmail(), // so that the same email cant be used by a banned user
			"private":    true,
			"sys.banned": true,
		}
		usersMutex.Unlock()
	} else {
		deleteAccountAtIndexFast(idx)
	}

	go broadcastUserUpdate(usernameLower, "sys._deleted", true)

	uId := usernameLower.Id()
	usersMutex.Lock()
	defer usersMutex.Unlock()

	for i := range users {
		target := &users[i]

		friends := target.GetFriends()
		for i, f := range friends {
			if f == uId {
				friends = append(friends[:i], friends[i+1:]...)
				target.SetFriends(friends)
				break
			}
		}

		requests := target.GetRequests()
		for i, r := range requests {
			if r == uId {
				requests = append(requests[:i], requests[i+1:]...)
				target.SetRequests(requests)
				break
			}
		}

		blocked := target.GetBlocked()
		for i, b := range blocked {
			if b == uId {
				blocked = append(blocked[:i], blocked[i+1:]...)
				target.SetBlocked(blocked)
				break
			}
		}
	}

	go saveUsers()

	go func(target UserId, username Username) {
		// Update posts
		postsMutex.Lock()
		for i := range posts {
			if posts[i].User == target {
				posts[i].User = "Deleted User"
			}
		}
		postsMutex.Unlock()
		go savePosts()

		// remove avatar and banner
		if err := deleteUserAvatar(username); err != nil {
			log.Printf("Error deleting user avatar: %v", err)
		}

		// Remove user storage
		userDir := string("rotur/user_storage/" + target)
		if err := removeUserDirectory(userDir); err != nil {
			log.Printf("Error removing user directory %s: %v", userDir, err)
		}

		// remove file system
		if err := fs.DeleteUserFileSystem(username); err != nil {
			log.Printf("Error deleting user file system: %v", err)
		}
	}(uId, usernameLower)
	return nil
}

func deleteUserAvatar(username Username) error {
	usernameLower := username.ToLower()

	pfpPath := string("rotur/avatars/" + usernameLower.String() + ".jpg")
	if _, err := os.Stat(pfpPath); err == nil {
		if err := os.Remove(pfpPath); err != nil {
			return fmt.Errorf("error removing user avatar %s: %v", pfpPath, err)
		}
	}

	bannerPath := string("rotur/banners/" + usernameLower.String() + ".jpg")
	if _, err := os.Stat(bannerPath); err == nil {
		if err := os.Remove(bannerPath); err != nil {
			return fmt.Errorf("error removing user banner %s: %v", bannerPath, err)
		}
	}

	return nil
}

func renameUserAvatar(oldUsername, newUsername Username) error {
	usernameLower := oldUsername.ToLower()
	newUsernameLower := newUsername.ToLower()

	fileTypes := []string{".jpg", ".gif"}

	for _, fileType := range fileTypes {
		filePath := "rotur/avatars/" + usernameLower.String() + fileType
		if _, err := os.Stat(filePath); err == nil {
			newFilePath := "rotur/avatars/" + newUsernameLower.String() + fileType
			if err := os.Rename(filePath, newFilePath); err != nil {
				return fmt.Errorf("error renaming user avatar %s: %v", filePath, err)
			}
		}

		bannerPath := "rotur/banners/" + usernameLower.String() + fileType
		if _, err := os.Stat(bannerPath); err == nil {
			newBannerPath := string("rotur/banners/" + newUsernameLower.String() + fileType)
			if err := os.Rename(bannerPath, newBannerPath); err != nil {
				return fmt.Errorf("error renaming user banner %s: %v", bannerPath, err)
			}
		}
	}
	return nil
}

var dailyClaimMutex sync.Mutex

func canClaimDaily(user *User) float64 {
	username := user.GetUsername().ToLower()

	claimsData := loadDailyClaims()

	nextClaimTime, ok := claimsData[username]
	if !ok || nextClaimTime == 0 {
		return 0
	}

	currentTime := float64(time.Now().Unix())

	elapsed := currentTime - nextClaimTime
	if elapsed < 86400 {
		return 86400 - elapsed
	}

	return 0
}

func timeUntilNextClaim(c *gin.Context) {
	user := c.MustGet("user").(*User)

	username := user.GetUsername().ToLower()

	claimsData := loadDailyClaims()

	nextClaimTime, ok := claimsData[username]
	if !ok {
		c.JSON(400, gin.H{"error": "No daily claim found"})
		return
	}

	currentTime := float64(time.Now().Unix())

	elapsed := currentTime - nextClaimTime
	if elapsed < 86400 {
		waitTime := 86400 - elapsed
		c.JSON(200, gin.H{"wait_time": waitTime})
		return
	}

	c.JSON(200, gin.H{"wait_time": 0})
}

func claimDaily(c *gin.Context) {
	user := c.MustGet("user").(*User)

	username := user.GetUsername().ToLower()

	waitTime := canClaimDaily(user)
	if waitTime > 0 {
		c.JSON(429, gin.H{
			"error":      "Daily claim already made",
			"wait_time":  waitTime,
			"wait_hours": strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%.1f", waitTime/3600), "0"), "."),
		})
		return
	}

	claimsData := loadDailyClaims()
	currentTime := float64(time.Now().Unix())
	claimsData[username] = currentTime
	saveDailyClaims(claimsData)

	benefits := user.GetSubscriptionBenefits()

	PerformCreditTransfer("rotur", username, float64(benefits.Daily_Credit_Multipler), "Daily claim")

	saveUsers()

	c.JSON(200, gin.H{"message": "Daily claim successful"})
}

// loadDailyClaims loads daily claims data from rotur_daily.json
func loadDailyClaims() map[Username]float64 {
	dailyClaimMutex.Lock()
	defer dailyClaimMutex.Unlock()
	data, err := os.ReadFile(DAILY_CLAIMS_FILE_PATH)
	if err != nil {
		// If file doesn't exist, return empty map
		return make(map[Username]float64)
	}

	var claimsData map[Username]float64
	if err := json.Unmarshal(data, &claimsData); err != nil {
		// If unmarshal fails, return empty map
		return make(map[Username]float64)
	}

	return claimsData
}

// saveDailyClaims saves daily claims data to rotur_daily.json
func saveDailyClaims(claimsData map[Username]float64) {
	dailyClaimMutex.Lock()
	defer dailyClaimMutex.Unlock()
	data, err := json.MarshalIndent(claimsData, "", "  ")
	if err != nil {
		return
	}

	atomicWrite(DAILY_CLAIMS_FILE_PATH, data, 0644)
}

func acceptTos(c *gin.Context) {
	if c.GetHeader("Origin") != "https://rotur.dev" {
		c.JSON(403, gin.H{"error": "This endpoint is only available on rotur.dev"})
		return
	}

	user := c.MustGet("user").(*User)

	// Accept the TOS by setting a flag in the user data
	user.Set("sys.tos_accepted", true)
	user.Set("sys.tos_time", time.Now().Unix())

	go saveUsers()

	c.JSON(200, gin.H{"message": "Terms of Service accepted"})
}

func tosUpdate(c *gin.Context) {
	if !authenticateAdmin(c) {
		return
	}

	// Loop through all users and set sys.tos_accepted to false
	usersMutex.Lock()
	for i := range users {
		users[i]["sys.tos_accepted"] = false
	}
	usersMutex.Unlock()

	go saveUsers()

	c.JSON(200, gin.H{"message": "All users marked as not having accepted the updated Terms of Service"})
}

// Badge API handlers

func getBadges(c *gin.Context) {
	user := c.MustGet("user").(*User)

	usersMutex.RLock()
	defer usersMutex.RUnlock()

	// Find user in users slice to get updated data
	for _, u := range users {
		if u.GetUsername() == user.GetUsername() {
			badgeNames := calculateUserBadges(u)

			c.JSON(200, gin.H{
				"badge_names": badgeNames,
			})
			return
		}
	}

	c.JSON(404, gin.H{"error": "User not found"})
}
