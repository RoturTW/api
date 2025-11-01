package main

import (
	"encoding/json"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

// webhook that kofi sends to us when transactions are made
/* The data is sent (posted) with a content type of application/x-www-form-urlencoded. A field named 'data' contains the payment infomation as a JSON string.

Your listener should return a status code of 200. If we don't receive this status code, we'll retry a reasonable number of times with the same message_id.

The type field will be Donation, Subscription, Commission, or Shop Order.

Monthly subscription payments will have is_subscription_payment set to true.

The first time someone subscribes, is_first_subscription_payment will be true.

If the subscription is for a membership tier, then tier_name will contain the name you have assigned to the tier.
*/
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
	verification := parsedData["verification_code"]
	if verification != os.Getenv("KOFI_CODE") {
		c.JSON(400, gin.H{"error": "Invalid verification code"})
		return
	}

	switch parsedData["type"] {
	case "Donation":
		// TODO: handle donations
	case "Subscription":
		name := getStringOrEmpty(parsedData["tier_name"])
		if name == "" {
			c.JSON(400, gin.H{"error": "No tier name found"})
			return
		}
		email := getStringOrEmpty(parsedData["email"])
		if email == "" {
			c.JSON(400, gin.H{"error": "No email found"})
			return
		}
		accounts, err := getAccountsBy("email", email, -1)
		if err != nil {
			discord_id := getStringOrEmpty(parsedData["discord_id"])
			if discord_id == "" {
				c.JSON(400, gin.H{"error": "No accounts found for email"})
				return
			}
			accounts, err = getAccountsBy("discord_id", discord_id, -1)
			if err != nil {
				c.JSON(400, gin.H{"error": "No accounts found for email or discord_id"})
				return
			}
		}
		if len(accounts) > 1 {
			c.JSON(400, gin.H{"error": "Multiple accounts found for email"})
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
		// TODO: handle shop orders
	}

	c.JSON(200, gin.H{"status": "success"})
}
