package main

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Item handlers
func transferItem(c *gin.Context) {
	name := strings.ToLower(c.Param("name"))

	user := c.MustGet("user").(*User)

	targetUsername := c.Query("username")
	if targetUsername == "" {
		targetUsername = c.Query("to")
	}
	if targetUsername == "" {
		c.JSON(400, gin.H{"error": "Target username is required"})
		return
	}

	// Check if target user exists
	idx := getIdxOfAccountBy("username", strings.ToLower(targetUsername))
	if idx == -1 {
		c.JSON(404, gin.H{"error": "Target user not found"})
		return
	}

	if strings.EqualFold(user.GetUsername(), targetUsername) {
		c.JSON(400, gin.H{"error": "You cannot transfer an item to yourself"})
		return
	}

	itemsMutex.Lock()
	defer itemsMutex.Unlock()

	var targetItem *Item
	for i := range items {
		if strings.ToLower(items[i].Name) == name {
			targetItem = &items[i]
			break
		}
	}

	if targetItem == nil {
		c.JSON(404, gin.H{"error": "Item not found"})
		return
	}

	if !strings.EqualFold(targetItem.Owner, user.GetUsername()) {
		c.JSON(403, gin.H{"error": "You are not authorized to transfer this item"})
		return
	}

	// Transfer the item
	oldOwner := targetItem.Owner
	targetItem.Owner = strings.ToLower(targetUsername)

	// Add transfer history
	transferRecord := TransferHistory{
		From:      &oldOwner,
		To:        strings.ToLower(targetUsername),
		Timestamp: float64(time.Now().Unix()),
		Type:      "transfer",
	}
	targetItem.TransferHistory = append(targetItem.TransferHistory, transferRecord)

	go saveItems()

	// Notify target user
	addUserEvent(strings.ToLower(targetUsername), "item_received", map[string]any{
		"item_name":     targetItem.Name,
		"from":          user.GetUsername(),
		"transfer_type": "transfer",
	})

	c.JSON(200, gin.H{
		"message": "Item '" + targetItem.Name + "' transferred successfully to " + targetUsername,
	})
}

func buyItem(c *gin.Context) {
	name := strings.ToLower(c.Param("name"))

	user := c.MustGet("user").(*User)

	itemsMutex.Lock()
	defer itemsMutex.Unlock()

	var targetItem *Item
	for i := range items {
		if strings.ToLower(items[i].Name) == name {
			targetItem = &items[i]
			break
		}
	}

	if targetItem == nil {
		c.JSON(404, gin.H{"error": "Item not found"})
		return
	}

	// Check if item is for sale
	if !targetItem.Selling {
		c.JSON(400, gin.H{"error": "Item is not for sale"})
		return
	}

	// Check if user is trying to buy their own item
	if strings.EqualFold(user.GetUsername(), targetItem.Owner) {
		c.JSON(400, gin.H{"error": "You cannot buy your own item"})
		return
	}

	// Check if user has enough currency
	userCurrency := user.GetCredits()

	if userCurrency < float64(targetItem.Price) {
		c.JSON(403, gin.H{"error": "Insufficient currency"})
		return
	}

	// Process the purchase
	oldOwner := targetItem.Owner
	targetItem.Owner = strings.ToLower(user.GetUsername())
	targetItem.Selling = false // Remove from sale after purchase

	// Add transfer history
	transferRecord := TransferHistory{
		From:      &oldOwner,
		To:        strings.ToLower(user.GetUsername()),
		Timestamp: float64(time.Now().Unix()),
		Type:      "purchase",
		Price:     &targetItem.Price,
	}
	targetItem.TransferHistory = append(targetItem.TransferHistory, transferRecord)

	// Update total income for the item
	targetItem.TotalIncome += targetItem.Price

	go saveItems()

	user.SetBalance(userCurrency - float64(targetItem.Price))
	go saveUsers()

	// Notify both users
	addUserEvent(oldOwner, "item_sold", map[string]any{
		"item_name": targetItem.Name,
		"buyer":     user.GetUsername(),
		"price":     targetItem.Price,
	})

	addUserEvent(strings.ToLower(user.GetUsername()), "item_purchased", map[string]any{
		"item_name": targetItem.Name,
		"seller":    oldOwner,
		"price":     targetItem.Price,
	})

	c.JSON(200, gin.H{
		"message": "Item '" + targetItem.Name + "' purchased successfully",
	})
}

func stopSellingItem(c *gin.Context) {
	name := strings.ToLower(c.Param("name"))

	user := c.MustGet("user").(*User)

	itemsMutex.Lock()
	defer itemsMutex.Unlock()

	var targetItem *Item
	for i := range items {
		if strings.ToLower(items[i].Name) == name {
			targetItem = &items[i]
			break
		}
	}

	if targetItem == nil {
		c.JSON(404, gin.H{"error": "Item not found"})
		return
	}

	if !strings.EqualFold(targetItem.Owner, user.GetUsername()) {
		c.JSON(403, gin.H{"error": "You are not authorized to modify this item"})
		return
	}

	targetItem.Selling = false
	go saveItems()

	c.JSON(200, gin.H{"message": "Item removed from sale"})
}

