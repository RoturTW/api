package main

import (
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/gin-gonic/gin"
)

type NotificationEndpoint struct {
	DeviceID  string `json:"device_id"`
	Endpoint  string `json:"endpoint"`
	P256dh    string `json:"p256dh"`
	Auth      string `json:"auth"`
	Source    string `json:"source"`
	CreatedAt int64  `json:"created_at"`
}

type NotificationEndpointsFile struct {
	Endpoints []NotificationEndpoint `json:"endpoints"`
}

type NotifyAllowedEntry struct {
	Count int64 `json:"count"`
}

type NotifyLogEntry struct {
	From   Username `json:"from"`
	Source string   `json:"source"`
	Title  string   `json:"title,omitempty"`
	Body   string   `json:"body,omitempty"`
	At     int64    `json:"at"`
}

type NotifyAllowedMap map[string]map[UserId]NotifyAllowedEntry
type NotifyLog []NotifyLogEntry

var notifyMutexes sync.RWMutex
var notifyUserMutexes = map[Username]*sync.RWMutex{}

var (
	vapidPrivateKey string
	vapidPublicKey  string
	vapidOnce       sync.Once
)

func loadOrGenerateVAPIDKeys() {
	keyPath := mustEnv("VAPID_KEY_PATH", "./vapid_keys.json")

	data, err := os.ReadFile(keyPath)
	if err == nil {
		var stored struct {
			PublicKey  string `json:"public_key"`
			PrivateKey string `json:"private_key"`
		}
		if json.Unmarshal(data, &stored) == nil &&
			stored.PublicKey != "" &&
			stored.PrivateKey != "" {

			vapidPublicKey = stored.PublicKey
			vapidPrivateKey = stored.PrivateKey
			log.Println("[VAPID] Loaded existing keys from", keyPath)
			return
		}
	}

	privateKey, publicKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		log.Fatalf("[VAPID] Failed to generate keys: %v", err)
	}

	vapidPrivateKey = privateKey
	vapidPublicKey = publicKey

	out, _ := json.MarshalIndent(map[string]string{
		"public_key":  publicKey,
		"private_key": privateKey,
	}, "", "  ")

	if err := os.WriteFile(keyPath, out, 0600); err != nil {
		log.Printf("[VAPID] Warning: could not save keys: %v", err)
	} else {
		log.Println("[VAPID] Generated and saved new keys to", keyPath)
	}
}

func marshalVAPIDKey(key *ecdsa.PrivateKey) (pubB64 string, privB64 string, err error) {
	ecdhKey, convErr := key.ECDH()
	if convErr != nil {
		return "", "", convErr
	}
	pubB64 = base64.RawURLEncoding.EncodeToString(ecdhKey.PublicKey().Bytes())
	privB64 = base64.RawURLEncoding.EncodeToString(key.D.Bytes())
	return pubB64, privB64, nil
}

func ensureVAPIDKeys() {
	vapidOnce.Do(loadOrGenerateVAPIDKeys)
}

