package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type TokenPermission string

const (
	PermDeleteAccount    TokenPermission = "account:delete"
	PermManageProfile    TokenPermission = "account:profile"
	PermManageSettings   TokenPermission = "account:settings"
	PermViewProfile      TokenPermission = "account:view"
	PermViewCredits      TokenPermission = "credits:view"
	PermManageCredits    TokenPermission = "credits:manage"
	PermTransferCredits  TokenPermission = "credits:transfer"
	PermClaimDaily       TokenPermission = "credits:daily"
	PermViewFriends      TokenPermission = "friends:view"
	PermManageFriends    TokenPermission = "friends:manage"
	PermSendFriendReq    TokenPermission = "friends:request"
	PermAcceptFriend     TokenPermission = "friends:accept"
	PermRemoveFriend     TokenPermission = "friends:remove"
	PermViewPosts        TokenPermission = "posts:view"
	PermCreatePost       TokenPermission = "posts:create"
	PermDeletePost       TokenPermission = "posts:delete"
	PermManagePosts      TokenPermission = "posts:manage"
	PermLikePost         TokenPermission = "posts:like"
	PermReplyPost        TokenPermission = "posts:reply"
	PermRepost           TokenPermission = "posts:repost"
	PermViewFollowing    TokenPermission = "following:view"
	PermFollow           TokenPermission = "following:follow"
	PermUnfollow         TokenPermission = "following:unfollow"
	PermViewFiles        TokenPermission = "files:view"
	PermManageFiles      TokenPermission = "files:manage"
	PermDeleteFiles      TokenPermission = "files:delete"
	PermViewKeys         TokenPermission = "keys:view"
	PermManageKeys       TokenPermission = "keys:manage"
	PermViewGroups       TokenPermission = "groups:view"
	PermManageGroups     TokenPermission = "groups:manage"
	PermJoinGroup        TokenPermission = "groups:join"
	PermLeaveGroup       TokenPermission = "groups:leave"
	PermViewNotifications TokenPermission = "notifications:view"
	PermSendNotifications TokenPermission = "notifications:send"
	PermViewGifts        TokenPermission = "gifts:view"
	PermCreateGift       TokenPermission = "gifts:create"
	PermClaimGift        TokenPermission = "gifts:claim"
	PermCancelGift       TokenPermission = "gifts:cancel"
	PermViewItems        TokenPermission = "items:view"
	PermBuyItems         TokenPermission = "items:buy"
	PermSellItems        TokenPermission = "items:sell"
	PermManageItems      TokenPermission = "items:manage"
	PermGenerateValidator TokenPermission = "validators:generate"
	PermViewBlocked      TokenPermission = "blocked:view"
	PermManageBlocked    TokenPermission = "blocked:manage"
	PermManageTokens     TokenPermission = "tokens:manage"
)

func AllPermissions() []TokenPermission {
	return []TokenPermission{
		PermDeleteAccount,
		PermManageProfile,
		PermManageSettings,
		PermViewProfile,
		PermViewCredits,
		PermManageCredits,
		PermTransferCredits,
		PermClaimDaily,
		PermViewFriends,
		PermManageFriends,
		PermSendFriendReq,
		PermAcceptFriend,
		PermRemoveFriend,
		PermViewPosts,
		PermCreatePost,
		PermDeletePost,
		PermManagePosts,
		PermLikePost,
		PermReplyPost,
		PermRepost,
		PermViewFollowing,
		PermFollow,
		PermUnfollow,
		PermViewFiles,
		PermManageFiles,
		PermDeleteFiles,
		PermViewKeys,
		PermManageKeys,
		PermViewGroups,
		PermManageGroups,
		PermJoinGroup,
		PermLeaveGroup,
		PermViewNotifications,
		PermSendNotifications,
		PermViewGifts,
		PermCreateGift,
		PermClaimGift,
		PermCancelGift,
		PermViewItems,
		PermBuyItems,
		PermSellItems,
		PermManageItems,
		PermGenerateValidator,
		PermViewBlocked,
		PermManageBlocked,
		PermManageTokens,
	}
}

