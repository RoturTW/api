package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type CosmeticType string

const (
	CosmeticTypeOverlay CosmeticType = "overlay"
)

type CosmeticPricingType string

const (
	CosmeticFree CosmeticPricingType = "free"
	CosmeticPaid CosmeticPricingType = "paid"
)

const (
	maxCosmeticIdLen          = 50
	maxCosmeticNameLen        = 100
	maxCosmeticDescriptionLen = 500
	maxCosmeticImageUrlLen    = 500
	maxOwnedCosmeticsPerUser  = 200
)

var cosmeticIdRe = regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)

type CosmeticCatalogEntry struct {
	Id           string              `json:"id"`
	CosmeticType CosmeticType        `json:"cosmetic_type"`
	Name         string              `json:"name"`
	Description  string              `json:"description"`
	ImageUrl     string              `json:"image_url"`
	PricingType  CosmeticPricingType `json:"pricing_type"`
	Price        float64             `json:"price"`
	CreatorId    UserId              `json:"creator_id"`
	CreatorPct   float64             `json:"creator_pct"`
	Featured     bool                `json:"featured"`
	Purchases    int                 `json:"purchases"`
	CreatedAt    int64               `json:"created_at"`
}

type CosmeticCatalogEntryPublic struct {
	Id           string              `json:"id"`
	CosmeticType CosmeticType        `json:"cosmetic_type"`
	Name         string              `json:"name"`
	Description  string              `json:"description"`
	ImageUrl     string              `json:"image_url"`
	PricingType  CosmeticPricingType `json:"pricing_type"`
	Price        float64             `json:"price"`
	Creator      Username            `json:"creator"`
	CreatorPct   float64             `json:"creator_pct"`
	Featured     bool                `json:"featured"`
	Purchases    int                 `json:"purchases"`
	CreatedAt    int64               `json:"created_at"`
}

func (e CosmeticCatalogEntry) ToPublic() CosmeticCatalogEntryPublic {
	return CosmeticCatalogEntryPublic{
		Id:           e.Id,
		CosmeticType: e.CosmeticType,
		Name:         e.Name,
		Description:  e.Description,
		ImageUrl:     e.ImageUrl,
		PricingType:  e.PricingType,
		Price:        e.Price,
		Creator:      e.CreatorId.User().GetUsername(),
		CreatorPct:   e.CreatorPct,
		Featured:     e.Featured,
		Purchases:    e.Purchases,
		CreatedAt:    e.CreatedAt,
	}
}

type UserCosmetics struct {
	ActiveCosmetics map[CosmeticType]string `json:"active_cosmetics"`
	OwnedCosmetics  []string                `json:"owned_cosmetics"`
}

var (
	cosmeticsCatalog   []CosmeticCatalogEntry
	cosmeticsCatalogMu sync.RWMutex
	userCosmeticsMu    sync.Mutex
)

func sanitizeCosmeticId(id string) string {
	return strings.TrimSpace(id)
}

func validateCosmeticId(id string) (string, bool) {
	id = sanitizeCosmeticId(id)
	if id == "" || len(id) > maxCosmeticIdLen {
		return id, false
	}
	if !cosmeticIdRe.MatchString(id) {
		return id, false
	}
	if strings.Contains(id, "..") {
		return id, false
	}
	return id, true
}

func sanitizeCosmeticField(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return s
}

func validateImageUrl(url string) bool {
	if url == "" {
		return true
	}
	if len(url) > maxCosmeticImageUrlLen {
		return false
	}
	lower := strings.ToLower(url)
	if strings.HasPrefix(lower, "javascript:") || strings.HasPrefix(lower, "data:") || strings.HasPrefix(lower, "vbscript:") {
		return false
	}
	if !strings.HasPrefix(lower, "https://") && !strings.HasPrefix(lower, "http://") {
		return false
	}
	return true
}

func validateCosmeticType(ct CosmeticType) bool {
	switch ct {
	case CosmeticTypeOverlay:
		return true
	}
	return false
}