func vapidSubject() string {
	for _, k := range []string{"EMAIL", "VAPID_SUBJECT"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return "mailto:admin@rotur.dev"
}

func getVAPIDKeys(c *gin.Context) {
	ensureVAPIDKeys()

	subject := os.Getenv("EMAIL")
	if subject == "" {
		subject = os.Getenv("VAPID_SUBJECT")
	}
	if subject == "" {
		subject = "mailto:admin@rotur.dev"
	}

	c.JSON(200, gin.H{
		"public_key": vapidPublicKey,
		"subject":    subject,
	})
}

func getNotifyMutex(username Username) *sync.RWMutex {
	notifyMutexes.Lock()
	defer notifyMutexes.Unlock()
	mu, ok := notifyUserMutexes[username]
	if !ok {
		mu = &sync.RWMutex{}
		notifyUserMutexes[username] = mu
	}
	return mu
}

func generateDeviceID(username Username, source string, fingerprint string) string {
	mac := hmac.New(sha256.New, []byte(os.Getenv("HMAC_KEY")))
	fmt.Fprintf(mac, "%s:%s:%s", username, source, fingerprint)
	return hex.EncodeToString(mac.Sum(nil))[:32]
}

func notifyEndpointsPath(username Username) string {
	return fmt.Sprintf("origin/(c) users/%s/application data/notify@rotur/endpoints.json", string(username))
}

func loadNotifyEndpoints(username Username) NotificationEndpointsFile {
	path := notifyEndpointsPath(username)
	dataStr := fs.ReadUserFileUnsafe(username, path)
	if dataStr == "" {
		return NotificationEndpointsFile{Endpoints: []NotificationEndpoint{}}
	}
	var file NotificationEndpointsFile
	if err := json.Unmarshal([]byte(dataStr), &file); err != nil {
		return NotificationEndpointsFile{Endpoints: []NotificationEndpoint{}}
	}
	if file.Endpoints == nil {
		file.Endpoints = []NotificationEndpoint{}
	}
	valid := make([]NotificationEndpoint, 0, len(file.Endpoints))
	for _, ep := range file.Endpoints {
		if ep.DeviceID != "" && ep.Endpoint != "" && ep.P256dh != "" && ep.Auth != "" && ep.Source != "" {
			valid = append(valid, ep)
		}
	}
	file.Endpoints = valid
	return file
}

func saveNotifyEndpoints(username Username, file NotificationEndpointsFile) error {
	data, err := json.Marshal(file)
	if err != nil {
		return err
	}
	path := notifyEndpointsPath(username)
	if err := fs.WriteUserFileUnsafe(username, path, string(data)); err != nil {
		return fmt.Errorf("failed to save endpoints: %w", err)
	}
	return nil
}

func getNotifyAllowed(u User) NotifyAllowedMap {
	raw := u.Get("sys.notify_allowed")
	if raw == nil {
		return NotifyAllowedMap{}
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return NotifyAllowedMap{}
	}
	var m NotifyAllowedMap
	if err := json.Unmarshal(b, &m); err != nil {
		return NotifyAllowedMap{}
	}
	return m
}

func setNotifyAllowed(u User, m NotifyAllowedMap) {
	b, _ := json.Marshal(m)
	var raw map[string]any
	json.Unmarshal(b, &raw)
	u.Set("sys.notify_allowed", raw)
}

func isNotifyAllowed(u User, source string, senderId UserId) bool {
	allowed := getNotifyAllowed(u)
	sourceMap, ok := allowed[source]
	if !ok {
		return false
	}
	_, exists := sourceMap[senderId]
	return exists
}

func addNotifyAllowed(u User, source string, senderId UserId) {
	allowed := getNotifyAllowed(u)
	if allowed == nil {
		allowed = NotifyAllowedMap{}
	}
	if allowed[source] == nil {
		allowed[source] = map[UserId]NotifyAllowedEntry{}
	}
	allowed[source][senderId] = NotifyAllowedEntry{Count: 0}
	setNotifyAllowed(u, allowed)
	go saveUsers()
}

func removeNotifyAllowed(u User, source string, senderId UserId) {
	allowed := getNotifyAllowed(u)
	if allowed == nil || allowed[source] == nil {
		return
	}
	delete(allowed[source], senderId)
	if len(allowed[source]) == 0 {
		delete(allowed, source)
	}
	setNotifyAllowed(u, allowed)
	go saveUsers()
}

func incrementNotifyCount(u User, source string, senderId UserId) {
	allowed := getNotifyAllowed(u)
	if allowed == nil || allowed[source] == nil {
		return
	}
	if entry, ok := allowed[source][senderId]; ok {
		entry.Count++
		allowed[source][senderId] = entry
		setNotifyAllowed(u, allowed)
		go saveUsers()
	}
}

func getNotifyLog(u User) []NotifyLogEntry {
	raw := u.Get("sys.notify_log")
	if raw == nil {
		return []NotifyLogEntry{}
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return []NotifyLogEntry{}
	}
	var entries []NotifyLogEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		return []NotifyLogEntry{}
	}
	return entries
}

func addNotifyLogEntry(u User, entry NotifyLogEntry) {
	entries := getNotifyLog(u)
	entries = append(entries, entry)
	if len(entries) > 200 {
		entries = entries[len(entries)-200:]
	}
	b, _ := json.Marshal(entries)
	var raw []any
	json.Unmarshal(b, &raw)
	u.Set("sys.notify_log", raw)
	go saveUsers()
}

