package main

import (
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func createKey(c *gin.Context) {
	authKey := c.Query("auth")
	if authKey == "" {
		c.JSON(403, gin.H{"error": "auth key is required"})
		return
	}

	user := authenticateWithKey(authKey)
	if user == nil {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		return
	}

	name := c.Query("name")
	if name == "" {
		c.JSON(400, gin.H{"error": "Key name is required"})
		return
	}

	description := c.Query("description")
	priceStr := c.Query("price")
	subscriptionFlag := strings.ToLower(c.Query("subscription")) // "true"/"1" to enable
	frequencyStr := c.Query("frequency")
	period := c.Query("period")

	// Defaults
	price := 0
	if priceStr != "" {
		if p, err := strconv.Atoi(priceStr); err == nil && p >= 0 {
			price = p
		}
	}
	frequency := 1
	if frequencyStr != "" {
		if f, err := strconv.Atoi(frequencyStr); err == nil && f > 0 {
			frequency = f
		}
	}
	if period == "" {
		period = "month"
	}

	keysMutex.Lock()
	defer keysMutex.Unlock()

	// Check if key name already exists
	for _, key := range keys {
		if key.Name != nil && *key.Name == name {
			c.JSON(400, gin.H{"error": "Key with this name already exists"})
			return
		}
	}

	newKey := Key{
		Key:         generateToken(),
		Creator:     strings.ToLower(user.GetUsername()),
		Users:       make(map[string]KeyUserData),
		Name:        &name,
		Price:       price,
		Type:        "standard",
		TotalIncome: 0,
	}

	if description != "" {
		newKey.Data = &description
	}

	// Handle subscription creation
	if subscriptionFlag == "true" || subscriptionFlag == "1" {
		now := time.Now()
		var nextBilling time.Time
		switch strings.ToLower(period) {
		case "day":
			nextBilling = now.AddDate(0, 0, frequency)
		case "week":
			nextBilling = now.AddDate(0, 0, 7*frequency)
		case "month":
			nextBilling = now.AddDate(0, frequency, 0)
		case "year":
			nextBilling = now.AddDate(frequency, 0, 0)
		default:
			period = "month"
			nextBilling = now.AddDate(0, frequency, 0)
		}
		newKey.Type = "subscription"
		newKey.Subscription = &Subscription{
			Active:      true,
			Frequency:   frequency,
			Period:      period,
			NextBilling: nextBilling.Unix(), // store as unix seconds for consistency
		}
	}

	// Add creator to key users
	newKey.Users[strings.ToLower(user.GetUsername())] = KeyUserData{
		Time: float64(time.Now().Unix()),
	}

	keys = append(keys, newKey)

	go saveKeys()

	c.JSON(200, gin.H{
		"status":       "Key created successfully",
		"key":          newKey.Key,
		"type":         newKey.Type,
		"price":        newKey.Price,
		"subscription": newKey.Subscription,
	})
}

func getMyKeys(c *gin.Context) {
	authKey := c.Query("auth")
	if authKey == "" {
		c.JSON(403, gin.H{"error": "auth key is required"})
		return
	}

	user := authenticateWithKey(authKey)
	if user == nil {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		return
	}

	username := strings.ToLower(user.GetUsername())

	keysMutex.RLock()
	userKeys := make([]Key, 0)
	for _, key := range keys {
		if _, hasAccess := key.Users[username]; hasAccess {
			if key.Creator != username {
				key.Users = make(map[string]KeyUserData)
				key.Data = nil
				key.TotalIncome = 0
			}
			userKeys = append(userKeys, key)
		}
	}
	keysMutex.RUnlock()

	c.JSON(200, userKeys)
}

func checkKey(c *gin.Context) {
	username := c.Param("username")
	keyToCheck := c.Query("key")

	if keyToCheck == "" {
		c.JSON(400, gin.H{"error": "Key is required"})
		return
	}

	hasKey := doesUserOwnKey(username, keyToCheck)

	c.JSON(200, gin.H{
		"owned":    hasKey,
		"username": username,
		"key":      keyToCheck,
	})
}

func revokeKey(c *gin.Context) {
	id := c.Param("id")
	authKey := c.Query("auth")
	if authKey == "" {
		c.JSON(403, gin.H{"error": "auth key is required"})
		return
	}

	user := authenticateWithKey(authKey)
	if user == nil {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		return
	}

	targetUser := c.Query("user")
	if targetUser == "" {
		c.JSON(400, gin.H{"error": "Target user is required"})
		return
	}

	keysMutex.Lock()
	defer keysMutex.Unlock()

	for i := range keys {
		if keys[i].Key == id {
			if !strings.EqualFold(keys[i].Creator, user.GetUsername()) {
				c.JSON(403, gin.H{"error": "You can only revoke access to keys you created"})
				return
			}
			if strings.EqualFold(targetUser, keys[i].Creator) {
				c.JSON(400, gin.H{"error": "You cannot revoke access from the key creator"})
				return
			}

			delete(keys[i].Users, strings.ToLower(targetUser))

			go saveKeys()

			c.JSON(200, gin.H{"status": "Key access revoked successfully"})
			return
		}
	}

	c.JSON(404, gin.H{"error": "Key not found"})
}

func deleteKey(c *gin.Context) {
	id := c.Param("id")
	authKey := c.Query("auth")
	if authKey == "" {
		c.JSON(403, gin.H{"error": "auth key is required"})
		return
	}

	user := authenticateWithKey(authKey)
	if user == nil {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		return
	}

	keysMutex.Lock()
	defer keysMutex.Unlock()

	for i, key := range keys {
		if key.Key == id {
			if !strings.EqualFold(key.Creator, user.GetUsername()) {
				c.JSON(403, gin.H{"error": "You can only delete keys you created"})
				return
			}

			// Remove the key
			keys = append(keys[:i], keys[i+1:]...)

			go saveKeys()

			c.JSON(200, gin.H{"status": "Key deleted successfully"})
			return
		}
	}

	c.JSON(404, gin.H{"error": "Key not found"})
}

func updateKey(c *gin.Context) {
	id := c.Param("id")
	authKey := c.Query("auth")
	key := c.Query("key")
	data := c.Query("data")
	if authKey == "" || data == "" || key == "" {
		c.JSON(403, gin.H{"error": "auth key, update key and data are required"})
		return
	}
	// data is {key: value}
	if !isValidJSON(data) {
		c.JSON(400, gin.H{"error": "Invalid JSON data"})
		return
	}

	user := authenticateWithKey(authKey)
	if user == nil {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		return
	}

	keysMutex.Lock()
	defer keysMutex.Unlock()

	for i := range keys {
		if keys[i].Key == id {
			if !strings.EqualFold(keys[i].Creator, user.GetUsername()) {
				c.JSON(403, gin.H{"error": "You can only update keys you created"})
				return
			}

			var parsedData any
			if err := json.Unmarshal([]byte(data), &parsedData); err != nil {
				c.JSON(400, gin.H{"error": "Failed to parse JSON data"})
				return
			}

			keys[i].setKey(key, parsedData)

			go saveKeys()

			c.JSON(200, gin.H{"status": "Key updated successfully"})
			return
		}
	}

	c.JSON(404, gin.H{"error": "Key not found"})
}

func setKeyName(c *gin.Context) {
	id := c.Param("id")
	authKey := c.Query("auth")
	if authKey == "" {
		c.JSON(403, gin.H{"error": "auth key is required"})
		return
	}

	user := authenticateWithKey(authKey)
	if user == nil {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		return
	}

	name := c.Query("name")
	if name == "" {
		c.JSON(400, gin.H{"error": "Name is required"})
		return
	}

	keysMutex.Lock()
	defer keysMutex.Unlock()

	for i := range keys {
		if keys[i].Key == id {
			if !strings.EqualFold(keys[i].Creator, user.GetUsername()) {
				c.JSON(403, gin.H{"error": "You can only rename keys you created"})
				return
			}

			keys[i].Name = &name

			go saveKeys()

			c.JSON(200, gin.H{"status": "Key name updated successfully"})
			return
		}
	}

	c.JSON(404, gin.H{"error": "Key not found"})
}

func getKey(c *gin.Context) {
	id := c.Param("id")

	keysMutex.RLock()
	defer keysMutex.RUnlock()

	for _, key := range keys {
		if key.Key == id {
			c.JSON(200, key)
			return
		}
	}

	c.JSON(404, gin.H{"error": "Key not found"})
}

func adminAddUserToKey(c *gin.Context) {
	id := c.Param("id")
	authKey := c.Query("auth")
	if authKey == "" {
		c.JSON(403, gin.H{"error": "auth key is required"})
		return
	}

	user := authenticateWithKey(authKey)
	if user == nil {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		return
	}

	targetUser := c.Query("user")
	if targetUser == "" {
		targetUser = c.Query("username")
	}
	if targetUser == "" {
		c.JSON(400, gin.H{"error": "Target user is required"})
		return
	}

	keysMutex.Lock()
	defer keysMutex.Unlock()

	for i := range keys {
		if keys[i].Key == id {
			keys[i].Users[strings.ToLower(targetUser)] = KeyUserData{
				Time: float64(time.Now().Unix()),
			}

			go saveKeys()

			c.JSON(200, gin.H{"status": "User added to key successfully"})
			return
		}
	}

	c.JSON(404, gin.H{"error": "Key not found"})
}

func adminRemoveUserFromKey(c *gin.Context) {
	id := c.Param("id")
	authKey := c.Query("auth")
	if authKey == "" {
		c.JSON(403, gin.H{"error": "auth key is required"})
		return
	}

	user := authenticateWithKey(authKey)
	if user == nil {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		return
	}

	targetUser := c.Query("user")
	if targetUser == "" {
		targetUser = c.Query("username")
	}
	if targetUser == "" {
		c.JSON(400, gin.H{"error": "Target user is required"})
		return
	}

	keysMutex.Lock()
	defer keysMutex.Unlock()

	for i := range keys {
		if keys[i].Key == id {
			delete(keys[i].Users, strings.ToLower(targetUser))

			go saveKeys()

			c.JSON(200, gin.H{"status": "User removed from key successfully"})
			return
		}
	}

	c.JSON(404, gin.H{"error": "Key not found"})
}

func buyKey(c *gin.Context) {
	id := c.Param("id")
	authKey := c.Query("auth")
	if authKey == "" {
		c.JSON(403, gin.H{"error": "auth key is required"})
		return
	}

	user := authenticateWithKey(authKey)
	if user == nil {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		return
	}

	keysMutex.Lock()
	defer keysMutex.Unlock()

	for i := range keys {
		if keys[i].Key == id {
			if keys[i].Price < 0 {
				c.JSON(400, gin.H{"error": "Key is not for sale"})
				return
			}

			username := strings.ToLower(user.GetUsername())
			if _, hasAccess := keys[i].Users[username]; hasAccess {
				c.JSON(400, gin.H{"error": "You already have access to this key"})
				return
			}

			// Add user to key
			userData := KeyUserData{
				Time:  float64(time.Now().Unix()),
				Price: keys[i].Price,
			}

			if keys[i].Subscription != nil {
				now := time.Now()

				// Calculate next billing based on subscription period and frequency
				frequency := keys[i].Subscription.Frequency
				if frequency == 0 {
					frequency = 1
				}
				period := keys[i].Subscription.Period
				if period == "" {
					period = "month"
				}

				var nextBillingTime time.Time
				switch strings.ToLower(period) {
				case "day":
					nextBillingTime = now.AddDate(0, 0, frequency)
				case "week":
					nextBillingTime = now.AddDate(0, 0, frequency*7)
				case "month":
					nextBillingTime = now.AddDate(0, frequency, 0)
				case "year":
					nextBillingTime = now.AddDate(frequency, 0, 0)
				default:
					nextBillingTime = now.AddDate(0, frequency, 0) // Default to month
				}

				userData.NextBilling = nextBillingTime.UnixMilli()
			}

			var balance = user.GetCredits()
			if balance < float64(keys[i].Price) {
				c.JSON(400, gin.H{"error": "Insufficient balance to buy this key"})
				return
			}

			keys[i].Users[username] = userData

			// Update total income for the key
			keys[i].TotalIncome += keys[i].Price

			go saveKeys()

			// Deduct the price from user's balance
			usersMutex.Lock()
			userIndex := -1
			for j, u := range users {
				if strings.EqualFold(u.GetUsername(), username) {
					userIndex = j
					break
				}
			}

			ownerIndex := -1
			for j, u := range users {
				if strings.EqualFold(u.GetUsername(), keys[i].Creator) {
					ownerIndex = j
					break
				}
			}

			if userIndex != -1 {
				usersMutex.Unlock()
				// Flexible extraction for sys.currency
				newCurrency := user.GetCredits() - float64(keys[i].Price)
				users[userIndex].SetBalance(newCurrency)

				// Pay the creator
				if ownerIndex != -1 && ownerIndex != userIndex {
					var ownerCurrency float64 = users[ownerIndex].GetCredits()
					users[ownerIndex].SetBalance(ownerCurrency + float64(keys[i].Price))
				}

				go saveUsers()
			} else {
				usersMutex.Unlock()
				c.JSON(500, gin.H{"error": "User not found in users list"})
				return
			}

			c.JSON(200, gin.H{"status": "Key purchased successfully"})
			return
		}
	}

	c.JSON(404, gin.H{"error": "Key not found"})
}

func cancelKey(c *gin.Context) {
	id := c.Param("id")
	authKey := c.Query("auth")
	if authKey == "" {
		c.JSON(403, gin.H{"error": "auth key is required"})
		return
	}

	user := authenticateWithKey(authKey)
	if user == nil {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		return
	}

	keysMutex.Lock()
	defer keysMutex.Unlock()

	for i := range keys {
		if keys[i].Key == id {
			if !strings.EqualFold(keys[i].Creator, user.GetUsername()) {
				c.JSON(403, gin.H{"error": "You can only cancel sales for keys you created"})
				return
			}

			keys[i].Price = 0

			go saveKeys()

			c.JSON(200, gin.H{"status": "Key sale cancelled successfully"})
			return
		}
	}

	c.JSON(404, gin.H{"error": "Key not found"})
}

func debugSubscriptionsEndpoint(c *gin.Context) {
	authKey := c.Query("auth")
	if authKey == "" {
		c.JSON(403, gin.H{"error": "auth key is required"})
		return
	}

	user := authenticateWithKey(authKey)
	if user == nil {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		return
	}

	// Only allow admin users to access debug info
	if strings.ToLower(user.GetUsername()) != "mist" {
		c.JSON(403, gin.H{"error": "Admin access required"})
		return
	}

	debugSubscriptions()
	c.JSON(200, gin.H{"message": "Subscription debug info logged to console"})
}

func checkSubscriptions() {
	ticker := time.NewTicker(time.Duration(SUBSCRIPTION_CHECK_INTERVAL) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		log.Println("Checking subscriptions...")

		keysMutex.Lock()
		subscriptionsProcessed := 0
		chargesProcessed := 0

		for keyIndex := range keys {
			key := &keys[keyIndex]
			if key.Subscription == nil {
				continue
			}

			subscriptionsProcessed++
			usersToRemove := make([]string, 0)

			for username, userData := range key.Users {
				if userData.NextBilling == nil {
					continue
				}

				var nextBilling int64
				switch v := userData.NextBilling.(type) {
				case float64:
					nextBilling = int64(v)
				case int64:
					nextBilling = v
				case int:
					nextBilling = int64(v)
				default:
					log.Printf("Warning: Invalid NextBilling type for user %s in key %s", username, key.Key)
					continue
				}

				currentTimeMs := time.Now().UnixMilli()
				nextBillingTime := time.Unix(nextBilling/1000, 0)

				log.Printf("User %s in key %s: Next billing %s, Current time %s",
					username, key.Key,
					nextBillingTime.Format("2006-01-02 15:04:05"),
					time.Now().Format("2006-01-02 15:04:05"))

				if currentTimeMs >= nextBilling {
					if userData.Price != 0 {
						log.Printf("Processing subscription payment for %s for key %s (amount: %.2f)", username, key.Key, float64(userData.Price))

						usersMutex.Lock()
						userIndex := -1
						for i := range users {
							if strings.EqualFold(users[i].GetUsername(), username) {
								userIndex = i
								break
							}
						}
						if userIndex == -1 {
							log.Printf("User %s not found for key %s", username, key.Key)
							usersMutex.Unlock()
							usersToRemove = append(usersToRemove, username)
							continue
						}

						var currencyFloat float64 = users[userIndex].GetCredits()
						if currencyFloat < float64(userData.Price) {
							log.Printf("User %s does not have enough currency for key %s (needed: %.2f, available: %.2f)",
								username, key.Key, float64(userData.Price), currencyFloat)
							usersMutex.Unlock()
							usersToRemove = append(usersToRemove, username)
							continue
						}
						currencyFloat -= float64(userData.Price)
						usersMutex.Unlock()
						users[userIndex].SetBalance(currencyFloat)
						go saveUsers()

						// Update total income for the key
						key.TotalIncome += userData.Price

						frequency := key.Subscription.Frequency
						if frequency == 0 {
							frequency = 1
						}
						period := key.Subscription.Period
						if period == "" {
							period = "month"
						}

						currentTime := time.Now()
						var nextBillingTime time.Time
						switch strings.ToLower(period) {
						case "day":
							nextBillingTime = currentTime.AddDate(0, 0, frequency)
						case "week":
							nextBillingTime = currentTime.AddDate(0, 0, frequency*7)
						case "month":
							nextBillingTime = currentTime.AddDate(0, frequency, 0)
						case "year":
							nextBillingTime = currentTime.AddDate(frequency, 0, 0)
						default:
							nextBillingTime = currentTime.AddDate(0, frequency, 0)
						}

						newNextBilling := nextBillingTime.UnixMilli()
						userData.NextBilling = newNextBilling
						key.Users[username] = userData

						log.Printf("Successfully billed user %s for key %s. Next billing: %s",
							username, key.Key, nextBillingTime.Format("2006-01-02 15:04:05"))
						chargesProcessed++
					} else {
						usersToRemove = append(usersToRemove, username)
					}
				}
			}

			for _, username := range usersToRemove {
				delete(key.Users, username)
				log.Printf("Removed user %s from key %s due to payment failure", username, key.Key)
			}
		}

		log.Printf("Subscription check completed: %d keys with subscriptions checked, %d charges processed", subscriptionsProcessed, chargesProcessed)
		keysMutex.Unlock()

		if len(keys) > 0 {
			go saveKeys()
		}
	}
}

func debugSubscriptions() {
	keysMutex.RLock()
	defer keysMutex.RUnlock()

	log.Println("=== SUBSCRIPTION DEBUG INFO ===")
	subscriptionCount := 0

	for _, key := range keys {
		if key.Subscription == nil {
			continue
		}

		subscriptionCount++
		log.Printf("Key: %s (Creator: %s)", key.Key, key.Creator)
		log.Printf("  Subscription Period: %s, Frequency: %d", key.Subscription.Period, key.Subscription.Frequency)

		for username, userData := range key.Users {
			if userData.NextBilling != nil {
				var nextBilling int64
				switch v := userData.NextBilling.(type) {
				case float64:
					nextBilling = int64(v)
				case int64:
					nextBilling = v
				case int:
					nextBilling = int64(v)
				default:
					log.Printf("  User %s: Invalid NextBilling type", username)
					continue
				}

				nextBillingTime := time.Unix(nextBilling/1000, 0)
				timeUntilBilling := time.Until(nextBillingTime)

				log.Printf("  User %s: Price %.2f, Next billing %s (in %s)",
					username, float64(userData.Price),
					nextBillingTime.Format("2006-01-02 15:04:05"),
					timeUntilBilling.String())
			} else {
				log.Printf("  User %s: No NextBilling set", username)
			}
		}
	}

	log.Printf("Total subscriptions: %d", subscriptionCount)
	log.Println("=== END SUBSCRIPTION DEBUG ===")
}
