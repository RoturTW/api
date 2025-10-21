package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	url        = "https://check.torproject.org/torbulkexitlist"
	outputPath = "./torips.json"
	maxAge     = 30 * time.Minute
)

var torips = []string{}

var mu sync.Mutex
var updating bool

func StartAutoUpdater(interval time.Duration) {
	go func() {
		for {
			UpdateIfNeeded()
			time.Sleep(interval)
		}
	}()
}

func UpdateIfNeeded() {
	mu.Lock()
	if updating {
		mu.Unlock()
		return
	}
	updating = true
	mu.Unlock()

	defer func() {
		mu.Lock()
		updating = false
		mu.Unlock()
	}()

	if info, err := os.Stat(outputPath); err == nil {
		if time.Since(info.ModTime()) < maxAge {
			log.Println("[torips] File is fresh, skipping update.")
			return
		}
	}

	log.Println("[torips] Fetching Tor exit node list...")
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("[torips] Fetch error: %v", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[torips] Read error: %v", err)
		return
	}

	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	torips = lines
	tmpFile := outputPath + ".tmp"
	file, err := os.Create(tmpFile)
	if err != nil {
		log.Printf("[torips] File create error: %v", err)
		return
	}
	defer file.Close()

	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	if err := enc.Encode(lines); err != nil {
		log.Printf("[torips] JSON encode error: %v", err)
		return
	}

	if err := os.Rename(tmpFile, outputPath); err != nil {
		log.Printf("[torips] Rename error: %v", err)
		return
	}

	log.Printf("[torips] Saved %d IPs to %s", len(lines), outputPath)
}
