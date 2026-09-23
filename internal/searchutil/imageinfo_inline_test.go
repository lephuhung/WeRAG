package searchutil

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestInlineImageText_ReplacesScannedPagePlaceholders(t *testing.T) {
	content := "![page1](resource://page1)\n\n![page2](resource://page2)"
	raw, err := json.Marshal([]types.ImageInfo{
		{URL: "resource://page1", OCRText: "Điều 1. Phạm vi điều chỉnh"},
		{URL: "resource://page2", OCRText: "Điều 2. Đối tượng áp dụng"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := InlineImageText(content, string(raw))
	for _, want := range []string{
		"Điều 1. Phạm vi điều chỉnh",
		"Điều 2. Đối tượng áp dụng",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in inlined text:\n%s", want, got)
		}
	}
	if strings.Contains(got, "![") {
		t.Fatalf("image placeholders should be replaced by OCR text: %s", got)
	}
}

func TestInlineImageText_FallsBackToCaption(t *testing.T) {
	content := "intro\n\n![chart](u1)\n\noutro"
	raw, err := json.Marshal([]types.ImageInfo{{URL: "u1", Caption: "revenue chart"}})
	if err != nil {
		t.Fatal(err)
	}
	got := InlineImageText(content, string(raw))
	if !strings.Contains(got, "revenue chart") {
		t.Fatalf("caption should stand in when OCR is absent: %s", got)
	}
	if strings.Contains(got, "![chart](u1)") {
		t.Fatalf("matched placeholder should be replaced: %s", got)
	}
	if !strings.Contains(got, "intro") || !strings.Contains(got, "outro") {
		t.Fatalf("surrounding text must be preserved: %s", got)
	}
}

func TestInlineImageText_KeepsImagesWithoutText(t *testing.T) {
	content := "before ![fig](u1) after"
	raw, err := json.Marshal([]types.ImageInfo{{URL: "u1"}})
	if err != nil {
		t.Fatal(err)
	}
	got := InlineImageText(content, string(raw))
	if got != content {
		t.Fatalf("image without text should keep its markup: %q", got)
	}
}

func TestInlineImageText_AppendsOrphanImageText(t *testing.T) {
	content := "plain text body"
	raw, err := json.Marshal([]types.ImageInfo{{URL: "u9", OCRText: "loose page text"}})
	if err != nil {
		t.Fatal(err)
	}
	got := InlineImageText(content, string(raw))
	if !strings.HasPrefix(got, "plain text body") || !strings.Contains(got, "loose page text") {
		t.Fatalf("unreferenced image text should append after content: %q", got)
	}
}

func TestInlineImageText_MatchesOriginalURL(t *testing.T) {
	content := "![p](local://img/p1.png)"
	raw, err := json.Marshal([]types.ImageInfo{
		{URL: "resource://served", OriginalURL: "local://img/p1.png", OCRText: "page text"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := InlineImageText(content, string(raw))
	if !strings.Contains(got, "page text") {
		t.Fatalf("original_url should match the placeholder: %s", got)
	}
}

func TestInlineImageText_EmptyInputs(t *testing.T) {
	if got := InlineImageText("body", ""); got != "body" {
		t.Fatalf("empty info: %q", got)
	}
	if got := InlineImageText("body", "not-json"); got != "body" {
		t.Fatalf("invalid info: %q", got)
	}
	raw, _ := json.Marshal([]types.ImageInfo{})
	if got := InlineImageText("body", string(raw)); got != "body" {
		t.Fatalf("empty info list: %q", got)
	}
}
