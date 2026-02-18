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

type ErrorResponse struct {
	Error string `json:"error"`
}

type keyCreationResp struct {
	Status       string        `json:"status,omitempty"`
	Key          string        `json:"key,omitempty"`
	Type         string        `json:"type,omitempty"`
	Price        int           `json:"price,omitempty"`
	Subscription *Subscription `json:"subscription,omitempty"`
}

func createKey(c *gin.Context) {
	user := c.MustGet("user").(*User)

	name := c.Query("name")
	if name == "" {
		c.JSON(400, ErrorResponse{Error: "Key name is required"})
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
	userId := user.GetUsername().Id()
	total_keys := 0
	for _, key := range keys {
		if key.Creator == userId {
			total_keys++
		}
	}
	if total_keys > max_keys {
		c.JSON(400, ErrorResponse{Error: fmt.Sprintf("You can only have up to %d free keys", max_keys)})
		return
	}

	newKey := Key{
		Key:         generateToken(),
		Creator:     userId,
		Users:       make(map[UserId]KeyUserData),
		Name:        name,
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
	newKey.Users[user.GetId()] = KeyUserData{
		Time: time.Now().Unix(),
	}

	keys = append(keys, newKey)

	go saveKeys()

	c.JSON(200, keyCreationResp{
		Status:       "Key created successfully",
		Key:          newKey.Key,
		Type:         newKey.Type,
		Price:        newKey.Price,
		Subscription: newKey.Subscription,
	})
}

func getMyKeys(c *gin.Context) {
	user := c.MustGet("user").(*User)

	userId := user.GetId()

	keysMutex.RLock()
	userKeys := make([]NetKey, 0)
	for _, key := range keys {
		if _, hasAccess := key.Users[userId]; hasAccess {
			users := make(map[Username]KeyUserData)
			if key.Creator != userId {
				key.Data = nil
				key.TotalIncome = 0
			} else {
				for k, v := range key.Users {
					users[k.User().GetUsername()] = v
				}
			}
			userKeys = append(userKeys, key.ToNet())
		}
	}
	keysMutex.RUnlock()

	c.JSON(200, userKeys)
}

func checkKey(c *gin.Context) {
	username := Username(c.Param("username"))
	userId := username.Id()
	keyToCheck := c.Query("key")

	if keyToCheck == "" {
		c.JSON(400, gin.H{"error": "Key is required"})
		return
	}

	hasKey := doesUserOwnKey(userId, keyToCheck)

	c.JSON(200, gin.H{
		"owned":    hasKey,
		"username": username,
		"key":      keyToCheck,
	})
}

func revokeKey(c *gin.Context) {
	id := c.Param("id")
	user := c.MustGet("user").(*User)

	targetId := Username(c.Query("user")).Id()
	if !accountExists(targetId) {
		c.JSON(400, gin.H{"error": "Target user not found"})
		return
	}

	keysMutex.Lock()
	defer keysMutex.Unlock()

	for i := range keys {
		if keys[i].Key == id {
			if keys[i].Creator != user.GetId() {
				c.JSON(403, gin.H{"error": "You can only revoke access to keys you created"})
				return
			}
			if targetId == keys[i].Creator {
				c.JSON(400, gin.H{"error": "You cannot revoke access from the key creator"})
				return
			}

			delete(keys[i].Users, targetId)

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
			if key.Creator != user.GetId() {
				c.JSON(403, ErrorResponse{Error: "You can only delete keys you created"})
				return
			}

			// Remove the key
			keys = append(keys[:i], keys[i+1:]...)

			go saveKeys()

			c.JSON(200, gin.H{"status": "Key deleted successfully"})
			return
		}
	}

	c.JSON(404, ErrorResponse{Error: "Key not found"})
}

func updateKey(c *gin.Context) {
	id := c.Param("id")
	key := c.Query("key")
	data := c.Query("data")
	user := c.MustGet("user").(*User)
	if key == "" {
		c.JSON(403, ErrorResponse{Error: "update key and data are required"})
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
			if keys[i].Creator != user.GetId() {
				c.JSON(403, ErrorResponse{Error: "You can only update keys you created"})
				return
			}

			keys[i].setKey(key, parsedData)

			go saveKeys()

			c.JSON(200, gin.H{"status": "Key updated successfully"})
			return
		}
	}

	c.JSON(404, ErrorResponse{Error: "Key not found"})
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
			if keys[i].Creator != user.GetId() {
				c.JSON(403, ErrorResponse{Error: "You can only rename keys you created"})
				return
			}

			keys[i].Name = name

			go saveKeys()

			c.JSON(200, gin.H{"status": "Key name updated successfully"})
			return
		}
	}

	c.JSON(404, ErrorResponse{Error: "Key not found"})
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

	c.JSON(404, ErrorResponse{Error: "Key not found"})
}

