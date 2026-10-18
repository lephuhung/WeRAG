package docparser

import (
	"net/url"
	"regexp"
	"strings"
)

// b64DataURIPattern splits an inline "data:image/<ext>;base64,<data>" URI,
// the form OCR services return page images in.
var b64DataURIPattern = regexp.MustCompile(`^data:image/(\w+);base64,(.+)$`)

// escapedImageSyntaxPattern matches markdown image references whose `[` was
// over-escaped to `\[` by html-to-markdown. The URL group mirrors the
// downstream image-extraction regex so escapes are only stripped for actual
// image references.
var escapedImageSyntaxPattern = regexp.MustCompile(`!\\\[(.*?)\\?\]\(([^()\n]*(?:\([^)]*\)[^()\n]*)*)\)`)

// unescapeMarkdownImageSyntax restores `![alt](url)` from html-to-markdown's
// over-escaped `!\[alt\](url)` form. Without this, the downstream image regex
// in ImageResolver fails to match and images are never persisted.
func unescapeMarkdownImageSyntax(content string) string {
	return escapedImageSyntaxPattern.ReplaceAllString(content, "![$1]($2)")
}

// mineruImageOriginalRefs returns the image references in mdContent that
// point at imagePath, however the OCR service spelled the path (escaped,
// "./", "images/" prefixed). The name is historical: MinerU was the first
// engine that needed it; PaddleOCR-VL layout parsing uses it now.
func mineruImageOriginalRefs(mdContent, imagePath string) []string {
	normalizedTarget := normalizeMinerUImagePath(imagePath)
	if normalizedTarget == "" {
		return nil
	}

	referenced := extractImageRefsFromContent(mdContent)
	seen := make(map[string]struct{}, len(referenced))
	var matched []string
	for _, refPath := range referenced {
		if normalizeMinerUImagePath(refPath) != normalizedTarget {
			continue
		}
		if _, ok := seen[refPath]; ok {
			continue
		}
		matched = append(matched, refPath)
		seen[refPath] = struct{}{}
	}

	return matched
}

// imgMarkdownPatternAllowSpaces matches markdown image syntax while allowing
// spaces in the URL group, so that paths like "images/trang 1.jpg" are still
// detected as image references.
var imgMarkdownPatternAllowSpaces = regexp.MustCompile(
	`!\[(.*?)\]\(([^()\n]*(?:\([^)]*\)[^()\n]*)*)\)`,
)

func extractImageRefsFromContent(content string) []string {
	var refs []string

	for _, match := range imgMarkdownPatternAllowSpaces.FindAllStringSubmatch(content, -1) {
		if len(match) >= 3 {
			refs = append(refs, strings.TrimSpace(match[2]))
		}
	}
	for _, match := range imgHTMLSrc.FindAllStringSubmatch(content, -1) {
		if len(match) >= 3 {
			refs = append(refs, match[2])
		}
	}

	return refs
}

func normalizeMinerUImagePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if decoded, err := url.PathUnescape(p); err == nil && decoded != "" {
		p = decoded
	}
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimPrefix(p, "images/")
	return p
}
