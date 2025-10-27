package main

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// System Management Functions
//
// The system loading functionality has been made modular to provide:
// 1. Centralized system management through storage.go
// 2. Thread-safe access with proper mutex locking
// 3. Helper functions for validation and retrieval
// 4. Administrative endpoints for system management
// 5. Integration with user registration process
//
// Usage:
// - Systems are automatically loaded on startup
// - Use isValidSystem(username) to check if a username is valid
// - Use validateSystemUsername(username) for detailed validation
// - Use getSystems endpoint to retrieve all systems
// - Use reloadSystems endpoint (admin only) to reload from file

// File operations
func loadUsers() {
	usersMutex.Lock()
	defer usersMutex.Unlock()

	if _, err := os.Stat(USERS_FILE_PATH); os.IsNotExist(err) {
		users = make([]User, 0)
		return
	}

	data, err := os.ReadFile(USERS_FILE_PATH)
	if err != nil {
		log.Printf("Error reading users file (keeping in-memory users): %v", err)
		return
	}
	if len(data) == 0 {
		log.Printf("users.json read returned 0 bytes; preserving existing in-memory users (%d)", len(users))
		return
	}

	var loaded []User
	if err := json.Unmarshal(data, &loaded); err != nil {
		log.Printf("Error unmarshaling users (keeping existing %d users): %v", len(users), err)
		return
	}
	users = loaded
}

func atomicWrite(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

var usersSaveMutex sync.Mutex

func saveUsers() {
	usersSaveMutex.Lock()
	defer usersSaveMutex.Unlock()
	// Take a deep snapshot under read lock to avoid concurrent map iteration during JSON marshal
	usersMutex.RLock()
	snapshot := make([]User, len(users))
	for i := range users {
		snapshot[i] = copyUser(users[i])
	}
	usersMutex.RUnlock()

	data, err := json.Marshal(snapshot)
	if err != nil {
		log.Printf("Error marshaling users: %v", err)
		return
	}

	if err := atomicWrite(USERS_FILE_PATH, data, 0644); err != nil {
		log.Printf("Error saving users (atomic write failed): %v", err)
	}
}

func copyUser(u User) User {
	if u == nil {
		return nil
	}
	out := make(User, len(u))
	for k, v := range u {
		out[k] = deepCopyValue(v)
	}
	return out
}

func deepCopyValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, vv := range t {
			m[k] = deepCopyValue(vv)
		}
		return m
	case []map[string]any:
		s := make([]map[string]any, len(t))
		for i := range t {
			// deep copy each map element
			mm := make(map[string]any, len(t[i]))
			for k, vv := range t[i] {
				mm[k] = deepCopyValue(vv)
			}
			s[i] = mm
		}
		return s
	case []any:
		s := make([]any, len(t))
		for i := range t {
			s[i] = deepCopyValue(t[i])
		}
		return s
	case []string:
		s := make([]string, len(t))
		copy(s, t)
		return s
	case []int:
		s := make([]int, len(t))
		copy(s, t)
		return s
	case []float64:
		s := make([]float64, len(t))
		copy(s, t)
		return s
	case []bool:
		s := make([]bool, len(t))
		copy(s, t)
		return s
	default:
		return v
	}
}

func loadFollowers() {
	followersMutex.Lock()
	defer followersMutex.Unlock()

	if _, err := os.Stat(FOLLOWERS_FILE_PATH); os.IsNotExist(err) {
		followersData = make(map[string]FollowerData)
		return
	}

	data, err := os.ReadFile(FOLLOWERS_FILE_PATH)
	if err != nil {
		log.Printf("Error reading followers file: %v", err)
		followersData = make(map[string]FollowerData)
		return
	}

	var tempData map[string]FollowerData
	if err := json.Unmarshal(data, &tempData); err != nil {
		log.Printf("Error unmarshaling followers: %v", err)
		followersData = make(map[string]FollowerData)
		return
	}

	followersData = make(map[string]FollowerData)
	for k, v := range tempData {
		followers := make([]string, len(v.Followers))
		for i, follower := range v.Followers {
			followers[i] = strings.ToLower(follower)
		}
		followersData[strings.ToLower(k)] = FollowerData{Followers: followers}
	}
}

func saveFollowers() {
	followersMutex.RLock()
	data, err := json.MarshalIndent(followersData, "", "  ")
	followersMutex.RUnlock()

	if err != nil {
		log.Printf("Error marshaling followers: %v", err)
		return
	}

	if err := os.WriteFile(FOLLOWERS_FILE_PATH, data, 0644); err != nil {
		log.Printf("Error saving followers: %v", err)
	}
}

func loadPosts() {
	postsMutex.Lock()
	defer postsMutex.Unlock()

	if _, err := os.Stat(LOCAL_POSTS_PATH); os.IsNotExist(err) {
		posts = make([]Post, 0)
		return
	}

	data, err := os.ReadFile(LOCAL_POSTS_PATH)
	if err != nil {
		log.Printf("Error reading posts file: %v", err)
		posts = make([]Post, 0)
		return
	}

	if err := json.Unmarshal(data, &posts); err != nil {
		log.Printf("Error unmarshaling posts: %v", err)
		posts = make([]Post, 0)
	}
}

