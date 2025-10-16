package main

import (
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

func getAllSystems() map[string]System {
	systemsMutex.RLock()
	defer systemsMutex.RUnlock()

	result := make(map[string]System, len(systems))
	for k, v := range systems {
		result[k] = v
	}
	return result
}

func reloadSystems() error {
	loadSystems()
	return nil
}

func getSystems(c *gin.Context) {
	systems := getAllSystems()
	c.JSON(200, systems)
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
