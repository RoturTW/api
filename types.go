package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

type subscription struct {
	Active       bool   `json:"active"`
	Tier         string `json:"tier"`
	Next_billing int64  `json:"next_billing"`
}

type sub_benefits struct {
	Max_Keys                int  `json:"max_keys"`
	Max_Login_History       int  `json:"max_login_history"`
	Max_Transaction_History int  `json:"max_transaction_history"`
	Max_Rmails              int  `json:"max_rmails"`
	FileSystem_Size         int  `json:"file_system_size"`
	Bio_Length              int  `json:"bio_length"`
	Has_Animated_Pfp        bool `json:"animated_pfp"`
	Has_Animated_Banner     bool `json:"animated_banner"`
	Has_Free_Banner_Uploads bool `json:"free_banner_uploads"`
	Has_Bio_templating      bool `json:"bio_templating"`
	Has_Profile_notes       bool `json:"profile_notes"`
	Daily_Credit_Multipler  int  `json:"daily_credit_multiplier"`
}

var userMutexesLock sync.Mutex
var userMutexes = map[string]*sync.Mutex{}

func getUserMutex(username string) *sync.Mutex {
	userMutexesLock.Lock()
	defer userMutexesLock.Unlock()
	mu, ok := userMutexes[username]
	if !ok {
		mu = &sync.Mutex{}
		userMutexes[username] = mu
	}
	return mu
}

var subs_benefits = map[string]sub_benefits{
	"free":  tierFree(),
	"lite":  tierLite(),
	"plus":  tierPlus(),
	"drive": tierDrive(),
	"pro":   tierPro(),
	"max":   tierMax(),
}

func tierFree() sub_benefits {
	benefits := sub_benefits{
		Max_Keys:                5,
		Max_Login_History:       10,
		Max_Rmails:              100,
		FileSystem_Size:         5_000_000,
		Bio_Length:              300,
		Max_Transaction_History: 20,
		Daily_Credit_Multipler:  1,
	}
	return benefits
}

func tierLite() sub_benefits {
	b := tierFree()
	b.FileSystem_Size = 10_000_000
	b.Has_Bio_templating = true
	return b
}

func tierPlus() sub_benefits {
	b := tierLite()
	b.FileSystem_Size = 15_000_000
	b.Has_Profile_notes = true
	return b
}

func tierDrive() sub_benefits {
	b := tierPlus()
	b.Max_Keys = 20
	b.Max_Login_History = 100
	b.Max_Rmails = 1000
	b.FileSystem_Size = 15_000_000
	b.Bio_Length = 500
	b.Has_Animated_Pfp = true
	b.Max_Transaction_History = 100
	b.Daily_Credit_Multipler = 2
	return b
}

func tierPro() sub_benefits {
	b := tierDrive()
	b.Max_Keys = 50
	b.Max_Rmails = 100_000
	b.FileSystem_Size = 1_000_000_000
	b.Bio_Length = 1000
	b.Has_Animated_Banner = true
	b.Has_Free_Banner_Uploads = true
	b.Max_Transaction_History = 500
	b.Daily_Credit_Multipler = 3
	return b
}

func tierMax() sub_benefits {
	b := tierPro()
	b.Max_Keys = 500
	b.FileSystem_Size = 10_000_000_000
	return b
}

// User represents a user with dynamic fields
type User map[string]any

// Helper methods for User
func (u User) GetUsername() string {
	if username, ok := u["username"]; ok {
		if str, ok := username.(string); ok {
			return str
		}
		return ""
	}
	return ""
}

func (u User) GetKey() string {
	if key, ok := u["key"]; ok {
		if str, ok := key.(string); ok {
			return str
		}
	}
	return ""
}

func (u User) GetPassword() string {
	if password, ok := u["password"]; ok {
		if str, ok := password.(string); ok {
			return str
		}
	}
	return ""
}

func (u User) GetSystem() string {
	return getStringOrDefault(u.Get("system"), "rotur")
}

func (u User) GetEmail() string {
	return getStringOrEmpty(u.Get("email"))
}

func (u User) SetBlocked(blocked []string) {
	u.Set("sys.blocked", blocked)
}

func (u User) GetBlocked() []string {
	return getStringSlice(u, "sys.blocked")
}

