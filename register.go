package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"mime"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/api/idtoken"
)

func verifyGoogleTokenImpl(ctx context.Context, token string) (*idtoken.Payload, error) {
	envOnce.Do(loadEnvFile)
	clientID := os.Getenv("GOOGLE_CLIENT_ID")
	if clientID == "" {
		return nil, errors.New("GOOGLE_CLIENT_ID environment variable not set")
	}

	payload, err := idtoken.Validate(ctx, token, clientID)
	if err != nil {
		return nil, err
	}

	if v, ok := payload.Claims["email_verified"]; ok {
		switch vv := v.(type) {
		case bool:
			if !vv {
				return nil, errors.New("email not verified")
			}
		case string:
			if strings.EqualFold(vv, "false") || vv == "0" {
				return nil, errors.New("email not verified")
			}
		}
	}

	return payload, nil
}

var verifyGoogleToken = verifyGoogleTokenImpl

func handleUserGoogle(c *gin.Context) {
	var req struct {
		IDToken string `json:"id_token"`
		System  string `json:"system"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request body"})
		return
	}
	if req.IDToken == "" {
		c.JSON(400, gin.H{"error": "id_token is required"})
		return
	}

	payload, err := verifyGoogleToken(c.Request.Context(), req.IDToken)
	if err != nil {
		c.JSON(401, gin.H{"error": "Invalid Google token"})
		return
	}

	var email string
	if v, ok := payload.Claims["email"]; ok {
		email = strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", v)))
	}
	if email == "" {
		c.JSON(400, gin.H{"error": "Google token missing email"})
		return
	}

	googleSub := strings.TrimSpace(fmt.Sprintf("%v", payload.Subject))
	if googleSub == "" {
		c.JSON(400, gin.H{"error": "Google token missing sub"})
		return
	}

	usersMutex.Lock()
	for i := range users {
		if v, ok := users[i]["sys.google"]; ok {
			if m, ok := v.(map[string]any); ok {
				if sub, ok := m["sub"]; ok {
					if strings.EqualFold(strings.TrimSpace(fmt.Sprintf("%v", sub)), googleSub) {
						now := time.Now().UnixMilli()
						users[i].Set("sys.last_login", now)
						users[i].Set("sys.total_logins", users[i].GetInt("sys.total_logins")+1)
						users[i].Set("sys.badges", calculateUserBadges(&users[i]))
						users[i].SetSubscription(users[i].GetSubscription())
						go saveUsers()

						userCopy := copyUser(users[i])
						delete(userCopy, "password")
						usersMutex.Unlock()
						c.JSON(200, userCopy)
						return
					}
				}
			}
		}

		if strings.EqualFold(users[i].GetEmail(), email) {
			usersMutex.Unlock()
			c.JSON(403, gin.H{"error": "Account not linked to Google"})
			return
		}
	}
	usersMutex.Unlock()

	isValid, errorMessage, matchedSystem := validateSystem(req.System)
	if !isValid {
		c.JSON(400, gin.H{"error": errorMessage})
		return
	}

	username, err := generateUsernameFromGoogle(payload)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	usernameLower := strings.ToLower(username)

	password := generateRandomMD5LikePasswordHash()

	fromURL := c.GetHeader("referer")
	if fromURL == "" {
		fromURL = c.GetHeader("origin")
		if fromURL == "" {
			fromURL = "unknown"
		}
	}

	newUser, err := createAccount(AccountCreateInput{
		Username:      username,
		Password:      password,
		Email:         email,
		System:        matchedSystem,
		Provider:      "google",
		RequestIP:     c.ClientIP(),
		RequestOrigin: fromURL,
		ExtraSys: map[string]any{
			"sys.google": map[string]any{
				"sub":     fmt.Sprintf("%v", payload.Subject),
				"email":   email,
				"name":    fmt.Sprintf("%v", payload.Claims["name"]),
				"picture": fmt.Sprintf("%v", payload.Claims["picture"]),
			},
		},
	})
	if err != nil {
		if strings.Contains(err.Error(), "username already") {
			c.JSON(409, gin.H{"error": "Username already in use"})
			return
		}
		if strings.Contains(err.Error(), "email already") {
			c.JSON(409, gin.H{"error": "Email already in use"})
			return
		}
		c.JSON(500, gin.H{"error": "Failed to create account"})
		return
	}

	if picURL, ok := payload.Claims["picture"]; ok {
		picture := strings.TrimSpace(fmt.Sprintf("%v", picURL))
		if picture != "" {
			go func(token string, username string, pictureURL string) {
				if err := uploadGoogleProfilePictureToRotur(token, pictureURL); err == nil {
					broadcastUserUpdate(username, "pfp", "https://avatars.rotur.dev/"+username)
					go saveUsers()
				}
			}(newUser.GetKey(), usernameLower, picture)
		}
	}

	userCopy := copyUser(newUser)
	delete(userCopy, "password")
	c.JSON(201, userCopy)
}

func uploadGoogleProfilePictureToRotur(userToken string, pictureURL string) error {
	envOnce.Do(loadEnvFile)
	if os.Getenv("ADMIN_TOKEN") == "" {
		return errors.New("ADMIN_TOKEN environment variable not set")
	}

	u, err := url.Parse(pictureURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return errors.New("invalid picture url")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(pictureURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("failed to fetch picture: status %d", resp.StatusCode)
	}

	const maxBytes = 2 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return errors.New("empty picture")
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = mime.TypeByExtension(".png")
	}
	if !strings.HasPrefix(contentType, "image/") {
		return errors.New("picture is not an image")
	}

	dataURI := "data:" + contentType + ";base64," + encodeBase64(data)
	resp2, err := uploadUserImage("pfp", dataURI, userToken)
	if err != nil {
		return err
	}
	if resp2.StatusCode != http.StatusOK {
		return fmt.Errorf("pfp upload failed: status %d", resp2.StatusCode)
	}
	return nil
}

func encodeBase64(b []byte) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var out strings.Builder
	out.Grow(((len(b) + 2) / 3) * 4)
	for i := 0; i < len(b); i += 3 {
		remain := len(b) - i
		var n uint32
		n |= uint32(b[i]) << 16
		if remain > 1 {
			n |= uint32(b[i+1]) << 8
		}
		if remain > 2 {
			n |= uint32(b[i+2])
		}
		out.WriteByte(alphabet[(n>>18)&63])
		out.WriteByte(alphabet[(n>>12)&63])
		if remain > 1 {
			out.WriteByte(alphabet[(n>>6)&63])
		} else {
			out.WriteByte('=')
		}
		if remain > 2 {
			out.WriteByte(alphabet[n&63])
		} else {
			out.WriteByte('=')
		}
	}
	return out.String()
}

func generateUsernameFromGoogle(payload *idtoken.Payload) (string, error) {
	var base string
	if v, ok := payload.Claims["given_name"]; ok {
		base = fmt.Sprintf("%v", v)
	}
	if base == "" {
		if v, ok := payload.Claims["name"]; ok {
			base = fmt.Sprintf("%v", v)
		}
	}
	if base == "" {
		if v, ok := payload.Claims["email"]; ok {
			email := fmt.Sprintf("%v", v)
			if idx := strings.Index(email, "@"); idx > 0 {
				base = email[:idx]
			}
		}
	}
	base = strings.ToLower(strings.TrimSpace(base))
	base = strings.ReplaceAll(base, " ", "_")
	base = strings.ReplaceAll(base, "-", "_")
	base = strings.ReplaceAll(base, ".", "_")

	re := regexp.MustCompile(`[^a-z0-9_]`)
	base = re.ReplaceAllString(base, "")
	for strings.Contains(base, "__") {
		base = strings.ReplaceAll(base, "__", "_")
	}
	base = strings.Trim(base, "_")

	if len(base) < 3 {
		base = "user"
	}
	if len(base) > 16 {
		base = base[:16]
	}

	sub := fmt.Sprintf("%v", payload.Subject)
	suffix := ""
	if len(sub) >= 6 {
		suffix = sub[len(sub)-6:]
	}
	if suffix != "" {
		suffix = regexp.MustCompile(`[^a-zA-Z0-9]`).ReplaceAllString(suffix, "")
		suffix = strings.ToLower(suffix)
		if suffix != "" {
			base = fmt.Sprintf("%s_%s", base, suffix)
		}
	}
	if len(base) > 20 {
		base = base[:20]
	}

	if len(base) < 3 || len(base) > 20 {
		return "", errors.New("generated username is invalid length")
	}
	if strings.Contains(base, " ") {
		return "", errors.New("generated username contains spaces")
	}
	if regexp.MustCompile("[^a-z0-9_]").FindStringIndex(base) != nil {
		return "", errors.New("generated username contains invalid characters")
	}
	return base, nil
}

func generateRandomMD5LikePasswordHash() string {
	const hexChars = "0123456789abcdef"
	b := make([]byte, 32)
	for i := range b {
		b[i] = hexChars[rand.Intn(len(hexChars))]
	}
	s := string(b)
	if s == "d41d8cd98f00b204e9800998ecf8427e" {
		b[0] = '0'
		return string(b)
	}
	return s
}
