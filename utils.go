package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// Helper functions
func generateToken() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func generateShortToken() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func roundVal(val float64) float64 {
	return math.Round(val*100) / 100
}

func getStringOrEmpty(val any) string {
	return getStringOrDefault(val, "")
}

func getStringOrDefault(val any, defaultVal string) string {
	if val == nil {
		return ""
	}
	if s, ok := val.(string); ok {
		return s
	}
	return defaultVal
}

func getIntOrDefault(val any, defaultVal int) int {
	if val == nil {
		return defaultVal
	}

	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}

	return defaultVal
}

func getFloatOrDefault(val any, defaultVal float64) float64 {
	if val == nil {
		return defaultVal
	}
	switch val := val.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case json.Number:
		f, _ := val.Float64()
		return f
	default:
		return defaultVal
	}
}

func requireTier(tier string) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := c.MustGet("user").(*User)
		user_tier := user.GetSubscription().Tier
		if hasTierOrHigher(user_tier, tier) {
			c.Next()
			return
		}
		c.JSON(403, gin.H{"error": "You need a higher subscription tier to access this endpoint"})
		c.Abort()
	}
}

func doAfter(fn func(any), data any, after time.Duration) {
	time.Sleep(after)
	go func() {
		fn(data)
	}()
}

func hasTierOrHigher(tier string, required string) bool {
	tier = strings.ToLower(tier)
	switch strings.ToLower(required) {
	case "max":
		return tier == "max"
	case "pro":
		return tier == "pro" || tier == "max"
	case "drive":
		return tier == "drive" || tier == "pro" || tier == "max"
	case "lite":
		return tier == "lite" || tier == "drive" || tier == "pro" || tier == "max"
	}
	return false
}

func loadBannedWords() {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(BANNED_WORDS_URL)
	if err != nil {
		log.Printf("Error loading banned words list: %v", err)
		derogatoryTerms = []string{} // Fallback to empty list
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("Error reading banned words: %v", err)
			return
		}

		words := strings.Split(string(body), "\n")
		derogatoryTerms = make([]string, 0, len(words))
		for _, word := range words {
			word = strings.TrimSpace(word)
			if word != "" {
				derogatoryTerms = append(derogatoryTerms, word)
			}
		}
		log.Printf("Loaded %d banned words", len(derogatoryTerms))
	} else {
		log.Printf("Failed to load banned words list: HTTP %d", resp.StatusCode)
		derogatoryTerms = []string{} // Fallback to empty list
	}
}

func containsDerogatory(text string) bool {
	if text == "" {
		return false
	}

	textLower := strings.ToLower(text)
	for _, term := range derogatoryTerms {
		pattern := `\b` + regexp.QuoteMeta(strings.ToLower(term)) + `\b`
		matched, _ := regexp.MatchString(pattern, textLower)
		if matched {
			return true
		}
	}
	return false
}

func accountExists(userId UserId) bool {
	usersMutex.RLock()
	defer usersMutex.RUnlock()

	_, ok := idToUser[userId]
	return ok
}

func isUserBlockedBy(user User, userId UserId) bool {
	usersMutex.RLock()
	defer usersMutex.RUnlock()

	blocked := user.GetBlocked()
	for _, blockedId := range blocked {
		if blockedId == userId {
			return true
		}
	}

	return false
}

func isFromBannedDomain(url string) bool {
	if url == "" {
		return false
	}

	urlLower := strings.ToLower(url)
	for _, domain := range bannedDomains {
		if strings.Contains(urlLower, domain) {
			return true
		}
	}
	return false
}

