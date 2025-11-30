package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

// webhook that kofi sends to us when transactions are made.
func handleKofiTransaction(c *gin.Context) {
	data := c.PostForm("data")
	if data == "" {
		c.JSON(400, gin.H{"error": "No data found"})
		return
	}

	parsedData := make(map[string]any)
	err := json.Unmarshal([]byte(data), &parsedData)
	if err != nil {
		c.JSON(400, gin.H{"error": "Invalid data format"})
		return
	}
	verification := parsedData["verification_token"]
	fmt.Println("Verification: " + getStringOrEmpty(verification))
	if verification != os.Getenv("KOFI_CODE") {
		c.JSON(400, gin.H{"error": "Invalid verification code"})
		return
	}
	fmt.Println(JSONStringify(parsedData))

	email := getStringOrEmpty(parsedData["email"])

	foundBy := "None"
	accounts := []User{}
	var account User = nil

	discord_id := getStringOrEmpty(parsedData["discord_id"])
	if discord_id != "" {
		accounts, err = getAccountsBy("discord_id", discord_id, -1)
		if err == nil {
			foundBy = "Discord"
		}
	}
	if email != "" && len(accounts) == 0 {
		accounts, err = getAccountsBy("email", email, -1)
		if err == nil {
			foundBy = "Email"
		}
	}
	accountInfo := "No linked account found"
	if len(accounts) > 0 {
		account = accounts[0]
		accountInfo = fmt.Sprintf("**Username:** %s", account.GetUsername())
	}

	switch getStringOrEmpty(parsedData["type"]) {
	case "Donation":
		// TODO: handle donations
	case "Subscription":
		name := getStringOrEmpty(parsedData["tier_name"])
		if name == "" {
			// this exits if its a monthly donation, subscriptions have teir_name
			c.JSON(400, gin.H{"error": "No tier name found"})
			return
		}

		sendDiscordWebhook([]map[string]any{
			{
				"title": "New Subscription",
				"description": fmt.Sprintf(
					"**From:** %s\n**Amount:** %s %s\n**Message:** %s\n**Email:** %s\n\n[View on Ko-fi](%s)\n**Found By:** %s\n\n%s",
					parsedData["from_name"],
					parsedData["amount"],
					parsedData["currency"],
					parsedData["message"],
					parsedData["email"],
					parsedData["url"],
					foundBy,
					accountInfo,
				),
				"timestamp": time.Now().Format(time.RFC3339),
			},
		})

		if len(accounts) == 0 {
			c.JSON(400, gin.H{"error": fmt.Sprintf("No accounts found for %s", foundBy)})
			return
		}
		account := accounts[0]
		sub := account.GetSubscription()
		sub.Tier = name
		sub.Active = true
		sub.Next_billing = int64(time.Now().Add(time.Hour * 24 * 30).UnixMilli())
		account.SetSubscription(sub)
		go saveUsers()

		c.JSON(200, gin.H{"status": "success"})
	case "Shop Order":
		shop_items := parsedData["shop_items"].([]any)
		for _, shop_item := range shop_items {
			item := shop_item.(map[string]any)
			sendDiscordWebhook([]map[string]any{
				{
					"title": "New Shop Order",
					"description": fmt.Sprintf("**User:** %s\n**Amount:** %s %s\n**Message:** %s\n**Email:** %s\n\n[View on Ko-fi](%s)\n**Found By:** %s\n\n%s",
						parsedData["from_name"],
						parsedData["amount"],
						parsedData["currency"],
						parsedData["message"],
						parsedData["email"],
						parsedData["url"],
						foundBy,
						accountInfo,
					),
					"timestamp": time.Now().Format(time.RFC3339),
				},
			})
			switch item["direct_link_code"].(string) {
			case "eebeb7269f":
				// add 100 rotur credits to the user
				account.SetBalance(float64(account.GetCredits()) + 30)
				go saveUsers()
			}
		}
	}

	c.JSON(200, gin.H{"status": "success"})
}

func setSubscription(c *gin.Context) {
	if !authenticateAdmin(c) {
		return
	}

	var data map[string]any
	if err := c.ShouldBindJSON(&data); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request body"})
		return
	}

	username := data["username"].(string)
	tier := data["tier"].(string)

	if username == "" || tier == "" {
		c.JSON(400, gin.H{"error": "Username and tier are required"})
		return
	}

	users, err := getAccountsBy("username", username, -1)
	if err != nil {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}
	user := users[0]
	user.SetSubscription(subscription{
		Tier:         tier,
		Active:       true,
		Next_billing: int64(time.Now().Add(time.Hour * 24 * 30).UnixMilli()),
	})
	go saveUsers()

	c.JSON(200, gin.H{"message": "Subscription updated successfully"})
}