func loadCosmeticsCatalog() {
	cosmeticsCatalogMu.Lock()
	defer cosmeticsCatalogMu.Unlock()

	if _, err := os.Stat(COSMETICS_FILE_PATH); os.IsNotExist(err) {
		cosmeticsCatalog = []CosmeticCatalogEntry{}
		return
	}

	data, err := os.ReadFile(COSMETICS_FILE_PATH)
	if err != nil {
		log.Printf("Error reading cosmetics catalog: %v", err)
		cosmeticsCatalog = []CosmeticCatalogEntry{}
		return
	}

	if err := json.Unmarshal(data, &cosmeticsCatalog); err != nil {
		log.Printf("Error unmarshaling cosmetics catalog: %v", err)
		cosmeticsCatalog = []CosmeticCatalogEntry{}
		return
	}

	log.Printf("Loaded %d cosmetics catalog entries", len(cosmeticsCatalog))
}

func saveCosmeticsCatalog() {
	cosmeticsCatalogMu.RLock()
	defer cosmeticsCatalogMu.RUnlock()
	saveJsonFile(COSMETICS_FILE_PATH, cosmeticsCatalog)
}

func getCosmeticsFilePath(username string) string {
	return filepath.Join(USERDATA_PATH, strings.ToLower(username), "cosmetics.json")
}

func loadUserCosmetics(username string) (*UserCosmetics, error) {
	path := getCosmeticsFilePath(username)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &UserCosmetics{
				ActiveCosmetics: map[CosmeticType]string{},
				OwnedCosmetics:  []string{},
			}, nil
		}
		return nil, err
	}

	var uc UserCosmetics
	if err := json.Unmarshal(data, &uc); err != nil {
		return nil, err
	}

	if uc.ActiveCosmetics == nil {
		uc.ActiveCosmetics = map[CosmeticType]string{}
	}
	if uc.OwnedCosmetics == nil {
		uc.OwnedCosmetics = []string{}
	}

	return &uc, nil
}

func saveUserCosmetics(username string, uc *UserCosmetics) error {
	path := getCosmeticsFilePath(username)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create cosmetics directory: %w", err)
	}

	data, err := json.MarshalIndent(uc, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cosmetics: %w", err)
	}

	return atomicWrite(path, data, 0644)
}

func getCatalogEntryById(id string) (*CosmeticCatalogEntry, bool) {
	cosmeticsCatalogMu.RLock()
	defer cosmeticsCatalogMu.RUnlock()

	for i := range cosmeticsCatalog {
		if cosmeticsCatalog[i].Id == id {
			return &cosmeticsCatalog[i], true
		}
	}
	return nil, false
}

func getCatalogEntryByIdForWrite(id string) (*CosmeticCatalogEntry, bool) {
	cosmeticsCatalogMu.Lock()
	defer cosmeticsCatalogMu.Unlock()

	for i := range cosmeticsCatalog {
		if cosmeticsCatalog[i].Id == id {
			return &cosmeticsCatalog[i], true
		}
	}
	return nil, false
}