func registerForNotifications(c *gin.Context) {
	user := c.MustGet("user").(*User)
	username := user.GetUsername()

	var req struct {
		Endpoint    string `json:"endpoint" binding:"required"`
		P256dh      string `json:"p256dh" binding:"required"`
		Auth        string `json:"auth" binding:"required"`
		Source      string `json:"source" binding:"required"`
		Fingerprint string `json:"fingerprint" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "endpoint, p256dh, auth, source, and fingerprint are required"})
		return
	}
	if len(req.Source) > 64 {
		c.JSON(400, gin.H{"error": "source too long (max 64 chars)"})
		return
	}
	if len(req.Endpoint) > 2048 {
		c.JSON(400, gin.H{"error": "endpoint URL too long (max 2048 chars)"})
		return
	}
	if !strings.HasPrefix(req.Endpoint, "http://") && !strings.HasPrefix(req.Endpoint, "https://") {
		c.JSON(400, gin.H{"error": "endpoint must be a valid HTTP(S) URL"})
		return
	}
	p256dhBytes, err := base64.RawURLEncoding.DecodeString(req.P256dh)
	if err != nil || len(p256dhBytes) != 65 || p256dhBytes[0] != 0x04 {
		c.JSON(400, gin.H{"error": "invalid p256dh key"})
		return
	}

	authBytes, err := base64.RawURLEncoding.DecodeString(req.Auth)
	if err != nil || len(authBytes) != 16 {
		c.JSON(400, gin.H{"error": "invalid auth key"})
		return
	}

	log.Printf("[Notify] Registered endpoint key lengths → p256dh: %d, auth: %d",
		len(p256dhBytes), len(authBytes))

	deviceID := generateDeviceID(username, req.Source, req.Fingerprint)
	mu := getNotifyMutex(username)
	mu.Lock()
	defer mu.Unlock()

	fs.mu.Lock()
	defer fs.mu.Unlock()

	file := loadNotifyEndpoints(username)
	found := false
	for i, ep := range file.Endpoints {
		if ep.DeviceID == deviceID && ep.Source == req.Source {
			file.Endpoints[i].Endpoint = req.Endpoint
			file.Endpoints[i].P256dh = req.P256dh
			file.Endpoints[i].Auth = req.Auth
			file.Endpoints[i].CreatedAt = time.Now().UnixMilli()
			found = true
			break
		}
	}
	if !found {
		if len(file.Endpoints) >= 20 {
			c.JSON(400, gin.H{"error": "maximum number of notification endpoints reached (20)"})
			return
		}
		file.Endpoints = append(file.Endpoints, NotificationEndpoint{
			DeviceID:  deviceID,
			Endpoint:  req.Endpoint,
			P256dh:    req.P256dh,
			Auth:      req.Auth,
			Source:    req.Source,
			CreatedAt: time.Now().UnixMilli(),
		})
	}

	if err := saveNotifyEndpoints(username, file); err != nil {
		log.Printf("[Notify] Error saving endpoints for %s: %v", username, err)
		c.JSON(500, gin.H{"error": "failed to save notification endpoint"})
		return
	}

	c.JSON(200, gin.H{
		"message":   "endpoint registered",
		"device_id": deviceID,
		"source":    req.Source,
		"updated":   found,
	})
}

func checkNotifyRegistration(c *gin.Context) {
	user := c.MustGet("user").(*User)
	username := user.GetUsername()
	source := c.Query("source")
	fingerprint := c.Query("fingerprint")
	if source == "" || fingerprint == "" {
		c.JSON(400, gin.H{"error": "source and fingerprint query params are required"})
		return
	}

	deviceID := generateDeviceID(username, source, fingerprint)
	mu := getNotifyMutex(username)
	mu.RLock()
	defer mu.RUnlock()

	fs.mu.RLock()
	defer fs.mu.RUnlock()

	file := loadNotifyEndpoints(username)
	for _, ep := range file.Endpoints {
		if ep.DeviceID == deviceID && ep.Source == source {
			c.JSON(200, gin.H{
				"registered": true,
				"device_id":  ep.DeviceID,
				"endpoint":   ep.Endpoint,
				"source":     ep.Source,
				"created_at": ep.CreatedAt,
			})
			return
		}
	}

	c.JSON(200, gin.H{"registered": false, "device_id": deviceID})
}

func getNotifyEndpoints(c *gin.Context) {
	user := c.MustGet("user").(*User)
	username := user.GetUsername()

	mu := getNotifyMutex(username)
	mu.RLock()
	defer mu.RUnlock()

	fs.mu.RLock()
	defer fs.mu.RUnlock()

	file := loadNotifyEndpoints(username)
	c.JSON(200, gin.H{
		"endpoints": file.Endpoints,
		"count":     len(file.Endpoints),
	})
}

func deleteNotifyDevice(c *gin.Context) {
	user := c.MustGet("user").(*User)
	username := user.GetUsername()
	deviceID := c.Param("device_id")
	if deviceID == "" {
		c.JSON(400, gin.H{"error": "device_id is required"})
		return
	}

	mu := getNotifyMutex(username)
	mu.Lock()
	defer mu.Unlock()

	fs.mu.Lock()
	defer fs.mu.Unlock()

	file := loadNotifyEndpoints(username)
	newEndpoints := make([]NotificationEndpoint, 0, len(file.Endpoints))
	removed := false
	for _, ep := range file.Endpoints {
		if ep.DeviceID == deviceID {
			removed = true
			continue
		}
		newEndpoints = append(newEndpoints, ep)
	}
	if !removed {
		c.JSON(404, gin.H{"error": "device not found"})
		return
	}
	file.Endpoints = newEndpoints
	if err := saveNotifyEndpoints(username, file); err != nil {
		log.Printf("[Notify] Error saving endpoints for %s: %v", username, err)
		c.JSON(500, gin.H{"error": "failed to save notification endpoints"})
		return
	}

	c.JSON(200, gin.H{"message": "device removed", "device_id": deviceID})
}

type SenderInfo struct {
	Username Username `json:"username"`
	Count    int64    `json:"count"`
}

func sortSenders(senders []SenderInfo) []SenderInfo {
	sort.Slice(senders, func(i, j int) bool {
		return senders[i].Username < senders[j].Username
	})
	return senders
}

func getNotifyAllowedSenders(c *gin.Context) {
	user := c.MustGet("user").(*User)
	allowed := getNotifyAllowed(*user)

	type SourceAllowed struct {
		Senders []SenderInfo `json:"senders"`
	}

	result := map[string]SourceAllowed{}
	for source, senderMap := range allowed {
		senders := make([]SenderInfo, 0, len(senderMap))
		for userId, entry := range senderMap {
			senders = append(senders, SenderInfo{
				Username: userId.User().GetUsername(),
				Count:    entry.Count,
			})
		}
		result[source] = SourceAllowed{
			Senders: sortSenders(senders),
		}
	}

	c.JSON(200, result)
}

func addNotifyAllowedSender(c *gin.Context) {
	user := c.MustGet("user").(*User)
	targetUsername := Username(c.Param("username"))
	if targetUsername == "" {
		c.JSON(400, gin.H{"error": "username is required"})
		return
	}

	var req struct {
		Source string `json:"source" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "source is required"})
		return
	}

	targetId := targetUsername.Id()
	if targetId == "" {
		c.JSON(404, gin.H{"error": "user not found"})
		return
	}

	mu := getUserMutex(user.GetUsername())
	mu.Lock()
	defer mu.Unlock()

	addNotifyAllowed(*user, req.Source, targetId)
	c.JSON(200, gin.H{
		"message":  "sender allowed",
		"username": targetUsername,
		"source":   req.Source,
	})
}

