package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

	c.JSON(http.StatusOK, gin.H{"index": index})
}

const (
	fileEntrySize = 14
	fileDir       = "./rotur/files"

	defaultOFSF = "./rotur/base.ofsf"
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

func (fs *FileSystem) handleAdd(username string, change UpdateChange) {
	fs.mu.Lock()
	path := filepath.Join(fileDir, username, change.UUID+".json")

	if _, err := os.Stat(path); err == nil {
		return
	}

	dta, ok := change.Dta.([]any)
	if !ok || len(dta) > fileEntrySize {
		return
	}

	meta := FileMetadata{
		Entry: dta,
		Index: 0,
	}

	data, _ := json.Marshal(meta)
	os.WriteFile(path, data, 0644)
	fs.mu.Unlock()

	idx, _ := fs.loadPathIndex(username)
	fs.mu.Lock()
	idx[entryToPath(dta)] = change.UUID
	fs.savePathIndex(username, idx)
	fs.mu.Unlock()
}

func (fs *FileSystem) handleReplace(username string, change UpdateChange) {
	entry, err := fs.GetFileByUUID(username, change.UUID)
	if err != nil {
		return
	}

	oldPath := entryToPath(entry)

	idx := extractIndex(change.Idx)
	if idx >= 0 && idx < len(entry) {
		entry[idx] = change.Dta
	}

	newPath := entryToPath(entry)

	fs.SetFileByUUID(username, change.UUID, entry)

	if oldPath != newPath {
		index, _ := fs.loadPathIndex(username)
		delete(index, oldPath)
		index[newPath] = change.UUID
		fs.savePathIndex(username, index)
	}
}

func (fs *FileSystem) handleDelete(username string, change UpdateChange) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	filePath := filepath.Join(fileDir, username, change.UUID+".json")
	os.Remove(filePath)

	idx, _ := fs.loadPathIndex(username)
	for path, uuid := range idx {
		if uuid == change.UUID {
			delete(idx, path)
			break
		}
	}

	fs.savePathIndex(username, idx)
}

func userIndexPath(username string) string {
	return filepath.Join(fileDir, username, ".index.json")
}

type PathIndex map[string]string

func (fs *FileSystem) rebuildPathIndex(username string) (PathIndex, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	userDir := filepath.Join(fileDir, username)

	idx := make(PathIndex)

	entries, err := os.ReadDir(userDir)
	if err != nil {
		if os.IsNotExist(err) {
			if err := fs.savePathIndex(username, idx); err != nil {
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

	if err := fs.savePathIndex(username, idx); err != nil {
		return nil, err
	}

	fmt.Printf("\033[93m[~] OFSF\033[0m | Rebuilt path index for %s (%d entries)\n",
		username, len(idx))

	return idx, nil
}

func (fs *FileSystem) loadPathIndex(username string) (PathIndex, error) {
	path := userIndexPath(username)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fs.rebuildPathIndex(username)
		}
		return nil, err
	}

	var idx PathIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return fs.rebuildPathIndex(username)
	}

	return idx, nil
}

func (fs *FileSystem) savePathIndex(username string, idx PathIndex) error {
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
	return getStringOrEmpty(entry[2]) + "/" +
		getStringOrEmpty(entry[1]) +
		getStringOrEmpty(entry[0])
}

func (fs *FileSystem) GetFileStats(username string, uuids []string) ([]FileStat, error) {
	if err := fs.migrateFromLegacy(username); err != nil {
		return nil, err
	}

	fs.mu.RLock()
	defer fs.mu.RUnlock()

	userDir := filepath.Join(fileDir, username)
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

func (fs *FileSystem) migrateFromLegacy(username string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	legacyPath := filepath.Join(fileDir, username+".ofsf")

	newPath := filepath.Join(fileDir, username)
	if exists, err := dirExists(newPath); err != nil {
		return err
	} else if !exists {
		copyAndReplace(defaultOFSF, legacyPath, "${USERNAME}", username)
	}

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
			entryData, _ := json.Marshal(metadata)
			filePath := filepath.Join(userDir, uuid+".json")
			os.WriteFile(filePath, entryData, 0644)
			index++
		}
	}

	filePath := filepath.Join(userDir, ".index.json")
	data, err = json.Marshal(pathIndex)
	if err == nil {
		os.WriteFile(filePath, data, 0644)
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

	userDir := filepath.Join(fileDir, username)
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

			if sizeThreshold > 0 && len(entryCopy) > 3 {
				if entryCopy[0] == ".folder" {
				} else if dataStr, ok := entryCopy[3].(string); ok && len(dataStr) > sizeThreshold {
					entryCopy[3] = false
				}
			}

			metadata.Entry = entryCopy
			allEntries = append(allEntries, metadata)
		}
	}

	sort.Slice(allEntries, func(i, j int) bool {
		return allEntries[i].Index < allEntries[j].Index
	})

	result := make([]any, 0)
	for _, meta := range allEntries {
		result = append(result, meta.Entry...)
	}

	return result, nil
}
