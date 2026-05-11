package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGenerateSubTokenID(t *testing.T) {
	id := generateSubTokenID()
	if id == "" {
		t.Fatal("Sub-token ID should not be empty")
	}
	if !startsWith(id, "st_") {
		t.Fatalf("Sub-token ID should start with 'st_', got: %s", id)
	}
}

func TestGenerateSubTokenValue(t *testing.T) {
	val := generateSubTokenValue()
	if val == "" {
		t.Fatal("Sub-token value should not be empty")
	}
	if !startsWith(val, "rotur_st_") {
		t.Fatalf("Sub-token value should start with 'rotur_st_', got: %s", val)
	}
	val2 := generateSubTokenValue()
	if val == val2 {
		t.Fatal("Two generated sub-token values should not be equal")
	}
}

func TestSubTokenPermissionCheck(t *testing.T) {
	token := SubToken{
		ID:          "st_test",
		Name:        "test",
		Token:       "rotur_st_test",
		Permissions: []TokenPermission{PermViewProfile, PermViewCredits, PermCreatePost},
	}

	if !token.hasPermission(PermViewProfile) {
		t.Error("Token should have PermViewProfile")
	}
	if !token.hasPermission(PermViewCredits) {
		t.Error("Token should have PermViewCredits")
	}
	if token.hasPermission(PermDeleteAccount) {
		t.Error("Token should NOT have PermDeleteAccount")
	}
	if token.hasPermission(PermManageTokens) {
		t.Error("Token should NOT have PermManageTokens")
	}
}

func TestSubTokenAllPermissions(t *testing.T) {
	allPerms := AllPermissions()
	if len(allPerms) == 0 {
		t.Fatal("AllPermissions should return at least one permission")
	}

	seen := make(map[TokenPermission]bool)
	for _, p := range allPerms {
		if seen[p] {
			t.Fatalf("Duplicate permission: %s", p)
		}
		seen[p] = true
	}

	t.Logf("Total permissions: %d", len(allPerms))
}

func TestSubTokenHasAllPermissions(t *testing.T) {
	token := SubToken{
		ID:          "st_test",
		Name:        "test",
		Token:       "rotur_st_test",
		Permissions: []TokenPermission{PermViewProfile, PermViewCredits},
	}

	if !token.hasAllPermissions([]TokenPermission{PermViewProfile}) {
		t.Error("Token should have all specified permissions (single)")
	}
	if !token.hasAllPermissions([]TokenPermission{PermViewProfile, PermViewCredits}) {
		t.Error("Token should have all specified permissions (multiple)")
	}
	if token.hasAllPermissions([]TokenPermission{PermViewProfile, PermDeleteAccount}) {
		t.Error("Token should NOT have all specified permissions (missing one)")
	}
}

func TestSubTokenToPublic(t *testing.T) {
	now := time.Now().UnixMilli()
	token := SubToken{
		ID:          "st_test",
		Name:        "test",
		Token:       "rotur_st_secret_value",
		Permissions: []TokenPermission{PermViewProfile},
		CreatedAt:   now,
	}

	public := token.ToPublic()

	if public.ID != token.ID {
		t.Error("Public ID should match")
	}
	if public.Name != token.Name {
		t.Error("Public Name should match")
	}
}

func TestPermissionGroups(t *testing.T) {
	groups := PermissionGroups()
	if len(groups) == 0 {
		t.Fatal("PermissionGroups should return at least one group")
	}

	for _, group := range groups {
		if group.Name == "" {
			t.Error("Group should have a name")
		}
		if len(group.Permissions) == 0 {
			t.Errorf("Group %s should have at least one permission", group.Name)
		}

		validPerms := make(map[TokenPermission]bool)
		for _, p := range AllPermissions() {
			validPerms[p] = true
		}

		for _, p := range group.Permissions {
			if !validPerms[p] {
				t.Errorf("Group %s has invalid permission: %s", group.Name, p)
			}
		}
	}

	t.Logf("Total groups: %d", len(groups))
}

func TestTokenStoreLoadSave(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "token_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origPath := USERDATA_PATH
	USERDATA_PATH = tmpDir
	defer func() { USERDATA_PATH = origPath }()

	username := "testuser"
	store, err := loadTokenStore(username)
	if err != nil {
		t.Fatalf("Failed to load token store: %v", err)
	}

	if len(store.Tokens) != 0 {
		t.Error("New token store should have no tokens")
	}

	now := time.Now().UnixMilli()
	store.Tokens = append(store.Tokens, SubToken{
		ID:          "st_test123",
		Name:        "My App",
		Token:       "rotur_st_testvalue123",
		Permissions: []TokenPermission{PermViewProfile, PermViewCredits},
		CreatedAt:   now,
	})

	err = saveTokenStore(username, store)
	if err != nil {
		t.Fatalf("Failed to save token store: %v", err)
	}

	path := getTokenStorePath(username)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("Token store file should exist at %s", path)
	}

	tokenStoreMutex.Lock()
	delete(tokenStoreCache, username)
	tokenStoreMutex.Unlock()

	store2, err := loadTokenStore(username)
	if err != nil {
		t.Fatalf("Failed to reload token store: %v", err)
	}

	if len(store2.Tokens) != 1 {
		t.Fatalf("Expected 1 token, got %d", len(store2.Tokens))
	}

	if store2.Tokens[0].ID != "st_test123" {
		t.Errorf("Token ID mismatch: got %s", store2.Tokens[0].ID)
	}
	if store2.Tokens[0].Name != "My App" {
		t.Errorf("Token Name mismatch: got %s", store2.Tokens[0].Name)
	}
	if store2.Tokens[0].Token != "rotur_st_testvalue123" {
		t.Errorf("Token value mismatch: got %s", store2.Tokens[0].Token)
	}
}