func getShop(c *gin.Context) {
	cosmeticType := c.Query("type")
	featured := c.Query("featured")
	search := c.Query("search")
	sortBy := c.DefaultQuery("sort", "newest")

	cosmeticsCatalogMu.RLock()
	entries := make([]CosmeticCatalogEntry, 0, len(cosmeticsCatalog))
	for _, e := range cosmeticsCatalog {
		if cosmeticType != "" && string(e.CosmeticType) != cosmeticType {
			continue
		}
		if featured == "true" && !e.Featured {
			continue
		}
		if search != "" {
			searchLower := strings.ToLower(search)
			if !strings.Contains(strings.ToLower(e.Name), searchLower) &&
				!strings.Contains(strings.ToLower(e.Description), searchLower) {
				continue
			}
		}
		entries = append(entries, e)
	}
	cosmeticsCatalogMu.RUnlock()

	switch sortBy {
	case "newest":
		for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
			entries[i], entries[j] = entries[j], entries[i]
		}
	case "price_low":
		for i := 0; i < len(entries)-1; i++ {
			for j := i + 1; j < len(entries); j++ {
				if entries[i].Price > entries[j].Price {
					entries[i], entries[j] = entries[j], entries[i]
				}
			}
		}
	case "price_high":
		for i := 0; i < len(entries)-1; i++ {
			for j := i + 1; j < len(entries); j++ {
				if entries[i].Price < entries[j].Price {
					entries[i], entries[j] = entries[j], entries[i]
				}
			}
		}
	case "popular":
		for i := 0; i < len(entries)-1; i++ {
			for j := i + 1; j < len(entries); j++ {
				if entries[i].Purchases < entries[j].Purchases {
					entries[i], entries[j] = entries[j], entries[i]
				}
			}
		}
	}

	limitStr := c.DefaultQuery("limit", "50")
	offsetStr := c.DefaultQuery("offset", "0")
	limit := 50
	offset := 0
	if v, err := fmt.Sscanf(limitStr, "%d", &limit); err != nil || v != 1 || limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	if v, err := fmt.Sscanf(offsetStr, "%d", &offset); err != nil || v != 1 || offset < 0 {
		offset = 0
	}

	if offset >= len(entries) {
		c.JSON(200, gin.H{
			"items":  []CosmeticCatalogEntryPublic{},
			"total":  len(entries),
			"offset": offset,
			"limit":  limit,
		})
		return
	}

	end := offset + limit
	if end > len(entries) {
		end = len(entries)
	}

	page := make([]CosmeticCatalogEntryPublic, 0, end-offset)
	for i := offset; i < end; i++ {
		page = append(page, entries[i].ToPublic())
	}

	c.JSON(200, gin.H{
		"items":  page,
		"total":  len(entries),
		"offset": offset,
		"limit":  limit,
	})
}

func getCosmeticDetail(c *gin.Context) {
	id := c.Param("id")

	id, valid := validateCosmeticId(id)
	if !valid {
		c.JSON(400, gin.H{"error": "Invalid cosmetic id"})
		return
	}

	entry, exists := getCatalogEntryById(id)
	if !exists {
		c.JSON(404, gin.H{"error": "Cosmetic not found"})
		return
	}

	c.JSON(200, entry.ToPublic())
}

func getMyCosmetics(c *gin.Context) {
	user := c.MustGet("user").(*User)
	username := string(user.GetUsername())

	uc, err := loadUserCosmetics(username)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to load cosmetics"})
		return
	}

	cosmeticsCatalogMu.RLock()
	activeDetails := make(map[CosmeticType]CosmeticCatalogEntryPublic)
	for ct, id := range uc.ActiveCosmetics {
		for _, e := range cosmeticsCatalog {
			if e.Id == id {
				activeDetails[ct] = e.ToPublic()
				break
			}
		}
	}

	ownedDetails := make([]CosmeticCatalogEntryPublic, 0, len(uc.OwnedCosmetics))
	for _, id := range uc.OwnedCosmetics {
		for _, e := range cosmeticsCatalog {
			if e.Id == id {
				ownedDetails = append(ownedDetails, e.ToPublic())
				break
			}
		}
	}
	cosmeticsCatalogMu.RUnlock()

	c.JSON(200, gin.H{
		"active_cosmetics": activeDetails,
		"owned_cosmetics":  ownedDetails,
	})
}

