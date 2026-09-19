package chunker

import (
	"strings"
	"testing"
	"unicode/utf8"
)

const vnLegalDoc = `QUỐC HỘI
CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM
Độc lập - Tự do - Hạnh phúc

Luật số: 24/2018/QH14

Chương I
NHỮNG QUY ĐỊNH CHUNG

Điều 1. Phạm vi điều chỉnh
Luật này quy định về an ninh mạng; hoạt động bảo vệ an ninh mạng.

Điều 2. Giải thích từ ngữ
1. Trong Luật này, các từ ngữ dưới đây được hiểu như sau:
a) An ninh mạng là sự bảo đảm các hoạt động trên không gian mạng.
b) Không gian mạng là môi trường kết nối hạ tầng công nghệ thông tin.
2. Tội phạm công nghệ cao là tội phạm sử dụng không gian mạng.

Điều 3. Áp dụng pháp luật về an ninh mạng
1. Hoạt động an ninh mạng trên lãnh thổ Việt Nam tuân theo luật này.
2. Trường hợp điều ước quốc tế có quy định khác thì áp dụng điều ước đó.

Điều 4. Nguyên tắc bảo đảm an ninh mạng
Việc bảo đảm an ninh mạng phải tuân theo nguyên tắc quy định của pháp luật.
`

func TestSplitVietnameseLegal_BoundariesAndMetadata(t *testing.T) {
	cfg := NormalizeSplitterConfig(SplitterConfig{
		ChunkSize: 300, ChunkOverlap: 40, Strategy: StrategyVietnameseLegal,
	})
	chunks, diag := SplitWithDiagnostics(vnLegalDoc, cfg)
	if len(chunks) == 0 {
		t.Fatal("no chunks")
	}
	if diag.SelectedTier != TierVietnameseLegal {
		t.Fatalf("selected tier = %s, want vietnamese_legal", diag.SelectedTier)
	}

	// Every chunk must satisfy the End-Start == len(Content) rune invariant.
	runes := []rune(vnLegalDoc)
	for i, c := range chunks {
		if c.End-c.Start != utf8.RuneCountInString(c.Content) {
			t.Errorf("chunk %d violates position invariant: %d-%d vs %d runes",
				i, c.Start, c.End, utf8.RuneCountInString(c.Content))
		}
		_ = runes
	}

	// At least one chunk must carry Điều metadata.
	var withLegal, withArticles int
	for _, c := range chunks {
		if c.Legal != nil {
			withLegal++
			if len(c.Legal.ArticleNos) > 0 {
				withArticles++
			}
		}
	}
	if withLegal == 0 || withArticles == 0 {
		t.Fatalf("legal metadata missing: withLegal=%d withArticles=%d", withLegal, withArticles)
	}

	// ContextHeader should carry the legal heading path on structured chunks.
	found := false
	for _, c := range chunks {
		if strings.Contains(c.ContextHeader, "Chương I") && strings.Contains(c.ContextHeader, "Điều") {
			found = true
		}
	}
	if !found {
		t.Error("no chunk carries a Chương > Điều ContextHeader")
	}
}

func TestSplitVietnameseLegal_AutoDetect(t *testing.T) {
	// Auto strategy must auto-select the legal tier for VN legal text.
	cfg := NormalizeSplitterConfig(SplitterConfig{
		ChunkSize: 300, ChunkOverlap: 40, Strategy: StrategyAuto,
	})
	_, diag := SplitWithDiagnostics(vnLegalDoc, cfg)
	if diag.SelectedTier != TierVietnameseLegal {
		t.Fatalf("auto tier = %s, want vietnamese_legal", diag.SelectedTier)
	}
}

func TestSplitVietnameseLegal_NonLegalFallsThrough(t *testing.T) {
	plain := strings.Repeat("This is a plain English paragraph with no legal structure. ", 30)
	cfg := NormalizeSplitterConfig(SplitterConfig{
		ChunkSize: 300, ChunkOverlap: 40, Strategy: StrategyVietnameseLegal,
	})
	chunks, diag := SplitWithDiagnostics(plain, cfg)
	if len(chunks) == 0 {
		t.Fatal("no chunks")
	}
	if diag.SelectedTier == TierVietnameseLegal {
		t.Fatal("non-legal doc must not select the legal tier")
	}
	for _, c := range chunks {
		if c.Legal != nil {
			t.Error("non-legal doc carries legal metadata")
		}
	}
}

func TestSplitVietnameseLegal_OversizedSectionByKhoan(t *testing.T) {
	// One Điều much larger than ChunkSize with khoản markers inside → split
	// along khoản boundaries, never mid-khoản into the next one.
	doc := "Chương I\nCHUNG\n\n" +
		"Điều 1. A\nnội dung\n\nĐiều 2. B\n1. " + strings.Repeat("Nội dung chi tiết của khoản một về an ninh mạng.\n", 15) +
		"2. " + strings.Repeat("Nội dung chi tiết của khoản hai về an ninh mạng.\n", 15) +
		"\nĐiều 3. C\nnội dung\n\nĐiều 4. D\nnội dung"
	cfg := NormalizeSplitterConfig(SplitterConfig{
		ChunkSize: 250, ChunkOverlap: 30, Strategy: StrategyVietnameseLegal,
	})
	chunks, diag := SplitWithDiagnostics(doc, cfg)
	if diag.SelectedTier != TierVietnameseLegal {
		t.Fatalf("tier = %s", diag.SelectedTier)
	}
	// Điều 2's section is ~1200 runes > 250 → must be sub-split.
	var d2Chunks []Chunk
	for _, c := range chunks {
		if c.Legal != nil {
			for _, a := range c.Legal.ArticleNos {
				if a == "2" {
					d2Chunks = append(d2Chunks, c)
				}
			}
		}
	}
	if len(d2Chunks) < 2 {
		t.Fatalf("oversized Điều 2 should produce multiple chunks, got %d", len(d2Chunks))
	}
	// No sub-chunk may exceed 2x chunk size (validator sanity bound).
	for _, c := range chunks {
		if utf8.RuneCountInString(c.Content) > 2*cfg.ChunkSize {
			t.Errorf("chunk exceeds 2x target: %d runes", utf8.RuneCountInString(c.Content))
		}
	}
}

func TestSplitVietnameseLegal_KhoanMetadata(t *testing.T) {
	doc := "Điều 1. A\nnội dung\n\nĐiều 2. Giải thích\n" +
		"1. Trong Luật này:\na) an ninh mạng\nb) không gian mạng\n" +
		"2. Tội phạm công nghệ cao.\n\n" +
		"Điều 3. C\nnội dung\n\nĐiều 4. D\nnội dung\n\nĐiều 5. E\nnội dung"
	cfg := NormalizeSplitterConfig(SplitterConfig{
		ChunkSize: 400, ChunkOverlap: 40, Strategy: StrategyVietnameseLegal,
	})
	chunks, _ := SplitWithDiagnostics(doc, cfg)
	var found bool
	for _, c := range chunks {
		if c.Legal == nil {
			continue
		}
		for _, r := range c.Legal.SubdivisionRefs {
			if r == "khoan:1/diem:a" || r == "khoan:1/diem:b" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected subdivision refs like khoan:1/diem:a")
	}
}