func removeNotifyAllowedSender(c *gin.Context) {
	user := c.MustGet("user").(*User)
	targetUsername := Username(c.Param("username"))
	source := c.Query("source")
	if targetUsername == "" || source == "" {
		c.JSON(400, gin.H{"error": "username and source are required"})
		return
	}

	targetId := targetUsername.Id()
	if targetId == "" {
		c.JSON(404, gin.H{"error": "user not found"})
		return
	}

	mu := getUserMutex(user.GetUsername())
	mu.Lock()
	defer mu.Unlock()

	removeNotifyAllowed(*user, source, targetId)
	c.JSON(200, gin.H{
		"message":  "sender removed",
		"username": targetUsername,
		"source":   source,
	})
}

func getNotifyLogHandler(c *gin.Context) {
	user := c.MustGet("user").(*User)
	entries := getNotifyLog(*user)
	c.JSON(200, gin.H{
		"log":   entries,
		"count": len(entries),
	})
}

type NotificationRequest struct {
	Source string         `json:"source" binding:"required"`
	Title  string         `json:"title"`
	Body   string         `json:"body"`
	Data   map[string]any `json:"data"`
	Users  []string       `json:"users"`
}

func sendPushNotificationToUser(target User, sender User, req NotificationRequest) (int, map[string]any) {
	if len(req.Title) > 256 {
		req.Title = req.Title[:256]
	}
	if len(req.Body) > 1024 {
		req.Body = req.Body[:1024]
	}

	username := target.GetUsername()
	mu := getNotifyMutex(username)
	mu.RLock()
	fs.mu.RLock()
	endpointsFile := loadNotifyEndpoints(username)
	fs.mu.RUnlock()
	mu.RUnlock()

	var targetEndpoints []NotificationEndpoint
	for _, ep := range endpointsFile.Endpoints {
		if ep.Source == req.Source {
			targetEndpoints = append(targetEndpoints, ep)
		}
	}
	if len(targetEndpoints) == 0 {
		return 404, gin.H{
			"error":  "target user has no registered endpoints for this source",
			"source": req.Source,
		}
	}

	payload := map[string]any{
		"type":    "notification",
		"from":    sender.GetUsername(),
		"title":   req.Title,
		"body":    req.Body,
		"sent_at": time.Now().UnixMilli(),
	}
	payloadBytes, _ := json.Marshal(payload)

	for _, ep := range targetEndpoints {
		go sendWebPush(username, ep, payloadBytes)
	}

	targetMu := getUserMutex(username)
	targetMu.Lock()
	incrementNotifyCount(target, req.Source, sender.GetId())
	addNotifyLogEntry(target, NotifyLogEntry{
		From:   sender.GetUsername(),
		Source: req.Source,
		Title:  req.Title,
		Body:   req.Body,
		At:     time.Now().UnixMilli(),
	})
	targetMu.Unlock()

	addUserEvent(target.GetId(), "notification", map[string]any{
		"from":   string(sender.GetUsername()),
		"source": req.Source,
		"title":  req.Title,
	})

	return 200, gin.H{
		"success": true,
		"message": "notification sent",
		"title":   req.Title,
		"body":    req.Body,
		"data":    req.Data,
	}
}