type PermissionGroup struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Permissions []TokenPermission `json:"permissions"`
}

func PermissionGroups() []PermissionGroup {
	return []PermissionGroup{
		{
			Name:        "read_only",
			Description: "Read-only access to your profile, posts, friends, and followers",
			Permissions: []TokenPermission{
				PermViewProfile, PermViewCredits, PermViewFriends,
				PermViewPosts, PermViewFollowing, PermViewFiles,
				PermViewKeys, PermViewGroups, PermViewNotifications,
				PermViewGifts, PermViewItems, PermViewBlocked,
			},
		},
		{
			Name:        "social",
			Description: "Read and interact with posts, friends, and following",
			Permissions: []TokenPermission{
				PermViewProfile, PermViewCredits, PermViewFriends,
				PermViewPosts, PermCreatePost, PermDeletePost, PermManagePosts,
				PermLikePost, PermReplyPost, PermRepost,
				PermViewFollowing, PermFollow, PermUnfollow,
				PermManageFriends, PermSendFriendReq, PermAcceptFriend,
				PermRemoveFriend, PermViewNotifications,
			},
		},
		{
			Name:        "economy",
			Description: "Manage credits, gifts, and marketplace items",
			Permissions: []TokenPermission{
				PermViewProfile, PermViewCredits, PermManageCredits,
				PermTransferCredits, PermClaimDaily,
				PermViewGifts, PermCreateGift, PermClaimGift, PermCancelGift,
				PermViewItems, PermBuyItems, PermSellItems, PermManageItems,
			},
		},
		{
			Name:        "storage",
			Description: "Manage files and storage",
			Permissions: []TokenPermission{
				PermViewProfile, PermViewFiles, PermManageFiles, PermDeleteFiles,
			},
		},
		{
			Name:        "full",
			Description: "Full access to everything except account deletion and token management",
			Permissions: func() []TokenPermission {
				perms := make([]TokenPermission, 0)
				for _, p := range AllPermissions() {
					if p != PermDeleteAccount && p != PermManageTokens {
						perms = append(perms, p)
					}
				}
				return perms
			}(),
		},
	}
}

type SubToken struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Token       string            `json:"token"`
	Permissions []TokenPermission `json:"permissions"`
	CreatedAt   int64             `json:"created_at"`
	LastUsedAt  *int64            `json:"last_used_at,omitempty"`
	ExpiresAt   *int64            `json:"expires_at,omitempty"`
	Revoked     bool              `json:"revoked"`
	RevokedAt   *int64            `json:"revoked_at,omitempty"`
	Origin      string            `json:"origin,omitempty"`
	Description string            `json:"description,omitempty"`
	Websites    []string          `json:"websites,omitempty"`
}

type TokenStore struct {
	Tokens    []SubToken `json:"tokens"`
	UpdatedAt int64      `json:"updated_at"`
}

var (
	tokenStoreCache = make(map[string]*TokenStore)
	tokenStoreMutex sync.RWMutex
)

func getTokenStorePath(username string) string {
	return filepath.Join(
		USERDATA_PATH,
		strings.ToLower(username),
		"tokens.json",
	)
}

func loadTokenStore(username string) (*TokenStore, error) {
	tokenStoreMutex.RLock()
	if cached, ok := tokenStoreCache[strings.ToLower(username)]; ok {
		tokenStoreMutex.RUnlock()
		return cached, nil
	}
	tokenStoreMutex.RUnlock()

	path := getTokenStorePath(username)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			store := &TokenStore{
				Tokens:    []SubToken{},
				UpdatedAt: time.Now().UnixMilli(),
			}
			tokenStoreMutex.Lock()
			tokenStoreCache[strings.ToLower(username)] = store
			tokenStoreMutex.Unlock()
			return store, nil
		}
		return nil, fmt.Errorf("failed to read token store: %w", err)
	}

	var store TokenStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("failed to parse token store: %w", err)
	}

	tokenStoreMutex.Lock()
	tokenStoreCache[strings.ToLower(username)] = &store
	tokenStoreMutex.Unlock()

	return &store, nil
}

