package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"sync"

	"github.com/esimov/colorquant"
	"github.com/logica0419/resigif"
)

// --- LRU cache for transformed images ---

type avatarCacheEntry struct {
	data []byte
	ct   string
	size int64
	key  string
	prev, next *avatarCacheEntry
}

type avatarLRUCache struct {
	mu       sync.RWMutex
	items    map[string]*avatarCacheEntry
	head     *avatarCacheEntry // most-recent
	tail     *avatarCacheEntry // least-recent
	maxItems int
	maxBytes int64
	curBytes int64
}

var avatarCache = newAvatarLRUCache(500, 100*1024*1024)

func newAvatarLRUCache(maxItems int, maxBytes int64) *avatarLRUCache {
	c := &avatarLRUCache{
		items:    make(map[string]*avatarCacheEntry),
		maxItems: maxItems,
		maxBytes: maxBytes,
	}
	return c
}

func (c *avatarLRUCache) Get(key string) ([]byte, string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.items[key]
	if !ok {
		return nil, "", false
	}
	return e.data, e.ct, true
}

func (c *avatarLRUCache) Set(key string, data []byte, ct string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	size := int64(len(data))

	// If key already exists, remove old entry first
	if existing, ok := c.items[key]; ok {
		c.curBytes -= existing.size
		c.removeEntry(existing)
	}

	for (len(c.items) >= c.maxItems || c.curBytes+size > c.maxBytes) && len(c.items) > 0 {
		c.evictOldest()
	}

	e := &avatarCacheEntry{data: data, ct: ct, size: size, key: key}
	c.items[key] = e
	c.curBytes += size
	c.pushFront(e)
}

func (c *avatarLRUCache) pushFront(e *avatarCacheEntry) {
	e.prev = nil
	e.next = c.head
	if c.head != nil {
		c.head.prev = e
	}
	c.head = e
	if c.tail == nil {
		c.tail = e
	}
}

func (c *avatarLRUCache) removeEntry(e *avatarCacheEntry) {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		c.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		c.tail = e.prev
	}
	e.prev = nil
	e.next = nil
}

func (c *avatarLRUCache) evictOldest() {
	if c.tail == nil {
		return
	}
	delete(c.items, c.tail.key)
	c.curBytes -= c.tail.size
	c.removeEntry(c.tail)
}

func (c *avatarLRUCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*avatarCacheEntry)
	c.head = nil
	c.tail = nil
	c.curBytes = 0
}

// --- Buffer pool ---

var avatarBufPool = sync.Pool{
	New: func() interface{} { return new(bytes.Buffer) },
}

// --- Image processing functions ---

// isPixelInCircle returns true if (x,y) falls within a circle of the given radius
// centered in a width x height rectangle.
func isPixelInCircle(x, y, width, height, radius int) bool {
	cx := width / 2
	cy := height / 2
	dx := x - cx
	dy := y - cy
	return dx*dx+dy*dy <= radius*radius
}

// applyCircleMask applies a circular mask to the image, making everything outside
// the circle transparent. Returns PNG-encoded bytes.
func applyCircleMask(imgData []byte, radius int) ([]byte, string, error) {
	img, _, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		return imgData, "image/jpeg", err
	}
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	// Auto-radius: if radius is 0 or >= min dimension/2, use a perfect circle
	if radius <= 0 || radius > w/2 || radius > h/2 {
		radius = min(w, h) / 2
	}

	result := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if isPixelInCircle(x-bounds.Min.X, y-bounds.Min.Y, w, h, radius) {
				result.Set(x, y, img.At(x, y))
			} else {
				result.Set(x, y, color.RGBA{0, 0, 0, 0})
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, result); err != nil {
		return imgData, "image/jpeg", err
	}
	return buf.Bytes(), "image/png", nil
}

// roundCorners applies rounded rectangle corners. Returns PNG-encoded bytes.
func roundCorners(imgData []byte, radius int) ([]byte, string, error) {
	img, _, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		return imgData, "image/jpeg", err
	}
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if radius > h/2 {
		radius = h / 2
	}

	result := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if isPixelInRoundedRect(x-bounds.Min.X, y-bounds.Min.Y, w, h, radius) {
				result.Set(x, y, img.At(x, y))
			} else {
				result.Set(x, y, color.RGBA{0, 0, 0, 0})
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, result); err != nil {
		return imgData, "image/jpeg", err
	}
	return buf.Bytes(), "image/png", nil
}

