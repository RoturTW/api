package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

func handleAI(c *gin.Context) {
	auth := c.Query("auth")
	current := authenticateWithKey(auth)
	if current == nil {
		c.JSON(401, gin.H{"error": "Unauthorised"})
		return
	}

	apiURL := "https://api.cerebras.ai/v1/chat/completions"
	apiKey := os.Getenv("CEREBRAS_API_KEY")
	if apiKey == "" {
		c.JSON(500, gin.H{"error": "Missing CEREBRAS_API_KEY in .env"})
		return
	}

	content := c.Query("content")
	user := c.DefaultQuery("user", "Guest")
	returnHistory := c.Query("history") == "1"
	historyData := c.Query("history_data")
	model := c.DefaultQuery("model", "llama3.3-70b")

	var history []map[string]any
	if historyData != "" {
		if err := json.Unmarshal([]byte(historyData), &history); err != nil {
			c.JSON(400, gin.H{"error": "Invalid JSON in 'history_data'"})
			return
		}
	}

	if content == "" && len(history) == 0 {
		c.JSON(400, gin.H{"error": "Missing 'content' or 'history_data'"})
		return
	}

	if content != "" {
		history = append(history, map[string]any{
			"role":    "user",
			"content": content,
		})
	}

	payload := map[string]any{
		"model":    model,
		"messages": history,
	}

	data, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(data))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		c.JSON(resp.StatusCode, gin.H{"error": string(body)})
		return
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		c.JSON(500, gin.H{"error": "Invalid JSON response from Cerebras"})
		return
	}

	choices, ok := parsed["choices"].([]any)
	if !ok || len(choices) == 0 {
		c.JSON(500, gin.H{"error": "No choices returned"})
		return
	}

	msg := choices[0].(map[string]any)["message"].(map[string]any)
	aiResponse := fmt.Sprint(msg["content"])

	history = append(history, map[string]any{
		"role":    "assistant",
		"content": aiResponse,
	})

	fmt.Printf("APPS - USER: %s, AI: %s\n", user, aiResponse)

	if returnHistory {
		c.JSON(200, history)
	} else {
		c.String(200, aiResponse)
	}
}