func getNotifiableUsers(c *gin.Context) {
	ensureVAPIDKeys()

	sender := c.MustGet("user").(*User)
	targetSource := c.Param("source")
	if targetSource == "" {
		c.JSON(400, gin.H{"error": "source is required"})
		return
	}

	usersMutex.RLock()
	defer usersMutex.RUnlock()
	var ableUsers []User
	for _, user := range users {
		if !isNotifyAllowed(user, targetSource, sender.GetId()) {
			continue
		}
		ableUsers = append(ableUsers, map[string]any{
			"username": user.GetUsername(),
			"id":       user.GetId(),
		})
	}

	c.JSON(200, gin.H{
		"success": true,
		"source":  targetSource,
		"users":   ableUsers,
	})
}

func notifyUser(c *gin.Context) {
	ensureVAPIDKeys()

	sender := c.MustGet("user").(*User)
	targetUsername := Username(c.Param("username"))
	if targetUsername == "" {
		c.JSON(400, gin.H{"error": "username is required"})
		return
	}
	var req NotificationRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "source is required"})
		return
	}
	if len(req.Title) > 256 {
		c.JSON(400, gin.H{"error": "title too long (max 256 chars)"})
		return
	}
	if len(req.Body) > 1024 {
		c.JSON(400, gin.H{"error": "body too long (max 1024 chars)"})
		return
	}

	target, err := getAccountByUsername(targetUsername)
	if err != nil {
		c.JSON(404, gin.H{"error": "target user not found"})
		return
	}

	senderId := sender.GetId()
	if senderId == "" {
		c.JSON(403, gin.H{"error": "invalid sender"})
		return
	}

	if !isNotifyAllowed(target, req.Source, senderId) {
		c.JSON(403, gin.H{"error": "you are not allowed to send notifications to this user from " + req.Source})
		return
	}

	if target.HasBlocked(senderId) {
		c.JSON(403, gin.H{"error": "cannot send notification to this user"})
		return
	}

	c.JSON(sendPushNotificationToUser(target, *sender, req))
}