func savePosts() {
	postsMutex.RLock()
	data, err := json.MarshalIndent(posts, "", "  ")
	postsMutex.RUnlock()

	if err != nil {
		log.Printf("Error marshaling posts: %v", err)
		return
	}

	if err := os.WriteFile(LOCAL_POSTS_PATH, data, 0644); err != nil {
		log.Printf("Error saving posts: %v", err)
	}
}

func loadItems() {
	itemsMutex.Lock()
	defer itemsMutex.Unlock()

	if _, err := os.Stat(ITEMS_FILE_PATH); os.IsNotExist(err) {
		items = make([]Item, 0)
		return
	}

	data, err := os.ReadFile(ITEMS_FILE_PATH)
	if err != nil {
		log.Printf("Error reading items file: %v", err)
		items = make([]Item, 0)
		return
	}

	if err := json.Unmarshal(data, &items); err != nil {
		log.Printf("Error unmarshaling items: %v", err)
		items = make([]Item, 0)
	}
}

func saveItems() {
	itemsMutex.RLock()
	data, err := json.MarshalIndent(items, "", "  ")
	itemsMutex.RUnlock()

	if err != nil {
		log.Printf("Error marshaling items: %v", err)
		return
	}

	if err := os.WriteFile(ITEMS_FILE_PATH, data, 0644); err != nil {
		log.Printf("Error saving items: %v", err)
	}
}

func loadKeys() {
	keysMutex.Lock()
	defer keysMutex.Unlock()

	if _, err := os.Stat(KEYS_FILE_PATH); os.IsNotExist(err) {
		keys = make([]Key, 0)
		return
	}

	data, err := os.ReadFile(KEYS_FILE_PATH)
	if err != nil {
		log.Printf("Error reading keys file: %v", err)
		keys = make([]Key, 0)
		return
	}

	if err := json.Unmarshal(data, &keys); err != nil {
		log.Printf("Error unmarshaling keys: %v", err)
		keys = make([]Key, 0)
	}
}

func saveKeys() {
	keysMutex.RLock()
	data, err := json.MarshalIndent(keys, "", "  ")
	keysMutex.RUnlock()

	if err != nil {
		log.Printf("Error marshaling keys: %v", err)
		return
	}

	if err := os.WriteFile(KEYS_FILE_PATH, data, 0644); err != nil {
		log.Printf("Error saving keys: %v", err)
	}
}

func loadSystems() {
	systemsMutex.Lock()
	defer systemsMutex.Unlock()

	if _, err := os.Stat(SYSTEMS_FILE_PATH); os.IsNotExist(err) {
		systems = make(map[string]System)
		return
	}

	data, err := os.ReadFile(SYSTEMS_FILE_PATH)
	if err != nil {
		log.Printf("Error reading systems file: %v", err)
		systems = make(map[string]System)
		return
	}

	if err := json.Unmarshal(data, &systems); err != nil {
		log.Printf("Error unmarshaling systems: %v", err)
		systems = make(map[string]System)
	}
}

func saveSystems() {
	systemsMutex.Lock()
	defer systemsMutex.Unlock()
	data, err := json.MarshalIndent(systems, "", "  ")
	if err != nil {
		log.Printf("Error marshaling systems: %v", err)
		return
	}

	if err := os.WriteFile(SYSTEMS_FILE_PATH, data, 0644); err != nil {
		log.Printf("Error saving systems: %v", err)
	}
}

func loadEventsHistory() {
	eventsHistoryMutex.Lock()
	defer eventsHistoryMutex.Unlock()

	if _, err := os.Stat(EVENTS_HISTORY_PATH); os.IsNotExist(err) {
		eventsHistory = make(map[string][]Event)
		return
	}

	data, err := os.ReadFile(EVENTS_HISTORY_PATH)
	if err != nil {
		log.Printf("Error reading events history file: %v", err)
		eventsHistory = make(map[string][]Event)
		return
	}

	if err := json.Unmarshal(data, &eventsHistory); err != nil {
		log.Printf("Error unmarshaling events history: %v", err)
		eventsHistory = make(map[string][]Event)
	}
}

func saveEventsHistory() {
	eventsHistoryMutex.RLock()
	data, err := json.MarshalIndent(eventsHistory, "", "  ")
	eventsHistoryMutex.RUnlock()

	if err != nil {
		log.Printf("Error marshaling events history: %v", err)
		return
	}

	if err := os.WriteFile(EVENTS_HISTORY_PATH, data, 0644); err != nil {
		log.Printf("Error saving events history: %v", err)
	}
}

func watchUsersFile() {
	var lastMtime time.Time
	if stat, err := os.Stat(USERS_FILE_PATH); err == nil {
		lastMtime = stat.ModTime()
	}

	for {
		time.Sleep(500 * time.Millisecond)
		if stat, err := os.Stat(USERS_FILE_PATH); err == nil {
			if stat.ModTime().After(lastMtime) {
				time.Sleep(500 * time.Millisecond)
				loadUsers()
				lastMtime = stat.ModTime()
			}
		}
	}
}
