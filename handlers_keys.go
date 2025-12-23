package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func createKey(c *gin.Context) {
	user := c.MustGet("user").(*User)

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

	max_keys := user.GetSubscriptionBenefits().Max_Keys

	keysMutex.Lock()
	defer keysMutex.Unlock()

	// Check if key name already exists
	username := strings.ToLower(user.GetUsername())
	total_keys := 0
	for _, key := range keys {
		if strings.EqualFold(key.Creator, username) {
			total_keys++
		}
	}
	if total_keys > max_keys {
		c.JSON(400, gin.H{"error": fmt.Sprintf("You can only have up to %d free keys", max_keys)})
		return
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
	user := c.MustGet("user").(*User)

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
	user := c.MustGet("user").(*User)

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
	user := c.MustGet("user").(*User)

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
	key := c.Query("key")
	data := c.Query("data")
	user := c.MustGet("user").(*User)
	if key == "" {
		c.JSON(403, gin.H{"error": "update key and data are required"})
		return
	}

	var parsedData any
	if isValidJSON(data) {
		json.Unmarshal([]byte(data), &parsedData)
	} else {
		parsedData = data
	}

	keysMutex.Lock()
	defer keysMutex.Unlock()

	for i := range keys {
		if keys[i].Key == id {
			if !strings.EqualFold(keys[i].Creator, user.GetUsername()) {
				c.JSON(403, gin.H{"error": "You can only update keys you created"})
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
	user := c.MustGet("user").(*User)

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
			c.JSON(200, key.ToPublic())
			return
		}
	}

	c.JSON(404, gin.H{"error": "Key not found"})
}

func adminAddUserToKey(c *gin.Context) {
	id := c.Param("id")
	user := c.MustGet("user").(*User)

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
			if !strings.EqualFold(keys[i].Creator, user.GetUsername()) {
				c.JSON(403, gin.H{"error": "You can only add users to your own keys"})
				return
			}
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
	user := c.MustGet("user").(*User)

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
			if !strings.EqualFold(keys[i].Creator, user.GetUsername()) {
				c.JSON(403, gin.H{"error": "You can only remove users from your own keys"})
				return
			}
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
	user := c.MustGet("user").(*User)

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

			var balance = user.GetCredits()
			if balance < float64(keys[i].Price) {
				c.JSON(400, gin.H{"error": "Insufficient balance to buy this key"})
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
			usersMutex.Unlock()

			if userIndex != -1 {
				// Flexible extraction for sys.currency
				newBal := user.GetCredits() - float64(keys[i].Price)
				user.SetBalance(newBal)
				user.addTransaction(map[string]any{
					"note":      "key purchase",
					"key_id":    keys[i].Key,
					"key_name":  keys[i].Name,
					"user":      user.GetUsername(),
					"amount":    float64(keys[i].Price),
					"type":      "key_buy",
					"new_total": newBal,
				})

				// Pay the creator
				if ownerIndex != -1 && ownerIndex != userIndex {
					owner, _ := getUserByIdx(ownerIndex)
					var ownerCurrency float64 = owner.GetCredits()
					// 10% tax on purchase
					value := float64(keys[i].Price) * 0.9
					newBal := ownerCurrency + value
					owner.SetBalance(newBal)
					owner.addTransaction(map[string]any{
						"note":      "key purchase",
						"key_id":    keys[i].Key,
						"key_name":  keys[i].Name,
						"user":      user.GetUsername(),
						"amount":    float64(keys[i].Price),
						"type":      "key_sale",
						"new_total": newBal,
					})
				}

				if len(*keys[i].Webhook) > 0 {
					_ = sendWebhook(*keys[i].Webhook, map[string]any{
						"username":  username,    // purchaser
						"key":       keys[i].Key, // id
						"price":     keys[i].Price,
						"content":   username + " purchased key " + keys[i].Key + " for " + strconv.Itoa(keys[i].Price) + " credits",
						"timestamp": time.Now().Unix(),
					})
				}

				go saveUsers()
			} else {
				c.JSON(500, gin.H{"error": "User not found in users list"})
				return
			}

			c.JSON(200, gin.H{"message": "Key purchased successfully"})
			return
		}
	}

	c.JSON(404, gin.H{"error": "Key not found"})
}

func cancelKey(c *gin.Context) {
	id := c.Param("id")
	user := c.MustGet("user").(*User)

	// remove the user from the key
	keysMutex.Lock()
	defer keysMutex.Unlock()

	for i := range keys {
		if keys[i].Key == id {
			delete(keys[i].Users, user.GetUsername())
			c.JSON(200, gin.H{"status": "Successfully cancelled"})
		}
	}

	c.JSON(404, gin.H{"error": "Key not found"})
}

func debugSubscriptionsEndpoint(c *gin.Context) {
	user := c.MustGet("user").(*User)

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

			ownerIndex := getIdxOfAccountBy("username", key.Creator)
			if ownerIndex == -1 {
				continue
			}

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

				log.Printf("User %s in key %s: Next billing %s",
					username, key.Key,
					nextBillingTime.Format("2006-01-02 15:04:05"))

				if currentTimeMs >= nextBilling {
					if userData.Price != 0 {
						log.Printf("Processing subscription payment for %s for key %s (amount: %.2f)", username, key.Key, float64(userData.Price))

						userIndex := getIdxOfAccountBy("username", username)

						if userIndex == -1 {
							log.Printf("User %s not found for key %s", username, key.Key)
							usersToRemove = append(usersToRemove, username)
							continue
						}

						if userIndex == ownerIndex {
							continue
						}

						usersMutex.RLock()
						purchaser := users[userIndex]
						owner := users[ownerIndex]
						usersMutex.RUnlock()

						var currencyFloat float64 = purchaser.GetCredits()
						price := float64(userData.Price)
						if currencyFloat < price {
							log.Printf("User %s does not have enough currency for key %s (needed: %.2f, available: %.2f)",
								username, key.Key, price, currencyFloat)

							// send an event
							go notify("sys.key_lost", map[string]any{
								"username": username,
								"key":      key.Key,
								"key_name": key.Name,
							})
							usersToRemove = append(usersToRemove, username)
							continue
						}
						currencyFloat -= price
						purchaser.SetBalance(currencyFloat)
						purchaser.addTransaction(map[string]any{
							"note":      "key purchase",
							"key_id":    key.Key,
							"key_name":  key.Name,
							"user":      key.Creator,
							"amount":    price,
							"type":      "key_buy",
							"new_total": currencyFloat,
						})

						// 10% tax on purchase
						value := price * 0.9
						newBal := owner.GetCredits() + value
						owner.SetBalance(newBal)
						owner.addTransaction(map[string]any{
							"note":      "key purchase",
							"key_id":    key.Key,
							"key_name":  key.Name,
							"user":      username,
							"amount":    value,
							"type":      "key_sale",
							"new_total": newBal,
						})
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

						if key.Webhook != nil && len(*key.Webhook) > 0 {
							_ = sendWebhook(*key.Webhook, map[string]any{
								"username":  username, // purchaser
								"key":       key.Key,  // id
								"price":     key.Price,
								"content":   username + " was charged by key: " + key.Key + " for " + strconv.Itoa(key.Price) + " credits",
								"timestamp": time.Now().Unix(),
							})
						}

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