func (u User) AddBlocked(username string) {
	if u.HasBlocked(username) {
		return
	}
	blocked := u.GetBlocked()
	mu := getUserMutex(u.GetUsername())
	mu.Lock()
	blocked = append(blocked, username)
	mu.Unlock()
	u.SetBlocked(blocked)
}

func (u User) RemoveBlocked(username string) {
	if !u.HasBlocked(username) {
		return
	}
	blocked := u.GetBlocked()
	mu := getUserMutex(u.GetUsername())
	mu.Lock()
	newBlocked := make([]string, 0, len(blocked)-1)
	for _, b := range blocked {
		if b != username {
			newBlocked = append(newBlocked, b)
		}
	}
	mu.Unlock()
	u.SetBlocked(newBlocked)
}

func (u User) HasBlocked(username string) bool {
	username = strings.ToLower(username)
	blocked := u.GetBlocked()
	for _, b := range blocked {
		if strings.ToLower(b) == username {
			return true
		}
	}
	return false
}

func (u User) IsBanned() bool {
	banned := u.Get("sys.banned")
	return banned == true || banned == "true"
}

func (u User) IsPrivate() bool {
	private := u.Get("private")
	return private == true
}

func (u User) SetFriends(friends []string) {
	u.Set("sys.friends", friends)
}

func (u User) SetRequests(requests []string) {
	u.Set("sys.requests", requests)
}

func (u User) AddRequest(username string) bool {
	if u.HasRequest(username) {
		return false
	}
	requests := u.GetRequests()
	mu := getUserMutex(u.GetUsername())
	mu.Lock()
	requests = append(requests, username)
	mu.Unlock()
	u.SetRequests(requests)
	return true
}

func (u User) RemoveRequest(username string) bool {
	if !u.HasRequest(username) {
		return false
	}
	requests := u.GetRequests()
	mu := getUserMutex(u.GetUsername())
	mu.Lock()
	requests = make([]string, 0, len(requests)-1)
	for _, r := range requests {
		if r != username {
			requests = append(requests, r)
		}
	}
	mu.Unlock()
	u.SetRequests(requests)
	return true
}

func (u User) HasRequest(username string) bool {
	requests := u.GetRequests()
	for _, r := range requests {
		if strings.EqualFold(r, username) {
			return true
		}
	}
	return false
}

func (u User) AddFriend(username string) bool {
	friends := u.GetFriends()
	if u.IsFriend(username) {
		return false
	}
	mu := getUserMutex(u.GetUsername())
	mu.Lock()
	friends = append(friends, username)
	mu.Unlock()
	u.SetFriends(friends)

	return true
}

func (u User) RemoveFriend(username string) bool {
	friends := u.GetFriends()
	if !u.IsFriend(username) {
		return false
	}
	mu := getUserMutex(u.GetUsername())
	mu.Lock()
	newFriends := make([]string, 0, len(friends)-1)
	for _, f := range friends {
		if f != username {
			newFriends = append(newFriends, f)
		}
	}
	mu.Unlock()
	u.SetFriends(newFriends)

	return true
}

func (u User) IsFriend(username string) bool {
	friends := u.GetFriends()
	for _, f := range friends {
		if strings.EqualFold(f, username) {
			return true
		}
	}
	return false
}

func (u User) GetFriends() []string {
	return getStringSlice(u, "sys.friends")
}

func (u User) GetRequests() []string {
	return getStringSlice(u, "sys.requests")
}

func (u User) GetCreated() int64 {
	if created, ok := u["created"]; ok {
		switch v := created.(type) {
		case int64:
			return v
		case float64:
			return int64(v)
		}
	}
	return 0
}