func purchaseCosmetic(c *gin.Context) {
	user := c.MustGet("user").(*User)
	username := string(user.GetUsername())
	id := c.Param("id")

	id, valid := validateCosmeticId(id)
	if !valid {
		c.JSON(400, gin.H{"error": "Invalid cosmetic id"})
		return
	}

	userCosmeticsMu.Lock()
	defer userCosmeticsMu.Unlock()

	entry, exists := getCatalogEntryByIdForWrite(id)
	if !exists {
		c.JSON(404, gin.H{"error": "Cosmetic not found"})
		return
	}

	if entry.CreatorPct < 0 || entry.CreatorPct > 100 {
		c.JSON(500, gin.H{"error": "Invalid creator percentage in catalog"})
		return
	}

	uc, err := loadUserCosmetics(username)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to load cosmetics data"})
		return
	}

	for _, owned := range uc.OwnedCosmetics {
		if owned == id {
			c.JSON(400, gin.H{"error": "You already own this cosmetic"})
			return
		}
	}

	if len(uc.OwnedCosmetics) >= maxOwnedCosmeticsPerUser {
		c.JSON(400, gin.H{"error": "You have reached the maximum number of owned cosmetics"})
		return
	}

	if entry.PricingType == CosmeticFree || entry.Price == 0 {
		uc.OwnedCosmetics = append(uc.OwnedCosmetics, id)
		entry.Purchases++
		if err := saveUserCosmetics(username, uc); err != nil {
			c.JSON(500, gin.H{"error": "Failed to save cosmetics data"})
			return
		}
		go saveCosmeticsCatalog()
		c.JSON(200, gin.H{
			"message":   "Cosmetic acquired successfully",
			"cosmetic":  entry.ToPublic(),
			"price":     0,
			"new_total": user.GetCredits(),
		})
		return
	}

	userCredits := user.GetCredits()
	if userCredits < entry.Price {
		c.JSON(400, gin.H{
			"error":     "Insufficient credits",
			"required":  entry.Price,
			"available": userCredits,
		})
		return
	}

	creatorShare := roundVal(entry.Price * (entry.CreatorPct / 100.0))
	platformShare := roundVal(entry.Price - creatorShare)

	newPurchaserBal := roundVal(userCredits - entry.Price)
	if newPurchaserBal < 0 {
		c.JSON(400, gin.H{"error": "Insufficient credits"})
		return
	}
	user.SetBalance(newPurchaserBal)
	now := time.Now().UnixMilli()
	user.addTransaction(Transaction{
		Note:      "Cosmetic purchase: " + entry.Name,
		User:      entry.CreatorId,
		Amount:    entry.Price,
		Type:      "cosmetic_purchase",
		Timestamp: now,
		NewTotal:  newPurchaserBal,
	})

	creatorUser := getUserById(entry.CreatorId)
	if len(creatorUser) > 0 {
		creatorBal := creatorUser.GetCredits()
		newCreatorBal := roundVal(creatorBal + creatorShare)
		creatorUser.SetBalance(newCreatorBal)
		creatorUser.addTransaction(Transaction{
			Note:      "Cosmetic sale: " + entry.Name,
			User:      user.GetId(),
			Amount:    creatorShare,
			Type:      "cosmetic_sale",
			Timestamp: now,
			NewTotal:  newCreatorBal,
		})
	}

	mistUser, mistErr := getAccountByUsername(Username("mist"))
	if mistErr == nil && len(mistUser) > 0 {
		mistBal := mistUser.GetCredits()
		newMistBal := roundVal(mistBal + platformShare)
		mistUser.SetBalance(newMistBal)
		if platformShare > 0 {
			mistUser.addTransaction(Transaction{
				Note:      "Cosmetic platform cut: " + entry.Name,
				User:      user.GetId(),
				Amount:    platformShare,
				Type:      "cosmetic_platform",
				Timestamp: now,
				NewTotal:  newMistBal,
			})
		}
	}

	entry.Purchases++

	uc.OwnedCosmetics = append(uc.OwnedCosmetics, id)
	if err := saveUserCosmetics(username, uc); err != nil {
		c.JSON(500, gin.H{"error": "Failed to save cosmetics data"})
		return
	}

	go saveCosmeticsCatalog()
	go saveUsers()

	c.JSON(200, gin.H{
		"message":        "Cosmetic purchased successfully",
		"cosmetic":       entry.ToPublic(),
		"price":          entry.Price,
		"creator_share":  creatorShare,
		"platform_share": platformShare,
		"new_total":      newPurchaserBal,
	})
}

