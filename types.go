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

type Username string

func (u Username) String() string {
	return string(u)
}

func (u Username) ToLower() Username {
	return Username(strings.ToLower(string(u)))
}

func (u Username) Id() UserId {
	return getIdByUsername(u)
}

type UserId string

func (u UserId) String() string {
	return string(u)
}

func (u UserId) User() User {
	return idToUser[u]
}

type Marriage struct {
	Status    string `json:"status"`
	Partner   UserId `json:"partner"`
	Timestamp int64  `json:"timestamp"`
	Proposer  UserId `json:"proposer"`
}

type MarriageNet struct {
	Status    string   `json:"status"`
	Partner   Username `json:"partner"`
	Timestamp int64    `json:"timestamp"`
	Proposer  Username `json:"proposer"`
}

func (m Marriage) ToNet() MarriageNet {
	return MarriageNet{
		Status:    m.Status,
		Partner:   m.Partner.User().GetUsername(),
		Timestamp: m.Timestamp,
		Proposer:  m.Proposer.User().GetUsername(),
	}
}

var userMutexesLock sync.Mutex
var userMutexes = map[Username]*sync.Mutex{}

func getUserMutex(username Username) *sync.Mutex {
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
		Bio_Length:              200,
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
func (u User) GetUsername() Username {
	if username, ok := u["username"]; ok {
		if str, ok := username.(string); ok {
			return Username(str)
		}
		return ""
	}
	return ""
}

func (u User) GetTheme() map[string]any {
	if theme, ok := u["theme"]; ok {
		if m, ok := theme.(map[string]any); ok {
			return m
		}
	}
	return map[string]any{}
}

func (u User) GetId() UserId {
	if id, ok := u["sys.id"]; ok {
		if str, ok := id.(string); ok {
			return UserId(str)
		}
		return ""
	}
	// fallback to username
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

func (u User) SetBlocked(blocked []UserId) {
	u.Set("sys.blocked", blocked)
}

func (u User) GetBlocked() []UserId {
	blocked := getStringSlice(u, "sys.blocked")
	out := make([]UserId, len(blocked))
	for i, b := range blocked {
		out[i] = UserId(b)
	}
	return out
}

func (u User) GetBlockedUsers() []Username {
	blocked := u.GetBlocked()
	out := make([]Username, len(blocked))
	for i, b := range blocked {
		out[i] = getUserById(b).GetUsername()
	}
	return out
}

func (u User) AddBlocked(userId UserId) {
	if u.HasBlocked(userId) {
		return
	}
	blocked := u.GetBlocked()
	mu := getUserMutex(u.GetUsername())
	mu.Lock()
	blocked = append(blocked, userId)
	mu.Unlock()
	u.SetBlocked(blocked)
}

func (u User) RemoveBlocked(userId UserId) {
	if !u.HasBlocked(userId) {
		return
	}
	blocked := u.GetBlocked()
	mu := getUserMutex(u.GetUsername())
	mu.Lock()
	newBlocked := make([]UserId, 0, len(blocked)-1)
	for _, b := range blocked {
		if b != userId {
			newBlocked = append(newBlocked, b)
		}
	}
	mu.Unlock()
	u.SetBlocked(newBlocked)
}

func (u User) HasBlocked(userId UserId) bool {
	blocked := u.GetBlocked()
	for _, b := range blocked {
		if b == userId {
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

func (u *User) GetMarriage() Marriage {
	marriage := u.Get("sys.marriage")
	if marriage == nil {
		return Marriage{
			Status:    "single",
			Partner:   UserId(""),
			Timestamp: 0,
			Proposer:  UserId(""),
		}
	}
	switch v := marriage.(type) {
	case map[string]any:
		return Marriage{
			Status:    getStringOrDefault(v["status"], "single"),
			Partner:   UserId(getStringOrDefault(v["partner"], "")),
			Timestamp: int64(getIntOrDefault(v["timestamp"], 0)),
			Proposer:  UserId(getStringOrDefault(v["proposer"], "")),
		}
	}
	// fallback to empty marriage
	return Marriage{
		Status:    "single",
		Partner:   UserId(""),
		Timestamp: 0,
		Proposer:  UserId(""),
	}
}

func (u *User) SetMarriage(marriage Marriage) {
	u.Set("sys.marriage", map[string]any{
		"status":    marriage.Status,
		"partner":   marriage.Partner.String(),
		"timestamp": marriage.Timestamp,
		"proposer":  marriage.Proposer.String(),
	})
}

func (u User) SetFriends(friends []UserId) {
	u.Set("sys.friends", friends)
}

func (u User) SetRequests(requests []UserId) {
	u.Set("sys.requests", requests)
}

func (u User) AddRequest(username Username) bool {
	if u.HasRequest(username) {
		return false
	}
	requests := u.GetRequests()
	mu := getUserMutex(u.GetUsername())
	userId := username.Id()
	mu.Lock()
	requests = append(requests, userId)
	mu.Unlock()
	u.SetRequests(requests)
	return true
}

func (u User) RemoveRequest(username Username) bool {
	if !u.HasRequest(username) {
		return false
	}
	requests := u.GetRequests()
	mu := getUserMutex(u.GetUsername())
	userId := username.Id()
	mu.Lock()
	requestIds := make([]UserId, 0, len(requests)-1)
	for _, r := range requestIds {
		if r != userId {
			requestIds = append(requestIds, r)
		}
	}
	mu.Unlock()
	u.SetRequests(requestIds)
	return true
}

func (u User) HasRequest(username Username) bool {
	requests := u.GetRequests()
	for _, r := range requests {
		if strings.EqualFold(string(r), string(username)) {
			return true
		}
	}
	return false
}

func (u User) AddFriend(username Username) bool {
	friends := u.GetFriends()
	if u.IsFriend(username) {
		return false
	}
	mu := getUserMutex(u.GetUsername())
	userId := username.Id()
	mu.Lock()
	friends = append(friends, userId)
	mu.Unlock()
	u.SetFriends(friends)

	return true
}

func (u User) RemoveFriend(username Username) bool {
	friends := u.GetFriends()
	if !u.IsFriend(username) {
		return false
	}
	mu := getUserMutex(u.GetUsername())
	userId := username.Id()
	mu.Lock()
	newFriends := make([]UserId, 0, len(friends)-1)
	for _, f := range friends {
		if f != userId {
			newFriends = append(newFriends, f)
		}
	}
	mu.Unlock()
	u.SetFriends(newFriends)

	return true
}

func (u User) IsFriend(username Username) bool {
	friends := u.GetFriends()
	for _, f := range friends {
		if strings.EqualFold(string(f), string(username)) {
			return true
		}
	}
	return false
}

func getIdByUsername(username Username) UserId {
	val, ok := usernameToId[username.ToLower()]
	if ok {
		return val
	}
	return UserId("")
}

func getUserById(id UserId) User {
	return idToUser[id]
}

func (u User) GetFriends() []UserId {
	friends := getStringSlice(u, "sys.friends")
	out := make([]UserId, len(friends))
	for i, f := range friends {
		out[i] = UserId(f)
	}
	return out
}

func (u User) GetFriendUsers() []Username {
	friends := getStringSlice(u, "sys.friends")
	out := make([]Username, len(friends))
	for i, f := range friends {
		out[i] = getUserById(UserId(f)).GetUsername()
	}
	return out
}

func (u User) GetRequests() []UserId {
	requests := getStringSlice(u, "sys.requests")
	out := make([]UserId, len(requests))
	for i, r := range requests {
		out[i] = UserId(r)
	}
	return out
}

func (u User) GetRequestedUsers() []Username {
	requests := getStringSlice(u, "sys.requests")
	out := make([]Username, len(requests))
	for i, r := range requests {
		out[i] = getUserById(UserId(r)).GetUsername()
	}
	return out
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

func (u User) GetNotes() map[UserId]string {
	notes := u.Get("sys.notes")
	if notes == nil {
		return map[UserId]string{}
	}
	m, ok := notes.(map[UserId]any)
	if !ok {
		return map[UserId]string{}
	}
	out := make(map[UserId]string)
	for k, v := range m {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

func (u User) SetNote(username Username, note string) error {
	if len(note) > 300 {
		return fmt.Errorf("note content is too long")
	}
	notes := u.GetNotes()
	mu := getUserMutex(u.GetUsername())
	userId := username.Id()
	mu.Lock()
	notes[userId] = note
	mu.Unlock()
	u.Set("sys.notes", notes)
	return nil
}

func (u User) RemoveNote(username Username) {
	notes := u.GetNotes()
	mu := getUserMutex(u.GetUsername())
	userId := username.Id()
	mu.Lock()
	delete(notes, userId)
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
	if strings.EqualFold(string(username), "mist") {
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
	Followers []UserId `json:"followers"`
}

// Post represents a social media post
type Post struct {
	ID           string   `json:"id"`
	Content      string   `json:"content"`
	User         UserId   `json:"user"`
	Timestamp    int64    `json:"timestamp"`
	Attachment   *string  `json:"attachment,omitempty"`
	ProfileOnly  bool     `json:"profile_only,omitempty"`
	OS           *string  `json:"os,omitempty"`
	Replies      []Reply  `json:"replies,omitempty"`
	Likes        []UserId `json:"likes,omitempty"`
	Pinned       bool     `json:"pinned,omitempty"`
	IsRepost     bool     `json:"is_repost,omitempty"`
	OriginalPost *Post    `json:"original_post,omitempty"`
}

type NetPost struct {
	ID           string     `json:"id"`
	Content      string     `json:"content"`
	User         Username   `json:"user"`
	Timestamp    int64      `json:"timestamp"`
	Attachment   *string    `json:"attachment,omitempty"`
	ProfileOnly  bool       `json:"profile_only,omitempty"`
	OS           *string    `json:"os,omitempty"`
	Replies      []NetReply `json:"replies,omitempty"`
	Likes        []Username `json:"likes,omitempty"`
	Pinned       bool       `json:"pinned,omitempty"`
	IsRepost     bool       `json:"is_repost,omitempty"`
	OriginalPost *Post      `json:"original_post,omitempty"`
}

func (p Post) ToNet() NetPost {
	replies := make([]NetReply, 0)
	likes := make([]Username, 0)
	for _, reply := range p.Replies {
		replies = append(replies, reply.ToNet())
	}
	for _, like := range p.Likes {
		likes = append(likes, like.User().GetUsername())
	}
	return NetPost{
		ID:           p.ID,
		Content:      p.Content,
		User:         p.User.User().GetUsername(),
		Attachment:   p.Attachment,
		ProfileOnly:  p.ProfileOnly,
		OS:           p.OS,
		Replies:      replies,
		Likes:        likes,
		Pinned:       p.Pinned,
		IsRepost:     p.IsRepost,
		OriginalPost: p.OriginalPost,
	}
}

// Reply represents a reply to a post
type Reply struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	User      UserId `json:"user"`
	Timestamp int64  `json:"timestamp"`
}

type NetReply struct {
	ID        string   `json:"id"`
	Content   string   `json:"content"`
	User      Username `json:"user"`
	Timestamp int64    `json:"timestamp"`
}

func (r Reply) ToNet() NetReply {
	return NetReply{
		ID:        r.ID,
		Content:   r.Content,
		User:      r.User.User().GetUsername(),
		Timestamp: r.Timestamp,
	}
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
			return v.Name.String(), nil
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
	Name      Username `json:"name"`
	DiscordID string   `json:"discord_id"`
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
		User    UserId `json:"user"`
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
	Author          UserId            `json:"author"`
	Owner           UserId            `json:"owner"`
	PrivateData     any               `json:"private_data,omitempty"`
	Created         int64             `json:"created"`
	TransferHistory []TransferHistory `json:"transfer_history"`
	TotalIncome     int               `json:"total_income"`
}

func (i Item) ToNet() NetItem {
	transferHistory := make([]NetTransferHistory, 0, len(i.TransferHistory))
	for _, history := range i.TransferHistory {
		transferHistory = append(transferHistory, history.ToNet())
	}
	return NetItem{
		Name:            i.Name,
		Description:     i.Description,
		Price:           i.Price,
		Selling:         i.Selling,
		Author:          i.Author.User().GetUsername(),
		Owner:           i.Owner.User().GetUsername(),
		PrivateData:     i.PrivateData,
		Created:         i.Created,
		TransferHistory: transferHistory,
		TotalIncome:     i.TotalIncome,
	}
}

type NetItem struct {
	Name            string               `json:"name"`
	Description     string               `json:"description"`
	Price           int                  `json:"price"`
	Selling         bool                 `json:"selling"`
	Author          Username             `json:"author"`
	Owner           Username             `json:"owner"`
	PrivateData     any                  `json:"private_data,omitempty"`
	Created         int64                `json:"created"`
	TransferHistory []NetTransferHistory `json:"transfer_history"`
	TotalIncome     int                  `json:"total_income"`
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
	From      *UserId `json:"from"`
	To        UserId  `json:"to"`
	Timestamp int64   `json:"timestamp"`
	Type      string  `json:"type"`
	Price     *int    `json:"price,omitempty"`
}

func (t TransferHistory) ToNet() NetTransferHistory {
	var from *Username

	username := t.From.User().GetUsername()
	if username != "" {
		from = &username
	}
	return NetTransferHistory{
		From:      from,
		To:        t.To.User().GetUsername(),
		Timestamp: t.Timestamp,
		Type:      t.Type,
		Price:     t.Price,
	}
}

type NetTransferHistory struct {
	From      *Username `json:"from"`
	To        Username  `json:"to"`
	Timestamp int64     `json:"timestamp"`
	Type      string    `json:"type"`
	Price     *int      `json:"price,omitempty"`
}

// Key represents an access key
type Key struct {
	Key          string                 `json:"key"`
	Creator      UserId                 `json:"creator"`
	Users        map[UserId]KeyUserData `json:"users"`
	Name         *string                `json:"name"`
	Price        int                    `json:"price"`
	Data         *string                `json:"data"`
	Subscription *Subscription          `json:"subscription,omitempty"`
	Type         string                 `json:"type"`
	TotalIncome  int                    `json:"total_income,omitempty"`
	Webhook      *string                `json:"webhook,omitempty"`
}

func (k *Key) ToNet() NetKey {
	users := make(map[Username]KeyUserData)
	for k, v := range k.Users {
		users[k.User().GetUsername()] = v
	}
	return NetKey{
		Key:          k.Key,
		Name:         k.Name,
		Price:        k.Price,
		Type:         k.Type,
		TotalIncome:  k.TotalIncome,
		Webhook:      k.Webhook,
		Subscription: k.Subscription,
		Users:        users,
		Creator:      k.Creator.User().GetUsername(),
	}
}

type NetKey struct {
	Key          string                   `json:"key"`
	Name         *string                  `json:"name"`
	Price        int                      `json:"price"`
	Type         string                   `json:"type"`
	TotalIncome  int                      `json:"total_income,omitempty"`
	Webhook      *string                  `json:"webhook,omitempty"`
	Subscription *Subscription            `json:"subscription,omitempty"`
	Users        map[Username]KeyUserData `json:"users"`
	Creator      Username                 `json:"creator"`
	Data         *string                  `json:"data,omitempty"`
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
	Time        int64 `json:"time"`
	Price       int   `json:"price,omitempty"`
	NextBilling any   `json:"next_billing,omitempty"`
	CancelAt    any   `json:"cancel_at,omitempty"` // unix ms; when reached, user should be removed from key
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

	users        []User
	usernameToId map[Username]UserId
	idToUser     map[UserId]User
	usersMutex   sync.RWMutex

	followersData  map[UserId]FollowerData
	followersMutex sync.RWMutex

	posts      []Post
	postsMutex sync.RWMutex

	items      []Item
	itemsMutex sync.RWMutex

	keys      []Key
	keysMutex sync.RWMutex

	systems      map[string]System
	systemsMutex sync.RWMutex

	eventsHistory      map[UserId][]Event
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
		User         UserId   `json:"user"`
		Attachment   *string  `json:"attachment,omitempty"`
		ProfileOnly  bool     `json:"profile_only,omitempty"`
		OS           *string  `json:"os,omitempty"`
		Replies      []Reply  `json:"replies,omitempty"`
		Likes        []UserId `json:"likes,omitempty"`
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
