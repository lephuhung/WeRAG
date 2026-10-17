package service

import (
	"strings"
	"testing"
)

func TestSanitizeGeneratedTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		raw           string
		want          string
		wantTruncated bool
	}{
		{
			name: "plain title is kept as is",
			raw:  "Tư vấn quy trình bồi thường bảo hiểm",
			want: "Tư vấn quy trình bồi thường bảo hiểm",
		},
		{
			name: "thinking prefix and surrounding whitespace are dropped",
			raw:  "<think>\n\n</think>  Claim filing steps \n",
			want: "Claim filing steps",
		},
		{
			name:          "over-long ascii title is truncated",
			raw:           strings.Repeat("a", 300),
			want:          strings.Repeat("a", maxSessionTitleRunes),
			wantTruncated: true,
		},
		{
			name:          "over-long cjk title is truncated by rune, not byte",
			raw:           strings.Repeat("ă", 300),
			want:          strings.Repeat("ă", maxSessionTitleRunes),
			wantTruncated: true,
		},
		{
			name: "title exactly at the limit is not truncated",
			raw:  strings.Repeat("ă", maxSessionTitleRunes),
			want: strings.Repeat("ă", maxSessionTitleRunes),
		},
		{
			name: "empty completion stays empty",
			raw:  "   ",
			want: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, truncated := sanitizeGeneratedTitle(tt.raw)
			if got != tt.want {
				t.Fatalf("title = %q, want %q", got, tt.want)
			}
			if truncated != tt.wantTruncated {
				t.Fatalf("truncated = %v, want %v", truncated, tt.wantTruncated)
			}
			if len([]rune(got)) > maxSessionTitleRunes {
				t.Fatalf("title still exceeds %d runes: %d", maxSessionTitleRunes, len([]rune(got)))
			}
		})
	}
}

// The database column is VARCHAR(255) in every shipped migration; guard the
// constant so nobody raises it past what the column can hold.
func TestMaxSessionTitleRunesFitsColumn(t *testing.T) {
	t.Parallel()
	// Worst case for UTF-8 is 4 bytes per rune, but the column counts characters
	// in PostgreSQL and bytes in some engines, so keep a conservative bound.
	if maxSessionTitleRunes > 255 {
		t.Fatalf("maxSessionTitleRunes=%d exceeds the sessions.title column limit", maxSessionTitleRunes)
	}
}