func (u User) GetNotes() map[string]string {
	notes := u.Get("sys.notes")
	if notes == nil {
		return map[string]string{}
	}
	m, ok := notes.(map[string]any)
	if !ok {
		return map[string]string{}
	}
	out := make(map[string]string)
	for k, v := range m {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

func (u User) SetNote(username string, note string) error {
	if len(note) > 300 {
		return fmt.Errorf("note content is too long")
	}
	notes := u.GetNotes()
	mu := getUserMutex(u.GetUsername())
	mu.Lock()
	notes[username] = note
	mu.Unlock()
	u.Set("sys.notes", notes)
	return nil
}

func (u User) RemoveNote(username string) {
	notes := u.GetNotes()
	mu := getUserMutex(u.GetUsername())
	mu.Lock()
	delete(notes, username)
	mu.Unlock()
	u.Set("sys.notes", notes)
}

func (u User) GetCredits() float64 {
	if credits, ok := u["sys.currency"]; ok {
		switch v := credits.(type) {
		case float64:
			return v
		case int64:
			return float64(v)
		case int:
			return float64(v)
		case string:
			if floatValue, err := strconv.ParseFloat(v, 64); err == nil {
				return floatValue
			}
		}
	}
	return 0
}

func (u User) SetBalance(balance any) {
	var fval float64
	switch v := balance.(type) {
	case float64:
		fval = v
	case float32:
		fval = float64(v)
	case int:
		fval = float64(v)
	case int64:
		fval = float64(v)
	case string:
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			fval = parsed
		} else {
			return
		}
	default:
		return
	}
	u.Set("sys.currency", roundVal(fval))
}

func (u User) GetLogins() []Login {
	raw := u.Get("sys.logins")
	if raw == nil {
		return nil
	}

	switch v := raw.(type) {
	case []Login:
		return v
	case []any:
		out := make([]Login, 0, len(v))
		for _, item := range v {
			switch l := item.(type) {
			case Login:
				out = append(out, l)
			case map[string]any:
				var login Login
				if b, err := json.Marshal(l); err == nil {
					_ = json.Unmarshal(b, &login)
					out = append(out, login)
				}
			}
		}
		return out
	default:
		return nil
	}
}

func (u User) GetSubscription() subscription {
	username := u.GetUsername()
	if strings.EqualFold(username, "mist") {
		// keep me as the sigma
		return subscription{
			Active:       true,
			Tier:         "Max",
			Next_billing: time.Now().UnixMilli() + (24 * 60 * 60 * 1000),
		}
	}
	usub := u.Get("sys.subscription")
	val := subscription{
		Active:       false,
		Tier:         "Free",
		Next_billing: 0,
	}
	checkExternalBilling := func() (ok bool) {
		next := getKeyNextBilling(username, "4f229157f0c40f5a98cbf28efd39cfe8")
		if next == 0 {
			return false
		}
		val.Active = true
		val.Tier = "Lite"
		val.Next_billing = next
		return true
	}
	if usub == nil {
		_ = checkExternalBilling()
		return val
	}
	sub, ok := usub.(map[string]any)
	if !ok {
		_ = checkExternalBilling()
		return val
	}
	val.Active = sub["active"] == true
	val.Tier = getStringOrDefault(sub["tier"], "Free")
	val.Next_billing = int64(getIntOrDefault(sub["next_billing"], 0))

	if val.Next_billing == 0 {
		val.Active = false
		val.Tier = "Free"
		return val
	}

	if val.Next_billing < time.Now().UnixMilli() && val.Active {
		if checkExternalBilling() {
			return val
		}
		go sendDiscordWebhook([]map[string]any{
			{
				"title": "Lost Subscription",
				"description": fmt.Sprintf("**User:** %s\n**Tier:** %s\n**Next Billing:** %s",
					username, val.Tier, time.Unix(val.Next_billing/1000, 0).Format(time.RFC3339)),
				"color":     0x57cdac,
				"timestamp": time.Now().Format(time.RFC3339),
			},
		})
		val.Active = false
		val.Next_billing = 0
		val.Tier = "Free"
		u.SetSubscription(val)
		go saveUsers()
	}
	return val
}

func (u User) GetSubscriptionBenefits() sub_benefits {
	tier := u.GetSubscription().Tier
	return subs_benefits[strings.ToLower(tier)]
}

func (u User) GetBlockedIps() []string {
	return getStringSlice(u, "blocked_ips")
}

// social links to display on the user's profile (max 3)
func (u User) GetSocialLinks() []string {
	return getStringSlice(u, "sys.social_links")
}

func (u User) SetSocialLinks(links []string) {
	u.Set("sys.social_links", links)
}

func (u User) SetSubscription(sub subscription) {
	u.Set("sys.subscription", map[string]any{
		"active":       sub.Active,
		"tier":         sub.Tier,
		"next_billing": sub.Next_billing,
	})
	u.Set("max_size", u.GetMaxSize())
}

func (u User) GetMaxSize() string {
	amt := strconv.Itoa(u.GetSubscriptionBenefits().FileSystem_Size)
	u.Set("max_size", amt)
	return amt
}

func (u User) GetTransactions() []map[string]any {
	raw := u.Get("sys.transactions")

	switch v := raw.(type) {
	case []map[string]any:
		return v
	case []any:
		txs := make([]map[string]any, 0)
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			txs = append(txs, m)
		}
		return txs
	default:
		return []map[string]any{}
	}
}

