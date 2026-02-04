package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

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

type FileEntry []any

type FileEntryStruct struct {
	Type          string   `json:"type"`
	Name          string   `json:"name"`
	Location      string   `json:"location"`
	Data          string   `json:"data"`
	DataSecondary any      `json:"data_secondary"`
	X             int64    `json:"x"`
	Y             int64    `json:"y"`
	Id            any      `json:"id"`
	Created       int64    `json:"created"`
	Edited        int64    `json:"edited"`
	Icon          string   `json:"icon"`
	Size          int64    `json:"size"`
	Permissions   []string `json:"permissions"`
	UUID          string   `json:"uuid"`
}

type FolderEntryStruct struct {
	Name          string   `json:"name"`
	Location      string   `json:"location"`
	Data          []any    `json:"data"`
	DataSecondary any      `json:"data_secondary"`
	X             int64    `json:"x"`
	Y             int64    `json:"y"`
	Id            any      `json:"id"`
	Created       int64    `json:"created"`
	Edited        int64    `json:"edited"`
	Icon          string   `json:"icon"`
	Size          int64    `json:"size"`
	Permissions   []string `json:"permissions"`
	UUID          string   `json:"uuid"`
}

type GetFileSizesRequest struct {
	UUIDs []string `json:"uuids" binding:"required"`
}

type FileStat struct {
	UUID    string    `json:"uuid"`
	Size    int64     `json:"size,omitempty"`
	ModTime time.Time `json:"mtime,omitempty"`
	Ok      bool      `json:"ok"`
}

var fs *FileSystem = NewFileSystem()

func updateFiles(c *gin.Context) {
	user := c.MustGet("user").(*User)

	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read request body"})
		return
	}

	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var req UpdateFileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fmt.Println("Raw body:", string(bodyBytes))

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

func getFileSizes(c *gin.Context) {
	user := c.MustGet("user").(*User)
	username := user.GetUsername()

	var req GetFileSizesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	stats, err := fs.GetFileStats(username, req.UUIDs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"stats": stats})
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

func getFileByPath(c *gin.Context) {
	user := c.MustGet("user").(*User)
	username := user.GetUsername()

	path := c.Param("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Path is required"})
		return
	}

	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	path = strings.ToLower(path)

	index, err := fs.loadPathIndex(username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load path index"})
		return
	}

	uuid, ok := index[path]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "File not found"})
		return
	}

	entry, err := fs.GetFileByUUID(username, uuid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	data, err := json.Marshal(entry)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to serialize file"})
		return
	}

	c.Header("Content-Length", fmt.Sprintf("%d", len(data)))
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Cache-Control", "no-cache, max-age=0")
	c.Data(http.StatusOK, "application/octet-stream", data)
}

func getPathIndex(c *gin.Context) {
	user := c.MustGet("user").(*User)
	username := user.GetUsername()

	index, err := fs.loadPathIndex(username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load path index"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"index": index, "username": username})
}

const (
	fileEntrySize = 14
	fileDir       = "./rotur/files"

	defaultOFSF = "./rotur/base.ofsf"
)

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

func (fs *FileSystem) HandleOFSFUpdate(username Username, updates []UpdateChange, maxSize int) UpdateResult {

	fmt.Printf("\033[92m[+] OFSF\033[0m | %s processing %d file updates\n", username, len(updates))

	if err := fs.migrateFromLegacy(username); err != nil {
		fmt.Printf("\033[91m[-] OFSF Error\033[0m | Migration failed: %v\n", err)
	}

	// Process all updates while holding the lock
	fs.mu.Lock()
	for _, change := range updates {
		switch change.Command {
		case "UUIDa":
			fs.handleAddUnsafe(username, change)
		case "UUIDr":
			fs.handleReplaceUnsafe(username, change)
		case "UUIDd":
			fs.handleDeleteUnsafe(username, change)
		}
	}
	fs.mu.Unlock()

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

func extractIndex(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x) - 1
	case int:
		return x - 1
	case string:
		var i int
		fmt.Sscanf(x, "%d", &i)
		return i - 1
	default:
		return 0
	}
}

