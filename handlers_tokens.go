package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func createSubToken(c *gin.Context) {
	user := c.MustGet("user").(*User)
	tokenType := c.MustGet("token_type").(string)

	if tokenType != "main" {
		c.JSON(403, gin.H{"error": "Only the main account token can create sub-tokens"})
		return
	}

	var req struct {
		Name         string   `json:"name"`
		Permissions  []string `json:"permissions"`
		ExpiresInHrs *int     `json:"expires_in_hrs,omitempty"`
		Origin       string   `json:"origin,omitempty"`
		Description  string   `json:"description,omitempty"`
		Websites     []string `json:"websites,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request body"})
		return
	}

	if req.Name == "" {
		c.JSON(400, gin.H{"error": "Token name is required"})
		return
	}

	if len(req.Name) > 50 {
		c.JSON(400, gin.H{"error": "Token name must be 50 characters or less"})
		return
	}

	if len(req.Permissions) == 0 {
		c.JSON(400, gin.H{"error": "At least one permission is required"})
		return
	}

	permissions := make([]TokenPermission, 0, len(req.Permissions))
	validPerms := make(map[TokenPermission]bool)
	for _, p := range AllPermissions() {
		validPerms[p] = true
	}

	for _, p := range req.Permissions {
		tp := TokenPermission(p)
		if !validPerms[tp] {
			c.JSON(400, gin.H{"error": fmt.Sprintf("Invalid permission: %s", p)})
			return
		}
		if tp == PermManageTokens {
			c.JSON(400, gin.H{"error": "Cannot grant tokens:manage permission to sub-tokens"})
			return
		}
		permissions = append(permissions, tp)
	}

	username := strings.ToLower(string(user.GetUsername()))
	store, err := loadTokenStore(username)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to load token store"})
		return
	}

	activeCount := 0
	for _, t := range store.Tokens {
		if !t.Revoked && (t.ExpiresAt == nil || *t.ExpiresAt > time.Now().UnixMilli()) {
			activeCount++
		}
	}
	if activeCount >= 25 {
		c.JSON(400, gin.H{"error": "Maximum of 25 active sub-tokens reached"})
		return
	}

	tokenID := generateSubTokenID()
	tokenValue := generateSubTokenValue()
	now := time.Now().UnixMilli()

	var expiresAt *int64
	if req.ExpiresInHrs != nil && *req.ExpiresInHrs > 0 {
		if *req.ExpiresInHrs > 8760 {
			c.JSON(400, gin.H{"error": "Maximum expiration is 1 year (8760 hours)"})
			return
		}
		exp := now + int64(*req.ExpiresInHrs)*60*60*1000
		expiresAt = &exp
	}

	websites := req.Websites
	if websites == nil {
		websites = []string{}
	}

	subToken := SubToken{
		ID:          tokenID,
		Name:        req.Name,
		Token:       tokenValue,
		Permissions: permissions,
		CreatedAt:   now,
		ExpiresAt:   expiresAt,
		Origin:      req.Origin,
		Description: req.Description,
		Websites:    websites,
	}

	store.Tokens = append(store.Tokens, subToken)

	if err := saveTokenStore(username, store); err != nil {
		c.JSON(500, gin.H{"error": "Failed to save token store"})
		return
	}

	addToSubTokenIndex(tokenValue, username, tokenID)

	c.JSON(201, SubTokenCreate{
		ID:          tokenID,
		Name:        req.Name,
		Token:       tokenValue,
		Permissions: permissions,
		CreatedAt:   now,
		ExpiresAt:   expiresAt,
		Origin:      req.Origin,
		Description: req.Description,
		Websites:    websites,
	})
}

func listSubTokens(c *gin.Context) {
	user := c.MustGet("user").(*User)

	username := strings.ToLower(string(user.GetUsername()))
	store, err := loadTokenStore(username)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to load token store"})
		return
	}

	tokens := make([]SubTokenPublic, 0, len(store.Tokens))
	for _, t := range store.Tokens {
		tokens = append(tokens, t.ToPublic())
	}

	c.JSON(200, gin.H{
		"tokens": tokens,
		"total":  len(tokens),
	})
}

func getSubToken(c *gin.Context) {
	user := c.MustGet("user").(*User)
	tokenID := c.Param("id")

	username := strings.ToLower(string(user.GetUsername()))
	store, err := loadTokenStore(username)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to load token store"})
		return
	}

	for _, t := range store.Tokens {
		if t.ID == tokenID {
			c.JSON(200, t.ToPublic())
			return
		}
	}

	c.JSON(404, gin.H{"error": "Token not found"})
}

func updateSubToken(c *gin.Context) {
	user := c.MustGet("user").(*User)
	tokenType := c.MustGet("token_type").(string)

	if tokenType != "main" {
		c.JSON(403, gin.H{"error": "Only the main account token can modify sub-tokens"})
		return
	}

	tokenID := c.Param("id")

	var req struct {
		Name        *string  `json:"name,omitempty"`
		Permissions []string `json:"permissions,omitempty"`
		Description *string  `json:"description,omitempty"`
		Websites    []string `json:"websites,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request body"})
		return
	}

	username := strings.ToLower(string(user.GetUsername()))
	store, err := loadTokenStore(username)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to load token store"})
		return
	}

	for i := range store.Tokens {
		t := &store.Tokens[i]
		if t.ID == tokenID {
			if t.Revoked {
				c.JSON(400, gin.H{"error": "Cannot update a revoked token"})
				return
			}

			if req.Name != nil {
				if *req.Name == "" || len(*req.Name) > 50 {
					c.JSON(400, gin.H{"error": "Token name must be between 1 and 50 characters"})
					return
				}
				t.Name = *req.Name
			}

			if req.Permissions != nil {
				validPerms := make(map[TokenPermission]bool)
				for _, p := range AllPermissions() {
					validPerms[p] = true
				}

				permissions := make([]TokenPermission, 0, len(req.Permissions))
				for _, p := range req.Permissions {
					tp := TokenPermission(p)
					if !validPerms[tp] {
						c.JSON(400, gin.H{"error": fmt.Sprintf("Invalid permission: %s", p)})
						return
					}
					if tp == PermManageTokens {
						c.JSON(400, gin.H{"error": "Cannot grant tokens:manage permission to sub-tokens"})
						return
					}
					permissions = append(permissions, tp)
				}
				t.Permissions = permissions
			}

			if req.Description != nil {
				t.Description = *req.Description
			}

			if req.Websites != nil {
				t.Websites = req.Websites
			}

			if err := saveTokenStore(username, store); err != nil {
				c.JSON(500, gin.H{"error": "Failed to save token store"})
				return
			}

			c.JSON(200, t.ToPublic())
			return
		}
	}

	c.JSON(404, gin.H{"error": "Token not found"})
}

