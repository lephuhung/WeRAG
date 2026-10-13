package service

import (
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestVnSlug(t *testing.T) {
	cases := map[string]string{
		"Quản lý thuế, phí và lệ phí": "quan-ly-thue-phi-va-le-phi",
		"Đường bộ":                    "duong-bo",
		"84/2025/QH15":                "84-2025-qh15",
		"  Hóa chất  ":                "hoa-chat",
		"":                            "",
	}
	for in, want := range cases {
		if got := vnSlug(in); got != want {
			t.Errorf("vnSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func date(s string) *time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return &t
}

func TestRenderCataloguePages(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb1", TenantID: 7, Name: "VBPL – Luật"}
	law := &types.LegalDocument{ID: "178964", DocumentNumber: "84/2025/QH15", DocType: "Luật", Title: "Luật Thanh tra",
		Issuer: "Quốc hội", Signer: "Trần Thanh Mẫn", SignerTitle: "Chủ tịch Quốc hội", Field: "Thanh tra",
		IssuedOn: date("2025-06-25"), EffectiveOn: date("2025-07-01"), Status: "Còn hiệu lực"}
	old := &types.LegalDocument{ID: "130780", DocumentNumber: "25/2018/QH14", DocType: "Luật", Title: "Luật Tố cáo",
		Issuer: "Quốc hội", Field: "Khiếu nại, tố cáo", IssuedOn: date("2018-06-12"), Status: "Hết hiệu lực một phần"}
	circular := &types.LegalDocument{ID: "c1", DocumentNumber: "01/2026/TT-TTCP", DocType: "Thông tư", Title: "Thông tư hướng dẫn | thanh tra",
		Issuer: "Thanh tra Chính phủ", IssuedOn: date("2026-01-05"), Status: "Còn hiệu lực"}

	docs := []*catalogueDoc{
		{Knowledge: &types.Knowledge{ID: "k-law"}, Doc: law, Slug: catalogueDocSlug(law), Summary: "# Summary\nLuật Thanh tra quy định về thanh tra.\n\nChi tiết.",
			Outgoing: map[types.LegalRelation][]string{types.LegalRelationAmends: {"130780"}},
			Incoming: map[types.LegalRelation][]string{types.LegalRelationBasedOn: {"c1"}}},
		{Knowledge: &types.Knowledge{ID: "k-old"}, Doc: old, Slug: catalogueDocSlug(old),
			Outgoing: map[types.LegalRelation][]string{}, Incoming: map[types.LegalRelation][]string{types.LegalRelationAmends: {"178964"}}},
	}
	rows := map[string]*types.LegalDocument{"178964": law, "130780": old, "c1": circular}
	for _, d := range docs {
		d.Summary = stripSummaryHeading(d.Summary)
		d.Fields = splitFields(d.Doc.Field)
	}

	pages := renderCataloguePages(kb, docs, rows)
	bySlug := map[string]*cataloguePage{}
	for _, p := range pages {
		if bySlug[p.Slug] != nil {
			t.Fatalf("duplicate slug %s", p.Slug)
		}
		bySlug[p.Slug] = p
	}
	// 2 docs + 2 fields + 1 issuer + 2 hubs + root
	if len(pages) != 8 {
		t.Fatalf("got %d pages, want 8: %v", len(pages), slugs(pages))
	}

	lawPage := bySlug["van-ban/84-2025-qh15-178964"]
	if lawPage == nil {
		t.Fatalf("law page missing: %v", slugs(pages))
	}
	if lawPage.PageType != types.WikiPageTypeSummary || lawPage.SourceRefs[0] != "k-law" {
		t.Errorf("law page type/source wrong: %+v", lawPage)
	}
	if lawPage.FolderPath[1] != "Thanh tra" {
		t.Errorf("folder path = %v", lawPage.FolderPath)
	}
	if lawPage.Summary != "Luật Thanh tra quy định về thanh tra." {
		t.Errorf("summary = %q", lawPage.Summary)
	}
	for _, want := range []string{
		"# Luật 84/2025/QH15",
		"| Người ký | Trần Thanh Mẫn (Chủ tịch Quốc hội) |",
		"| Ngày ban hành | 25/06/2025 |",
		"[[co-quan/quoc-hoi|Quốc hội]]",
		"[[linh-vuc/thanh-tra|Thanh tra]]",
		"## Tóm tắt",
		"### Sửa đổi, bổ sung (1)",
		"[[van-ban/25-2018-qh14-130780|25/2018/QH14]] — Luật Tố cáo",
		"### Là căn cứ ban hành của (1)",
		"[Thông tư 01/2026/TT-TTCP](https://vbpl.vn/van-ban/chi-tiet/c1) — Thông tư hướng dẫn \\| thanh tra _(Còn hiệu lực)_",
	} {
		if !strings.Contains(lawPage.Content, want) {
			t.Errorf("law page missing %q\n%s", want, lawPage.Content)
		}
	}

	oldPage := bySlug["van-ban/25-2018-qh14-130780"]
	if !strings.Contains(oldPage.Content, "### Được sửa đổi, bổ sung bởi (1)") ||
		!strings.Contains(oldPage.Content, "[[van-ban/84-2025-qh15-178964|84/2025/QH15]]") {
		t.Errorf("old page relations wrong:\n%s", oldPage.Content)
	}
	if strings.Contains(oldPage.Content, "## Tóm tắt") {
		t.Errorf("old page should have no summary section")
	}

	field := bySlug["linh-vuc/thanh-tra"]
	if field == nil || field.PageType != types.WikiPageTypeIndex || !strings.Contains(field.Content, "## Còn hiệu lực (1)") {
		t.Errorf("field index wrong: %+v", field)
	}
	hub := bySlug["linh-vuc"]
	if hub == nil || !strings.Contains(hub.Content, "[[linh-vuc/khieu-nai-to-cao|Khiếu nại, tố cáo]] (1)") {
		t.Errorf("field hub wrong: %+v", hub)
	}
	root := bySlug["index"]
	if root == nil || !strings.Contains(root.Content, "- Còn hiệu lực: 1") || !strings.Contains(root.Content, "[[co-quan|Cơ quan ban hành]]") {
		t.Errorf("root wrong: %+v", root)
	}
	if existingHash(&types.WikiPage{PageMetadata: types.JSON(`{"generator":"vbpl-catalogue","content_hash":"abc"}`)}) != "abc" {
		t.Errorf("existingHash should read catalogue metadata")
	}
	if existingHash(&types.WikiPage{PageMetadata: types.JSON(`{"content_hash":"abc"}`)}) != "" {
		t.Errorf("existingHash must ignore pages from other generators")
	}
}

func slugs(pages []*cataloguePage) []string {
	out := make([]string, 0, len(pages))
	for _, p := range pages {
		out = append(out, p.Slug)
	}
	return out
}

func TestSplitFields(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", []string{"Chưa phân loại"}},
		{"Đất đai", []string{"Đất đai"}},
		{"Quản lý thuế, phí và lệ phí", []string{"Quản lý thuế, phí và lệ phí"}},
		{"Đất đai, Thủy sản", []string{"Đất đai", "Thủy sản"}},
		{"Thủy sản, Tài nguyên, môi trường biển và hải đảo, Đất đai", []string{"Thủy sản", "Tài nguyên, môi trường biển và hải đảo", "Đất đai"}},
		{"Khuyến nông, Thuế, phí và lệ phí, Thú y", []string{"Khuyến nông", "Thuế, phí và lệ phí", "Thú y"}},
		{"Chưa phân loại, An ninh quốc gia", []string{"Chưa phân loại", "An ninh quốc gia"}},
		{"Vận tải, Công nghệ thông tin,điện tử", []string{"Vận tải", "Công nghệ thông tin,điện tử"}},
		{"Đất đai, Đất đai", []string{"Đất đai"}},
		{"TCNH và thị trường TC, trái phiếu, khác", []string{"TCNH và thị trường TC, trái phiếu, khác"}},
	}
	for _, c := range cases {
		got := splitFields(c.in)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("splitFields(%q) = %v, want %v", c.in, got, c.want)
		}
	}
	long := strings.Repeat("Lĩnh vực rất dài ", 20)
	if n := len([]rune(folderName(long))); n > catalogueMaxFolderRunes+1 {
		t.Errorf("folderName not clipped: %d runes", n)
	}
}
