package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type lookupOnlyCatalog struct {
	interfaces.LegalCatalogService
	asked []string
	docs  map[string][]*types.LegalDocument
}

func (c *lookupOnlyCatalog) Lookup(
	_ context.Context, number, _ string, _ int,
) ([]*types.LegalStatusReport, error) {
	c.asked = append(c.asked, number)
	var out []*types.LegalStatusReport
	for _, d := range c.docs[number] {
		out = append(out, &types.LegalStatusReport{Document: d})
	}
	return out, nil
}

func TestLegalCatalogPromptReportsNamedDocuments(t *testing.T) {
	cat := &lookupOnlyCatalog{docs: map[string][]*types.LegalDocument{
		"04/2026/NQ-HĐND": {
			{DocumentNumber: "04/2026/NQ-HĐND", Title: "Nghị quyết về Đội dân phòng", Issuer: "HĐND Tỉnh Nghệ An", Source: "vbpl"},
			{DocumentNumber: "04/2026/NQ-HĐND", Title: "Nghị quyết về phân bổ vốn", Issuer: "HĐND Tỉnh Hà Tĩnh", Source: "vbpl"},
		},
	}}
	s := &sessionService{legalCatalog: cat}

	got := s.legalCatalogPrompt(context.Background(),
		"Nghị quyết 04/2026/NQ-HĐND và 04/2026/NQ-HĐND, 9/2026/QĐ-UBND quy định gì?")
	if len(cat.asked) != 2 {
		t.Fatalf("each number looked up once, got %v", cat.asked)
	}
	for _, want := range []string{"## Legal catalogue", "HĐND Tỉnh Nghệ An", "HĐND Tỉnh Hà Tĩnh",
		"2 documents match", "### 9/2026/QĐ-UBND", "do not conclude it was repealed"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt lacks %q:\n%s", want, got)
		}
	}
}

func TestLegalCatalogPromptEmptyWithoutNumbers(t *testing.T) {
	cat := &lookupOnlyCatalog{}
	s := &sessionService{legalCatalog: cat}
	if got := s.legalCatalogPrompt(context.Background(), "Luật đất đai quy định gì?"); got != "" || len(cat.asked) != 0 {
		t.Fatalf("no number, no lookup: got %q, asked %v", got, cat.asked)
	}
	if got := (&sessionService{}).legalCatalogPrompt(context.Background(), "QĐ 02/2026/QĐ-UBND"); got != "" {
		t.Fatalf("no catalogue: got %q", got)
	}
}
