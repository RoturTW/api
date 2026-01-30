package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

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
	usernameToIdInner := make(map[Username]UserId, len(loaded))
	idToUserInner := make(map[UserId]User, len(loaded))
	for _, u := range loaded {
		id := u.GetId()
		usernameToIdInner[u.GetUsername().ToLower()] = id
		idToUserInner[id] = u
	}
	fmt.Println("Loaded", len(loaded), "users")
	usernameToId = usernameToIdInner
	idToUser = idToUserInner
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
		followersData = make(map[UserId]FollowerData)
		return
	}

	data, err := os.ReadFile(FOLLOWERS_FILE_PATH)
	if err != nil {
		log.Printf("Error reading followers file: %v", err)
		followersData = make(map[UserId]FollowerData)
		return
	}

	var tempData map[UserId]FollowerData
	if err := json.Unmarshal(data, &tempData); err != nil {
		log.Printf("Error unmarshaling followers: %v", err)
		followersData = make(map[UserId]FollowerData)
		return
	}

	followersData = make(map[UserId]FollowerData)
	for k, v := range tempData {
		followers := make([]UserId, len(v.Followers))
		copy(followers, v.Followers)
		followersData[k] = FollowerData{
			Followers: followers,
			Username:  getUserById(k).GetUsername(),
			UserId:    k,
		}
	}

	log.Printf("Loaded %d followers", len(followersData))
}

func saveFollowers() {
	followersMutex.RLock()
	defer followersMutex.RUnlock()
	saveJsonFile(FOLLOWERS_FILE_PATH, followersData)
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

	log.Printf("Loaded %d posts", len(posts))
}

func savePosts() {
	postsMutex.RLock()
	defer postsMutex.RUnlock()
	saveJsonFile(LOCAL_POSTS_PATH, posts)
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

	log.Printf("Loaded %d items", len(items))
}

func saveItems() {
	itemsMutex.RLock()
	defer itemsMutex.RUnlock()
	saveJsonFile(ITEMS_FILE_PATH, items)
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

	log.Printf("Loaded %d keys", len(keys))
}

func saveKeys() {
	keysMutex.RLock()
	defer keysMutex.RUnlock()
	saveJsonFile(KEYS_FILE_PATH, keys)
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

	log.Printf("Loaded %d systems", len(systems))
}

func saveSystems() {
	systemsMutex.Lock()
	defer systemsMutex.Unlock()
	saveJsonFile(SYSTEMS_FILE_PATH, systems)
}

func loadEventsHistory() {
	eventsHistoryMutex.Lock()
	defer eventsHistoryMutex.Unlock()

	if _, err := os.Stat(EVENTS_HISTORY_PATH); os.IsNotExist(err) {
		eventsHistory = make(map[UserId][]Event)
		return
	}

	data, err := os.ReadFile(EVENTS_HISTORY_PATH)
	if err != nil {
		log.Printf("Error reading events history file: %v", err)
		eventsHistory = make(map[UserId][]Event)
		return
	}

	if err := json.Unmarshal(data, &eventsHistory); err != nil {
		log.Printf("Error unmarshaling events history: %v", err)
		eventsHistory = make(map[UserId][]Event)
	}

	log.Printf("Loaded %d events history", len(eventsHistory))
}

func saveEventsHistory() {
	eventsHistoryMutex.RLock()
	defer eventsHistoryMutex.RUnlock()
	saveJsonFile(EVENTS_HISTORY_PATH, eventsHistory)
}

func saveJsonFile(path string, v any) bool {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		log.Printf("Error marshaling JSON: %v", err)
		return false
	}

	if err := atomicWrite(path, data, 0644); err != nil {
		log.Printf("Error saving JSON file: %v", err)
		return false
	}
	return true
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
