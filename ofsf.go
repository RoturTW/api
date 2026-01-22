package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

type UpdateFileRequest struct {
	Updates []UpdateChange `json:"updates" binding:"required"`
}

type GetFilesRequest struct {
	Username string   `json:"username" binding:"required"`
	UUIDs    []string `json:"uuids" binding:"required"`
}

type FileMetadata struct {
	Entry FileEntry `json:"entry"`
	Index int       `json:"index"`
}

var fs *FileSystem = NewFileSystem()

func updateFiles(c *gin.Context) {
	user := c.MustGet("user").(*User)

	var req UpdateFileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	maxSize := user.GetSubscriptionBenefits().FileSystem_Size

	result := fs.HandleOFSFUpdate(user.GetUsername(), req.Updates, maxSize)

	statusCode := http.StatusOK
	if result.Payload == "Max Upload Size Exceeded" {
		statusCode = http.StatusRequestEntityTooLarge
	} else if result.Payload != "Successfully Updated Origin Files" {
		statusCode = http.StatusBadRequest
	}

	c.JSON(statusCode, result)
}

func getFilesByUUIDs(c *gin.Context) {
	user := c.MustGet("user").(*User)

	var req GetFilesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	files, err := fs.GetFilesByUUIDs(user.GetUsername(), req.UUIDs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"files": files})
}

func getUserFileSize(c *gin.Context) {
	user := c.MustGet("user").(*User)

	username := user.GetUsername()

	size, err := fs.GetUserFileSize(username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"username": username, "size": size})
}

func deleteAllUserFiles(c *gin.Context) {
	user := c.MustGet("user").(*User)

	username := user.GetUsername()

	if err := fs.DeleteUserFileSystem(username); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "deleted", "username": username})
}

func getFilesIndex(c *gin.Context) {
	user := c.MustGet("user").(*User)

	username := user.GetUsername()
	if err := fs.migrateFromLegacy(username); err != nil {
		fmt.Printf("\033[91m[-] OFSF Error\033[0m | Migration failed: %v\n", err)
	}
	index, err := fs.GetFilesIndexWithThreshold(username, 50*1024)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	jsonData, err := json.Marshal(index)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to serialize OFSF data"})
		return
	}

	c.Header("Content-Length", fmt.Sprintf("%d", len(jsonData)))
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Cache-Control", "no-cache")
	c.Data(http.StatusOK, "application/octet-stream", jsonData)
}

func getFilesAll(c *gin.Context) {
	user := c.MustGet("user").(*User)

	username := user.GetUsername()
	if err := fs.migrateFromLegacy(username); err != nil {
		fmt.Printf("\033[91m[-] OFSF Error\033[0m | Migration failed: %v\n", err)
	}
	index, err := fs.GetFilesIndexWithThreshold(username, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	jsonData, err := json.Marshal(index)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to serialize OFSF data"})
		return
	}

	c.Header("Content-Length", fmt.Sprintf("%d", len(jsonData)))
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Cache-Control", "no-cache")
	c.Data(http.StatusOK, "application/octet-stream", jsonData)
}

func getFileByUUID(c *gin.Context) {
	user := c.MustGet("user").(*User)

	uuid := c.Query("uuid")
	if uuid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "UUID is required"})
		return
	}

	username := user.GetUsername()
	if err := fs.migrateFromLegacy(username); err != nil {
		fmt.Printf("\033[91m[-] OFSF Error\033[0m | Migration failed: %v\n", err)
	}

	file, err := fs.GetFileByUUID(username, uuid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	jsonData, err := json.Marshal(file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to serialize OFSF data"})
		return
	}

	c.Header("Content-Length", fmt.Sprintf("%d", len(jsonData)))
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Cache-Control", "no-cache, max-age=0")
	c.Data(http.StatusOK, "application/octet-stream", jsonData)
}

const (
	fileEntrySize = 14
	fileDir       = "./rotur/files"
)

type FileEntry []any

type UpdateChange struct {
	Command string `json:"command"`
	UUID    string `json:"uuid"`
	Dta     any    `json:"dta"`
	Idx     any    `json:"idx"`
}

type UpdateRequest struct {
	Payload []UpdateChange `json:"payload"`
	Offset  string         `json:"offset"`
}

type UpdateResult struct {
	Payload       string `json:"payload"`
	UsedSize      int    `json:"used_size,omitempty"`
	AvailableSize int    `json:"available_size,omitempty"`
}

type FileSystem struct {
	mu sync.RWMutex
}

func NewFileSystem() *FileSystem {
	return &FileSystem{}
}