// handleAddUnsafe assumes the lock is already held
func (fs *FileSystem) handleAddUnsafe(username Username, change UpdateChange) {
	if len(change.UUID) != 32 {
		return
	}
	path := filepath.Join(fileDir, string(username), change.UUID+".json")

	if _, err := os.Stat(path); err == nil {
		return
	}

	dta, ok := change.Dta.([]any)
	if !ok || len(dta) > fileEntrySize {
		return
	}

	dta[7] = time.Now().UnixMilli()
	dta[8] = dta[7]

	meta := FileMetadata{
		Entry: dta,
		Index: 0,
	}

	data, err := json.Marshal(meta)
	if err != nil {
		log.Printf("Error marshaling metadata: %v", err)
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		log.Printf("Error writing file %s: %v", path, err)
		return
	}

	// Load and update path index (unsafe version - no locking)
	idx, _ := fs.loadPathIndexUnsafe(username)
	idx[entryToPath(dta)] = change.UUID
	fs.savePathIndexUnsafe(username, idx)
}

// handleReplaceUnsafe assumes the lock is already held
func (fs *FileSystem) handleReplaceUnsafe(username Username, change UpdateChange) {
	entry, err := fs.getFileByUUIDUnsafe(username, change.UUID)
	if err != nil {
		return
	}

	oldPath := entryToPath(entry)

	idx := extractIndex(change.Idx)
	entry[8] = time.Now().UnixMilli()
	if idx >= 0 && idx < len(entry) {
		entry[idx] = change.Dta
	}

	newPath := entryToPath(entry)

	fs.setFileByUUIDUnsafe(username, change.UUID, entry)

	if oldPath != newPath {
		index, _ := fs.loadPathIndexUnsafe(username)
		delete(index, oldPath)
		index[newPath] = change.UUID
		fs.savePathIndexUnsafe(username, index)
	}
}

// handleDeleteUnsafe assumes the lock is already held
func (fs *FileSystem) handleDeleteUnsafe(username Username, change UpdateChange) {
	filePath := filepath.Join(fileDir, string(username), change.UUID+".json")
	os.Remove(filePath)

	idx, _ := fs.loadPathIndexUnsafe(username)
	for path, uuid := range idx {
		if uuid == change.UUID {
			delete(idx, path)
			break
		}
	}

	fs.savePathIndexUnsafe(username, idx)
}

func userIndexPath(username Username) string {
	return filepath.Join(fileDir, string(username), ".index.json")
}

func (fs *FileSystem) RenameUserFileSystem(oldUsername Username, newUsername Username) {
	index, err := fs.loadPathIndex(oldUsername)
	if err != nil {
		fmt.Printf("\033[91m[-] OFSF Error\033[0m | Failed to load path index: %v\n", err)
		return
	}

	oldLocationPrefix := strings.ToLower("origin/(c) users/" + oldUsername.String())
	newLocationPrefix := strings.ToLower("origin/(c) users/" + newUsername.String())
	fs.mu.Lock()
	defer fs.mu.Unlock()
	for path, uuid := range index {
		cut, ok := strings.CutPrefix(strings.ToLower(path), oldLocationPrefix)
		if !ok {
			continue
		}
		newPath := newLocationPrefix + cut
		index[newPath] = uuid
		fs.handleReplaceUnsafe(oldUsername, UpdateChange{
			Command: "UUIDr",
			UUID:    uuid,
			Dta:     []any{newPath},
			Idx:     2,
		})
		delete(index, path)
	}

	fs.savePathIndexUnsafe(oldUsername, index)

	oldUserDir := filepath.Join(fileDir, string(oldUsername))
	newUserDir := filepath.Join(fileDir, string(newUsername))
	if err := os.Rename(oldUserDir, newUserDir); err != nil {
		fmt.Printf("\033[91m[-] OFSF Error\033[0m | Failed to rename user directory: %v\n", err)
	}
}

type PathIndex map[string]string

// rebuildPathIndexUnsafe assumes the lock is already held
func (fs *FileSystem) rebuildPathIndexUnsafe(username Username) (PathIndex, error) {
	userDir := filepath.Join(fileDir, string(username))

	idx := make(PathIndex)

	entries, err := os.ReadDir(userDir)
	if err != nil {
		if os.IsNotExist(err) {
			if err := fs.savePathIndexUnsafe(username, idx); err != nil {
				return nil, err
			}
			return idx, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		if entry.Name() == ".index.json" {
			continue
		}

		filePath := filepath.Join(userDir, entry.Name())

		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		var meta FileMetadata
		if err := json.Unmarshal(data, &meta); err != nil || meta.Entry == nil {
			continue
		}

		path := entryToPath(meta.Entry)
		uuid := strings.TrimSuffix(entry.Name(), ".json")

		idx[path] = uuid
	}

	if err := fs.savePathIndexUnsafe(username, idx); err != nil {
		return nil, err
	}

	fmt.Printf("\033[93m[~] OFSF\033[0m | Rebuilt path index for %s (%d entries)\n",
		username, len(idx))

	return idx, nil
}

// loadPathIndexUnsafe assumes the lock is already held
func (fs *FileSystem) loadPathIndexUnsafe(username Username) (PathIndex, error) {
	path := userIndexPath(username)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fs.rebuildPathIndexUnsafe(username)
		}
		return nil, err
	}

	var idx PathIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return fs.rebuildPathIndexUnsafe(username)
	}

	return idx, nil
}

