package docparser

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestScanMarkdownImageTargets(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		targets []string
	}{
		{
			name:    "without title",
			input:   `before ![a](images/a.png) after`,
			targets: []string{`images/a.png`},
		},
		{
			name:    "double quoted title",
			input:   `![a](images/a.png "hình")`,
			targets: []string{`images/a.png "hình"`},
		},
		{
			name:    "single quoted title",
			input:   `![a](images/a.png 'hình')`,
			targets: []string{`images/a.png 'hình'`},
		},
		{
			name:    "parenthesized title",
			input:   `![a](images/a.png (hình))`,
			targets: []string{`images/a.png (hình)`},
		},
		{
			name:    "title containing right paren",
			input:   `![a](images/a.png "giai đoạn 1) kết quả")`,
			targets: []string{`images/a.png "giai đoạn 1) kết quả"`},
		},
		{
			name:    "title containing both parens",
			input:   `![a](images/a.png 'giai đoạn (1) kết quả')`,
			targets: []string{`images/a.png 'giai đoạn (1) kết quả'`},
		},
		{
			name:    "escaped quote in title",
			input:   `![a](images/a.png "giai đoạn \"1\"")`,
			targets: []string{`images/a.png "giai đoạn \"1\""`},
		},
		{
			name:    "multiline title",
			input:   "![a](images/a.png\n  \"nhiều dòng title\")",
			targets: []string{"images/a.png\n  \"nhiều dòng title\""},
		},
		{
			name:    "spaced path",
			input:   `![a](images/trang 1.png)`,
			targets: []string{`images/trang 1.png`},
		},
		{
			name:    "path containing balanced parens",
			input:   `![a](images/a_(1).png "title")`,
			targets: []string{`images/a_(1).png "title"`},
		},
		{
			name:    "angle destination",
			input:   `![a](<images/a b.png> "title")`,
			targets: []string{`<images/a b.png> "title"`},
		},
		{
			name:    "malformed image is skipped",
			input:   `![a](images/a.png "unterminated title) after ![b](images/b.png)`,
			targets: []string{`images/b.png`},
		},
		{
			name:    "escaped image marker is skipped",
			input:   `\![a](images/a.png) ![b](images/b.png)`,
			targets: []string{`images/b.png`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spans := scanMarkdownImageTargets(tt.input)
			if len(spans) != len(tt.targets) {
				t.Fatalf("got %d spans, want %d: %#v", len(spans), len(tt.targets), spans)
			}
			for i, span := range spans {
				got := tt.input[span.TargetStart:span.TargetEnd]
				if got != tt.targets[i] {
					t.Fatalf("target %d = %q, want %q", i, got, tt.targets[i])
				}
			}
		})
	}
}

func TestSplitMarkdownImageTarget(t *testing.T) {
	refMap := map[string]types.ImageRef{
		"images/a.png":             {OriginalRef: "images/a.png"},
		"images/trang 1.png":       {OriginalRef: "images/trang 1.png"},
		"images/a_(1).png":         {OriginalRef: "images/a_(1).png"},
		"images/trang 1 (thử).gif": {OriginalRef: "images/trang 1 (thử).gif"},
	}

	tests := []struct {
		name     string
		raw      string
		wantPath string
		wantText string
		wantOK   bool
	}{
		{
			name:     "without title",
			raw:      `images/a.png`,
			wantPath: `images/a.png`,
			wantText: `local://stored`,
			wantOK:   true,
		},
		{
			name:     "double quoted title",
			raw:      `images/a.png "hình"`,
			wantPath: `images/a.png`,
			wantText: `local://stored "hình"`,
			wantOK:   true,
		},
		{
			name:     "single quoted title",
			raw:      `images/a.png 'hình'`,
			wantPath: `images/a.png`,
			wantText: `local://stored 'hình'`,
			wantOK:   true,
		},
		{
			name:     "parenthesized title",
			raw:      `images/a.png (giai đoạn (1) kết quả)`,
			wantPath: `images/a.png`,
			wantText: `local://stored (giai đoạn (1) kết quả)`,
			wantOK:   true,
		},
		{
			name:     "multiline title",
			raw:      "images/a.png\n  \"nhiều dòng title\"",
			wantPath: `images/a.png`,
			wantText: "local://stored\n  \"nhiều dòng title\"",
			wantOK:   true,
		},
		{
			name:     "path with spaces wins without title",
			raw:      `images/trang 1.png`,
			wantPath: `images/trang 1.png`,
			wantText: `local://stored`,
			wantOK:   true,
		},
		{
			name:     "path with balanced parens",
			raw:      `images/a_(1).png "title"`,
			wantPath: `images/a_(1).png`,
			wantText: `local://stored "title"`,
			wantOK:   true,
		},
		{
			name:     "angle destination replaces only inner path",
			raw:      `<images/trang 1 (thử).gif> "giai đoạn 1) hình ảnh"`,
			wantPath: `images/trang 1 (thử).gif`,
			wantText: `<local://stored> "giai đoạn 1) hình ảnh"`,
			wantOK:   true,
		},
		{
			name:   "unknown reference",
			raw:    `images/missing.png "title"`,
			wantOK: false,
		},
		{
			name:   "blank line in title is invalid",
			raw:    "images/a.png \"line1\n\nline2\"",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, pathStart, pathEnd, ok := splitMarkdownImageTarget(tt.raw, refMap)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if path != tt.wantPath {
				t.Fatalf("path = %q, want %q", path, tt.wantPath)
			}
			replaced := tt.raw[:pathStart] + "local://stored" + tt.raw[pathEnd:]
			if replaced != tt.wantText {
				t.Fatalf("replacement = %q, want %q", replaced, tt.wantText)
			}
			if strings.Contains(replaced, tt.wantPath) {
				t.Fatalf("replacement still contains original path: %q", replaced)
			}
		})
	}
}

func TestStripMarkdownImages(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple image",
			input: "before ![a](images/a.png) after",
			want:  "before  after",
		},
		{
			name:  "title containing right paren",
			input: `![a](images/a.png "giai đoạn 1) kết quả")`,
			want:  "",
		},
		{
			name:  "prose around image",
			input: "See figure: ![fig](resource://x) below.",
			want:  "See figure:  below.",
		},
		{
			name:  "escaped image marker is kept",
			input: `\![a](images/a.png) kept`,
			want:  `\![a](images/a.png) kept`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripMarkdownImages(tt.input); got != tt.want {
				t.Fatalf("StripMarkdownImages(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