func setItemPrice(c *gin.Context) {
	name := strings.ToLower(c.Param("name"))

	user := c.MustGet("user").(*User)

	priceStr := c.Query("price")
	if priceStr == "" {
		c.JSON(400, gin.H{"error": "Price is required"})
		return
	}

	newPrice, err := strconv.Atoi(priceStr)
	if err != nil {
		c.JSON(400, gin.H{"error": "Invalid price"})
		return
	}

	if newPrice < 0 {
		c.JSON(400, gin.H{"error": "Price cannot be negative"})
		return
	}

	itemsMutex.Lock()
	defer itemsMutex.Unlock()

	var targetItem *Item
	for i := range items {
		if strings.ToLower(items[i].Name) == name {
			targetItem = &items[i]
			break
		}
	}

	if targetItem == nil {
		c.JSON(404, gin.H{"error": "Item not found"})
		return
	}

	if !strings.EqualFold(targetItem.Owner, user.GetUsername()) {
		c.JSON(403, gin.H{"error": "You are not authorized to modify this item"})
		return
	}

	targetItem.Price = newPrice
	go saveItems()

	c.JSON(200, gin.H{
		"message": "Item price updated to " + strconv.Itoa(newPrice),
	})
}

func createItem(c *gin.Context) {
	user := c.MustGet("user").(*User)

	itemStr := c.Query("item")
	if itemStr == "" {
		c.JSON(400, gin.H{"error": "Item data is required"})
		return
	}

	var itemData map[string]any
	if err := json.Unmarshal([]byte(itemStr), &itemData); err != nil {
		c.JSON(400, gin.H{"error": "Invalid item data"})
		return
	}

	// Check if item name contains only ASCII characters
	itemName, ok := itemData["name"].(string)
	if !ok || itemName == "" {
		c.JSON(400, gin.H{"error": "Item name is required"})
		return
	}

	// Check ASCII characters
	for _, r := range itemName {
		if r > 127 {
			c.JSON(400, gin.H{"error": "Item name must contain only ASCII characters"})
			return
		}
	}

	// Check if item name already exists (case-insensitive)
	itemNameLower := strings.ToLower(itemName)
	itemsMutex.RLock()
	for _, existingItem := range items {
		if strings.ToLower(existingItem.Name) == itemNameLower {
			itemsMutex.RUnlock()
			c.JSON(400, gin.H{"error": "Item with this name already exists"})
			return
		}
	}
	itemsMutex.RUnlock()

	// Extract and validate fields
	description, _ := itemData["description"].(string)

	var price int
	if priceVal, ok := itemData["price"]; ok {
		switch v := priceVal.(type) {
		case float64:
			price = int(v)
		case int:
			price = v
		}
	}

	if price < 0 {
		c.JSON(400, gin.H{"error": "Price cannot be negative"})
		return
	}

	selling, _ := itemData["selling"].(bool)

	newItem := Item{
		Name:        itemName,
		Description: description,
		Price:       price,
		Selling:     selling,
		Author:      strings.ToLower(user.GetUsername()),
		Owner:       strings.ToLower(user.GetUsername()),
		PrivateData: itemData["data"],
		Created:     float64(time.Now().Unix()),
		TransferHistory: []TransferHistory{{
			From:      nil,
			To:        strings.ToLower(user.GetUsername()),
			Timestamp: float64(time.Now().Unix()),
			Type:      "creation",
		}},
		TotalIncome: 0,
	}

	itemsMutex.Lock()
	items = append(items, newItem)
	itemsMutex.Unlock()

	go saveItems()

	c.JSON(201, newItem)
}

func getItem(c *gin.Context) {
	name := strings.ToLower(c.Param("name"))

	itemsMutex.RLock()
	var targetItem *Item
	for _, item := range items {
		if strings.ToLower(item.Name) == name {
			targetItem = &item
			break
		}
	}
	itemsMutex.RUnlock()

	if targetItem == nil {
		c.JSON(404, gin.H{"error": "Item not found"})
		return
	}

	itemToReturn := *targetItem

	authKey := c.Query("auth")
	var user *User
	if authKey != "" {
		user = authenticateWithKey(authKey)
	}

	// Hide private data unless user is the owner
	if user == nil || !strings.EqualFold(user.GetUsername(), targetItem.Owner) {
		itemToReturn.PrivateData = nil
	}

	c.JSON(200, itemToReturn)
}