func revokeSubToken(c *gin.Context) {
	user := c.MustGet("user").(*User)
	tokenType := c.MustGet("token_type").(string)

	if tokenType != "main" {
		c.JSON(403, gin.H{"error": "Only the main account token can revoke sub-tokens"})
		return
	}

	tokenID := c.Param("id")

	username := strings.ToLower(string(user.GetUsername()))
	store, err := loadTokenStore(username)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to load token store"})
		return
	}

	for i := range store.Tokens {
		t := &store.Tokens[i]
		if t.ID == tokenID {
			if t.Revoked {
				c.JSON(400, gin.H{"error": "Token is already revoked"})
				return
			}

			now := time.Now().UnixMilli()
			t.Revoked = true
			t.RevokedAt = &now

			if err := saveTokenStore(username, store); err != nil {
				c.JSON(500, gin.H{"error": "Failed to save token store"})
				return
			}

			removeFromSubTokenIndex(t.Token)

			c.JSON(200, gin.H{"message": "Token revoked successfully", "id": tokenID})
			return
		}
	}

	c.JSON(404, gin.H{"error": "Token not found"})
}

func deleteSubToken(c *gin.Context) {
	user := c.MustGet("user").(*User)
	tokenType := c.MustGet("token_type").(string)

	if tokenType != "main" {
		c.JSON(403, gin.H{"error": "Only the main account token can delete sub-tokens"})
		return
	}

	tokenID := c.Param("id")

	username := strings.ToLower(string(user.GetUsername()))
	store, err := loadTokenStore(username)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to load token store"})
		return
	}

	var tokenValue string
	newTokens := make([]SubToken, 0, len(store.Tokens))
	found := false

	for _, t := range store.Tokens {
		if t.ID == tokenID {
			tokenValue = t.Token
			found = true
			continue
		}
		newTokens = append(newTokens, t)
	}

	if !found {
		c.JSON(404, gin.H{"error": "Token not found"})
		return
	}

	store.Tokens = newTokens
	if err := saveTokenStore(username, store); err != nil {
		c.JSON(500, gin.H{"error": "Failed to save token store"})
		return
	}

	removeFromSubTokenIndex(tokenValue)

	c.JSON(200, gin.H{"message": "Token deleted successfully", "id": tokenID})
}

