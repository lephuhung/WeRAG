package service

import (
	"os"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

const legalIdentityHeader = "| ỦY BAN NHÂN DÂN TỈNH | CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM |\n" +
	"| Số: 45/KH-UBND | Độc lập - Tự do - Hạnh phúc |\n\nKẾ HOẠCH\nTriển khai chuyển đổi số năm 2026\n\n" +
	"Căn cứ Quyết định số 12/2024/QĐ-UBND …"

func TestApplyLegalIdentity_HeaderWins(t *testing.T) {
	p := applyLegalIdentity(&types.KnowledgeProfile{
		Gist: "x", DocType: "Quyết định", DocumentNumber: "12/2024/QĐ-UBND",
	}, legalIdentityHeader, legalIdentityHeader)
	if p.DocumentNumber != "45/KH-UBND" || p.DocType != "Kế hoạch" || p.DocTypeCode != "ke_hoach" {
		t.Fatalf("got %+v", p)
	}
}

func TestApplyLegalIdentity_ModelNumberVerified(t *testing.T) {
	text := "Báo cáo nội bộ\nsố hiệu văn bản 08 / TTr-SYT gửi kèm"
	p := applyLegalIdentity(&types.KnowledgeProfile{Gist: "x", DocType: "TỜ TRÌNH", DocumentNumber: "Số: 08/TTr-SYT"}, text, text)
	if p.DocumentNumber != "08/TTr-SYT" || p.DocType != "Tờ trình" || p.DocTypeCode != "to_trinh" {
		t.Fatalf("got %+v", p)
	}
	// A number the text does not contain is an invention — dropped.
	p = applyLegalIdentity(&types.KnowledgeProfile{Gist: "x", DocumentNumber: "99/KH-UBND"}, text, text)
	if p.DocumentNumber != "" {
		t.Fatalf("invented number kept: %+v", p)
	}
}

func TestApplyLegalIdentity_PlainProfile(t *testing.T) {
	p := applyLegalIdentity(&types.KnowledgeProfile{Gist: "x", DocType: "user manual"}, "How to install", "How to install")
	if p.DocType != "user manual" || p.DocTypeCode != "" || p.DocumentNumber != "" {
		t.Fatalf("got %+v", p)
	}
	if applyLegalIdentity(nil, "plain text", "plain text") != nil {
		t.Fatal("nil profile with no header must stay nil")
	}
	if p := applyLegalIdentity(nil, legalIdentityHeader, legalIdentityHeader); p == nil || p.DocumentNumber != "45/KH-UBND" {
		t.Fatalf("legacy text summary must still get the header identity: %+v", p)
	}
}

func TestParseDocumentSummaryOutput_DocumentNumber(t *testing.T) {
	r := parseDocumentSummaryOutput(`{"summary":"s","gist":"g","doc_type":"Giấy mời","document_number":"3/GM-UBND"}`)
	if r.Profile == nil || r.Profile.DocumentNumber != "3/GM-UBND" || r.Profile.DocTypeCode != "giay_moi" {
		t.Fatalf("got %+v", r.Profile)
	}
}

// The summary prompt lists the doc_type labels; it must name every type of
// the vietnamese_legal registry so model labels canonicalise.
func TestSummaryPromptListsAllDocTypes(t *testing.T) {
	b, err := os.ReadFile("../../../config/prompt_templates/generate_summary.yaml")
	if err != nil {
		t.Skip(err)
	}
	for _, d := range vietnamese_legal.DocTypes {
		if !strings.Contains(string(b), `"`+d.Name+`"`) {
			t.Errorf("generate_summary.yaml does not list %q", d.Name)
		}
	}
}

func TestApplyLegalIdentity_RestoresOCRSymbol(t *testing.T) {
	header := "CHÍNH PHỦ\nCỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM\nSố: 361/2025/ND-CP\nNGHI ĐỊNH\nQuy định về vị trí việc làm"
	p := applyLegalIdentity(&types.KnowledgeProfile{Gist: "x"}, header, header)
	if p.DocumentNumber != "361/2025/NĐ-CP" || p.DocTypeCode != "nghi_dinh" {
		t.Fatalf("got %+v", p)
	}
	// Model said hợp đồng, number printed with HD: the type settles HĐ.
	text := "Hợp đồng số 3/HD-UBND về thuê văn phòng"
	p = applyLegalIdentity(&types.KnowledgeProfile{Gist: "x", DocType: "Hợp đồng", DocumentNumber: "3/HD-UBND"}, text, text)
	if p.DocumentNumber != "3/HĐ-UBND" {
		t.Fatalf("got %+v", p)
	}
}