// isPixelInRoundedRect reports whether a pixel is inside a rounded rectangle.
func isPixelInRoundedRect(x, y, width, height, radius int) bool {
	corners := []struct{ cx, cy int }{
		{radius, radius},
		{width - radius - 1, radius},
		{radius, height - radius - 1},
		{width - radius - 1, height - radius - 1},
	}
	switch {
	case x < radius && y < radius:
		dx, dy := x-corners[0].cx, y-corners[0].cy
		return dx*dx+dy*dy <= radius*radius
	case x >= width-radius && y < radius:
		dx, dy := x-corners[1].cx, y-corners[1].cy
		return dx*dx+dy*dy <= radius*radius
	case x < radius && y >= height-radius:
		dx, dy := x-corners[2].cx, y-corners[2].cy
		return dx*dx+dy*dy <= radius*radius
	case x >= width-radius && y >= height-radius:
		dx, dy := x-corners[3].cx, y-corners[3].cy
		return dx*dx+dy*dy <= radius*radius
	default:
		return true
	}
}

// roundGIF applies a rounded rectangle mask to all frames of a GIF.
func roundGIF(src *gif.GIF, radius int) (*gif.GIF, error) {
	if len(src.Image) == 0 {
		return nil, fmt.Errorf("no frames in GIF")
	}
	bounds := image.Rect(0, 0, src.Config.Width, src.Config.Height)
	w, h := bounds.Dx(), bounds.Dy()
	if radius > w/2 {
		radius = w / 2
	}
	if radius > h/2 {
		radius = h / 2
	}
	if radius <= 0 {
		return src, nil
	}

	dst := &gif.GIF{
		LoopCount: src.LoopCount,
		Delay:     src.Delay,
		Disposal:  make([]byte, len(src.Disposal)),
		Image:     make([]*image.Paletted, len(src.Image)),
		Config:    src.Config,
	}

	var bgColor color.Color
	if src.BackgroundIndex < byte(len(src.Image[0].Palette)) {
		bgColor = src.Image[0].Palette[src.BackgroundIndex]
	} else {
		bgColor = color.Transparent
	}

	compositor := image.NewRGBA(bounds)
	draw.Draw(compositor, bounds, &image.Uniform{bgColor}, image.Point{}, draw.Src)

	var prev *image.RGBA
	frameRect := bounds

	for i := range src.Image {
		frame := src.Image[i]
		frameRect = frame.Bounds()

		if src.Disposal[i] == gif.DisposalPrevious {
			prev = image.NewRGBA(bounds)
			draw.Draw(prev, bounds, compositor, image.Point{}, draw.Src)
		}

		draw.Draw(compositor, frameRect, frame, frameRect.Min, draw.Over)

		inputRGBA := image.NewRGBA(bounds)
		draw.Draw(inputRGBA, bounds, compositor, image.Point{}, draw.Src)

		paletted := image.NewPaletted(bounds, palette.WebSafe)
		ditherer := colorquant.Dither{
			Filter: [][]float32{
				{0.0, 0.0, 7.0 / 16.0},
				{3.0 / 16.0, 5.0 / 16.0, 1.0 / 16.0},
			},
		}
		_, ok := uniqueColors(inputRGBA, 255)
		var outputRGBA *image.RGBA
		if !ok {
			outputRGBA = toRGBA(ditherer.Quantize(inputRGBA, paletted, 255, true, false))
		} else {
			unique := make(map[color.Color]struct{})
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					unique[inputRGBA.At(x, y)] = struct{}{}
				}
			}
			colorIndex := make(map[color.Color]uint8)
			var pal color.Palette
			idx := uint8(0)
			for col := range unique {
				pal = append(pal, col)
				colorIndex[col] = idx
				idx++
			}
			paletted.Palette = pal
			stride := paletted.Stride
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					paletted.Pix[y*stride+x] = colorIndex[inputRGBA.At(x, y)]
				}
			}
			outputRGBA = toRGBA(paletted)
		}

		// Apply rounded-rect mask
		pix := outputRGBA.Pix
		stride := outputRGBA.Stride
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if !isPixelInRoundedRect(x, y, w, h, radius) {
					pix[(y*stride+x*4)+3] = 0
				}
			}
		}

		// Ensure transparent color in palette
		hasTrans := false
		for _, c := range paletted.Palette {
			_, _, _, a := c.RGBA()
			if a == 0 {
				hasTrans = true
				break
			}
		}
		if !hasTrans {
			if len(paletted.Palette) >= 256 {
				return nil, fmt.Errorf("no room for transparent color after quantization")
			}
			paletted.Palette = append(paletted.Palette, color.Transparent)
		}

		var transIndex uint8
		for idx, c := range paletted.Palette {
			_, _, _, a := c.RGBA()
			if a == 0 {
				transIndex = uint8(idx)
				break
			}
		}

		stride = paletted.Stride
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				_, _, _, a := outputRGBA.At(x, y).RGBA()
				if a == 0 {
					paletted.Pix[y*stride+x] = transIndex
				}
			}
		}

		dst.Image[i] = paletted
		dst.Disposal[i] = gif.DisposalNone

		switch src.Disposal[i] {
		case gif.DisposalBackground:
			draw.Draw(compositor, frameRect, &image.Uniform{bgColor}, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			if prev != nil {
				draw.Draw(compositor, bounds, prev, image.Point{}, draw.Src)
			}
		}
	}
	return dst, nil
}