func (fs *FileSystem) HandleOFSFUpdate(username string, updates []UpdateChange, maxSize int) UpdateResult {

	fmt.Printf("\033[92m[+] OFSF\033[0m | %s processing %d file updates\n", username, len(updates))

	if err := fs.migrateFromLegacy(username); err != nil {
		fmt.Printf("\033[91m[-] OFSF Error\033[0m | Migration failed: %v\n", err)
	}

	for _, change := range updates {
		switch change.Command {
		case "UUIDa":
			fs.handleAdd(username, change)
		case "UUIDr":
			fs.handleReplace(username, change)
		case "UUIDd":
			fs.handleDelete(username, change)
		}
	}

	usedSize, err := fs.calculateTotalSize(username)
	if err != nil {
		return UpdateResult{Payload: "Error calculating size"}
	}

	availableSize := maxSize - usedSize

	if usedSize > maxSize {
		fmt.Printf("\033[91m[-] OFSF Error\033[0m | User %s exceeded upload storage limit (used: %d, available: %d)\n",
			username, usedSize, availableSize)
		return UpdateResult{
			Payload:       "Max Upload Size Exceeded",
			UsedSize:      usedSize,
			AvailableSize: availableSize,
		}
	}

	fmt.Printf("\033[92m[+] OFSF\033[0m | Updated %s files (used: %d, available: %d)\n",
		username, usedSize, availableSize)

	return UpdateResult{
		Payload:       "Successfully Updated Origin Files",
		UsedSize:      usedSize,
		AvailableSize: availableSize,
	}
}

func (fs *FileSystem) handleAdd(username string, change UpdateChange) {
	path := filepath.Join(fileDir, username, change.UUID+".json")

	if _, err := os.Stat(path); err == nil {
		fmt.Printf("\033[91m[-] OFSF Error\033[0m | File %s already exists\n", change.UUID)
		return
	}

	fmt.Printf("\033[92m[+] OFSF\033[0m | Adding file %s\n", change.UUID)
	dta := []any{}
	if dta, ok := change.Dta.([]any); !ok || len(dta) > fileEntrySize {
		fmt.Printf("\033[91m[-] OFSF Error\033[0m | Invalid file data\n")
		return
	}
	data, _ := json.Marshal(FileMetadata{
		Entry: dta,
		Index: 0,
	})

	if err := os.WriteFile(path, data, 0644); err != nil {
		fmt.Printf("\033[91m[-] OFSF Error\033[0m | Failed to write file %s: %v\n", change.UUID, err)
		return
	}
}

func (fs *FileSystem) handleReplace(username string, change UpdateChange) {
	path := filepath.Join(fileDir, username, change.UUID+".json")

	if _, err := os.Stat(path); err != nil {
		fmt.Printf("\033[91m[-] OFSF Error\033[0m | File %s does not exist\n", change.UUID)
		return
	}

	entry, err := fs.GetFileByUUID(username, change.UUID)
	if err != nil {
		fmt.Printf("\033[91m[-] OFSF Error\033[0m | Failed to read file %s: %v\n", change.UUID, err)
		return
	}

	idx := 0
	switch v := change.Idx.(type) {
	case float64:
		idx = int(v)
	case int:
		idx = v
	case string:
		fmt.Sscanf(v, "%d", &idx)
	}

	if idx > 0 {
		idx--
	}

	if idx >= 0 && idx < len(entry) {
		entry[idx] = change.Dta
	}
	fs.SetFileByUUID(username, change.UUID, entry)

}

func (fs *FileSystem) handleDelete(username string, change UpdateChange) {
	path := filepath.Join(fileDir, username, change.UUID+".json")

	if _, err := os.Stat(path); err != nil {
		return
	}

	os.Remove(path)
}

func (fs *FileSystem) migrateFromLegacy(username string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	legacyPath := filepath.Join(fileDir, username+".ofsf")

	if _, err := os.Stat(legacyPath); os.IsNotExist(err) {
		return nil
	}

	fmt.Printf("\033[93m[~] OFSF\033[0m | Migrating %s from legacy format\n", username)

	data, err := os.ReadFile(legacyPath)
	if err != nil {
		return err
	}

	if len(data) == 0 {
		os.Remove(legacyPath)
		return nil
	}

	var filesList []any
	if err := json.Unmarshal(data, &filesList); err != nil {
		return err
	}

	userDir := filepath.Join(fileDir, username)
	if err := os.MkdirAll(userDir, 0755); err != nil {
		return err
	}

	index := 0
	for i := 0; i+fileEntrySize <= len(filesList); i += fileEntrySize {
		entry := filesList[i : i+fileEntrySize]
		if uuid, ok := entry[13].(string); ok {
			metadata := FileMetadata{
				Entry: entry,
				Index: index,
			}
			entryData, _ := json.Marshal(metadata)
			filePath := filepath.Join(userDir, uuid+".json")
			os.WriteFile(filePath, entryData, 0644)
			index++
		}
	}

	os.Remove(legacyPath)
	fmt.Printf("\033[92m[+] OFSF\033[0m | Migration complete for %s\n", username)

	return nil
}

