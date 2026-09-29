package session

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http/httptest"
	"testing"
)

func encodeTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestServeImageThumbnail_DownscalesPNG(t *testing.T) {
	src := encodeTestPNG(t, 200, 100)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/download?width=50", nil)
	if !serveImageThumbnail(rec, req, src, "generated-image.png", 50) {
		t.Fatal("expected thumbnail to be served")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content-type = %q, want image/png", ct)
	}
	decoded, err := png.Decode(rec.Body)
	if err != nil {
		t.Fatalf("thumbnail is not a valid png: %v", err)
	}
	if got := decoded.Bounds().Dx(); got != 50 {
		t.Fatalf("thumbnail width = %d, want 50", got)
	}
	if got := decoded.Bounds().Dy(); got != 25 {
		t.Fatalf("thumbnail height = %d, want 25 (aspect preserved)", got)
	}
}

func TestServeImageThumbnail_NeverUpscales(t *testing.T) {
	src := encodeTestPNG(t, 40, 20)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/download?width=80", nil)
	if serveImageThumbnail(rec, req, src, "generated-image.png", 80) {
		t.Fatal("expected no thumbnail for an image already narrower than the target")
	}
}

func TestServeImageThumbnail_RejectsGarbage(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/download?width=50", nil)
	if serveImageThumbnail(rec, req, []byte("not an image"), "generated-image.png", 50) {
		t.Fatal("expected false for undecodable bytes")
	}
}

func TestParseThumbnailWidth(t *testing.T) {
	if _, ok := parseThumbnailWidth(""); ok {
		t.Error("empty width must be rejected")
	}
	if _, ok := parseThumbnailWidth("0"); ok {
		t.Error("zero width must be rejected")
	}
	if _, ok := parseThumbnailWidth("999999"); ok {
		t.Error("oversized width must be rejected")
	}
	if w, ok := parseThumbnailWidth("512"); !ok || w != 512 {
		t.Errorf("width 512 rejected: %d %v", w, ok)
	}
}

func TestThumbnailableExtension(t *testing.T) {
	for _, name := range []string{"a.png", "b.JPG", "c.jpeg", "d.jpg"} {
		if !thumbnailableExtension(name) {
			t.Errorf("%s should be thumbnailable", name)
		}
	}
	for _, name := range []string{"a.gif", "b.webp", "c.svg", "d.pdf", "e"} {
		if thumbnailableExtension(name) {
			t.Errorf("%s should NOT be thumbnailable", name)
		}
	}
}
