package session

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/filetransport"
)

// maxThumbnailSourceBytes caps how much of an artifact we buffer in memory to
// produce a downscaled variant. Skill-generated images are a few MB; anything
// larger falls back to serving the original bytes untouched.
const maxThumbnailSourceBytes = 64 << 20

// Bounds clamp the client-requested ?width= so a caller cannot ask for a 0px
// or absurdly large render.
const (
	minThumbnailWidth = 16
	maxThumbnailWidth = 2048
)

// parseThumbnailWidth reads the ?width= query parameter. The second return is
// false when the parameter is absent or out of the accepted range, meaning the
// caller must serve the original bytes.
func parseThumbnailWidth(raw string) (int, bool) {
	if raw == "" {
		return 0, false
	}
	width, err := strconv.Atoi(raw)
	if err != nil || width < minThumbnailWidth || width > maxThumbnailWidth {
		return 0, false
	}
	return width, true
}

// thumbnailableExtension reports whether the artifact extension can be
// downscaled with the stdlib decoders registered in this file. Animated and
// exotic formats (gif, webp, svg, avif) are deliberately excluded — re-encoding
// them would either drop animation or require extra dependencies.
func thumbnailableExtension(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png":
		return true
	}
	return false
}

// serveThumbnailResponse writes an already-encoded thumbnail body as an
// inline, cacheable response.
func serveThumbnailResponse(w http.ResponseWriter, r *http.Request, body *bytes.Buffer, contentType string) {
	_ = filetransport.Serve(w, r, io.NopCloser(bytes.NewReader(body.Bytes())), filetransport.Options{
		Filename:     "thumbnail",
		ContentType:  contentType,
		CacheControl: "private, max-age=604800",
		Size:         int64(body.Len()),
	})
}

// serveImageThumbnail decodes data, downscales it so the width is at most
// maxWidth (never upscaling), and writes the re-encoded image as an inline
// response. It returns false when the payload cannot be thumbnailized — the
// caller then serves the original bytes instead.
func serveImageThumbnail(w http.ResponseWriter, r *http.Request, data []byte, filename string, maxWidth int) bool {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return false
	}
	bounds := src.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	if srcW <= maxWidth || srcW <= 0 || srcH <= 0 {
		// Nothing to shrink — the original already fits the request.
		return false
	}
	dstH := int(float64(srcH) * float64(maxWidth) / float64(srcW))
	if dstH < 1 {
		dstH = 1
	}
	dst := resizeBilinear(src, maxWidth, dstH)

	var body bytes.Buffer
	var contentType string
	if strings.EqualFold(filepath.Ext(filename), ".png") {
		if err := png.Encode(&body, dst); err != nil {
			return false
		}
		contentType = "image/png"
	} else {
		if err := jpeg.Encode(&body, dst, &jpeg.Options{Quality: 82}); err != nil {
			return false
		}
		contentType = "image/jpeg"
	}
	serveThumbnailResponse(w, r, &body, contentType)
	return true
}

// resizeBilinear scales src to exactly dstW x dstH with bilinear sampling.
// Pure stdlib — the project avoids pulling golang.org/x/image in just for
// thumbnails, and bilinear quality is ample for sub-1024px previews.
func resizeBilinear(src image.Image, dstW, dstH int) *image.RGBA {
	bounds := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	srcW, srcH := bounds.Dx(), bounds.Dy()
	if srcW == 0 || srcH == 0 {
		return dst
	}
	// Map destination pixel centers back into source space; the -0.5/+0.5
	// shift keeps the sample aligned with pixel centers instead of edges.
	xRatio := float64(srcW) / float64(dstW)
	yRatio := float64(srcH) / float64(dstH)
	for y := 0; y < dstH; y++ {
		sy := (float64(y)+0.5)*yRatio - 0.5
		if sy < 0 {
			sy = 0
		}
		y0 := int(sy)
		if y0 >= srcH-1 {
			y0 = srcH - 2
		}
		if y0 < 0 {
			y0 = 0
		}
		fy := sy - float64(y0)
		rowOffset := y * dst.Stride
		for x := 0; x < dstW; x++ {
			sx := (float64(x)+0.5)*xRatio - 0.5
			if sx < 0 {
				sx = 0
			}
			x0 := int(sx)
			if x0 >= srcW-1 {
				x0 = srcW - 2
			}
			if x0 < 0 {
				x0 = 0
			}
			fx := sx - float64(x0)

			r00, g00, b00, a00 := src.At(bounds.Min.X+x0, bounds.Min.Y+y0).RGBA()
			r10, g10, b10, a10 := src.At(bounds.Min.X+x0+1, bounds.Min.Y+y0).RGBA()
			r01, g01, b01, a01 := src.At(bounds.Min.X+x0, bounds.Min.Y+y0+1).RGBA()
			r11, g11, b11, a11 := src.At(bounds.Min.X+x0+1, bounds.Min.Y+y0+1).RGBA()

			// Bilinear blend of the four surrounding source pixels, all in
			// 16-bit component space (RGBA() range 0..65535).
			r := lerp(lerp(float64(r00), float64(r10), fx), lerp(float64(r01), float64(r11), fx), fy)
			g := lerp(lerp(float64(g00), float64(g10), fx), lerp(float64(g01), float64(g11), fx), fy)
			b := lerp(lerp(float64(b00), float64(b10), fx), lerp(float64(b01), float64(b11), fx), fy)
			a := lerp(lerp(float64(a00), float64(a10), fx), lerp(float64(a01), float64(a11), fx), fy)

			offset := rowOffset + x*4
			dst.Pix[offset+0] = clampComponent(r)
			dst.Pix[offset+1] = clampComponent(g)
			dst.Pix[offset+2] = clampComponent(b)
			dst.Pix[offset+3] = clampComponent(a)
		}
	}
	return dst
}

func lerp(a, b float64, t float64) float64 {
	return a + (b-a)*t
}

// clampComponent converts a 16-bit blended component (0..65535, with tiny
// float drift) into the 8-bit range image.RGBA stores.
func clampComponent(v float64) uint8 {
	v /= 257.0
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}