// take a list of usernames and send a notification to each of them
// return a list of usernames that worked for
func notifyManyUsers(c *gin.Context) {
	ensureVAPIDKeys()

	sender := c.MustGet("user").(*User)
	var req NotificationRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "source is required"})
		return
	}
	if len(req.Title) > 256 {
		c.JSON(400, gin.H{"error": "title too long (max 256 chars)"})
		return
	}
	if len(req.Body) > 1024 {
		c.JSON(400, gin.H{"error": "body too long (max 1024 chars)"})
		return
	}

	senderId := sender.GetId()
	if senderId == "" {
		c.JSON(403, gin.H{"error": "invalid sender"})
		return
	}

	var users []User
	for _, username := range req.Users {
		username = strings.ToLower(username)
		target, err := getAccountByUsername(username)
		if err != nil {
			continue
		}
		if target.HasBlocked(senderId) {
			continue
		}
		if !isNotifyAllowed(target, req.Source, senderId) {
			continue
		}
		users = append(users, target)
	}

	results := make([]map[string]any, len(users))
	for i, user := range users {
		code, result := sendPushNotificationToUser(user, *sender, req)
		results[i] = map[string]any{
			"username": user.GetUsername(),
			"code":     code,
			"result":   result,
		}
	}

	c.JSON(200, gin.H{
		"success": true,
		"results": results,
	})

}

func pruneNotifyDevice(username Username, deviceID string) {
	mu := getNotifyMutex(username)
	mu.Lock()
	defer mu.Unlock()

	fs.mu.Lock()
	defer fs.mu.Unlock()

	file := loadNotifyEndpoints(username)
	newEndpoints := make([]NotificationEndpoint, 0, len(file.Endpoints))
	for _, ep := range file.Endpoints {
		if ep.DeviceID != deviceID {
			newEndpoints = append(newEndpoints, ep)
		}
	}
	file.Endpoints = newEndpoints
	if err := saveNotifyEndpoints(username, file); err != nil {
		log.Printf("[Notify] Error pruning device %s for %s: %v", deviceID, username, err)
	}
}

func sendWebPush(username Username, ep NotificationEndpoint, payload []byte) {
	sub := &webpush.Subscription{
		Endpoint: ep.Endpoint,
		Keys: webpush.Keys{
			P256dh: ep.P256dh,
			Auth:   ep.Auth,
		},
	}

	options := &webpush.Options{
		Subscriber:      vapidSubject(),
		VAPIDPublicKey:  vapidPublicKey,
		VAPIDPrivateKey: vapidPrivateKey,
		TTL:             30,
		RecordSize:      3000,
	}

	for true {
		resp, err := webpush.SendNotification(payload, sub, options)
		if err != nil {
			log.Printf("[Notify] Web Push error for device %s: %v", ep.DeviceID, err)
			return
		}

		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNotFound {
			pruneNotifyDevice(username, ep.DeviceID)
			break
		} else if resp.StatusCode == http.StatusRequestEntityTooLarge {
			if options.RecordSize <= 1000 {
				log.Printf("[Notify] Push service returned 413 for device %s (too large), giving up", ep.DeviceID)
				break
			}
			options.RecordSize = options.RecordSize / 2
			log.Printf("[Notify] Push service returned 413 for device %s (too large), retrying with smaller record size: %d", ep.DeviceID, options.RecordSize)
		} else if resp.StatusCode >= 400 {
			bodyString := string(bodyBytes)
			log.Printf("[Notify] Push service returned %s (%d) for device %s", bodyString, resp.StatusCode, ep.DeviceID)
			pruneNotifyDevice(username, ep.DeviceID)
			break
		} else {
			break
		}
	}
}
