package main

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nfnt/resize"
)

var (
	avatarBaseDir        string
	bannerBaseDir        string
	defaultAvatarContent []byte
	defaultAvatarEtag    string
	defaultBannerContent []byte
)

const defaultAvatarURL = "https://raw.githubusercontent.com/Mistium/Origin-OS/main/Resources/no-pfp.jpeg"

func loadAvatarConfig() {
	documentPath := os.Getenv("HOME")
	if documentPath == "" {
		documentPath = "/tmp"
	}
	avatarBaseDir = mustEnv("AVATAR_DIR", filepath.Join(documentPath, "Documents", "rotur", "avatars"))
	bannerBaseDir = mustEnv("BANNER_DIR", filepath.Join(documentPath, "Documents", "rotur", "banners"))
}

func init() {
	loadAvatarConfig()
	// Try to fetch the real default image; fall back to generated placeholder
	resp, err := http.Get(defaultAvatarURL)
	if err != nil || resp.StatusCode != 200 {
		log.Printf("[avatars] could not load default avatar from URL, using placeholder")
		loadDefaultAvatar()
	} else {
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			loadDefaultAvatar()
		} else {
			defaultAvatarContent = body
			defaultAvatarEtag = fmt.Sprintf("%x", md5.Sum(body))
		}
	}
	loadDefaultBanner()
	loadOverlays()
}

// --- Avatar metadata helpers ---

func getAvatarMetadata(username string) (filePath, contentType, etag string, err error) {
	base := strings.ToLower(username)
	for _, ext := range []string{".gif", ".jpg"} {
		fp := filepath.Join(avatarBaseDir, base+ext)
		info, statErr := os.Stat(fp)
		if statErr == nil {
			ct := "image/jpeg"
			if ext == ".gif" {
				ct = "image/gif"
			}
			return fp, ct, fmt.Sprintf("%s-%d", username, info.ModTime().Unix()), nil
		}
	}
	return "", "", "", os.ErrNotExist
}

func deleteAvatars(username string) {
	base := strings.ToLower(username)
	for _, ext := range []string{".gif", ".jpg"} {
		os.Remove(filepath.Join(avatarBaseDir, base+ext))
	}
}

// --- Avatar handler ---