func adminAddUserToKey(c *gin.Context) {
	id := c.Param("id")
	user := c.MustGet("user").(*User)

	targetUser := Username(c.Query("user"))
	if targetUser == "" {
		targetUser = Username(c.Query("username"))
	}
	if targetUser == "" {
		c.JSON(400, ErrorResponse{Error: "Target user is required"})
		return
	}
	targetId := targetUser.Id()
	if !accountExists(targetId) {
		c.JSON(400, ErrorResponse{Error: "Target user not found"})
		return
	}

	keysMutex.Lock()
	defer keysMutex.Unlock()

	for i := range keys {
		if keys[i].Key == id {
			if keys[i].Creator != user.GetId() {
				c.JSON(403, ErrorResponse{Error: "You can only add users to your own keys"})
				return
			}
			keys[i].Users[targetId] = KeyUserData{
				Time: time.Now().Unix(),
			}

			go saveKeys()

			c.JSON(200, gin.H{"status": "User added to key successfully"})
			return
		}
	}

	c.JSON(404, ErrorResponse{Error: "Key not found"})
}

func adminRemoveUserFromKey(c *gin.Context) {
	id := c.Param("id")
	user := c.MustGet("user").(*User)

	targetUser := Username(c.Query("user"))
	if targetUser == "" {
		targetUser = Username(c.Query("username"))
	}
	if targetUser == "" {
		c.JSON(400, ErrorResponse{Error: "Target user is required"})
		return
	}
	targetId := targetUser.Id()
	if !accountExists(targetId) {
		c.JSON(400, ErrorResponse{Error: "Target user not found"})
		return
	}

	keysMutex.Lock()
	defer keysMutex.Unlock()

	for i := range keys {
		if keys[i].Key == id {
			if keys[i].Creator != user.GetId() {
				c.JSON(403, ErrorResponse{Error: "You can only remove users from your own keys"})
				return
			}
			delete(keys[i].Users, targetId)

			go saveKeys()

			c.JSON(200, gin.H{"status": "User removed from key successfully"})
			return
		}
	}

	c.JSON(404, ErrorResponse{Error: "Key not found"})
}