func saveTokenStore(username string, store *TokenStore) error {
	store.UpdatedAt = time.Now().UnixMilli()

	path := getTokenStorePath(username)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create token directory: %w", err)
	}

	data, err := json.MarshalIndent(store, "", " ")
	if err != nil {
		return fmt.Errorf("failed to marshal token store: %w", err)
	}

	if err := atomicWrite(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write token store: %w", err)
	}

	tokenStoreMutex.Lock()
	tokenStoreCache[strings.ToLower(username)] = store
	tokenStoreMutex.Unlock()

	return nil
}

func generateSubTokenID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return "st_" + base64.URLEncoding.EncodeToString(b)
}

func generateSubTokenValue() string {
	b := make([]byte, 32)
	rand.Read(b)
	return "rotur_st_" + base64.URLEncoding.EncodeToString(b)
}

func authenticateWithSubToken(tokenValue string) (*User, *SubToken, error) {
	usersMutex.RLock()
	defer usersMutex.RUnlock()

	for i := range users {
		username := strings.ToLower(string(users[i].GetUsername()))
		store, err := loadTokenStore(username)
		if err != nil {
			continue
		}

		for j := range store.Tokens {
			t := &store.Tokens[j]
			if t.Token == tokenValue {
				if t.Revoked {
					return nil, nil, fmt.Errorf("token has been revoked")
				}
				if t.ExpiresAt != nil && *t.ExpiresAt < time.Now().UnixMilli() {
					return nil, nil, fmt.Errorf("token has expired")
				}
				now := time.Now().UnixMilli()
				t.LastUsedAt = &now
				go saveTokenStore(username, store)
				return &users[i], t, nil
			}
		}
	}

	return nil, nil, fmt.Errorf("sub-token not found")
}

func (t *SubToken) hasPermission(perm TokenPermission) bool {
	for _, p := range t.Permissions {
		if p == perm {
			return true
		}
	}
	return false
}

func (t *SubToken) hasAllPermissions(perms []TokenPermission) bool {
	for _, perm := range perms {
		if !t.hasPermission(perm) {
			return false
		}
	}
	return true
}

func (t *SubToken) ToPublic() SubTokenPublic {
	return SubTokenPublic{
		ID:          t.ID,
		Name:        t.Name,
		Permissions: t.Permissions,
		CreatedAt:   t.CreatedAt,
		LastUsedAt:  t.LastUsedAt,
		ExpiresAt:   t.ExpiresAt,
		Revoked:     t.Revoked,
		RevokedAt:   t.RevokedAt,
		Origin:      t.Origin,
		Description: t.Description,
		Websites:    t.Websites,
	}
}

type SubTokenPublic struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Permissions []TokenPermission `json:"permissions"`
	CreatedAt   int64             `json:"created_at"`
	LastUsedAt  *int64            `json:"last_used_at,omitempty"`
	ExpiresAt   *int64            `json:"expires_at,omitempty"`
	Revoked     bool              `json:"revoked"`
	RevokedAt   *int64            `json:"revoked_at,omitempty"`
	Origin      string            `json:"origin,omitempty"`
	Description string            `json:"description,omitempty"`
	Websites    []string          `json:"websites,omitempty"`
}

type SubTokenCreate struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Token       string            `json:"token"`
	Permissions []TokenPermission `json:"permissions"`
	CreatedAt   int64             `json:"created_at"`
	ExpiresAt   *int64            `json:"expires_at,omitempty"`
	Origin      string            `json:"origin,omitempty"`
	Description string            `json:"description,omitempty"`
	Websites    []string          `json:"websites,omitempty"`
}

var (
	subTokenIndex     = make(map[string]*subTokenEntry)
	subTokenIndexMutex sync.RWMutex
)

type subTokenEntry struct {
	Username string
	TokenID  string
}