func renameSubToken(c *gin.Context) {
	user := c.MustGet("user").(*User)
	tokenType := c.MustGet("token_type").(string)

	if tokenType != "main" {
		c.JSON(403, gin.H{"error": "Only the main account token can rename sub-tokens"})
		return
	}

	tokenID := c.Param("id")

	var req struct {
		Name string `json:"name"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request body"})
		return
	}

	if req.Name == "" || len(req.Name) > 50 {
		c.JSON(400, gin.H{"error": "Token name must be between 1 and 50 characters"})
		return
	}

	username := strings.ToLower(string(user.GetUsername()))
	store, err := loadTokenStore(username)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to load token store"})
		return
	}

	for i := range store.Tokens {
		t := &store.Tokens[i]
		if t.ID == tokenID {
			t.Name = req.Name
			if err := saveTokenStore(username, store); err != nil {
				c.JSON(500, gin.H{"error": "Failed to save token store"})
				return
			}
			c.JSON(200, gin.H{"message": "Token renamed successfully", "id": tokenID, "name": req.Name})
			return
		}
	}

	c.JSON(404, gin.H{"error": "Token not found"})
}

func listPermissions(c *gin.Context) {
	perms := AllPermissions()
	groups := PermissionGroups()
	c.JSON(200, gin.H{
		"permissions": perms,
		"groups":      groups,
	})
}

func listActiveSubTokens(c *gin.Context) {
	user := c.MustGet("user").(*User)

	username := strings.ToLower(string(user.GetUsername()))
	store, err := loadTokenStore(username)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to load token store"})
		return
	}

	now := time.Now().UnixMilli()
	tokens := make([]SubTokenPublic, 0)
	for _, t := range store.Tokens {
		if t.Revoked {
			continue
		}
		if t.ExpiresAt != nil && *t.ExpiresAt < now {
			continue
		}
		tokens = append(tokens, t.ToPublic())
	}

	c.JSON(200, gin.H{
		"tokens": tokens,
		"total":  len(tokens),
	})
}

func getSubTokenActivity(c *gin.Context) {
	user := c.MustGet("user").(*User)
	tokenID := c.Param("id")

	username := strings.ToLower(string(user.GetUsername()))
	store, err := loadTokenStore(username)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to load token store"})
		return
	}

	for _, t := range store.Tokens {
		if t.ID == tokenID {
			status := "active"
			if t.Revoked {
				status = "revoked"
			} else if t.ExpiresAt != nil && *t.ExpiresAt < time.Now().UnixMilli() {
				status = "expired"
			}

			c.JSON(200, gin.H{
				"id":           t.ID,
				"name":         t.Name,
				"status":       status,
				"permissions":  t.Permissions,
				"created_at":   t.CreatedAt,
				"last_used_at": t.LastUsedAt,
				"expires_at":   t.ExpiresAt,
				"revoked_at":   t.RevokedAt,
				"origin":       t.Origin,
				"description":  t.Description,
				"websites":     t.Websites,
			})
			return
		}
	}

	c.JSON(404, gin.H{"error": "Token not found"})
}

func getTokenAbilities(c *gin.Context) {
	tokenType := c.MustGet("token_type").(string)

	if tokenType == "main" {
		c.JSON(200, gin.H{
			"token_type":  "main",
			"permissions": AllPermissions(),
		})
		return
	}

	subTokenVal, exists := c.Get("sub_token")
	if !exists {
		c.JSON(200, gin.H{
			"token_type":  "main",
			"permissions": AllPermissions(),
		})
		return
	}

	subToken, ok := subTokenVal.(*SubToken)
	if !ok {
		c.JSON(200, gin.H{
			"token_type":  "main",
			"permissions": AllPermissions(),
		})
		return
	}

	c.JSON(200, gin.H{
		"token_type":  "sub",
		"name":        subToken.Name,
		"id":          subToken.ID,
		"permissions": subToken.Permissions,
	})
}
