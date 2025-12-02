package main

import (
	"fmt"
	"maps"
	"strings"

	"github.com/gin-gonic/gin"
)

func validateSystem(system string) (bool, string, System) {
	systemsMutex.RLock()
	defer systemsMutex.RUnlock()

	if system == "" {
		return false, "System cannot be empty", System{}
	}

	systemLower := strings.ToLower(system)

	if len(systemLower) < 3 {
		return false, "System must be at least 3 characters long", System{}
	}

	if len(systemLower) > 20 {
		return false, "System must not exceed 20 characters", System{}
	}

	for systemName, system := range systems {
		if strings.EqualFold(systemName, systemLower) {
			return true, "Valid system", system
		}
	}

	return false, "System must match a valid system", System{}
}

func renameSystem(systemName string, newName string) error {
	system, ok := systems[systemName]
	if !ok {
		return fmt.Errorf("system not found")
	}
	delete(systems, systemName)
	system.Name = newName
	systems[newName] = system

	users, err := getAccountsBy("system", systemName, -1)
	if err != nil {
		return nil
	}
	usersMutex.Lock()
	for _, user := range users {
		user.Set("system", newName)
	}
	usersMutex.Unlock()
	go saveUsers()
	return nil
}

func updateSystem(c *gin.Context) {
	var req struct {
		System string `json:"system"`
		Key    string `json:"key"`
		Value  any    `json:"value"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request body"})
		return
	}

	isValid, errorMessage, system := validateSystem(req.System)
	if !isValid {
		c.JSON(400, gin.H{"error": errorMessage})
		return
	}

	user := c.MustGet("user").(*User)

	// only allow the owner of the system or mist to update it
	if !strings.EqualFold(system.Owner.Name, user.GetUsername()) && strings.ToLower(user.GetUsername()) != "mist" {
		c.JSON(403, gin.H{"error": "Insufficient permissions"})
		return
	}

	err := setSystem(system.Name, req.Key, req.Value)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "System updated successfully"})
}

func setSystem(systemName string, key string, value any) error {
	defer saveSystems()
	systemsMutex.Lock()
	defer systemsMutex.Unlock()

	system, ok := systems[systemName]
	if !ok {
		return fmt.Errorf("system not found")
	}

	switch key {
	case "name":
		if v, ok := value.(string); ok {
			renameSystem(system.Name, v)
			return nil
		}
	case "owner_name":
		if v, ok := value.(string); ok {
			system.Owner.Name = v
			systems[systemName] = system
			return nil
		}
	case "owner_discord_id":
		if v, ok := value.(int64); ok {
			system.Owner.DiscordID = v
			systems[systemName] = system
			return nil
		}
	case "wallpaper":
		if v, ok := value.(string); ok {
			system.Wallpaper = v
			systems[systemName] = system
			return nil
		}
	case "designation":
		if v, ok := value.(string); ok {
			system.Designation = v
			systems[systemName] = system
			return nil
		}
	case "icon":
		if v, ok := value.(string); ok {
			system.Icon = v
			systems[systemName] = system
			return nil
		}
	}

	return fmt.Errorf("invalid system key: %s", key)
}

func getAllSystems() map[string]System {
	systemsMutex.RLock()
	defer systemsMutex.RUnlock()

	result := make(map[string]System, len(systems))
	maps.Copy(result, systems)
	return result
}

func reloadSystems() error {
	loadSystems()
	return nil
}

func getSystems(c *gin.Context) {
	allSystems := getAllSystems()
	c.JSON(200, allSystems)
}

func getSystemUsers(c *gin.Context) {
	user := c.MustGet("user").(*User)

	// Only allow mist or the owner of the system to view users

	system_data := getAllSystems()
	system_name := c.Query("system")
	if system_name == "" {
		c.JSON(400, gin.H{"error": "System is required"})
		return
	}
	system, ok := system_data[system_name]
	if !ok {
		c.JSON(404, gin.H{"error": "System not found"})
		return
	}

	if !strings.EqualFold(system.Owner.Name, user.GetUsername()) && strings.ToLower(user.GetUsername()) != "mist" {
		c.JSON(403, gin.H{"error": "Insufficient permissions"})
		return
	}

	foundUsers, err := getAccountsBy("system", system.Name, -1)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	usernames := make([]string, 0, len(foundUsers))
	for _, user := range foundUsers {
		usernames = append(usernames, user.GetUsername())
	}
	c.JSON(200, usernames)
}

func reloadSystemsEndpoint(c *gin.Context) {
	user := c.MustGet("user").(*User)

	// Only allow mist user to reload systems (admin privilege)
	if strings.ToLower(user.GetUsername()) != "mist" {
		c.JSON(403, gin.H{"error": "Insufficient permissions"})
		return
	}

	err := reloadSystems()
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to reload systems"})
		return
	}

	c.JSON(200, gin.H{"message": "Systems reloaded successfully"})
}