func buildSubTokenIndex() {
	subTokenIndexMutex.Lock()
	defer subTokenIndexMutex.Unlock()

	subTokenIndex = make(map[string]*subTokenEntry)

	usersMutex.RLock()
	defer usersMutex.RUnlock()

	for i := range users {
		username := strings.ToLower(string(users[i].GetUsername()))
		store, err := loadTokenStore(username)
		if err != nil {
			continue
		}

		for _, t := range store.Tokens {
			if !t.Revoked && (t.ExpiresAt == nil || *t.ExpiresAt > time.Now().UnixMilli()) {
				subTokenIndex[t.Token] = &subTokenEntry{
					Username: username,
					TokenID:  t.ID,
				}
			}
		}
	}

	log.Printf("Built sub-token index with %d active tokens", len(subTokenIndex))
}

func authenticateWithSubTokenFast(tokenValue string) (*User, *SubToken, error) {
	subTokenIndexMutex.RLock()
	entry, ok := subTokenIndex[tokenValue]
	subTokenIndexMutex.RUnlock()

	if !ok {
		return nil, nil, fmt.Errorf("sub-token not found")
	}

	usersMutex.RLock()
	var foundUser *User
	for i := range users {
		if strings.ToLower(string(users[i].GetUsername())) == entry.Username {
			foundUser = &users[i]
			break
		}
	}
	usersMutex.RUnlock()

	if foundUser == nil {
		return nil, nil, fmt.Errorf("user not found for sub-token")
	}

	store, err := loadTokenStore(entry.Username)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load token store: %w", err)
	}

	for j := range store.Tokens {
		t := &store.Tokens[j]
		if t.ID == entry.TokenID && t.Token == tokenValue {
			if t.Revoked {
				subTokenIndexMutex.Lock()
				delete(subTokenIndex, tokenValue)
				subTokenIndexMutex.Unlock()
				return nil, nil, fmt.Errorf("token has been revoked")
			}
			if t.ExpiresAt != nil && *t.ExpiresAt < time.Now().UnixMilli() {
				subTokenIndexMutex.Lock()
				delete(subTokenIndex, tokenValue)
				subTokenIndexMutex.Unlock()
				return nil, nil, fmt.Errorf("token has expired")
			}
			now := time.Now().UnixMilli()
			t.LastUsedAt = &now
			go saveTokenStore(entry.Username, store)
			return foundUser, t, nil
		}
	}

	subTokenIndexMutex.Lock()
	delete(subTokenIndex, tokenValue)
	subTokenIndexMutex.Unlock()

	return nil, nil, fmt.Errorf("sub-token not found in store")
}

func addToSubTokenIndex(tokenValue string, username string, tokenID string) {
	subTokenIndexMutex.Lock()
	defer subTokenIndexMutex.Unlock()
	subTokenIndex[tokenValue] = &subTokenEntry{
		Username: username,
		TokenID:  tokenID,
	}
}

func removeFromSubTokenIndex(tokenValue string) {
	subTokenIndexMutex.Lock()
	defer subTokenIndexMutex.Unlock()
	delete(subTokenIndex, tokenValue)
}

func cleanExpiredSubTokens() {
	for {
		time.Sleep(1 * time.Hour)

		usersMutex.RLock()
		usernames := make([]string, 0)
		for i := range users {
			usernames = append(usernames, strings.ToLower(string(users[i].GetUsername())))
		}
		usersMutex.RUnlock()

		now := time.Now().UnixMilli()
		for _, username := range usernames {
			store, err := loadTokenStore(username)
			if err != nil {
				continue
			}

			changed := false
			for j := range store.Tokens {
				t := &store.Tokens[j]
				if !t.Revoked && t.ExpiresAt != nil && *t.ExpiresAt < now {
					t.Revoked = true
					revokedAt := now
					t.RevokedAt = &revokedAt
					removeFromSubTokenIndex(t.Token)
					changed = true
				}
			}

			if changed {
				go saveTokenStore(username, store)
			}
		}
	}
}