func avatarHandler(c *gin.Context) {
	username, _ := strings.CutSuffix(strings.ToLower(c.Param("username")), ".gif")
	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username is required"})
		return
	}

	radiusStr := c.Query("radius")
	sizeStr := c.Query("s")
	clientEtag := c.GetHeader("If-None-Match")

	filePath, contentType, baseEtag, metaErr := getAvatarMetadata(username)

	user, userErr := getAccountByUsername(Username(username))
	tier := "Free"
	if userErr == nil {
		tier = user.GetSubscription().Tier
	}
	isPro := hasTierOrHigher(tier, "Drive")
	forceFirstFrameJpeg := !isPro && metaErr == nil && contentType == "image/gif"
	finalEtagBase := baseEtag
	if metaErr != nil {
		contentType = "image/jpeg"
		finalEtagBase = defaultAvatarEtag
	}

	var sb strings.Builder
	sb.WriteString(finalEtagBase)
	if sizeStr != "" {
		sb.WriteString("-s=")
		sb.WriteString(sizeStr)
	}
	if radiusStr != "" {
		sb.WriteString("-r=")
		sb.WriteString(radiusStr)
	}
	if forceFirstFrameJpeg {
		sb.WriteString("-ffjpg")
		contentType = "image/jpeg"
	}

	cacheKey := sb.String()
	modifier := sizeStr != "" || radiusStr != ""

	if !modifier && metaErr == nil && !forceFirstFrameJpeg {
		etagQuoted := `"` + finalEtagBase + `"`
		if clientEtag == etagQuoted {
			c.Status(http.StatusNotModified)
			return
		}
		c.Header("ETag", etagQuoted)
		c.Header("Content-Type", contentType)
		c.Header("Cache-Control", "public, max-age=0, must-revalidate")
		if c.Request.Method == http.MethodHead {
			c.Status(200)
			return
		}
		c.File(filePath)
		return
	}

	etagQuoted := `"` + cacheKey + `"`
	if c.Request.Method == http.MethodHead {
		c.Header("Content-Type", contentType)
		c.Header("Cache-Control", "public, max-age=0, must-revalidate")
		c.Header("ETag", etagQuoted)
		c.Status(200)
		return
	}

	if cached, ct, ok := avatarCache.Get(cacheKey); ok {
		if clientEtag == etagQuoted {
			c.Status(http.StatusNotModified)
			return
		}
		c.Header("ETag", etagQuoted)
		c.Header("Cache-Control", "public, max-age=0, must-revalidate")
		c.Data(http.StatusOK, ct, cached)
		return
	}

	// Load image data once
	var imageData []byte
	if metaErr != nil {
		imageData = defaultAvatarContent
		contentType = "image/jpeg"
	} else {
		var err error
		imageData, err = os.ReadFile(filePath)
		if err != nil {
			imageData = defaultAvatarContent
			contentType = "image/jpeg"
		}
	}

	if forceFirstFrameJpeg {
		if img, err := decodeFirstGIFFrame(imageData); err == nil {
			if encoded, err := encodeJPEG(img, 85); err == nil {
				imageData = encoded
				contentType = "image/jpeg"
			}
		}
	}

	// --- GIF path ---
	if contentType == "image/gif" {
		if sizeStr != "" {
			if sz, err := strconv.Atoi(sizeStr); err == nil && sz > 0 && sz <= 256 {
				if resized, err := resizeGIF(imageData, sz, sz); err == nil {
					imageData = resized
				}
			}
		}

		if radiusStr != "" {
			radiusInt, err := strconv.Atoi(strings.TrimSuffix(radiusStr, "px"))
			if err == nil && radiusInt > 0 {
				if radiusInt >= 128 {
					radiusInt = 128
				}
				if src, err := gif.DecodeAll(bytes.NewReader(imageData)); err == nil {
					if rounded, err := roundGIF(src, radiusInt); err == nil {
						buf := avatarBufPool.Get().(*bytes.Buffer)
						buf.Reset()
						defer avatarBufPool.Put(buf)
						if err := gif.EncodeAll(buf, rounded); err == nil {
							imageData = make([]byte, buf.Len())
							copy(imageData, buf.Bytes())
						}
					}
				}
			}
		}

		avatarCache.Set(cacheKey, imageData, "image/gif")
		if clientEtag == etagQuoted {
			c.Status(http.StatusNotModified)
			return
		}
		c.Header("Content-Type", "image/gif")
		c.Header("Cache-Control", "public, max-age=0, must-revalidate")
		c.Header("ETag", etagQuoted)
		c.Data(http.StatusOK, "image/gif", imageData)
		return
	}

	// --- Non-GIF path: decode once, reuse img across all transforms ---
	img, _, err := image.Decode(bytes.NewReader(imageData))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Error decoding image"})
		return
	}

	if radiusStr != "" {
		radiusInt, err := strconv.Atoi(strings.TrimSuffix(radiusStr, "px"))
		if err == nil && radiusInt > 0 {
			if rounded, newCt, err := roundCorners(imageData, radiusInt); err == nil {
				imageData = rounded
				contentType = newCt
				// Re-decode so overlay/resize work on rounded image
				img, _, _ = image.Decode(bytes.NewReader(imageData))
			}
		}
	}

	if sizeStr != "" {
		if sz, err := strconv.Atoi(sizeStr); err == nil && sz > 0 && sz <= 256 {
			resized := resize.Resize(uint(sz), 0, img, resize.Lanczos3)
			buf := avatarBufPool.Get().(*bytes.Buffer)
			buf.Reset()
			defer avatarBufPool.Put(buf)
			if contentType == "image/png" {
				png.Encode(buf, resized)
			} else {
				jpeg.Encode(buf, resized, &jpeg.Options{Quality: 85})
			}
			imageData = make([]byte, buf.Len())
			copy(imageData, buf.Bytes())
			contentType = "image/jpeg"
		}
	}

	avatarCache.Set(cacheKey, imageData, contentType)
	if clientEtag == etagQuoted {
		c.Status(http.StatusNotModified)
		return
	}
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "public, max-age=86400, must-revalidate")
	c.Header("ETag", etagQuoted)
	c.Data(http.StatusOK, contentType, imageData)
}

func overlayHandler(c *gin.Context) {
	username, _ := strings.CutSuffix(strings.ToLower(c.Param("username")), ".gif")

	user, err := getAccountByUsername(Username(username))
	if err != nil {
		sendEmpty(c)
		return
	}

	overlayName := user.GetString("sys.overlay")
	if overlayName == "" {
		sendEmpty(c)
		return
	}

	path := filepath.Join("./overlays", overlayName+".gif")
	if !fileExists(path) {
		sendEmpty(c)
		return
	}

	c.File(path)
}

func sendEmpty(c *gin.Context) {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Data(http.StatusOK, "image/png", buf.Bytes())
}