func equipCosmetic(c *gin.Context) {
	user := c.MustGet("user").(*User)
	username := string(user.GetUsername())
	id := c.Param("id")

	id, valid := validateCosmeticId(id)
	if !valid {
		c.JSON(400, gin.H{"error": "Invalid cosmetic id"})
		return
	}

	entry, exists := getCatalogEntryById(id)
	if !exists {
		c.JSON(404, gin.H{"error": "Cosmetic not found"})
		return
	}

	userCosmeticsMu.Lock()
	defer userCosmeticsMu.Unlock()

	uc, err := loadUserCosmetics(username)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to load cosmetics data"})
		return
	}

	owned := false
	for _, ownedId := range uc.OwnedCosmetics {
		if ownedId == id {
			owned = true
			break
		}
	}

	if !owned {
		c.JSON(403, gin.H{"error": "You do not own this cosmetic"})
		return
	}

	uc.ActiveCosmetics[entry.CosmeticType] = id
	if err := saveUserCosmetics(username, uc); err != nil {
		c.JSON(500, gin.H{"error": "Failed to save cosmetics data"})
		return
	}

	if entry.CosmeticType == CosmeticTypeOverlay {
		user.Set("sys.overlay", id)
		go saveUsers()
	}

	c.JSON(200, gin.H{
		"message": "Cosmetic equipped successfully",
	})
}

func unequipCosmetic(c *gin.Context) {
	user := c.MustGet("user").(*User)
	username := string(user.GetUsername())

	cosmeticType := c.Query("type")
	if cosmeticType == "" {
		c.JSON(400, gin.H{"error": "type query parameter is required (e.g. overlay)"})
		return
	}

	ct := CosmeticType(cosmeticType)
	if !validateCosmeticType(ct) {
		c.JSON(400, gin.H{"error": "Unknown cosmetic type"})
		return
	}

	userCosmeticsMu.Lock()
	defer userCosmeticsMu.Unlock()

	uc, err := loadUserCosmetics(username)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to load cosmetics data"})
		return
	}

	if _, ok := uc.ActiveCosmetics[ct]; !ok {
		c.JSON(400, gin.H{"error": "No active cosmetic of that type to remove"})
		return
	}

	delete(uc.ActiveCosmetics, ct)
	if err := saveUserCosmetics(username, uc); err != nil {
		c.JSON(500, gin.H{"error": "Failed to save cosmetics data"})
		return
	}

	if ct == CosmeticTypeOverlay {
		user.Set("sys.overlay", "")
		go saveUsers()
	}

	c.JSON(200, gin.H{
		"message": "Cosmetic removed successfully",
	})
}