// loadPathIndex is the public version that acquires the lock
func (fs *FileSystem) loadPathIndex(username Username) (PathIndex, error) {
	if err := fs.migrateFromLegacy(username); err != nil {
		fmt.Printf("\033[91m[-] OFSF Error\033[0m | Migration failed: %v\n", err)
	}

	path := userIndexPath(username)

	// First check if index exists with read lock
	fs.mu.RLock()
	_, err := os.Stat(path)
	fs.mu.RUnlock()

	if err == nil {
		fs.mu.RLock()
		data, readErr := os.ReadFile(path)
		fs.mu.RUnlock()

		if readErr == nil {
			fmt.Println("Loading path index for", username)
			var idx PathIndex
			if unmarshalErr := json.Unmarshal(data, &idx); unmarshalErr == nil {
				return idx, nil
			}
		}
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	fmt.Println("Rebuilding path index for", username)

	return fs.rebuildPathIndexUnsafe(username)
}

// savePathIndexUnsafe assumes the lock is already held
func (fs *FileSystem) savePathIndexUnsafe(username Username, idx PathIndex) error {
	path := userIndexPath(username)

	tmp := path + ".tmp"
	data, err := json.Marshal(idx)
	if err != nil {
		return err
	}

	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}

	return os.Rename(tmp, path) // atomic on POSIX
}

func entryToPath(entry FileEntry) string {
	return strings.ToLower(getStringOrEmpty(entry[2]) + "/" +
		getStringOrEmpty(entry[1]) +
		getStringOrEmpty(entry[0]))
}

func (fs *FileSystem) GetFileStats(username Username, uuids []string) ([]FileStat, error) {
	if err := fs.migrateFromLegacy(username); err != nil {
		return nil, err
	}

	fs.mu.RLock()
	defer fs.mu.RUnlock()

	userDir := filepath.Join(fileDir, string(username))
	stats := make([]FileStat, 0, len(uuids))

	for _, uuid := range uuids {
		path := filepath.Join(userDir, uuid+".json")

		info, err := os.Stat(path)
		if err != nil {
			stats = append(stats, FileStat{
				UUID: uuid,
				Ok:   false,
			})
			continue
		}

		stats = append(stats, FileStat{
			UUID:    uuid,
			Size:    info.Size(),
			ModTime: info.ModTime().UTC(),
			Ok:      true,
		})
	}

	return stats, nil
}

func (fs *FileSystem) migrateFromLegacy(username Username) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	legacyPath := filepath.Join(fileDir, string(username)+".ofsf")

	newPath := filepath.Join(fileDir, string(username))
	if dirExists(newPath) {
		return nil
	}
	if !fileExists(legacyPath) {
		copyAndReplace(defaultOFSF, legacyPath, "${USERNAME}", username.String())
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

	userDir := filepath.Join(fileDir, username.String())
	if err := os.MkdirAll(userDir, 0755); err != nil {
		return err
	}

	pathIndex := PathIndex{}

	index := 0
	for i := 0; i+fileEntrySize <= len(filesList); i += fileEntrySize {
		entry := filesList[i : i+fileEntrySize]
		if uuid, ok := entry[13].(string); ok {
			metadata := FileMetadata{
				Entry: entry,
				Index: index,
			}
			internalPath := entryToPath(entry)
			pathIndex[internalPath] = uuid
			entryData, err := json.Marshal(metadata)
			if err != nil {
				log.Printf("Error marshaling entry data: %v", err)
				continue
			}
			filePath := filepath.Join(userDir, uuid+".json")
			if err := os.WriteFile(filePath, entryData, 0644); err != nil {
				log.Printf("Error writing file %s: %v", filePath, err)
				continue
			}
			index++
		}
	}

	filePath := filepath.Join(userDir, ".index.json")
	data, err = json.Marshal(pathIndex)
	if err == nil {
		if writeErr := os.WriteFile(filePath, data, 0644); writeErr != nil {
			log.Printf("Error writing index file %s: %v", filePath, writeErr)
		}
	} else {
		log.Printf("Error marshaling path index: %v", err)
	}

	os.Remove(legacyPath)
	fmt.Printf("\033[92m[+] OFSF\033[0m | Migration complete for %s\n", username)

	return nil
}

