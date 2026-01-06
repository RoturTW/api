package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
)

var (
	usernameAllowedRe = regexp.MustCompile("^[a-z0-9_]+$")

	bannedWordsOnce sync.Once
	bannedWords     []string
	bannedWordsErr  error
)

func loadBannedWordsLocal() ([]string, error) {
	bannedWordsOnce.Do(func() {
		file, err := os.Open("./banned_words.json")
		if err != nil {
			bannedWordsErr = fmt.Errorf("error opening banned_words.json: %w", err)
			return
		}
		defer file.Close()

		var words []string
		if err := json.NewDecoder(file).Decode(&words); err != nil {
			bannedWordsErr = fmt.Errorf("error decoding banned_words.json: %w", err)
			return
		}
		bannedWords = words
	})
	return bannedWords, bannedWordsErr
}

func ValidateUsername(username string) (bool, string) {
	usernameLower := strings.ToLower(username)

	if usernameLower == "" {
		return false, "Username is required"
	}
	if len(usernameLower) < 3 || len(usernameLower) > 20 {
		return false, "Username must be between 3 and 20 characters"
	}
	if strings.Contains(usernameLower, " ") {
		return false, "Username cannot contain spaces"
	}
	if !usernameAllowedRe.MatchString(usernameLower) {
		return false, "Username contains invalid characters"
	}

	words, err := loadBannedWordsLocal()
	if err == nil {
		for _, banned := range words {
			u := strings.ReplaceAll(username, "1", "l")
			u = strings.ReplaceAll(u, "3", "e")
			u = strings.ReplaceAll(u, "5", "s")
			u = strings.ReplaceAll(u, "7", "t")
			u = strings.ReplaceAll(u, "9", "i")
			u = strings.ReplaceAll(u, "0", "o")
			u = strings.ReplaceAll(u, "8", "b")
			u = strings.ReplaceAll(u, "@", "a")

			if strings.Contains(strings.ToLower(u), strings.ToLower(banned)) {
				return false, "Username contains a banned word"
			}
		}
	}

	return true, ""
}

func ValidatePasswordHash(password string) (bool, string) {
	if password == "" {
		return false, "Username and password are required"
	}
	if len(password) != 32 {
		return false, "Invalid password hash"
	}
	if password == "d41d8cd98f00b204e9800998ecf8427e" {
		return false, "Password cannot be empty"
	}
	if regexp.MustCompile("^[a-fA-F0-9]{32}$").FindStringIndex(password) == nil {
		return false, "Invalid password hash"
	}
	return true, ""
}

func IsIpInBannedList(ip string) bool {
	ips := strings.SplitSeq(os.Getenv("BANNED_IPS"), ",")
	for ipAddr := range ips {
		ipAddr = strings.TrimSpace(ipAddr)
		if ipAddr == "" {
			continue
		}
		if strings.EqualFold(ipAddr, ip) {
			return true
		}
	}
	return false
}
