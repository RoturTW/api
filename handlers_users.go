package main

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func getAccountsBy(key string, value string, max int) ([]User, error) {
	usersMutex.RLock()
	defer usersMutex.RUnlock()

	var matches []User
	if key == "username" {
		valueLower := strings.ToLower(value)
		for _, user := range users {
			if strings.ToLower(user.GetUsername()) == valueLower {
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

func findAccountByLogin(username string, password string) User {
	usersMutex.RLock()
	defer usersMutex.RUnlock()

	username = strings.ToLower(username)
	for _, user := range users {
		if strings.ToLower(user.GetUsername()) == username && user.GetPassword() == password {
			return user
		}
	}

	return nil
}

func getIdxOfAccountBy(key string, value string) int {
	usersMutex.RLock()
	defer usersMutex.RUnlock()

	if key == "username" {
		for i, user := range users {
			if strings.EqualFold(user.GetUsername(), value) {
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
func setAccountKey(username, key string, value any) error {

	i := getIdxOfAccountBy("username", username)

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

	users, err := getAccountsBy(key, value, 1)
	if err != nil {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}

	copy := copyUser(users[0])
	delete(copy, "password")

	c.JSON(200, copy)
}

func getUser(c *gin.Context) {
	authKey := c.Query("auth")
	usersMutex.RLock()
	defer usersMutex.RUnlock()

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
		foundUser = findAccountByLogin(username, password)
	}

	if foundUser != nil {
		if foundUser.Get("sys.tos_accepted") != true {
			// early return - TOS not accepted
			c.JSON(403, gin.H{"error": "Terms-Of-Service are not accepted or outdated", "username": foundUser.GetUsername(), "token": foundUser.GetKey(), "sys.tos_accepted": false})
			return
		}
		now := time.Now().UnixMilli()
		foundUser.Set("sys.last_login", now)
		foundUser.Set("sys.total_logins", foundUser.GetInt("sys.total_logins")+1)
		foundUser.Set("sys.badges", calculateUserBadges(foundUser))

		logins := foundUser.GetLogins()
		ip := c.ClientIP()
		hostname := c.GetHeader("Origin")
		userAgent := c.Request.UserAgent()
		logins = append(logins, Login{
			Origin:    hostname,
			UserAgent: userAgent,
			IP_hmac:   hmacIp(ip),
			Country:   c.GetHeader("CF-IPCountry"),
			Timestamp: now,
		})
		if n := len(logins); n > 10 {
			logins = logins[n-10:]
		}
		foundUser.Set("sys.logins", logins)
		go saveUsers()
		userCopy := copyUser(foundUser)
		delete(userCopy, "password")
		c.JSON(200, userCopy)
		return
	}

	c.JSON(403, gin.H{"error": "Missing authentication credentials"})
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

	if isBannedIp(ip) {
		c.JSON(403, gin.H{"error": "so sad, stay mad"})
		return
	}

	if !verifyHCaptcha(req.Captcha) {
		c.JSON(400, gin.H{"error": "hCaptcha verification failed"})
		return
	}

	username := req.Username
	password := req.Password
	email := req.Email
	system := req.System

	if username == "" || password == "" {
		c.JSON(400, gin.H{"error": "Username and password are required"})
		return
	}

	usernameLower := strings.ToLower(username)

	re := regexp.MustCompile("[^a-z0-9_ ]")
	if re.FindStringIndex(usernameLower) != nil {
		c.JSON(400, gin.H{"error": "Username contains invalid characters"})
		return
	}
	if strings.Contains(usernameLower, " ") {
		c.JSON(400, gin.H{"error": "Username cannot contain spaces"})
		return
	}

	file, err := os.Open("./banned_words.json")
	if err != nil {
		fmt.Println("Error opening banned_words.json:", err)
		return
	}
	defer file.Close()

	var bannedWords []string
	if err := json.NewDecoder(file).Decode(&bannedWords); err == nil {
		for _, banned := range bannedWords {
			// check leetspeek
			u := strings.ReplaceAll(username, "1", "l")
			u = strings.ReplaceAll(u, "3", "e")
			u = strings.ReplaceAll(u, "5", "s")
			u = strings.ReplaceAll(u, "7", "t")
			u = strings.ReplaceAll(u, "9", "i")
			u = strings.ReplaceAll(u, "0", "o")
			u = strings.ReplaceAll(u, "8", "b")
			u = strings.ReplaceAll(u, "@", "a")

			if strings.Contains(strings.ToLower(u), strings.ToLower(banned)) {
				c.JSON(400, gin.H{"error": "Username contains a banned word"})
				return
			}
		}
	}

	ips := strings.SplitSeq(os.Getenv("BANNED_IPS"), ",")
	for ipAddr := range ips {
		if strings.EqualFold(ipAddr, ip) {
			c.JSON(400, gin.H{"error": "IP address is banned"})
			return
		}
	}

	usersMutex.Lock()
	defer usersMutex.Unlock()

	for _, user := range users {
		if strings.EqualFold(user.GetUsername(), usernameLower) {
			c.JSON(400, gin.H{"error": "Username already in use"})
			return
		}
	}

	if len(password) != 32 {
		c.JSON(400, gin.H{"error": "Invalid password hash"})
		return
	}

	if password == "d41d8cd98f00b204e9800998ecf8427e" {
		c.JSON(400, gin.H{"error": "Password cannot be empty"})
		return
	}

	// Validate username against systems with detailed feedback
	isValid, errorMessage, matchedSystem := validateSystem(system)
	if !isValid {
		c.JSON(400, gin.H{"error": errorMessage})
		return
	}

	webhook := os.Getenv("ACCOUNT_CREATION_WEBHOOK")

	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(c.ClientIP())))

	if webhook != "" {
		data := map[string]any{
			"embeds": []map[string]any{
				{
					"title": "New Account Registered",
					"description": fmt.Sprintf("**Username:** %s\n**Email:** %s\n**System:** %s\n**IP:** %s\n**Host:** %s",
						username, email, matchedSystem.Name, hash, c.Request.Host),
					"color":     0x57cdac,
					"timestamp": time.Now().Format(time.RFC3339),
				},
			},
		}

		go func() {
			if err := sendWebhook(webhook, data); err != nil {
				log.Println("Failed to send account creation webhook:", err)
			}
		}()
	}

	newUser := User{
		"username":         username,
		"pfp":              "https://avatars.rotur.dev/" + usernameLower,
		"password":         password,
		"email":            email,
		"key":              generateAccountToken(),
		"system":           matchedSystem.Name,
		"max_size":         5000000,
		"sys.last_login":   time.Now().UnixMilli(),
		"sys.total_logins": 0,
		"sys.friends":      []string{},
		"sys.requests":     []string{},
		"sys.links":        []map[string]any{},
		"sys.currency":     float64(0),
		"sys.transactions": []any{},
		"sys.items":        []any{},
		"sys.badges":       []string{},
		"sys.purchases":    []any{},
		"private":          false,
		"id":               strconv.FormatInt(time.Now().UnixNano(), 10),
		"theme": map[string]any{
			"primary":    "#222",
			"secondary":  "#555",
			"tertiary":   "#777",
			"text":       "#fff",
			"background": "#050505",
			"accent":     "#57cdac",
		},
		"onboot": []string{
			"Origin/(A) System/System Apps/originWM.osl",
			"Origin/(A) System/System Apps/Desktop.osl",
			"Origin/(A) System/Docks/Dock.osl",
			"Origin/(A) System/System Apps/Quick_Settings.osl",
		},
		"created":          time.Now().UnixMilli(),
		"wallpaper":        matchedSystem.Wallpaper,
		"sys.tos_accepted": false,
	}

	users = append(users, newUser)
	go saveUsers()
	userCopy := copyUser(newUser)
	delete(userCopy, "password")
	c.JSON(201, userCopy)
}

func findUserSize(username string) int {
	totalSize := 0
	usersMutex.RLock()
	for _, u := range users {
		if strings.EqualFold(u.GetUsername(), username) {
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
	usersMutex.RUnlock()
	return totalSize
}

func uploadUserImage(imageType, imageData, token string) (int, error) {
	// Avatar/banner uploads can be slow; allow extra time to avoid spurious 500s
	client := &http.Client{Timeout: 20 * time.Second}
	var url string
	switch imageType {
	case "banner":
		url = "https://avatars.rotur.dev/rotur-upload-banner"
	case "pfp":
		url = "https://avatars.rotur.dev/rotur-upload-pfp"
	default:
		return 0, fmt.Errorf("invalid image type")
	}
	payload := fmt.Sprintf(`{"image":"%s","token":"%s"}`, imageData, token)
	req, err := http.NewRequest("POST", url, strings.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
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
		userIndex := getIdxOfAccountBy("username", username)
		if userIndex == -1 {
			c.JSON(403, gin.H{"error": "User not found"})
			return
		}

		var currencyFloat float64 = users[userIndex].GetCredits()
		if currencyFloat < 10 {
			c.JSON(403, gin.H{"error": "Not enough credits to set banner (10 required)"})
			return
		}
		statusCode, err := uploadUserImage("banner", imageData, user.GetKey())
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to upload banner"})
			return
		}
		if statusCode != 200 {
			c.JSON(statusCode, gin.H{"error": "Banner upload failed"})
			return
		}
		users[userIndex].SetBalance(currencyFloat - 10)
		users[userIndex].Set("sys.banner", "https://avatars.rotur.dev/.banners/"+user.GetUsername())

		go saveUsers()
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
		statusCode, err := uploadUserImage("pfp", imageData, user.GetKey())
		if err != nil {
			c.JSON(500, gin.H{"error": err})
			return
		}
		if statusCode != 200 {
			c.JSON(statusCode, gin.H{"error": err})
			return
		}
		go broadcastUserUpdate(user.GetUsername(), "pfp", "https://avatars.rotur.dev/"+user.GetUsername())
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

	sub := getSubscription(user)
	if key == "bio" && len(stringValue) > 200 && sub != "Free" && sub != "Supporter" {
		c.JSON(400, gin.H{"error": "Bio length exceeds 200 characters, only for supporters"})
		return
	}

	if len(stringValue) > 1000 {
		c.JSON(400, gin.H{"error": "Value length exceeds 1000 characters"})
		return
	}
	if strings.HasPrefix(key, "sys.") && !admin {
		c.JSON(400, gin.H{"error": "System keys cannot be modified directly"})
		return
	}
	if len(key) > 20 {
		c.JSON(400, gin.H{"error": "Key length exceeds 20 characters"})
		return
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

	c.JSON(200, gin.H{"message": "User key updated successfully", "username": username, "key": key, "value": value})
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
			if len(key) > 50 {
				c.JSON(400, gin.H{"error": fmt.Sprintf("Key '%s' length exceeds 50 characters", key)})
				return
			}
			if strVal, ok := value.(string); ok && len(strVal) > 5000 {
				c.JSON(400, gin.H{"error": fmt.Sprintf("Value for key '%s' length exceeds 5000 characters", key)})
				return
			}

			// Ensure sys.currency stays a float64 when updated via admin endpoint
			if key == "sys.currency" {
				users[userIndex].SetBalance(value)
			} else {
				users[userIndex].Set(key, value)
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

			users[userIndex].DelKey(key)

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
	user := c.MustGet("user").(*User)

	var req struct {
		Amount float64 `json:"amount"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request payload"})
		return
	}

	nAmount, ok := normalizeEscrowAmount(req.Amount)
	if !ok {
		c.JSON(400, gin.H{"error": "Minimum amount is 0.01"})
		return
	}

	cf := user.GetCredits()
	if cf < nAmount {
		c.JSON(400, gin.H{"error": "Insufficient funds"})
		return
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	if r.Intn(100) < 40 {
		user.SetBalance(roundVal(cf + nAmount))
		c.JSON(200, gin.H{"message": "You won!", "won": true, "amount": nAmount, "balance": user.GetCredits()})
	} else {
		user.SetBalance(roundVal(cf - nAmount))
		c.JSON(200, gin.H{"message": "You lost!", "won": false, "amount": nAmount, "balance": user.GetCredits()})
	}
	go saveUsers()
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

	user.DelKey(key)

	go saveUsers()

	c.JSON(204, gin.H{"message": "User key deleted successfully", "username": username, "key": key})
}

// PerformCreditTransfer performs a credit transfer between two users.
// Handles tax, transaction logging, and safety rules.
// Returns an error if the transfer cannot be completed.
func PerformCreditTransfer(fromUsername, toUsername string, amount float64, note string) error {
	const totalTax = 1.0
	const taxRecipientShare = 0.5

	// normalize + validate amount
	nAmount, ok := normalizeEscrowAmount(amount)
	if !ok {
		return fmt.Errorf("minimum amount is 0.01")
	}

	fromUsers, err := getAccountsBy("username", fromUsername, 1)
	if err != nil {
		return fmt.Errorf("sender user not found")
	}
	fromUser := fromUsers[0]

	toUsers, err := getAccountsBy("username", toUsername, 1)
	if err != nil {
		return fmt.Errorf("recipient user not found")
	}
	toUser := toUsers[0]

	if strings.EqualFold(fromUser.GetUsername(), toUser.GetUsername()) {
		return fmt.Errorf("cannot send credits to yourself")
	}

	fromCurrency := roundVal(fromUser.GetCredits())
	if fromCurrency < (nAmount + totalTax) {
		return fmt.Errorf("insufficient funds (required: %.2f, available: %.2f)", nAmount+totalTax, fromCurrency)
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

	// Helper: get or fix transaction slice
	ensureTxSlice := func(u *User) []map[string]any {
		raw := (*u)["sys.transactions"]
		list := make([]map[string]any, 0)
		switch v := raw.(type) {
		case nil:
		case []any:
			for _, item := range v {
				if m, ok := item.(map[string]any); ok {
					list = append(list, m)
				}
			}
		case []map[string]any:
			list = v
		}
		return list
	}
	appendTx := func(u *User, tx map[string]any) {
		txs := ensureTxSlice(u)
		txs = append([]map[string]any{tx}, txs...)
		if len(txs) > 20 {
			txs = txs[:20]
		}
		(*u)["sys.transactions"] = txs
	}

	// Tax handling
	taxRecipient := "mist"
	fromSystem := fromUser.Get("system")
	if fromSystem != nil {
		systemsMutex.RLock()
		if sys, ok := systems[fromSystem.(string)]; ok {
			taxRecipient = sys.Owner.Name
		}
		systemsMutex.RUnlock()
	}

	// Apply tax to taxRecipient if exists
	if idx := getIdxOfAccountBy("username", taxRecipient); idx != -1 {
		taxUser := users[idx]
		curr := taxUser.GetCredits()
		taxUser.SetBalance(roundVal(curr + taxRecipientShare))
		appendTx(&taxUser, map[string]any{
			"note":      "transfer tax",
			"user":      fromUser.GetUsername(),
			"time":      now,
			"amount":    taxRecipientShare,
			"type":      "tax",
			"new_total": curr + taxRecipientShare,
		})
		go broadcastUserUpdate(taxUser.GetUsername(), "sys.transactions", taxUser.Get("sys.transactions"))
	}

	// Update balances
	if fromUser.GetUsername() != "rotur" {
		fromUser.SetBalance(roundVal(fromCurrency - (nAmount + totalTax)))
	}
	if toUser.GetUsername() != "rotur" {
		toUser.SetBalance(roundVal(toCurrency + nAmount))
	}

	// Log transactions
	appendTx(&fromUser, map[string]any{
		"note":      note,
		"user":      toUser.GetUsername(),
		"time":      now,
		"amount":    nAmount + totalTax,
		"type":      "out",
		"new_total": fromCurrency - nAmount - totalTax,
	})
	appendTx(&toUser, map[string]any{
		"note":      note,
		"user":      fromUser.GetUsername(),
		"time":      now,
		"amount":    nAmount,
		"type":      "in",
		"new_total": toCurrency + nAmount,
	})

	go broadcastUserUpdate(fromUser.GetUsername(), "sys.transactions", fromUser.Get("sys.transactions"))
	go broadcastUserUpdate(toUser.GetUsername(), "sys.transactions", toUser.Get("sys.transactions"))
	go saveUsers()

	return nil
}

func transferCredits(c *gin.Context) {
	user := c.MustGet("user").(*User)

	var req struct {
		To     string  `json:"to"`
		Amount float64 `json:"amount"`
		Note   string  `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request payload"})
		return
	}
	// normalize + validate
	nAmount, ok := normalizeEscrowAmount(req.Amount)
	if !ok {
		c.JSON(400, gin.H{"error": "Minimum amount is 0.01"})
		return
	}

	toUsername := strings.ToLower(req.To)
	if toUsername == "" {
		c.JSON(400, gin.H{"error": "Recipient username and amount must be provided"})
		return
	}
	if toUsername == strings.ToLower(user.GetUsername()) {
		c.JSON(400, gin.H{"error": "Cannot send credits to yourself"})
		return
	}

	err := PerformCreditTransfer(user.GetUsername(), toUsername, nAmount, req.Note)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "Transfer successful", "from": user.GetUsername(), "to": toUsername, "amount": nAmount, "debited": nAmount + 1.0})
}

func deleteUser(c *gin.Context) {
	user := c.MustGet("user").(*User)

	username := c.Param("username")
	if username == "" {
		c.JSON(400, gin.H{"error": "Username is required"})
		return
	}

	usernameLower := strings.ToLower(username)
	requester := strings.ToLower(user.GetUsername())
	if requester != "mist" && requester != usernameLower {
		c.JSON(403, gin.H{"error": "Insufficient permissions to delete this user"})
		return
	}

	if err := performUserDeletion(username, false); err != nil {
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

	username := req.Username
	if username == "" {
		c.JSON(400, gin.H{"error": "Username is required"})
		return
	}

	if err := performUserDeletion(username, true); err != nil {
		c.JSON(404, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "User deleted successfully"})
}

func transferCreditsAdmin(c *gin.Context) {
	if !authenticateAdmin(c) {
		return
	}

	toUsername := c.Query("to")
	amountStr := c.Query("amount")
	fromUsername := c.Query("from")

	amountNum, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	err = PerformCreditTransfer(fromUsername, toUsername, amountNum, "")
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"message": "Transfer successful", "from": fromUsername, "to": toUsername, "amount": amountNum, "debited": amountNum + 1.0})
}

func removeUserDirectory(path string) error {
	return os.RemoveAll(path)
}

func performUserDeletion(username string, isAdmin bool) error {
	usernameLower := strings.ToLower(username)

	idx := getIdxOfAccountBy("username", usernameLower)
	if idx == -1 {
		return fmt.Errorf("user not found")
	}

	logPrefix := "Deleting user"
	if isAdmin {
		logPrefix = "Admin deleting user"
	}
	log.Printf("%s %s (total before=%d)", logPrefix, usernameLower, len(users))

	usersMutex.Lock()
	defer usersMutex.Unlock()

	users = append(users[:idx], users[idx+1:]...)

	go broadcastUserUpdate(usernameLower, "sys._deleted", true)

	for i := range users {
		target := &users[i]

		friends := target.GetFriends()
		if len(friends) > 0 {
			filtered := make([]string, 0, len(friends))
			for _, f := range friends {
				if !strings.EqualFold(f, usernameLower) {
					filtered = append(filtered, f)
				}
				if len(filtered) != len(friends) {
					target.SetFriends(filtered)
				}
			}
		}

		requests := target.GetRequests()
		if len(requests) > 0 {
			filtered := make([]string, 0, len(requests))
			for _, r := range requests {
				if !strings.EqualFold(r, usernameLower) {
					filtered = append(filtered, r)
				}
			}
			if len(filtered) != len(requests) {
				target.SetRequests(filtered)
			}
		}

		blocked := target.GetBlocked()
		if len(blocked) > 0 {
			filtered := make([]string, 0, len(blocked))
			for _, b := range blocked {
				if !strings.EqualFold(b, usernameLower) {
					filtered = append(filtered, b)
				}
			}
			if len(filtered) != len(blocked) {
				target.SetBlocked(filtered)
			}
		}
	}

	go saveUsers()

	go func(target string) {
		// Update posts
		postsMutex.Lock()
		for i := range posts {
			if strings.EqualFold(posts[i].User, target) {
				posts[i].User = "Deleted User"
			}
		}
		postsMutex.Unlock()
		go savePosts()

		// Remove user storage
		userDir := "rotur/user_storage/" + target
		if err := removeUserDirectory(userDir); err != nil {
			log.Printf("Error removing user directory %s: %v", userDir, err)
		}

		userFile := "/Users/admin/Documents/rotur/files/" + target + ".ofsf"
		if err := removeUserDirectory(userFile); err != nil {
			log.Printf("Error removing user file %s: %v", userFile, err)
		}
	}(usernameLower)

	log.Printf("%s %s (total after=%d)", logPrefix, usernameLower, len(users))
	return nil
}

func claimDaily(c *gin.Context) {
	user := c.MustGet("user").(*User)

	username := strings.ToLower(user.GetUsername())

	// Load daily claims data
	claimsData := loadDailyClaims()
	currentTime := float64(time.Now().Unix())

	// Check if user has already claimed today
	if lastClaim, exists := claimsData[username]; exists {
		timeDiff := currentTime - lastClaim
		if timeDiff < 86400 { // 24 hours
			waitTime := (86400 - timeDiff) / 3600
			c.JSON(400, gin.H{"error": "Daily claim already made, please wait " +
				strings.TrimSuffix(strings.TrimSuffix(
					fmt.Sprintf("%.1f", waitTime), "0"), ".") + " hours"})
			return
		}
	}

	// Update claim time
	claimsData[username] = currentTime
	saveDailyClaims(claimsData)

	// Add daily credits (rounded)
	usersMutex.Lock()
	userIndex := -1
	for i, u := range users {
		if strings.EqualFold(u.GetUsername(), username) {
			userIndex = i
			break
		}
	}
	if userIndex == -1 {
		usersMutex.Unlock()
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}
	curr := user.GetCredits()
	newCurrency := roundVal(curr + 1.00)
	usersMutex.Unlock()
	users[userIndex].SetBalance(newCurrency)
	go saveUsers()

	c.JSON(200, gin.H{"message": "Daily claim successful"})
}

// loadDailyClaims loads daily claims data from rotur_daily.json
func loadDailyClaims() map[string]float64 {
	data, err := os.ReadFile(DAILY_CLAIMS_FILE_PATH)
	if err != nil {
		// If file doesn't exist, return empty map
		return make(map[string]float64)
	}

	var claimsData map[string]float64
	if err := json.Unmarshal(data, &claimsData); err != nil {
		// If unmarshal fails, return empty map
		return make(map[string]float64)
	}

	return claimsData
}

// saveDailyClaims saves daily claims data to rotur_daily.json
func saveDailyClaims(claimsData map[string]float64) {
	data, err := json.MarshalIndent(claimsData, "", "  ")
	if err != nil {
		return
	}

	os.WriteFile(DAILY_CLAIMS_FILE_PATH, data, 0644)
}

func acceptTos(c *gin.Context) {
	if c.GetHeader("Origin") != "https://rotur.dev" {
		c.JSON(403, gin.H{"error": "This endpoint is only available on rotur.dev"})
		return
	}

	user := c.MustGet("user").(*User)

	// Accept the TOS by setting a flag in the user data
	go patchUserUpdate(user.GetUsername(), "sys.tos_accepted", true)
	go patchUserUpdate(user.GetUsername(), "sys.tos_time", time.Now().Unix())

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