func adminCreateCosmetic(c *gin.Context) {
	if !authenticateAdmin(c) {
		return
	}

	var req struct {
		Id           string  `json:"id"`
		CosmeticType string  `json:"cosmetic_type"`
		Name         string  `json:"name"`
		Description  string  `json:"description"`
		ImageUrl     string  `json:"image_url"`
		PricingType  string  `json:"pricing_type"`
		Price        float64 `json:"price"`
		Creator      string  `json:"creator"`
		CreatorPct   float64 `json:"creator_pct"`
		Featured     bool    `json:"featured"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request payload"})
		return
	}

	req.Id = sanitizeCosmeticId(req.Id)
	if req.Id == "" {
		c.JSON(400, gin.H{"error": "Cosmetic id is required"})
		return
	}

	if validId, ok := validateCosmeticId(req.Id); !ok {
		c.JSON(400, gin.H{"error": "Cosmetic id must contain only alphanumeric characters, hyphens and underscores"})
		return
	} else {
		req.Id = validId
	}

	req.Name = sanitizeCosmeticField(req.Name, maxCosmeticNameLen)
	if req.Name == "" {
		c.JSON(400, gin.H{"error": "Name is required"})
		return
	}

	if req.CosmeticType == "" {
		c.JSON(400, gin.H{"error": "cosmetic_type is required"})
		return
	}

	ct := CosmeticType(req.CosmeticType)
	if !validateCosmeticType(ct) {
		c.JSON(400, gin.H{"error": "Unknown cosmetic_type: " + req.CosmeticType})
		return
	}

	req.Description = sanitizeCosmeticField(req.Description, maxCosmeticDescriptionLen)

	if !validateImageUrl(req.ImageUrl) {
		c.JSON(400, gin.H{"error": "image_url must be a valid HTTP(S) URL"})
		return
	}

	pt := CosmeticPaid
	if req.PricingType == "free" {
		pt = CosmeticFree
	}

	if req.CreatorPct < 0 || req.CreatorPct > 100 {
		c.JSON(400, gin.H{"error": "Creator percentage must be between 0 and 100"})
		return
	}

	if req.Price < 0 {
		c.JSON(400, gin.H{"error": "Price cannot be negative"})
		return
	}
	req.Price = math.Round(req.Price*100) / 100

	req.Creator = strings.TrimSpace(req.Creator)
	if req.Creator == "" {
		c.JSON(400, gin.H{"error": "Creator username is required"})
		return
	}

	creatorId := Username(req.Creator).Id()
	if creatorId == "" {
		c.JSON(404, gin.H{"error": "Creator user not found"})
		return
	}

	cosmeticsCatalogMu.Lock()
	for _, e := range cosmeticsCatalog {
		if e.Id == req.Id {
			cosmeticsCatalogMu.Unlock()
			c.JSON(400, gin.H{"error": "Cosmetic with this id already exists"})
			return
		}
	}

	entry := CosmeticCatalogEntry{
		Id:           req.Id,
		CosmeticType: ct,
		Name:         req.Name,
		Description:  req.Description,
		ImageUrl:     req.ImageUrl,
		PricingType:  pt,
		Price:        req.Price,
		CreatorId:    creatorId,
		CreatorPct:   req.CreatorPct,
		Featured:     req.Featured,
		Purchases:    0,
		CreatedAt:    time.Now().UnixMilli(),
	}

	cosmeticsCatalog = append(cosmeticsCatalog, entry)
	cosmeticsCatalogMu.Unlock()

	go saveCosmeticsCatalog()

	c.JSON(201, gin.H{
		"message":  "Cosmetic added to catalog successfully",
		"cosmetic": entry.ToPublic(),
	})
}

func adminUpdateCosmetic(c *gin.Context) {
	if !authenticateAdmin(c) {
		return
	}

	id := c.Param("id")

	id, valid := validateCosmeticId(id)
	if !valid {
		c.JSON(400, gin.H{"error": "Invalid cosmetic id"})
		return
	}

	var req struct {
		Name         *string  `json:"name"`
		Description  *string  `json:"description"`
		ImageUrl     *string  `json:"image_url"`
		PricingType  *string  `json:"pricing_type"`
		Price        *float64 `json:"price"`
		Creator      *string  `json:"creator"`
		CreatorPct   *float64 `json:"creator_pct"`
		Featured     *bool    `json:"featured"`
		CosmeticType *string  `json:"cosmetic_type"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request payload"})
		return
	}

	cosmeticsCatalogMu.Lock()
	var target *CosmeticCatalogEntry
	for i := range cosmeticsCatalog {
		if cosmeticsCatalog[i].Id == id {
			target = &cosmeticsCatalog[i]
			break
		}
	}

	if target == nil {
		cosmeticsCatalogMu.Unlock()
		c.JSON(404, gin.H{"error": "Cosmetic not found"})
		return
	}

	if req.Name != nil {
		target.Name = sanitizeCosmeticField(*req.Name, maxCosmeticNameLen)
		if target.Name == "" {
			cosmeticsCatalogMu.Unlock()
			c.JSON(400, gin.H{"error": "Name cannot be empty"})
			return
		}
	}
	if req.Description != nil {
		target.Description = sanitizeCosmeticField(*req.Description, maxCosmeticDescriptionLen)
	}
	if req.ImageUrl != nil {
		if !validateImageUrl(*req.ImageUrl) {
			cosmeticsCatalogMu.Unlock()
			c.JSON(400, gin.H{"error": "image_url must be a valid HTTP(S) URL"})
			return
		}
		target.ImageUrl = *req.ImageUrl
	}
	if req.PricingType != nil {
		switch *req.PricingType {
		case "free":
			target.PricingType = CosmeticFree
		case "paid":
			target.PricingType = CosmeticPaid
		}
	}
	if req.Price != nil {
		if *req.Price < 0 {
			cosmeticsCatalogMu.Unlock()
			c.JSON(400, gin.H{"error": "Price cannot be negative"})
			return
		}
		target.Price = math.Round(*req.Price*100) / 100
	}
	if req.Creator != nil {
		creatorName := strings.TrimSpace(*req.Creator)
		creatorId := Username(creatorName).Id()
		if creatorId == "" {
			cosmeticsCatalogMu.Unlock()
			c.JSON(404, gin.H{"error": "Creator user not found"})
			return
		}
		target.CreatorId = creatorId
	}
	if req.CreatorPct != nil {
		if *req.CreatorPct < 0 || *req.CreatorPct > 100 {
			cosmeticsCatalogMu.Unlock()
			c.JSON(400, gin.H{"error": "Creator percentage must be between 0 and 100"})
			return
		}
		target.CreatorPct = *req.CreatorPct
	}
	if req.Featured != nil {
		target.Featured = *req.Featured
	}
	if req.CosmeticType != nil {
		ct := CosmeticType(*req.CosmeticType)
		if !validateCosmeticType(ct) {
			cosmeticsCatalogMu.Unlock()
			c.JSON(400, gin.H{"error": "Unknown cosmetic_type: " + *req.CosmeticType})
			return
		}
		target.CosmeticType = ct
	}

	updated := *target
	cosmeticsCatalogMu.Unlock()

	go saveCosmeticsCatalog()

	c.JSON(200, gin.H{
		"message":  "Cosmetic updated successfully",
		"cosmetic": updated.ToPublic(),
	})
}