func (fs *FileSystem) GetFileByUUID(username string, uuid string) (FileEntry, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	userDir := filepath.Join(fileDir, username)

	filePath := filepath.Join(userDir, uuid+".json")

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var metadata FileMetadata
	if err := json.Unmarshal(data, &metadata); err == nil && metadata.Entry != nil {
		return metadata.Entry, nil
	}

	return nil, fmt.Errorf("file not found with the provided UUID")
}

func (fs *FileSystem) SetFileByUUID(username string, uuid string, file FileEntry) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	userDir := filepath.Join(fileDir, username)

	filePath := filepath.Join(userDir, uuid+".json")

	data, err := json.Marshal(FileMetadata{
		Entry: file,
		Index: 0,
	})

	if err != nil {
		return err
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return err
	}

	return nil
}

func (fs *FileSystem) calculateTotalSize(username string) (int, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	userDir := filepath.Join(fileDir, username)
	totalSize := 0

	entries, err := os.ReadDir(userDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		totalSize += int(info.Size())
	}

	return totalSize, nil
}

func (fs *FileSystem) GetFilesByUUIDs(username string, uuids []string) (map[string]FileEntry, error) {
	if err := fs.migrateFromLegacy(username); err != nil {
		fmt.Printf("\033[91m[-] OFSF Error\033[0m | Migration failed: %v\n", err)
	}
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	result := make(map[string]FileEntry)
	userDir := filepath.Join(fileDir, username)

	for _, uuid := range uuids {
		filePath := filepath.Join(userDir, uuid+".json")

		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		var metadata FileMetadata
		if err := json.Unmarshal(data, &metadata); err == nil && metadata.Entry != nil {
			result[uuid] = metadata.Entry
		}
	}

	return result, nil
}

func (fs *FileSystem) DeleteUserFileSystem(username string) error {
	if err := fs.migrateFromLegacy(username); err != nil {
		fmt.Printf("\033[91m[-] OFSF Error\033[0m | Migration failed: %v\n", err)
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()

	userDir := filepath.Join(fileDir, username)

	if err := os.RemoveAll(userDir); err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("No files found for user %s to delete\n", username)
			return nil
		}
		return err
	}

	legacyPath := filepath.Join(fileDir, username+".ofsf")
	os.Remove(legacyPath)

	fmt.Printf("Successfully deleted files for user %s\n", username)
	return nil
}

func (fs *FileSystem) GetUserFileSize(username string) (string, error) {
	if err := fs.migrateFromLegacy(username); err != nil {
		fmt.Printf("\033[91m[-] OFSF Error\033[0m | Migration failed: %v\n", err)
	}
	size, err := fs.calculateTotalSize(username)
	if err != nil {
		return "", err
	}

	switch {
	case size >= 1<<30:
		return fmt.Sprintf("%.4f GB", float64(size)/(1<<30)), nil
	case size >= 1<<20:
		return fmt.Sprintf("%.2f MB", float64(size)/(1<<20)), nil
	case size >= 1<<10:
		return fmt.Sprintf("%.2f KB", float64(size)/(1<<10)), nil
	default:
		return fmt.Sprintf("%d bytes", size), nil
	}
}

func (fs *FileSystem) GetFilesIndexWithThreshold(username string, sizeThreshold int) ([]any, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	result := make([]any, 0)
	userDir := filepath.Join(fileDir, username)

	entries, err := os.ReadDir(userDir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		filePath := filepath.Join(userDir, entry.Name())

		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		var metadata FileMetadata
		if err := json.Unmarshal(data, &metadata); err == nil && metadata.Entry != nil {
			entry := metadata.Entry

			if sizeThreshold > 0 && len(entry) > 3 {
				if dataStr, ok := entry[3].(string); ok && len(dataStr) > sizeThreshold {
					entryCopy := make(FileEntry, len(entry))
					copy(entryCopy, entry)
					entryCopy[3] = false
					entry = entryCopy
				}
			}

			result = append(result, entry...)
		}
	}

	return result, nil
}