// getFileByUUIDUnsafe assumes the lock is already held
func (fs *FileSystem) getFileByUUIDUnsafe(username Username, uuid string) (FileEntry, error) {
	userDir := filepath.Join(fileDir, username.String())

	filePath := filepath.Join(userDir, uuid+".json")

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var metadata FileMetadata
	err = json.Unmarshal(data, &metadata)
	if err != nil || metadata.Entry == nil {
		return nil, fmt.Errorf("file not found with the provided UUID")
	}

	if metadata.Entry[0] != ".folder" {
		switch metadata.Entry[3].(type) {
		case map[string]any, []any:
			metadata.Entry[3] = JSONStringify(metadata.Entry[3])
		}
	}
	return metadata.Entry, nil
}

// GetFileByUUID is the public version that acquires the lock
func (fs *FileSystem) GetFileByUUID(username Username, uuid string) (FileEntry, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.getFileByUUIDUnsafe(username, uuid)
}

// setFileByUUIDUnsafe assumes the lock is already held
func (fs *FileSystem) setFileByUUIDUnsafe(username Username, uuid string, file FileEntry) error {
	userDir := filepath.Join(fileDir, username.String())

	filePath := filepath.Join(userDir, uuid+".json")

	if file[0] != ".folder" {
		switch file[3].(type) {
		case map[string]any, []any:
			file[3] = JSONStringify(file[3])
		}
	}

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

// SetFileByUUID is the public version that acquires the lock
func (fs *FileSystem) SetFileByUUID(username Username, uuid string, file FileEntry) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.setFileByUUIDUnsafe(username, uuid, file)
}

func (fs *FileSystem) calculateTotalSize(username Username) (int, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	userDir := filepath.Join(fileDir, username.String())
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

func (fs *FileSystem) GetFilesByUUIDs(username Username, uuids []string) (map[string]FileEntry, error) {
	if err := fs.migrateFromLegacy(username); err != nil {
		fmt.Printf("\033[91m[-] OFSF Error\033[0m | Migration failed: %v\n", err)
	}
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	result := make(map[string]FileEntry)
	userDir := filepath.Join(fileDir, username.String())

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

func (fs *FileSystem) DeleteUserFileSystem(username Username) error {
	if err := fs.migrateFromLegacy(username); err != nil {
		fmt.Printf("\033[91m[-] OFSF Error\033[0m | Migration failed: %v\n", err)
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()

	userDir := filepath.Join(fileDir, username.String())

	if err := os.RemoveAll(userDir); err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("No files found for user %s to delete\n", username)
			return nil
		}
		return err
	}

	legacyPath := filepath.Join(fileDir, username.String()+".ofsf")
	os.Remove(legacyPath)

	fmt.Printf("Successfully deleted files for user %s\n", username)
	return nil
}

func (fs *FileSystem) GetUserFileSize(username Username) (string, error) {
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

func (fs *FileSystem) GetFilesIndexWithThreshold(username Username, sizeThreshold int) ([]any, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	userDir := filepath.Join(fileDir, username.String())
	entries, err := os.ReadDir(userDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var allEntries []FileMetadata

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
			entryCopy := make(FileEntry, len(metadata.Entry))
			copy(entryCopy, metadata.Entry)

			if sizeThreshold > 0 && len(entryCopy) == 14 {
				if entryCopy[0] == ".folder" {
					arr, ok := entryCopy[3].([]any)
					if ok {
						entryCopy[3] = JSONStringify(arr)
						entryCopy[11] = len(arr)
					}
				} else {
					dataStr := ""
					switch entryCopy[3].(type) {
					case string:
						dataStr = entryCopy[3].(string)
					case []any, map[string]any:
						dataStr = JSONStringify(entryCopy[3])
						entryCopy[3] = dataStr
					}
					entryCopy[11] = len(dataStr)
					if entryCopy[11].(int) > sizeThreshold {
						entryCopy[3] = false
					}
				}
			}

			metadata.Entry = entryCopy
			allEntries = append(allEntries, metadata)
		}
	}

	sort.Slice(allEntries, func(i, j int) bool {
		return len(allEntries[i].Entry[2].(string)) < len(allEntries[j].Entry[2].(string))
	})

	result := make([]any, 0)
	for _, meta := range allEntries {
		result = append(result, meta.Entry...)
	}

	return result, nil
}