func (u User) addTransaction(tx map[string]any) {
	txs := u.GetTransactions()
	benefits := u.GetSubscriptionBenefits()

	tx["time"] = time.Now().UnixMilli()

	noteData, ok := tx["note"]
	if !ok {
		tx["note"] = ""
	}
	noteStr := strings.TrimSpace(getStringOrEmpty(noteData))
	if len(noteStr) > 50 {
		noteStr = noteStr[:50]
	}
	tx["note"] = noteStr

	txs = append([]map[string]any{tx}, txs...)
	if len(txs) > benefits.Max_Transaction_History {
		txs = txs[:benefits.Max_Transaction_History]
	}
	u.Set("sys.transactions", txs)
}

func (u User) SetLogins(logins []Login) {
	u.Set("sys.logins", logins)
}

func (u User) Has(key string) bool {
	mu := getUserMutex(u.GetUsername())
	mu.Lock()
	defer mu.Unlock()
	_, ok := u[key]
	return ok
}

func (u User) Get(key string) any {
	mu := getUserMutex(u.GetUsername())
	mu.Lock()
	defer mu.Unlock()
	value, ok := u[key]
	if ok {
		return value
	}
	return nil
}

func (u User) GetString(key string) string {
	mu := getUserMutex(u.GetUsername())
	mu.Lock()
	defer mu.Unlock()
	value, ok := u[key]
	if ok {
		switch v := value.(type) {
		case string:
			return v
		case int:
			return strconv.Itoa(v)
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
	}
	return ""
}

func (u User) GetInt(key string) int {
	mu := getUserMutex(u.GetUsername())
	mu.Lock()
	defer mu.Unlock()
	value, ok := u[key]
	if ok {
		switch v := value.(type) {
		case int:
			return v
		case float64:
			return int(v)
		case int64:
			return int(v)
		case string:
			if intValue, err := strconv.Atoi(v); err == nil {
				return intValue
			}
		}
	}
	return 0
}

func (u User) DelKey(key string) error {
	mu := getUserMutex(u.GetUsername())
	mu.Lock()
	defer mu.Unlock()
	delete(u, key)
	go notify("sys.delete", map[string]any{
		"username": u.GetUsername(),
		"key":      key,
	})
	return nil
}

func (u User) Set(key string, value any) {
	mu := getUserMutex(u.GetUsername())
	mu.Lock()
	defer mu.Unlock()
	oldValue := u[key]
	if reflect.DeepEqual(oldValue, value) {
		return
	}
	u[key] = value
	valueCopy := deepCopyValue(value)
	if key != "key" && key != "password" {
		username := u.GetUsername()
		go broadcastUserUpdate(username, key, valueCopy)
	}
}

// FollowerData represents follower information
type FollowerData struct {
	Followers []string `json:"followers"`
}

// Post represents a social media post
type Post struct {
	ID           string   `json:"id"`
	Content      string   `json:"content"`
	User         string   `json:"user"`
	Timestamp    int64    `json:"timestamp"`
	Attachment   *string  `json:"attachment,omitempty"`
	ProfileOnly  bool     `json:"profile_only,omitempty"`
	OS           *string  `json:"os,omitempty"`
	Replies      []Reply  `json:"replies,omitempty"`
	Likes        []string `json:"likes,omitempty"`
	Pinned       bool     `json:"pinned,omitempty"`
	IsRepost     bool     `json:"is_repost,omitempty"`
	OriginalPost *Post    `json:"original_post,omitempty"`
}

// Reply represents a reply to a post
type Reply struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	User      string `json:"user"`
	Timestamp int64  `json:"timestamp"`
}

type Badge struct {
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	Description string `json:"description"`
}

// System represents a system definition
type System struct {
	Name        string      `json:"name"`
	Owner       SystemOwner `json:"owner"`
	Wallpaper   string      `json:"wallpaper"`
	Designation string      `json:"designation"`
	Icon        string      `json:"icon"`
}

