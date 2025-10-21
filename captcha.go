package main

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
)

func verifyHCaptcha(token string) bool {
	secret := os.Getenv("HCAPTCHA_SECRET")
	if secret == "" {
		log.Println("⚠️ HCAPTCHA_SECRET not set in environment")
		return false
	}

	form := url.Values{}
	form.Add("secret", secret)
	form.Add("response", token)

	resp, err := http.PostForm("https://hcaptcha.com/siteverify", form)
	if err != nil {
		log.Println("hCaptcha request failed:", err)
		return false
	}
	defer resp.Body.Close()

	var result struct {
		Success    bool     `json:"success"`
		ErrorCodes []string `json:"error-codes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Println("hCaptcha decode error:", err)
		return false
	}

	if !result.Success {
		log.Println("hCaptcha failed:", result.ErrorCodes)
	}
	return result.Success
}