func TestTokenStoreDirectoryCreation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "token_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origPath := USERDATA_PATH
	USERDATA_PATH = filepath.Join(tmpDir, "nested", "path")
	defer func() { USERDATA_PATH = origPath }()

	username := "newuser"
	store, err := loadTokenStore(username)
	if err != nil {
		t.Fatalf("Failed to load token store with new path: %v", err)
	}

	err = saveTokenStore(username, store)
	if err != nil {
		t.Fatalf("Failed to save token store with new path: %v", err)
	}

	dirPath := filepath.Join(USERDATA_PATH, username)
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		t.Fatalf("Token directory should exist at %s", dirPath)
	}
}

func TestTokenStoreJSON(t *testing.T) {
	now := time.Now().UnixMilli()
	expiry := now + 3600000
	revokedAt := now + 7200000
	lastUsed := now + 1000

	store := TokenStore{
		Tokens: []SubToken{
			{
				ID:          "st_abc123",
				Name:        "Test Token",
				Token:       "rotur_st_secret",
				Permissions: []TokenPermission{PermViewProfile, PermTransferCredits},
				CreatedAt:   now,
				LastUsedAt:  &lastUsed,
				ExpiresAt:   &expiry,
				Revoked:     true,
				RevokedAt:   &revokedAt,
				Origin:      "https://example.com",
				Description: "A test token",
				Websites:    []string{"https://example.com", "https://test.com"},
			},
		},
		UpdatedAt: now,
	}

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal token store: %v", err)
	}

	var decoded TokenStore
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal token store: %v", err)
	}

	if len(decoded.Tokens) != 1 {
		t.Fatalf("Expected 1 token, got %d", len(decoded.Tokens))
	}

	token := decoded.Tokens[0]
	if token.ID != "st_abc123" {
		t.Errorf("ID mismatch: got %s", token.ID)
	}
	if token.Name != "Test Token" {
		t.Errorf("Name mismatch: got %s", token.Name)
	}
	if !token.Revoked {
		t.Error("Token should be revoked")
	}
	if token.LastUsedAt == nil {
		t.Error("LastUsedAt should not be nil")
	}
	if token.ExpiresAt == nil {
		t.Error("ExpiresAt should not be nil")
	}
	if len(token.Websites) != 2 {
		t.Errorf("Expected 2 websites, got %d", len(token.Websites))
	}
}

func TestSubTokenIndex(t *testing.T) {
	addToSubTokenIndex("rotur_st_test1", "user1", "st_id1")
	addToSubTokenIndex("rotur_st_test2", "user2", "st_id2")

	subTokenIndexMutex.RLock()
	entry1, ok1 := subTokenIndex["rotur_st_test1"]
	entry2, ok2 := subTokenIndex["rotur_st_test2"]
	subTokenIndexMutex.RUnlock()

	if !ok1 || entry1.Username != "user1" || entry1.TokenID != "st_id1" {
		t.Error("Index entry 1 not found or incorrect")
	}
	if !ok2 || entry2.Username != "user2" || entry2.TokenID != "st_id2" {
		t.Error("Index entry 2 not found or incorrect")
	}

	removeFromSubTokenIndex("rotur_st_test1")

	subTokenIndexMutex.RLock()
	_, ok1 = subTokenIndex["rotur_st_test1"]
	_, ok2 = subTokenIndex["rotur_st_test2"]
	subTokenIndexMutex.RUnlock()

	if ok1 {
		t.Error("Entry 1 should be removed from index")
	}
	if !ok2 {
		t.Error("Entry 2 should still be in index")
	}

	removeFromSubTokenIndex("rotur_st_test2")
}

func TestFullPermissionGroupExcludesSensitive(t *testing.T) {
	groups := PermissionGroups()
	var fullGroup *PermissionGroup
	for i := range groups {
		if groups[i].Name == "full" {
			fullGroup = &groups[i]
			break
		}
	}

	if fullGroup == nil {
		t.Fatal("full permission group not found")
	}

	for _, perm := range fullGroup.Permissions {
		if perm == PermDeleteAccount {
			t.Error("full group should NOT include PermDeleteAccount")
		}
		if perm == PermManageTokens {
			t.Error("full group should NOT include PermManageTokens")
		}
	}
}

func TestReadOnlyPermissionGroup(t *testing.T) {
	groups := PermissionGroups()
	var readOnlyGroup *PermissionGroup
	for i := range groups {
		if groups[i].Name == "read_only" {
			readOnlyGroup = &groups[i]
			break
		}
	}

	if readOnlyGroup == nil {
		t.Fatal("read_only permission group not found")
	}

	for _, perm := range readOnlyGroup.Permissions {
		if !startsWith(string(perm), "account:view") &&
			!startsWith(string(perm), "credits:view") &&
			!startsWith(string(perm), "friends:view") &&
			!startsWith(string(perm), "posts:view") &&
			!startsWith(string(perm), "following:view") &&
			!startsWith(string(perm), "files:view") &&
			!startsWith(string(perm), "keys:view") &&
			!startsWith(string(perm), "groups:view") &&
			!startsWith(string(perm), "notifications:view") &&
			!startsWith(string(perm), "gifts:view") &&
			!startsWith(string(perm), "items:view") &&
			!startsWith(string(perm), "blocked:view") {
			t.Errorf("read_only group should only contain view permissions, found: %s", perm)
		}
	}
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