func (s *System) Set(key string, value any) (string, error) {
	switch key {
	case "name":
		if v, ok := value.(string); ok {
			renameSystem(s.Name, v)
			return v, nil
		} else {
			return "", fmt.Errorf("invalid name value: %v", value)
		}
	case "owner":
		if v, ok := value.(SystemOwner); ok {
			s.Owner = v
			return v.Name, nil
		} else {
			return "", fmt.Errorf("invalid owner value: %v", value)
		}
	case "wallpaper":
		if v, ok := value.(string); ok {
			s.Wallpaper = v
			return v, nil
		} else {
			return "", fmt.Errorf("invalid wallpaper value: %v", value)
		}
	case "designation":
		if v, ok := value.(string); ok {
			s.Designation = v
			return v, nil
		} else {
			return "", fmt.Errorf("invalid designation value: %v", value)
		}
	}
	return "", fmt.Errorf("invalid system key: %s", key)
}

// SystemOwner represents the owner of a system
type SystemOwner struct {
	Name      string `json:"name"`
	DiscordID string `json:"discord_id"`
}

type Transaction struct {
	Type      string  `json:"type"`
	From      string  `json:"from"`
	To        string  `json:"to"`
	Amount    float64 `json:"amount"`
	Note      string  `json:"note"`
	Timestamp int64   `json:"timestamp"`
	New_total float64 `json:"new_total"`
}

// UnmarshalJSON custom unmarshaler to handle timestamp as string or number
func (r *Reply) UnmarshalJSON(data []byte) error {
	// First try to unmarshal into a map to handle flexible timestamp type
	var rawData map[string]any
	if err := json.Unmarshal(data, &rawData); err != nil {
		return err
	}

	// Handle timestamp field that can be string or number
	var timestamp int64
	if timestampVal, exists := rawData["timestamp"]; exists {
		switch v := timestampVal.(type) {
		case string:
			var err error
			timestamp, err = strconv.ParseInt(v, 10, 64)
			if err != nil {
				return err
			}
		case float64:
			timestamp = int64(v)
		case int64:
			timestamp = v
		}
	}

	// Define a temporary struct without timestamp to unmarshal the rest
	type TempReply struct {
		ID      string `json:"id"`
		Content string `json:"content"`
		User    string `json:"user"`
	}

	var temp TempReply
	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	// Copy all fields to the actual Reply
	r.ID = temp.ID
	r.Content = temp.Content
	r.User = temp.User
	r.Timestamp = timestamp

	return nil
}

// Item represents a marketplace item
type Item struct {
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	Price           int               `json:"price"`
	Selling         bool              `json:"selling"`
	Author          string            `json:"author"`
	Owner           string            `json:"owner"`
	PrivateData     any               `json:"private_data,omitempty"`
	Created         float64           `json:"created"`
	TransferHistory []TransferHistory `json:"transfer_history"`
	TotalIncome     int               `json:"total_income"`
}

type Login struct {
	Origin      string `json:"origin"`
	UserAgent   string `json:"userAgent"`
	IP_hmac     string `json:"ip_hmac"`
	Country     string `json:"country"`
	Timestamp   int64  `json:"timestamp"`
	Device_type string `json:"device_type"`
	Message     string `json:"message"`
}

// TransferHistory represents item transfer history
type TransferHistory struct {
	From      *string `json:"from"`
	To        string  `json:"to"`
	Timestamp float64 `json:"timestamp"`
	Type      string  `json:"type"`
	Price     *int    `json:"price,omitempty"`
}

// Key represents an access key
type Key struct {
	Key          string                 `json:"key"`
	Creator      string                 `json:"creator"`
	Users        map[string]KeyUserData `json:"users"`
	Name         *string                `json:"name"`
	Price        int                    `json:"price"`
	Data         *string                `json:"data"`
	Subscription *Subscription          `json:"subscription,omitempty"`
	Type         string                 `json:"type"`
	TotalIncome  int                    `json:"total_income,omitempty"`
	Webhook      *string                `json:"webhook,omitempty"`
}

