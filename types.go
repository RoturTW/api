package main

import (
	"encoding/json"
	"strconv"
	"sync"
	"time"
)

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
			usersMutex.Lock()
			defer usersMutex.Unlock()
			return
		}
	default:
		usersMutex.Lock()
		defer usersMutex.Unlock()
		return
	}
	usersMutex.Lock()
	defer usersMutex.Unlock()
	u.Set("sys.currency", roundVal(fval))
}

func (u User) Get(key string) any {
	value, ok := u[key]
	if ok {
		return value
	}
	return nil
}

func (u User) GetInt(key string) int {
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

func (u User) Set(key string, value any) {
	u[key] = value
	if key != "key" && key != "password" {
		go broadcastUserUpdate(u.GetUsername(), key, value)
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

// System represents a system definition
type System struct {
	Name        string      `json:"name"`
	Owner       SystemOwner `json:"owner"`
	Wallpaper   string      `json:"wallpaper"`
	Designation string      `json:"designation"`
}

// SystemOwner represents the owner of a system
type SystemOwner struct {
	Name      string `json:"name"`
	DiscordID int64  `json:"discord_id"`
}

type Transaction struct {
	Type      string  `json:"type"`
	From      string  `json:"from"`
	To        string  `json:"to"`
	Amount    float64 `json:"amount"`
	Note      string  `json:"note"`
	Timestamp int64   `json:"timestamp"`
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
	rateLimitMutex   = sync.RWMutex{}

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