func adminDeleteCosmetic(c *gin.Context) {
	if !authenticateAdmin(c) {
		return
	}

	id := c.Param("id")

	id, valid := validateCosmeticId(id)
	if !valid {
		c.JSON(400, gin.H{"error": "Invalid cosmetic id"})
		return
	}

	cosmeticsCatalogMu.Lock()
	found := false
	newCatalog := make([]CosmeticCatalogEntry, 0, len(cosmeticsCatalog))
	for _, e := range cosmeticsCatalog {
		if e.Id == id {
			found = true
			continue
		}
		newCatalog = append(newCatalog, e)
	}

	if !found {
		cosmeticsCatalogMu.Unlock()
		c.JSON(404, gin.H{"error": "Cosmetic not found"})
		return
	}

	cosmeticsCatalog = newCatalog
	cosmeticsCatalogMu.Unlock()

	go saveCosmeticsCatalog()

	c.JSON(200, gin.H{
		"message": "Cosmetic removed from catalog successfully",
	})
}

func adminListCosmetics(c *gin.Context) {
	if !authenticateAdmin(c) {
		return
	}

	cosmeticType := c.Query("type")

	cosmeticsCatalogMu.RLock()
	entries := make([]CosmeticCatalogEntry, 0)
	for _, e := range cosmeticsCatalog {
		if cosmeticType != "" && string(e.CosmeticType) != cosmeticType {
			continue
		}
		entries = append(entries, e)
	}
	cosmeticsCatalogMu.RUnlock()

	c.JSON(200, gin.H{
		"cosmetics": entries,
		"total":     len(entries),
	})
}