func (k *Key) setKey(key string, value any) {
	switch key {
	case "name":
		if v, ok := value.(string); ok {
			k.Name = &v
		}
	case "price":
		if v, ok := value.(int); ok {
			k.Price = v
		} else if v, ok := value.(float64); ok {
			k.Price = int(v)
		}
	case "data":
		if v, ok := value.(string); ok {
			k.Data = &v
		}
	case "subscription":
		if v, ok := value.(Subscription); ok {
			k.Subscription = &v
		}
	case "type":
		if v, ok := value.(string); ok {
			k.Type = v
		}
	case "webhook":
		if v, ok := value.(string); ok {
			k.Webhook = &v
		}
	}
}

func (k *Key) ToPublic() map[string]any {
	return map[string]any{
		"key":   k.Key,
		"name":  k.Name,
		"price": k.Price,
		"type":  k.Type,
	}
}

// KeyUserData represents user data for a key
type KeyUserData struct {
	Time        float64 `json:"time"`
	Price       int     `json:"price,omitempty"`
	NextBilling any     `json:"next_billing,omitempty"`
}

// Subscription represents subscription information
type Subscription struct {
	Active      bool   `json:"active"`
	Frequency   int    `json:"frequency"`
	Period      string `json:"period"`
	NextBilling any    `json:"next_billing"`
}

// Event represents a user event/notification
type Event struct {
	Type      string         `json:"type"`
	Data      map[string]any `json:"data"`
	Timestamp int64          `json:"timestamp"`
	ID        string         `json:"id"`
}

// RateLimit represents rate limiting data
type RateLimit struct {
	Count   int
	ResetAt int64
}

// RateLimitConfig represents rate limiting configuration
type RateLimitConfig struct {
	Count  int
	Period int
}

// Global variables
var (
	startTime = time.Now()

	users      []User
	usersMutex sync.RWMutex

	followersData  map[string]FollowerData
	followersMutex sync.RWMutex

	posts      []Post
	postsMutex sync.RWMutex

	items      []Item
	itemsMutex sync.RWMutex

	keys      []Key
	keysMutex sync.RWMutex

	systems      map[string]System
	systemsMutex sync.RWMutex

	eventsHistory      map[string][]Event
	eventsHistoryMutex sync.RWMutex

	rateLimitStorage = make(map[string]*RateLimit)
	rateLimitMutex   sync.RWMutex

	keyOwnershipCacheMutex sync.RWMutex

	derogatoryTerms = make([]string, 0)
)

// UnmarshalJSON custom unmarshaler to handle timestamp as string or number
func (p *Post) UnmarshalJSON(data []byte) error {
	// First try to unmarshal into a map to handle flexible timestamp type
	var rawData map[string]any
	if err := json.Unmarshal(data, &rawData); err != nil {
		return err
	}

	// Handle timestamp field that can be string or number
	var timestamp int64
	if timestampVal, exists := rawData["timestamp"]; exists {
		switch v := timestampVal.(type) {
		case string:
			var err error
			timestamp, err = strconv.ParseInt(v, 10, 64)
			if err != nil {
				return err
			}
		case float64:
			timestamp = int64(v)
		case int64:
			timestamp = v
		}
	}

	// Define a temporary struct without timestamp to unmarshal the rest
	type TempPost struct {
		ID           string   `json:"id"`
		Content      string   `json:"content"`
		User         string   `json:"user"`
		Attachment   *string  `json:"attachment,omitempty"`
		ProfileOnly  bool     `json:"profile_only,omitempty"`
		OS           *string  `json:"os,omitempty"`
		Replies      []Reply  `json:"replies,omitempty"`
		Likes        []string `json:"likes,omitempty"`
		Pinned       bool     `json:"pinned,omitempty"`
		IsRepost     bool     `json:"is_repost,omitempty"`
		OriginalPost *Post    `json:"original_post,omitempty"`
	}

	var temp TempPost
	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	// Copy all fields to the actual Post
	p.ID = temp.ID
	p.Content = temp.Content
	p.User = temp.User
	p.Timestamp = timestamp
	p.Attachment = temp.Attachment
	p.ProfileOnly = temp.ProfileOnly
	p.OS = temp.OS
	p.Replies = temp.Replies
	p.Likes = temp.Likes
	p.Pinned = temp.Pinned
	p.IsRepost = temp.IsRepost
	p.OriginalPost = temp.OriginalPost

	return nil
}
