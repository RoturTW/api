package main

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
				user = copyUser(user)
				delete(user, "password")
				matches = append(matches, user)
				if max != -1 && len(matches) >= max {
					return matches, nil
				}
			}
		}
	} else {
		for _, user := range users {
			if fmt.Sprintf("%v", user.Get(key)) == value {
				user = copyUser(user)
				delete(user, "password")
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
			user = copyUser(user)
			delete(user, "password")
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

	c.JSON(200, users[0])
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
		foundUser.Set("sys.last_login", time.Now().UnixMilli())
		foundUser.Set("sys.total_logins", foundUser.GetInt("sys.total_logins")+1)
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
	authKey := c.Query("auth")
	if authKey == "" {
		c.JSON(403, gin.H{"error": "auth key is required"})
		return
	}

	user := authenticateWithKey(authKey)
	if user == nil {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		return
	}

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
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request body"})
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

	newUser := User{
		"username":         usernameLower,
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

	totalSize := findUserSize(username)
	if totalSize+len(fmt.Sprintf("%v", value)) > 25000 {
		c.JSON(400, gin.H{"error": "Total account size exceeds 25000 bytes"})
		return
	}

	if key == "banner" {
		// Allow both data URIs and normal URLs
		var imageData string
		if strings.HasPrefix(stringValue, "data:") {
			// Use data URI as-is
			imageData = stringValue
		} else if strings.HasPrefix(stringValue, "http://") || strings.HasPrefix(stringValue, "https://") {
			// Proxy normal URLs through proxy.mistium.com
			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Get(stringValue)
			if err != nil {
				c.JSON(400, gin.H{"error": "Failed to fetch profile picture URL", "detail": err.Error()})
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				c.JSON(resp.StatusCode, gin.H{"error": "Failed to fetch profile picture URL"})
				return
			}
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				c.JSON(500, gin.H{"error": "Failed to read profile picture data"})
				return
			}
			ct := resp.Header.Get("Content-Type")
			if ct == "" {
				ct = http.DetectContentType(data)
			}
			imageData = "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(data)
		} else {
			c.JSON(400, gin.H{"error": "Banner must be a valid data URI or HTTP/HTTPS URL"})
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
			// Use data URI as-is
			imageData = stringValue
		} else if strings.HasPrefix(stringValue, "https://") {
			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Get(stringValue)
			if err != nil {
				c.JSON(400, gin.H{"error": "Failed to fetch profile picture URL", "detail": err.Error()})
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				c.JSON(resp.StatusCode, gin.H{"error": "Failed to fetch profile picture URL"})
				return
			}
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				c.JSON(500, gin.H{"error": "Failed to read profile picture data"})
				return
			}
			ct := resp.Header.Get("Content-Type")
			if ct == "" {
				ct = http.DetectContentType(data)
			}
			imageData = "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(data)
		} else {
			c.JSON(400, gin.H{"error": "Profile picture must be a valid data URI or HTTP/HTTPS URL"})
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
	authKey := c.Query("auth")
	if authKey == "" {
		c.JSON(403, gin.H{"error": "auth key is required"})
		return
	}

	user := authenticateWithKey(authKey)
	if user == nil {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		return
	}

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

func transferCredits(c *gin.Context) {
	authKey := c.Query("auth")
	if authKey == "" {
		c.JSON(403, gin.H{"error": "auth key is required"})
		return
	}
	user := authenticateWithKey(authKey)
	if user == nil {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		return
	}
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
	// Flat tax of 1.0 taken from sender; 0.5 goes to 'mist', 0.5 is burned
	const totalTax = 1.0
	const taxRecipientShare = 0.5

	// Helper to normalize / validate transactions slice
	ensureTxSlice := func(u *User) []map[string]any {
		raw := (*u)["sys.transactions"]
		list := make([]map[string]any, 0)
		switch v := raw.(type) {
		case nil:
			// leave empty
		case []any:
			for _, item := range v {
				if m, ok := item.(map[string]any); ok {
					list = append(list, m)
				}
			}
		case []map[string]any:
			list = v
		default:
			// invalid type, reset
		}
		return list
	}
	appendTx := func(u *User, tx map[string]any) {
		txs := ensureTxSlice(u)
		txs = append([]map[string]any{tx}, txs...)
		if len(txs) > 20 { // keep most recent 20
			txs = txs[:20]
		}
		(*u)["sys.transactions"] = txs
	}
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

	idx := getIdxOfAccountBy("username", user.GetUsername())
	if idx == -1 {
		c.JSON(404, gin.H{"error": "Sender user not found"})
		return
	}
	var fromUser User = users[idx]
	fromCurrency := roundVal(fromUser.GetCredits())

	if fromCurrency < (nAmount + totalTax) {
		c.JSON(400, gin.H{"error": "Insufficient funds including tax", "required": nAmount + totalTax, "available": fromCurrency})
		return
	}

	idx = getIdxOfAccountBy("username", toUsername)
	if idx == -1 {
		c.JSON(404, gin.H{"error": "Recipient user not found"})
		return
	}
	var toUser User = users[idx]
	toCurrency := roundVal(toUser.GetCredits())

	now := time.Now().UnixMilli()

	idx = getIdxOfAccountBy("username", "mist")
	if idx != -1 {
		var taxUser User = users[idx]
		curr := taxUser.GetCredits()
		taxUser.SetBalance(roundVal(curr + taxRecipientShare))
		usersMutex.Lock()
		appendTx(&taxUser, map[string]any{
			"note":   "transfer tax",
			"user":   fromUser.GetUsername(),
			"time":   now,
			"amount": taxRecipientShare,
			"type":   "tax",
		})
		usersMutex.Unlock()
		go broadcastUserUpdate(taxUser.GetUsername(), "sys.transactions", taxUser.Get("sys.transactions"))
	}
	if fromUser.GetUsername() != "rotur" { // prevent the rotur account from having currency deducted
		fromUser.SetBalance(roundVal(fromCurrency - (nAmount + totalTax)))
	}
	if toUser.GetUsername() != "rotur" { // prevent the rotur account from accumulating currency
		toUser.SetBalance(roundVal(toCurrency + nAmount))
	}
	usersMutex.Lock()
	defer usersMutex.Unlock()
	note := mkNote(req.Note)
	appendTx(&fromUser, map[string]any{
		"note":   note,
		"user":   toUser.GetUsername(),
		"time":   now,
		"amount": nAmount + totalTax,
		"type":   "out",
	})
	appendTx(&toUser, map[string]any{
		"note":   note,
		"user":   fromUser.GetUsername(),
		"time":   now,
		"amount": nAmount,
		"type":   "in",
	})

	go saveUsers()

	go broadcastUserUpdate(fromUser.GetUsername(), "sys.transactions", fromUser.Get("sys.transactions"))
	go broadcastUserUpdate(toUser.GetUsername(), "sys.transactions", toUser.Get("sys.transactions"))

	c.JSON(200, gin.H{"message": "Transfer successful", "from": fromUser.GetUsername(), "to": toUsername, "amount": nAmount, "debited": nAmount + totalTax})
}

func deleteUser(c *gin.Context) {
	authKey := c.Query("auth")
	if authKey == "" {
		c.JSON(403, gin.H{"error": "auth key is required"})
		return
	}

	user := authenticateWithKey(authKey)
	if user == nil {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		return
	}

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

func removeUserDirectory(path string) error {
	return os.RemoveAll(path)
}

func performUserDeletion(username string, isAdmin bool) error {
	usernameLower := strings.ToLower(username)

	usersMutex.Lock()
	idx := -1
	for i, u := range users {
		if strings.EqualFold(u.GetUsername(), usernameLower) {
			idx = i
			break
		}
	}
	if idx == -1 {
		usersMutex.Unlock()
		return fmt.Errorf("user not found")
	}

	if isAdmin {
		log.Printf("Admin deleting user %s (total before=%d)", usernameLower, len(users))
	} else {
		log.Printf("Deleting user %s (total before=%d)", usernameLower, len(users))
	}
	users = append(users[:idx], users[idx+1:]...)

	for i := range users {
		var friendsUpdated, requestsUpdated bool
		if friends, ok := users[i]["sys.friends"].([]string); ok {
			filtered := friends[:0]
			for _, f := range friends {
				if !strings.EqualFold(f, usernameLower) {
					filtered = append(filtered, f)
				}
			}
			if len(filtered) != len(friends) {
				users[i]["sys.friends"] = filtered
				friendsUpdated = true
			}
		}
		if requests, ok := users[i]["sys.requests"].([]string); ok {
			filtered := requests[:0]
			for _, r := range requests {
				if !strings.EqualFold(r, usernameLower) {
					filtered = append(filtered, r)
				}
			}
			if len(filtered) != len(requests) {
				users[i]["sys.requests"] = filtered
				requestsUpdated = true
			}
		}

		if friendsUpdated || requestsUpdated {
			username := users[i].GetUsername()
			if friendsUpdated {
				go broadcastUserUpdate(username, "sys.friends", users[i]["sys.friends"])
			}
			if requestsUpdated {
				go broadcastUserUpdate(username, "sys.requests", users[i]["sys.requests"])
			}
		}
	}
	usersAfter := len(users)
	usersMutex.Unlock()

	saveUsers()

	go func(target string) {
		postsMutex.Lock()
		for i := range posts {
			if strings.EqualFold(posts[i].User, target) {
				posts[i].User = "Deleted User"
			}
		}
		postsMutex.Unlock()
		go savePosts()
	}(usernameLower)

	go func() {
		userDir := "rotur/user_storage/" + usernameLower
		_ = removeUserDirectory(userDir)
		userFile := "/Users/admin/Documents/rotur/files/" + usernameLower + ".ofsf"
		_ = removeUserDirectory(userFile)
	}()

	if isAdmin {
		log.Printf("Admin deleted user %s (total after=%d)", usernameLower, usersAfter)
	} else {
		log.Printf("Deleted user %s (total after=%d)", usernameLower, usersAfter)
	}
	return nil
}

func claimDaily(c *gin.Context) {
	authKey := c.Query("auth")
	if authKey == "" {
		c.JSON(403, gin.H{"error": "auth key is required"})
		return
	}

	user := authenticateWithKey(authKey)
	if user == nil {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		return
	}

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
	authKey := c.Query("auth")
	if authKey == "" {
		c.JSON(403, gin.H{"error": "auth key is required"})
		return
	}

	if c.GetHeader("Origin") != "https://rotur.dev" {
		c.JSON(403, gin.H{"error": "This endpoint is only available on rotur.dev"})
		return
	}

	user := authenticateWithKey(authKey)
	if user == nil {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		return
	}

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
	authKey := c.Query("auth")
	if authKey == "" {
		c.JSON(403, gin.H{"error": "auth key is required"})
		return
	}

	user := authenticateWithKey(authKey)
	if user == nil {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		return
	}

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