func deleteItem(c *gin.Context) {
	name := strings.ToLower(c.Param("name"))

	user := c.MustGet("user").(*User)

	itemsMutex.Lock()
	defer itemsMutex.Unlock()

	var targetItem *Item
	newItems := make([]Item, 0)

	for _, item := range items {
		if strings.ToLower(item.Name) == name {
			targetItem = &item
			// Check authorization
			if !strings.EqualFold(user.GetUsername(), item.Owner) {
				c.JSON(403, gin.H{"error": "You are not authorized to delete this item"})
				return
			}
		} else {
			newItems = append(newItems, item)
		}
	}

	if targetItem == nil {
		c.JSON(404, gin.H{"error": "Item not found"})
		return
	}

	items = newItems
	go saveItems()

	c.JSON(200, gin.H{"message": "Item deleted successfully"})
}

func listItems(c *gin.Context) {
	username := strings.ToLower(c.Param("username"))

	itemsMutex.RLock()
	userItems := make([]Item, 0)
	for _, item := range items {
		if strings.ToLower(item.Owner) == username {
			// Remove private data before returning
			itemCopy := item
			itemCopy.PrivateData = nil
			userItems = append(userItems, itemCopy)
		}
	}
	itemsMutex.RUnlock()

	c.JSON(200, userItems)
}

func updateItem(c *gin.Context) {
	name := strings.ToLower(c.Param("name"))

	user := c.MustGet("user").(*User)

	newDataStr := c.Query("data")
	if newDataStr == "" {
		c.JSON(400, gin.H{"error": "New data is required"})
		return
	}

	var newData map[string]any
	if err := json.Unmarshal([]byte(newDataStr), &newData); err != nil {
		c.JSON(400, gin.H{"error": "Invalid data"})
		return
	}

	itemsMutex.Lock()
	defer itemsMutex.Unlock()

	var targetItem *Item
	for i := range items {
		if strings.ToLower(items[i].Name) == name {
			targetItem = &items[i]
			break
		}
	}

	if targetItem == nil {
		c.JSON(404, gin.H{"error": "Item not found"})
		return
	}

	if !strings.EqualFold(targetItem.Owner, user.GetUsername()) {
		c.JSON(403, gin.H{"error": "You are not authorized to update this item"})
		return
	}

	// Update allowed fields
	if description, ok := newData["description"].(string); ok {
		targetItem.Description = description
	}
	if privateData, ok := newData["private_data"]; ok {
		targetItem.PrivateData = privateData
	}

	go saveItems()

	c.JSON(200, *targetItem)
}

func sellItem(c *gin.Context) {
	name := strings.ToLower(c.Param("name"))

	user := c.MustGet("user").(*User)

	itemsMutex.Lock()
	defer itemsMutex.Unlock()

	var targetItem *Item
	for i := range items {
		if strings.ToLower(items[i].Name) == name {
			targetItem = &items[i]
			break
		}
	}

	if targetItem == nil {
		c.JSON(404, gin.H{"error": "Item not found"})
		return
	}

	if !strings.EqualFold(targetItem.Owner, user.GetUsername()) {
		c.JSON(403, gin.H{"error": "You are not authorized to sell this item"})
		return
	}

	targetItem.Selling = true
	go saveItems()

	c.JSON(200, gin.H{"message": "Item is now for sale"})
}

func getSellingItems(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "50")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	itemsMutex.RLock()
	sellingItems := make([]Item, 0)
	for _, item := range items {
		if item.Price > 0 && item.Selling {
			// Remove private data
			itemCopy := item
			itemCopy.PrivateData = nil
			sellingItems = append(sellingItems, itemCopy)
		}
	}
	itemsMutex.RUnlock()

	// Get last 'limit' items and reverse
	var result = make([]Item, 0)
	if len(sellingItems) > limit {
		result = sellingItems[len(sellingItems)-limit:]
	} else {
		result = sellingItems
	}

	// Reverse to show newest first
	for i := len(result)/2 - 1; i >= 0; i-- {
		opp := len(result) - 1 - i
		result[i], result[opp] = result[opp], result[i]
	}

	c.JSON(200, result)
}

func adminAddUserToItem(c *gin.Context) {
	itemID := c.Param("id")

	user := c.MustGet("user").(*User)

	if strings.ToLower(user.GetUsername()) != "mist" {
		c.JSON(403, gin.H{"error": "Invalid authentication key"})
		return
	}

	username := c.Query("username")
	if username == "" {
		username = c.Query("name")
	}
	if username == "" {
		c.JSON(400, gin.H{"error": "Username is required"})
		return
	}

	itemsMutex.Lock()
	defer itemsMutex.Unlock()

	var targetItem *Item
	for i := range items {
		if items[i].Name == itemID {
			targetItem = &items[i]
			break
		}
	}

	if targetItem == nil {
		c.JSON(404, gin.H{"error": "Item not found"})
		return
	}

	targetItem.Owner = strings.ToLower(username)
	go saveItems()

	c.JSON(200, *targetItem)
}