func isValidMimeType(url string, allowedTypes []string) bool {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Head(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	return slices.Contains(allowedTypes, contentType)
}

// Rate limiting functions
func applyRateLimit(key string, limitType string) (bool, int, float64) {
	rateLimitMutex.Lock()
	defer rateLimitMutex.Unlock()

	currentTime := time.Now().Unix()
	limits, exists := rateLimits[limitType]
	if !exists {
		limits = rateLimits["default"]
	}

	compositeKey := limitType + ":" + key

	rateLimit, exists := rateLimitStorage[compositeKey]
	if !exists || currentTime > rateLimit.ResetAt {
		rateLimitStorage[compositeKey] = &RateLimit{
			Count:   0,
			ResetAt: currentTime + int64(limits.Period),
		}
		rateLimit = rateLimitStorage[compositeKey]
	}

	rateLimit.Count++

	isAllowed := rateLimit.Count <= limits.Count
	remaining := max(limits.Count-rateLimit.Count, 0)

	// If rate limit exceeded, add 10 seconds penalty
	// Fuck scrapers and bots ngl
	if !isAllowed {
		rateLimit.ResetAt += 10
	}

	resetTime := float64(rateLimit.ResetAt)

	return isAllowed, remaining, resetTime
}

func getRateLimitKey(c *gin.Context) string {
	authKey := c.Query("auth")
	if authKey != "" {
		return authKey
	}

	clientIP := c.ClientIP()
	if clientIP != "" {
		return clientIP
	}

	return "unknown_client"
}

func cleanRateLimitStorage() {
	for {
		time.Sleep(5 * time.Minute)
		currentTime := time.Now().Unix()

		rateLimitMutex.Lock()
		keysToRemove := make([]string, 0)

		for key, data := range rateLimitStorage {
			if currentTime > data.ResetAt {
				keysToRemove = append(keysToRemove, key)
			}
		}

		for _, key := range keysToRemove {
			delete(rateLimitStorage, key)
		}

		rateLimitMutex.Unlock()
	}
}

func getUserByIdx(idx int) (*User, error) {
	usersMutex.RLock()
	defer usersMutex.RUnlock()
	if idx < 0 || len(users) <= idx {
		return nil, fmt.Errorf("index out of bounds")
	}
	user := &users[idx]
	return user, nil
}

func rateLimit(limitType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		rateLimitKey := getRateLimitKey(c)
		isAllowed, remaining, resetTime := applyRateLimit(rateLimitKey, limitType)

		if !isAllowed {
			c.Header("X-RateLimit-Limit", strconv.Itoa(rateLimits[limitType].Count))
			c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))
			c.Header("X-RateLimit-Reset", strconv.FormatFloat(resetTime, 'f', 0, 64))
			c.JSON(429, gin.H{"error": "Rate limit exceeded. Rate limit extended by 10 seconds due to violation.", "reset_time": resetTime, "remaining": remaining})
			c.Abort()
			return
		}

		c.Next()
	}
}

func requiresAuth(c *gin.Context) {
	authKey := c.Query("auth")
	if authKey == "" {
		c.JSON(403, gin.H{"error": "auth key is required"})
		c.Abort()
		return
	}

	user := authenticateWithKey(authKey)
	if user == nil {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		c.Abort()
		return
	}

	if user.IsBanned() {
		c.JSON(403, gin.H{"error": "User is banned"})
		return
	}
	user.GetSubscription()

	c.Set("user", user)
	c.Next()
}

func getBannedIPs() []string {
	file, err := os.Open("/Users/admin/Documents/rotur/banned.json")
	if err != nil {
		return []string{}
	}
	defer file.Close()

	var data struct {
		IPs []string `json:"ips"`
	}

	if err := json.NewDecoder(file).Decode(&data); err != nil {
		return []string{}
	}

	return data.IPs
}

func isBannedIp(ip string) bool {
	bannedIPs := getBannedIPs()
	if slices.Contains(bannedIPs, ip) {
		return true
	}
	for _, bannedIP := range bannedIPs {
		// handle when ipv6 all start with the same prefix, so ban a block of them
		if bannedIP[4] == ":"[0] && strings.HasPrefix(ip, bannedIP) {
			return true
		}
	}
	return false
}

func corsMiddleware() gin.HandlerFunc {
	config := cors.DefaultConfig()
	config.AllowAllOrigins = true
	config.AllowMethods = []string{"GET", "POST", "PATCH", "DELETE", "PUT", "OPTIONS"}
	config.AllowHeaders = []string{"Content-Type", "Authorization"}
	return cors.New(config)
}

func JSONStringify(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`%v`, v)
	}
	return string(data)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return !os.IsNotExist(err) && info.Mode().IsRegular()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err == nil {
		return info.IsDir()
	}
	if os.IsNotExist(err) {
		return false
	}
	return false
}

func copyAndReplace(src, dst, old, new string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	updated := strings.ReplaceAll(string(data), old, new)

	return os.WriteFile(dst, []byte(updated), 0644)
}

func removeUserPath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // already gone, not an error
		}
		return err
	}

	if info.IsDir() {
		return os.RemoveAll(path)
	}

	return os.Remove(path)
}

func sendWebhook(url string, data map[string]any) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 204 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code: %d (%s)", resp.StatusCode, string(body))
	}
	return nil
}

func hmacIp(ip string) string {
	mac := hmac.New(sha256.New, []byte(os.Getenv("HMAC_KEY")))
	mac.Write([]byte(ip))
	return hex.EncodeToString(mac.Sum(nil))
}

func sendDiscordWebhook(data []map[string]any) {
	webhook := os.Getenv("ACCOUNT_CREATION_WEBHOOK")

	if webhook == "" {
		log.Println("No webhook configured, not sending Discord webhook")
		return
	}
	body := map[string]any{
		"embeds": data,
	}

	go func() {
		if err := sendWebhook(webhook, body); err != nil {
			log.Println("Failed to send account creation webhook:", err)
		}
	}()
}

func clamp(num int, low int, high int) int {
	if num > high {
		return high
	}
	if num < low {
		return low
	}
	return num
}

func deleteAccountAtIndexFast(idx int) error {
	usersMutex.Lock()
	defer usersMutex.Unlock()

	if idx < 0 || idx >= len(users) {
		return fmt.Errorf("index out of range")
	}

	users[idx] = users[len(users)-1]
	users = users[:len(users)-1]

	go saveUsers()
	return nil
}
