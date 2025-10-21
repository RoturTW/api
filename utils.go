package main

import (
	"bytes"
	"crypto/rand"
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
	if s, ok := val.(string); ok {
		return s
	}
	return ""
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
	for _, allowedType := range allowedTypes {
		if contentType == allowedType {
			return true
		}
	}
	return false
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

	c.Set("user", user)
	c.Next()
}

func getBannedIPs() []string {
	bannedIPs := os.Getenv("BANNED_IPS")
	if bannedIPs == "" {
		return []string{}
	}
	return strings.Split(bannedIPs, ",")
}

func isBannedIp(ip string) bool {
	bannedIPs := getBannedIPs()
	if slices.Contains(bannedIPs, ip) {
		return true
	}

	if slices.Contains(torips, ip) {
		return true
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
