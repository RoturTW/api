package main

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

func serveOverlayAsset(c *gin.Context) {

	raw := strings.TrimPrefix(c.Param("filepath"), "/")
	if raw == "" {
		c.Status(http.StatusBadRequest)
		return
	}

	if strings.Contains(raw, "..") {
		c.Status(http.StatusBadRequest)
		return
	}

	if !strings.HasSuffix(strings.ToLower(raw), ".gif") {
		c.Status(http.StatusBadRequest)
		return
	}

	cleaned := filepath.Clean(raw)
	if cleaned != raw {
		c.Status(http.StatusBadRequest)
		return
	}

	base, _ := filepath.Abs(filepath.Join(COSMETICS_ASSETS_PATH, "overlays"))
	targetPath := filepath.Join(base, cleaned)
	resolved, err := filepath.Abs(targetPath)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(resolved, base+string(filepath.Separator)) {
		c.Status(http.StatusBadRequest)
		return
	}

	if !fileExists(resolved) {
		c.Status(http.StatusNotFound)
		return
	}

	c.File(resolved)
}