func buyKey(c *gin.Context) {
	id := c.Param("id")
	user := c.MustGet("user").(*User)

	keysMutex.Lock()
	defer keysMutex.Unlock()

	for i := range keys {
		if keys[i].Key == id {
			if keys[i].Price < 0 {
				c.JSON(400, ErrorResponse{Error: "Key is not for sale"})
				return
			}

			userId := user.GetId()
			if _, hasAccess := keys[i].Users[userId]; hasAccess {
				c.JSON(400, ErrorResponse{Error: "You already have access to this key"})
				return
			}

			var balance = user.GetCredits()
			if balance < float64(keys[i].Price) {
				c.JSON(400, ErrorResponse{Error: "Insufficient balance to buy this key"})
				return
			}

			// Add user to key
			userData := KeyUserData{
				Time:  time.Now().Unix(),
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

			keys[i].Users[userId] = userData

			// Update total income for the key
			keys[i].TotalIncome += keys[i].Price

			go saveKeys()

			// Deduct the price from user's balance
			usersMutex.Lock()
			userIndex := -1
			for j, u := range users {
				if u.GetId() == userId {
					userIndex = j
					break
				}
			}

			ownerIndex := -1
			for j, u := range users {
				if u.GetId() == keys[i].Creator {
					ownerIndex = j
					break
				}
			}
			usersMutex.Unlock()

			if userIndex != -1 {
				// Flexible extraction for sys.currency
				newBal := user.GetCredits() - float64(keys[i].Price)
				user.SetBalance(newBal)
				user.addTransaction(Transaction{
					Note:      "key purchase",
					User:      user.GetId(),
					Amount:    float64(keys[i].Price),
					Type:      "key_buy",
					NewTotal:  newBal,
					Timestamp: time.Now().UnixMilli(),
					KeyName:   keys[i].Name,
					KeyId:     keys[i].Key,
				})

				// Pay the creator
				if ownerIndex != -1 && ownerIndex != userIndex {
					owner, _ := getUserByIdx(ownerIndex)
					var ownerCurrency float64 = owner.GetCredits()
					// 10% tax on purchase
					value := float64(keys[i].Price) * 0.9
					newBal := ownerCurrency + value
					owner.SetBalance(newBal)
					owner.addTransaction(Transaction{
						Note:      "key purchase",
						User:      user.GetId(),
						Amount:    float64(keys[i].Price),
						Type:      "key_sale",
						NewTotal:  newBal,
						Timestamp: time.Now().UnixMilli(),
						KeyName:   keys[i].Name,
						KeyId:     keys[i].Key,
					})
				}

				if len(*keys[i].Webhook) > 0 {
					username := user.GetUsername()
					_ = sendWebhook(*keys[i].Webhook, map[string]any{
						"username":  username,    // purchaser
						"key":       keys[i].Key, // id
						"price":     keys[i].Price,
						"content":   string(username) + " purchased key " + keys[i].Key + " for " + strconv.Itoa(keys[i].Price) + " credits",
						"timestamp": time.Now().Unix(),
					})
				}

				go saveUsers()
			} else {
				c.JSON(500, ErrorResponse{Error: "User not found in users list"})
				return
			}

			c.JSON(200, gin.H{"message": "Key purchased successfully"})
			return
		}
	}

	c.JSON(404, ErrorResponse{Error: "Key not found"})
}

func cancelKey(c *gin.Context) {
	id := c.Param("id")
	user := c.MustGet("user").(*User)

	userId := user.GetId()

	keysMutex.Lock()
	defer keysMutex.Unlock()

	for i := range keys {
		if keys[i].Key != id {
			continue
		}

		userData, ok := keys[i].Users[userId]
		if !ok {
			c.JSON(404, ErrorResponse{Error: "You don't have this key"})
			return
		}

		// Subscription keys: keep access until next billing date, then remove.
		if keys[i].Subscription != nil {
			if userData.NextBilling == nil {
				c.JSON(400, ErrorResponse{Error: "This subscription has no next_billing date"})
				return
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
				c.JSON(400, ErrorResponse{Error: "Invalid next_billing type"})
				return
			}

			// We always store cancel_at as unix ms to match other per-user billing fields.
			if nextBilling < 10_000_000_000 {
				// looks like seconds; convert to ms
				nextBilling = nextBilling * 1000
			}

			userData.CancelAt = nextBilling
			keys[i].Users[userId] = userData
			go saveKeys()

			c.JSON(200, gin.H{
				"status":    "Cancellation scheduled",
				"cancel_at": nextBilling,
			})
			return
		}

		// Non-subscription keys: cancel immediately.
		delete(keys[i].Users, userId)
		go saveKeys()
		c.JSON(200, gin.H{"status": "Cancelled"})
		return
	}

	c.JSON(404, gin.H{"error": "Key not found"})
}

func debugSubscriptionsEndpoint(c *gin.Context) {
	user := c.MustGet("user").(*User)

	// Only allow admin users to access debug info
	if user.GetUsername().ToLower() != "mist" {
		c.JSON(403, gin.H{"error": "Admin access required"})
		return
	}

	debugSubscriptions()
	c.JSON(200, gin.H{"message": "Subscription debug info logged to console"})
}

func computeNextBilling(subscription *Subscription) time.Time {
	frequency := subscription.Frequency
	if frequency == 0 {
		frequency = 1
	}
	period := subscription.Period
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
	return nextBillingTime
}

func checkSubscriptions() {
	ticker := time.NewTicker(time.Duration(SUBSCRIPTION_CHECK_INTERVAL) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		log.Println("Checking subscriptions...")

		keysMutex.Lock()
		subscriptionsProcessed := 0
		chargesProcessed := 0

		usersDirty := false

		for keyIndex := range keys {
			key := &keys[keyIndex]
			if key.Subscription == nil {
				continue
			}

			subscriptionsProcessed++
			usersToRemove := make([]UserId, 0)

			owner, err := getAccountByUserId(key.Creator)
			if err != nil {
				continue
			}

			ownerId := owner.GetId()

			for userId, userData := range key.Users {
				if userData.NextBilling == nil {
					continue
				}

				// If user scheduled cancellation and we've reached that time, remove them now.
				if userData.CancelAt != nil {
					var cancelAt int64
					switch v := userData.CancelAt.(type) {
					case float64:
						cancelAt = int64(v)
					case int64:
						cancelAt = v
					case int:
						cancelAt = int64(v)
					}
					if cancelAt > 0 {
						if cancelAt < 10_000_000_000 {
							cancelAt = cancelAt * 1000
						}
						if time.Now().UnixMilli() >= cancelAt {
							usersToRemove = append(usersToRemove, userId)
							continue
						}
					}
				}

				username := userId.User().GetUsername()

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

						purchaser, err := getAccountByUsername(username)

						if err != nil {
							log.Printf("User %s not found for key %s", username, key.Key)
							usersToRemove = append(usersToRemove, userId)
							continue
						}

						if purchaser.GetId() == ownerId {
							nextBillingTime := computeNextBilling(key.Subscription)
							userData.NextBilling = nextBillingTime.UnixMilli()
							key.Users[userId] = userData
							continue
						}

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
							usersToRemove = append(usersToRemove, userId)
							continue
						}
						currencyFloat -= price
						purchaser.SetBalance(currencyFloat)
						purchaser.addTransaction(Transaction{
							Note:      "key purchase",
							User:      key.Creator,
							Amount:    price,
							Type:      "key_buy",
							NewTotal:  currencyFloat,
							Timestamp: time.Now().UnixMilli(),
							KeyName:   key.Name,
							KeyId:     key.Key,
						})

						// 10% tax on purchase
						value := price * 0.9
						newBal := owner.GetCredits() + value
						owner.SetBalance(newBal)
						owner.addTransaction(Transaction{
							Note:      "key purchase",
							User:      username.Id(),
							Amount:    value,
							Type:      "key_sale",
							NewTotal:  newBal,
							Timestamp: time.Now().UnixMilli(),
							KeyName:   key.Name,
							KeyId:     key.Key,
						})
						usersDirty = true

						// Update total income for the key
						key.TotalIncome += userData.Price

						nextBillingTime := computeNextBilling(key.Subscription)

						newNextBilling := nextBillingTime.UnixMilli()
						userData.NextBilling = newNextBilling
						key.Users[userId] = userData

						if key.Webhook != nil && len(*key.Webhook) > 0 {
							_ = sendWebhook(*key.Webhook, map[string]any{
								"username":  username, // purchaser
								"key":       key.Key,  // id
								"price":     key.Price,
								"content":   string(username) + " was charged by key: " + key.Key + " for " + strconv.Itoa(key.Price) + " credits",
								"timestamp": time.Now().Unix(),
							})
						}

						log.Printf("Successfully billed user %s for key %s. Next billing: %s",
							username, key.Key, nextBillingTime.Format("2006-01-02 15:04:05"))
						chargesProcessed++
					}
				}
			}

			for _, username := range usersToRemove {
				delete(key.Users, username)
				log.Printf("Removed user %s from key %s due to payment failure", username, key.Key)
			}
		}

		if usersDirty {
			go saveUsers()
		}

		log.Printf("Subscription check completed: %d keys with subscriptions checked, %d charges processed", subscriptionsProcessed, chargesProcessed)
		keysMutex.Unlock()

		if len(keys) > 0 {
			saveKeys()
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