// circleMaskGIF applies a circular mask to all frames of a GIF.
func circleMaskGIF(src *gif.GIF, radius int) (*gif.GIF, error) {
	if len(src.Image) == 0 {
		return nil, fmt.Errorf("no frames in GIF")
	}
	bounds := image.Rect(0, 0, src.Config.Width, src.Config.Height)
	w, h := bounds.Dx(), bounds.Dy()
	if radius <= 0 || radius > w/2 || radius > h/2 {
		radius = min(w, h) / 2
	}

	dst := &gif.GIF{
		LoopCount: src.LoopCount,
		Delay:     src.Delay,
		Disposal:  make([]byte, len(src.Disposal)),
		Image:     make([]*image.Paletted, len(src.Image)),
		Config:    src.Config,
	}

	var bgColor color.Color
	if src.BackgroundIndex < byte(len(src.Image[0].Palette)) {
		bgColor = src.Image[0].Palette[src.BackgroundIndex]
	} else {
		bgColor = color.Transparent
	}

	compositor := image.NewRGBA(bounds)
	draw.Draw(compositor, bounds, &image.Uniform{bgColor}, image.Point{}, draw.Src)

	var prev *image.RGBA
	frameRect := bounds

	for i := range src.Image {
		frame := src.Image[i]
		frameRect = frame.Bounds()

		if src.Disposal[i] == gif.DisposalPrevious {
			prev = image.NewRGBA(bounds)
			draw.Draw(prev, bounds, compositor, image.Point{}, draw.Src)
		}

		draw.Draw(compositor, frameRect, frame, frameRect.Min, draw.Over)

		inputRGBA := image.NewRGBA(bounds)
		draw.Draw(inputRGBA, bounds, compositor, image.Point{}, draw.Src)

		paletted := image.NewPaletted(bounds, palette.WebSafe)
		ditherer := colorquant.Dither{
			Filter: [][]float32{
				{0.0, 0.0, 7.0 / 16.0},
				{3.0 / 16.0, 5.0 / 16.0, 1.0 / 16.0},
			},
		}
		_, ok := uniqueColors(inputRGBA, 255)
		var outputRGBA *image.RGBA
		if !ok {
			outputRGBA = toRGBA(ditherer.Quantize(inputRGBA, paletted, 255, true, false))
		} else {
			unique := make(map[color.Color]struct{})
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					unique[inputRGBA.At(x, y)] = struct{}{}
				}
			}
			colorIndex := make(map[color.Color]uint8)
			var pal color.Palette
			idx := uint8(0)
			for col := range unique {
				pal = append(pal, col)
				colorIndex[col] = idx
				idx++
			}
			paletted.Palette = pal
			stride := paletted.Stride
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					paletted.Pix[y*stride+x] = colorIndex[inputRGBA.At(x, y)]
				}
			}
			outputRGBA = toRGBA(paletted)
		}

		// Apply circle mask
		pix := outputRGBA.Pix
		stride := outputRGBA.Stride
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if !isPixelInCircle(x, y, w, h, radius) {
					pix[(y*stride+x*4)+3] = 0
				}
			}
		}

		hasTrans := false
		for _, c := range paletted.Palette {
			_, _, _, a := c.RGBA()
			if a == 0 {
				hasTrans = true
				break
			}
		}
		if !hasTrans {
			if len(paletted.Palette) >= 256 {
				return nil, fmt.Errorf("no room for transparent color after quantization")
			}
			paletted.Palette = append(paletted.Palette, color.Transparent)
		}

		var transIndex uint8
		for idx, c := range paletted.Palette {
			_, _, _, a := c.RGBA()
			if a == 0 {
				transIndex = uint8(idx)
				break
			}
		}

		stride = paletted.Stride
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				_, _, _, a := outputRGBA.At(x, y).RGBA()
				if a == 0 {
					paletted.Pix[y*stride+x] = transIndex
				}
			}
		}

		dst.Image[i] = paletted
		dst.Disposal[i] = gif.DisposalNone

		switch src.Disposal[i] {
		case gif.DisposalBackground:
			draw.Draw(compositor, frameRect, &image.Uniform{bgColor}, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			if prev != nil {
				draw.Draw(compositor, bounds, prev, image.Point{}, draw.Src)
			}
		}
	}
	return dst, nil
}