// decodeGIFFrame decodes a single composited frame from a GIF at the given index.
func decodeGIFFrame(g *gif.GIF, frameIdx int) image.Image {
	if frameIdx >= len(g.Image) {
		return nil
	}
	bounds := image.Rect(0, 0, g.Config.Width, g.Config.Height)
	dst := image.NewRGBA(bounds)
	for i := 0; i <= frameIdx; i++ {
		frame := g.Image[i]
		draw.Draw(dst, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
	}
	return dst
}

func uploadPfpHandler(c *gin.Context) {
	var req struct {
		Image string `json:"image"`
		Token string `json:"token"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON data"})
		return
	}

	user := authenticateWithKey(req.Token)
	if user == nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "Invalid token"})
		return
	}
	if req.Image == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing image"})
		return
	}

	parts := strings.Split(req.Image, ",")
	if len(parts) != 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid image format"})
		return
	}

	mimeHeader := parts[0]
	estimatedSize := (len(parts[1]) * 3) / 4
	if estimatedSize > 10*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Image too large"})
		return
	}

	imageData, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid image data"})
		return
	}

	os.MkdirAll(avatarBaseDir, 0755)
	username := strings.ToLower(string(user.GetUsername()))
	tier := strings.ToLower(user.GetSubscription().Tier)
	benefits := subs_benefits[tier]

	var ext, contentType string
	switch {
	case strings.Contains(mimeHeader, "image/gif"):
		if benefits.Has_Animated_Pfp {
			ext = ".gif"
			contentType = "image/gif"
		} else {
			ext = ".jpg"
			contentType = "image/jpeg"
		}
	default:
		ext = ".jpg"
		contentType = "image/jpeg"
	}

	deleteAvatars(username)
	filePath := filepath.Join(avatarBaseDir, username+ext)

	if contentType == "image/gif" {
		resizedData, err := resizeGIF(imageData, 256, 256)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error resizing GIF"})
			return
		}
		if err := os.WriteFile(filePath, resizedData, 0644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error saving GIF"})
			return
		}
	} else {
		img, _, err := image.Decode(bytes.NewReader(imageData))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Error decoding image"})
			return
		}
		resized := resize.Resize(256, 256, img, resize.Lanczos3)
		out, err := os.Create(filePath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error saving image"})
			return
		}
		defer out.Close()
		jpeg.Encode(out, resized, &jpeg.Options{Quality: 85})
	}

	avatarCache.Clear()
	c.JSON(http.StatusOK, gin.H{
		"status":  "Success",
		"message": "Profile picture uploaded successfully",
	})
}

// --- Banner handler ---

func getBannerPath(username string) (string, string, string, time.Time, error) {
	base := strings.ToLower(username)
	for _, ext := range []string{".gif", ".jpg", ".png"} {
		fp := filepath.Join(bannerBaseDir, base+ext)
		fi, err := os.Stat(fp)
		if err == nil {
			ct := "image/jpeg"
			switch ext {
			case ".gif":
				ct = "image/gif"
			case ".png":
				ct = "image/png"
			}
			return fp, ct, fmt.Sprintf("%s-%d", username, time.Now().Unix()), fi.ModTime(), nil
		}
	}
	return "", "", "", time.Time{}, os.ErrNotExist
}

func deleteBanners(username string) {
	base := strings.ToLower(username)
	for _, ext := range []string{".gif", ".jpg", ".png"} {
		os.Remove(filepath.Join(bannerBaseDir, base+ext))
	}
}

func bannerHandler(c *gin.Context) {
	username, _ := strings.CutSuffix(strings.ToLower(c.Param("username")), ".gif")
	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username is required"})
		return
	}

	radiusStr := c.Query("radius")
	radiusInt, parseErr := strconv.Atoi(strings.TrimSuffix(radiusStr, "px"))
	needRounding := radiusStr != "" && parseErr == nil && radiusInt > 0

	user, userErr := getAccountByUsername(Username(username))
	tier := "Free"
	if userErr == nil {
		tier = user.GetSubscription().Tier
	}
	isPro := hasTierOrHigher(tier, "Pro")

	bannerPath, contentType, etag, modTime, err := getBannerPath(username)
	forceFirstFrameJpeg := !isPro && err == nil && contentType == "image/gif"

	if forceFirstFrameJpeg {
		contentType = "image/jpeg"
		if etag != "" {
			etag = etag + "-ffjpg"
		}
	}

	var imageData []byte
	if err != nil {
		imageData = defaultBannerContent
		contentType = "image/jpeg"
		needRounding = false
	}

	if !needRounding {
		c.Header("Content-Type", contentType)
		if etag != "" {
			c.Header("ETag", etag)
		}
		if !modTime.IsZero() {
			c.Header("Last-Modified", modTime.Format(http.TimeFormat))
		}
		if contentType == "image/gif" {
			c.Header("Cache-Control", "public, max-age=86400, must-revalidate")
		} else {
			c.Header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		}
		if c.Request.Method == http.MethodHead {
			c.Status(200)
			return
		}
		if forceFirstFrameJpeg {
			if bannerPath != "" {
				imageData, err = os.ReadFile(bannerPath)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading banner file"})
					return
				}
			}
			img, err := decodeFirstGIFFrame(imageData)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error decoding GIF"})
				return
			}
			jpegData, err := encodeJPEG(img, 85)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error encoding JPEG"})
				return
			}
			c.Data(http.StatusOK, "image/jpeg", jpegData)
			return
		}
		if bannerPath != "" {
			c.File(bannerPath)
		} else {
			c.Data(http.StatusOK, contentType, imageData)
		}
		return
	}

	if c.Request.Method == http.MethodHead {
		c.Header("Content-Type", contentType)
		c.Status(200)
		return
	}

	if bannerPath != "" {
		imageData, err = os.ReadFile(bannerPath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error reading banner file"})
			return
		}
	}

	if forceFirstFrameJpeg {
		img, err := decodeFirstGIFFrame(imageData)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error decoding GIF"})
			return
		}
		imageData, err = encodeJPEG(img, 85)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error encoding JPEG"})
			return
		}
		contentType = "image/jpeg"
	}

	if contentType == "image/gif" {
		src, err := gif.DecodeAll(bytes.NewReader(imageData))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error decoding GIF"})
			return
		}
		rounded, err := roundGIF(src, radiusInt)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error rounding GIF"})
			return
		}
		buf := bytes.NewBuffer(nil)
		if err := gif.EncodeAll(buf, rounded); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error encoding GIF"})
			return
		}
		c.Header("Content-Type", "image/gif")
		c.Header("Cache-Control", "public, max-age=86400, must-revalidate")
		c.Data(http.StatusOK, "image/gif", buf.Bytes())
		return
	}

	rounded, newContentType, err := roundCorners(imageData, radiusInt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error rounding image"})
		return
	}
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	c.Data(http.StatusOK, newContentType, rounded)
}

func reloadOverlays(c *gin.Context) {
	if !isAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden"})
		return
	}
	loadOverlays()
	c.JSON(http.StatusOK, gin.H{"status": "Success"})
}

// --- Upload banner handler ---

func uploadBannerHandler(c *gin.Context) {
	var req struct {
		Image string `json:"image"`
		Token string `json:"token"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON data"})
		return
	}

	user := authenticateWithKey(req.Token)
	if user == nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "Invalid token"})
		return
	}
	if req.Image == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing image"})
		return
	}

	parts := strings.Split(req.Image, ",")
	if len(parts) != 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid image format"})
		return
	}

	mimeHeader := parts[0]
	imageData, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid image data"})
		return
	}
	if len(imageData) > 10*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Image too large"})
		return
	}

	img, _, err := image.Decode(bytes.NewReader(imageData))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Error decoding image"})
		return
	}

	tier := strings.ToLower(user.GetSubscription().Tier)
	benefits := subs_benefits[tier]

	var ext, contentType string
	switch {
	case strings.Contains(mimeHeader, "image/gif"):
		if benefits.Has_Animated_Banner {
			ext = ".gif"
			contentType = "image/gif"
		} else {
			ext = ".jpg"
			contentType = "image/jpeg"
		}
	case strings.Contains(mimeHeader, "image/png"):
		ext = ".png"
		contentType = "image/png"
	default:
		ext = ".jpg"
		contentType = "image/jpeg"
	}

	username := strings.ToLower(string(user.GetUsername()))
	os.MkdirAll(bannerBaseDir, 0755)
	deleteBanners(username)
	filePath := filepath.Join(bannerBaseDir, username+ext)

	if contentType == "image/gif" {
		resizedData, err := resizeGIF(imageData, 900, 300)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error resizing GIF"})
			return
		}
		if err := os.WriteFile(filePath, resizedData, 0644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error saving GIF"})
			return
		}
	} else {
		resized := resize.Resize(900, 300, img, resize.Lanczos3)
		file, err := os.Create(filePath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error saving banner"})
			return
		}
		defer file.Close()
		if err := jpeg.Encode(file, resized, &jpeg.Options{Quality: 85}); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error encoding banner"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "Success",
		"message": "Banner uploaded successfully",
	})
}