func toRGBA(src image.Image) *image.RGBA {
	bounds := src.Bounds()
	rgba := image.NewRGBA(bounds)
	draw.Draw(rgba, bounds, src, bounds.Min, draw.Src)
	return rgba
}

func uniqueColors(img image.Image, limit int) (int, bool) {
	seen := make(map[color.Color]struct{})
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := img.At(x, y)
			if _, ok := seen[c]; !ok {
				seen[c] = struct{}{}
				if len(seen) > limit {
					return len(seen), false
				}
			}
		}
	}
	return len(seen), true
}

func resizeGIF(data []byte, width, height int) ([]byte, error) {
	src, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	dstImg, err := resigif.Resize(ctx, src, width, height, resigif.WithAspectRatio(resigif.Ignore))
	if err != nil {
		return nil, err
	}
	buf := new(bytes.Buffer)
	if err := gif.EncodeAll(buf, dstImg); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeFirstGIFFrame(data []byte) (image.Image, error) {
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if len(g.Image) == 0 {
		return nil, fmt.Errorf("no frames in GIF")
	}
	b := image.Rect(0, 0, g.Config.Width, g.Config.Height)
	dst := image.NewRGBA(b)
	bg := &image.Uniform{C: image.Transparent}
	if g.BackgroundIndex < byte(len(g.Image[0].Palette)) {
		bg = &image.Uniform{C: g.Image[0].Palette[g.BackgroundIndex]}
	}
	draw.Draw(dst, b, bg, image.Point{}, draw.Src)
	frame := g.Image[0]
	draw.Draw(dst, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
	return dst, nil
}

func encodeJPEG(img image.Image, quality int) ([]byte, error) {
	if quality <= 0 {
		quality = 85
	}
	buf := avatarBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer avatarBufPool.Put(buf)
	if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	return result, nil
}

// loadDefaultAvatar loads a fallback default profile picture.
func loadDefaultAvatar() {
	// Simple grey placeholder
	img := image.NewRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 200, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85})
	defaultAvatarContent = buf.Bytes()
	defaultAvatarEtag = fmt.Sprintf("%x", md5.Sum(defaultAvatarContent))
}

// loadDefaultBanner loads a fallback default banner.
func loadDefaultBanner() {
	img := image.NewRGBA(image.Rect(0, 0, 3, 1))
	var buf bytes.Buffer
	png.Encode(&buf, img)
	defaultBannerContent = buf.Bytes()
}
