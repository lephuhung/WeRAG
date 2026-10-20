package service

import (
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
	"github.com/stretchr/testify/require"
)

func amendDay(s string) LegalAmendDate { return legalDay(s, time.UTC) }

func amendAt(s string) time.Time { return amendDay(s).At }

var (
	nd68  = LegalAmendDoc{CatalogID: "nd68", Number: "68/2026/NĐ-CP", Type: "Nghị định"}
	nd141 = LegalAmendDoc{CatalogID: "nd141", Number: "141/2026/NĐ-CP", Type: "Nghị định", KnowledgeBaseID: "kb-pub", ChunkID: "c141"}
)

// NĐ 68/2026 Điều 3 as stored on dev (chunk 8e3bdd48).
const nd68Dieu3 = "## Chương II CHÍNH SÁCH THUẾ ĐỐI VỚI HỘ KINH DOANH, CÁ NHÂN KINH DOANH\n\n#### Điều 3. Thuế giá trị gia tăng\n\n" +
	"1. Hộ kinh doanh, cá nhân kinh doanh có hoạt động sản xuất, kinh doanh có mức doanh thu năm từ 500 triệu đồng trở xuống thuộc đối tượng không chịu thuế giá trị gia tăng.\n\n" +
	"2. Hộ kinh doanh, cá nhân kinh doanh có hoạt động sản xuất, kinh doanh có mức doanh thu năm trên 500 triệu đồng thuộc đối tượng chịu thuế giá trị gia tăng và áp dụng phương pháp tính trực tiếp theo doanh thu bằng tỷ lệ % nhân (x) doanh thu."

func nd68Chunk(content string, article string, khoans ...string) LegalProvisionChunk {
	var refs []string
	for _, k := range khoans {
		refs = append(refs, "khoan:"+k)
	}
	return LegalProvisionChunk{ID: "c68-" + article, KnowledgeBaseID: "kb-pub", Content: content, Doc: nd68,
		Legal: &vietnamese_legal.LegalMetadata{ArticleNos: []string{article}, KhoanNos: khoans, SubdivisionRefs: refs}}
}

func phraseEdge(article, khoan, from, to string, amender LegalAmendDoc, eff string, tier LegalAmendTier) LegalAmendEdge {
	op := LegalOpThayCumTu
	if to == "" {
		op = LegalOpBaiBoCumTu
	}
	return LegalAmendEdge{Op: op, Amender: amender, Target: nd68, Provision: LegalProvisionRef{Article: article, Khoan: khoan},
		From: from, To: to, EffFrom: amendDay(eff), Tier: tier, Sources: []string{LegalAmendSourceParser}}
}

// The case that started it all: NĐ 141/2026 replaces "500 triệu đồng" by
// "01 tỷ đồng" in Điều 3 of NĐ 68/2026 from 01/01/2026. The legacy parser
// phrase tier gives exactly today's LegalCurrentWording text.
func TestResolveND68PhraseCase(t *testing.T) {
	edges := []LegalAmendEdge{phraseEdge("3", "", "500 triệu đồng", "01 tỷ đồng", nd141, "2026-01-01", LegalTierLegacyParserPhrase)}
	c := nd68Chunk(nd68Dieu3, "3", "1", "2")
	old := applyPhraseChanges(nd68Dieu3, []legalPhraseChange{{From: "500 triệu đồng", To: "01 tỷ đồng",
		Amender: legalHistoryDoc{Number: nd141.Number, Type: nd141.Type, EffFrom: "2026-01-01"}}}, amendAt("2026-10-20"))

	after := ResolveLegalProvisions(c, edges, amendAt("2026-10-20"), LegalResolveOptions{})
	require.Equal(t, old, after.ModelContent, "byte-identical to today's wording")
	require.Contains(t, after.ModelContent, "từ 01 tỷ đồng [thay cho “500 triệu đồng” theo Nghị định 141/2026/NĐ-CP, từ 01/01/2026] trở xuống")
	require.Equal(t, LegalStateCurrentVerified, after.State)

	before := ResolveLegalProvisions(c, edges, amendAt("2025-12-31"), LegalResolveOptions{})
	require.Equal(t, LegalStateOriginalInForce, before.State)
	require.Contains(t, before.ModelContent, "từ 500 triệu đồng [sẽ được thay bằng “01 tỷ đồng” theo Nghị định 141/2026/NĐ-CP, từ 01/01/2026] trở xuống")

	// the same edge as tier 2: the stored words stay, wrapped
	edges[0].Tier = LegalTierUnverified
	t2 := ResolveLegalProvisions(c, edges, amendAt("2026-10-20"), LegalResolveOptions{})
	require.Equal(t, LegalStateUnverified, t2.State)
	require.Contains(t, t2.ModelContent, "[LỜI VĂN HIỆN HÀNH CHƯA XÁC MINH: cụm từ “500 triệu đồng” được thay bằng “01 tỷ đồng” tại Điều 3 theo Nghị định 141/2026/NĐ-CP, từ 01/01/2026 (nguồn: bộ đọc vbpl), chưa xác minh]\n\n1. Hộ kinh doanh")
	require.Contains(t, t2.ModelContent, "từ 500 triệu đồng trở xuống")
}

func unitByProv(t *testing.T, r LegalResolution, p LegalProvisionRef) LegalProvisionResult {
	t.Helper()
	for _, u := range r.Units {
		if u.Provision == p {
			return u
		}
	}
	t.Fatalf("no unit %v in %+v", p, r.Units)
	return LegalProvisionResult{}
}

const dieu5 = "#### Điều 5. Hồ sơ\n\n1. Nộp hồ sơ trong 15 ngày tại Tổng cục Thuế.\n\n2. Hồ sơ gồm:\n\na) tờ khai;\n\nb) bản sao giấy phép.\n\n3. Cơ quan thuế trả lời trong 10 ngày."

func TestResolveLegalProvisionStates(t *testing.T) {
	b1 := LegalAmendDoc{CatalogID: "b1", Number: "1/2025/NĐ-CP", Type: "Nghị định", KnowledgeBaseID: "kb-pub"}
	b2 := LegalAmendDoc{CatalogID: "b2", Number: "2/2026/NĐ-CP", Type: "Nghị định", KnowledgeBaseID: "kb-pub"}
	b3 := LegalAmendDoc{CatalogID: "b3", Number: "3/2026/NĐ-CP", Type: "Nghị định", KnowledgeBaseID: "kb-pub"}
	private := LegalAmendDoc{CatalogID: "bp", Number: "9/2026/NĐ-CP", Type: "Nghị định", KnowledgeBaseID: "kb-private"}
	k1 := LegalProvisionRef{Article: "5", Khoan: "1"}
	k2 := LegalProvisionRef{Article: "5", Khoan: "2"}
	k2a := LegalProvisionRef{Article: "5", Khoan: "2", Diem: "a"}
	k3 := LegalProvisionRef{Article: "5", Khoan: "3"}
	quote := func(op LegalAmendOp, p LegalProvisionRef, text string, by LegalAmendDoc, eff string, tier LegalAmendTier) LegalAmendEdge {
		return LegalAmendEdge{Op: op, Amender: by, Target: nd68, Provision: p, QuoteText: text, EffFrom: amendDay(eff), Tier: tier, Sources: []string{LegalAmendSourceParser}}
	}
	op := func(o LegalAmendOp, p LegalProvisionRef, by LegalAmendDoc, from, to string) LegalAmendEdge {
		return LegalAmendEdge{Op: o, Amender: by, Target: nd68, Provision: p, EffFrom: amendDay(from), EffTo: amendDay(to), Tier: LegalTierVerified}
	}
	c := nd68Chunk(dieu5, "5", "1", "2", "3")
	c.Legal.SubdivisionRefs = append(c.Legal.SubdivisionRefs, "khoan:2/diem:a", "khoan:2/diem:b")
	now := amendAt("2026-10-20")

	cases := []struct {
		name   string
		edges  []LegalAmendEdge
		asOf   time.Time
		opt    LegalResolveOptions
		prov   LegalProvisionRef
		state  LegalProvisionState
		has    []string
		hasnt  []string
		under  LegalProvisionState
		gated  bool
		cond   LegalConditionMatch
		chunks string // ModelContent must contain
	}{
		{name: "original in force", prov: k1, state: LegalStateOriginalInForce, has: []string{"1. Nộp hồ sơ trong 15 ngày"}},
		{name: "sua_doi tier 1 replaces exactly the khoản",
			edges: []LegalAmendEdge{quote(LegalOpSuaDoi, k1, "1. Nộp hồ sơ trong 30 ngày tại Cục Thuế.", b1, "2025-07-01", LegalTierVerified)},
			prov:  k1, state: LegalStateCurrentVerified,
			has:    []string{"[Lời văn hiện hành theo Nghị định 1/2025/NĐ-CP, từ 01/07/2025]\n\n1. Nộp hồ sơ trong 30 ngày tại Cục Thuế.", "[trước 01/07/2025: 1. Nộp hồ sơ trong 15 ngày tại Tổng cục Thuế.]"},
			chunks: "3. Cơ quan thuế trả lời trong 10 ngày."},
		{name: "sua_doi tier 2 wraps with the new text",
			edges: []LegalAmendEdge{quote(LegalOpSuaDoi, k1, "1. Nộp hồ sơ trong 30 ngày.", b1, "2025-07-01", LegalTierUnverified)},
			prov:  k1, state: LegalStateUnverified, under: LegalStateCurrentVerified, gated: true,
			has: []string{"[LỜI VĂN HIỆN HÀNH CHƯA XÁC MINH: khoản 1 Điều 5 có thể đã được sửa đổi theo Nghị định 1/2025/NĐ-CP, từ 01/07/2025 (nguồn: bộ đọc vbpl). Lời văn mới theo nguồn này: “1. Nộp hồ sơ trong 30 ngày.”]\n\n1. Nộp hồ sơ trong 15 ngày"}},
		{name: "sua_doi not yet in force",
			edges: []LegalAmendEdge{quote(LegalOpSuaDoi, k1, "1. Mới.", b1, "2027-01-01", LegalTierVerified)},
			prov:  k1, state: LegalStateOriginalInForce, has: []string{"15 ngày"}},
		{name: "issue date only = unknown date",
			edges: []LegalAmendEdge{{Op: LegalOpSuaDoi, Amender: b1, Target: nd68, Provision: k1, QuoteText: "1. Mới.", Tier: LegalTierVerified}},
			prov:  k1, state: LegalStateUnresolved, gated: true, has: []string{"chưa rõ ngày hiệu lực", "15 ngày"}},
		{name: "bai_bo tier 1",
			edges: []LegalAmendEdge{op(LegalOpBaiBo, k3, b1, "2025-07-01", "")},
			prov:  k3, state: LegalStateCurrentVerified, has: []string{"[khoản 3 Điều 5 đã bị bãi bỏ theo Nghị định 1/2025/NĐ-CP, từ 01/07/2025]"}, hasnt: []string{"10 ngày"}},
		{name: "bo_sung tier 1 inserted after the parent",
			edges: []LegalAmendEdge{{Op: LegalOpBoSung, Amender: b1, Target: nd68, Provision: LegalProvisionRef{Article: "5", Khoan: "2"}, NewLabel: "c",
				QuoteText: "c) bản sao căn cước.", EffFrom: amendDay("2025-07-01"), Tier: LegalTierVerified}},
			prov: LegalProvisionRef{Article: "5", Khoan: "2", Diem: "c"}, state: LegalStateCurrentVerified,
			has:    []string{"[Bổ sung theo Nghị định 1/2025/NĐ-CP, từ 01/07/2025]\n\nc) bản sao căn cước."},
			chunks: "b) bản sao giấy phép.\n\n[Bổ sung theo Nghị định 1/2025/NĐ-CP, từ 01/07/2025]\n\nc) bản sao căn cước.\n\n3. Cơ quan thuế"},
		{name: "bo_sung of a Điều goes after the Điều it extends",
			edges: []LegalAmendEdge{{Op: LegalOpBoSung, Amender: b1, Target: nd68, NewLabel: "5a", QuoteText: "Điều 5a. Mới", EffFrom: amendDay("2025-07-01"), Tier: LegalTierVerified}},
			prov:  LegalProvisionRef{Article: "5a"}, state: LegalStateCurrentVerified,
			chunks: "trả lời trong 10 ngày.\n\n[Bổ sung theo Nghị định 1/2025/NĐ-CP, từ 01/07/2025]\n\nĐiều 5a. Mới\n"},
		{name: "phrase chain A→B→C (two amenders, newer last)",
			edges: []LegalAmendEdge{
				{Op: LegalOpThayCumTu, Amender: b1, Target: nd68, Provision: LegalProvisionRef{Article: "5"}, From: "Tổng cục Thuế", To: "Cục Thuế", EffFrom: amendDay("2025-03-01"), Tier: LegalTierLegacyParserPhrase},
				{Op: LegalOpThayCumTu, Amender: b2, Target: nd68, Provision: LegalProvisionRef{Article: "5"}, From: "Cục Thuế", To: "Bộ Tài chính", EffFrom: amendDay("2026-02-01"), Tier: LegalTierLegacyParserPhrase},
			},
			prov: k1, state: LegalStateCurrentVerified,
			has: []string{"tại Bộ Tài chính [thay cho “Tổng cục Thuế” theo Nghị định 1/2025/NĐ-CP, từ 01/03/2025; Nghị định 2/2026/NĐ-CP, từ 01/02/2026]"}},
		{name: "chain: later phrase checked against the amended text",
			edges: []LegalAmendEdge{
				quote(LegalOpSuaDoi, k1, "1. Nộp hồ sơ trong 30 ngày tại Cục Thuế.", b1, "2025-07-01", LegalTierVerified),
				{Op: LegalOpThayCumTu, Amender: b2, Target: nd68, Provision: k1, From: "30 ngày", To: "20 ngày", EffFrom: amendDay("2026-02-01"), Tier: LegalTierLegacyParserPhrase},
			},
			prov: k1, state: LegalStateCurrentVerified,
			has: []string{"1. Nộp hồ sơ trong 20 ngày [thay cho “30 ngày” theo Nghị định 2/2026/NĐ-CP, từ 01/02/2026] tại Cục Thuế."}},
		{name: "phrase From absent from the version before B",
			edges: []LegalAmendEdge{{Op: LegalOpThayCumTu, Amender: b1, Target: nd68, Provision: k1, From: "25 ngày", To: "20 ngày", EffFrom: amendDay("2025-07-01"), Tier: LegalTierLegacyParserPhrase}},
			prov:  k1, state: LegalStateUnresolved, gated: true, has: []string{"không thấy cụm từ “25 ngày” trong khoản 1 Điều 5"}},
		{name: "earlier edge unresolved → later phrase unresolved",
			edges: []LegalAmendEdge{
				{Op: LegalOpSuaDoi, Amender: b1, Target: nd68, Provision: k1, QuoteText: "1. x", EffFrom: amendDay("2025-07-01"), Tier: LegalTierVerified, Unresolved: "văn bản đích không xác định"},
				{Op: LegalOpThayCumTu, Amender: b2, Target: nd68, Provision: k1, From: "15 ngày", To: "20 ngày", EffFrom: amendDay("2026-02-01"), Tier: LegalTierLegacyParserPhrase},
			},
			prov: k1, state: LegalStateUnresolved, has: []string{"văn bản đích không xác định", "chưa giải được các sửa đổi trước đó"}},
		{name: "conflict: same khoản same date different text",
			edges: []LegalAmendEdge{
				quote(LegalOpSuaDoi, k1, "1. Phiên bản A.", b2, "2026-02-01", LegalTierVerified),
				quote(LegalOpSuaDoi, k1, "1. Phiên bản B.", b3, "2026-02-01", LegalTierVerified),
			},
			prov: k1, state: LegalStateConflict, gated: true, has: []string{"xung đột cùng ngày 01/02/2026"}},
		{name: "same content from two sources is no conflict",
			edges: []LegalAmendEdge{
				quote(LegalOpSuaDoi, k1, "1. Phiên bản A.", b2, "2026-02-01", LegalTierVerified),
				quote(LegalOpSuaDoi, k1, "1.  Phiên bản A.", b2, "2026-02-01", LegalTierVerified),
			},
			prov: k1, state: LegalStateCurrentVerified},
		{name: "renumbering inside one B applies at once",
			edges: []LegalAmendEdge{
				{Op: LegalOpThayCumTu, Amender: b1, Target: nd68, Provision: LegalProvisionRef{Article: "5"}, From: "10 ngày", To: "15 ngày", EffFrom: amendDay("2025-07-01"), Tier: LegalTierLegacyParserPhrase},
				{Op: LegalOpThayCumTu, Amender: b1, Target: nd68, Provision: LegalProvisionRef{Article: "5"}, From: "15 ngày", To: "20 ngày", EffFrom: amendDay("2025-07-01"), Tier: LegalTierLegacyParserPhrase},
			},
			prov: k3, state: LegalStateCurrentVerified, has: []string{"trong 15 ngày [thay cho “10 ngày” theo Nghị định 1/2025/NĐ-CP, từ 01/07/2025]."}, hasnt: []string{"20 ngày"},
			chunks: "trong 20 ngày [thay cho “15 ngày” theo Nghị định 1/2025/NĐ-CP, từ 01/07/2025] tại"},
		{name: "amender expiring later reverts nothing",
			// b1 itself expired in 2026 (not an edge): the change stays
			edges: []LegalAmendEdge{quote(LegalOpSuaDoi, k1, "1. Nộp hồ sơ trong 30 ngày.", b1, "2025-07-01", LegalTierVerified)},
			asOf:  amendAt("2027-06-01"), prov: k1, state: LegalStateCurrentVerified, has: []string{"30 ngày"}},
		{name: "tam_ngung inside its interval",
			edges: []LegalAmendEdge{op(LegalOpTamNgung, k3, b2, "2026-01-01", "2026-12-31")},
			prov:  k3, state: LegalStateNotApplicable, gated: true, has: []string{"[Tạm ngưng hiệu lực từ 01/01/2026 đến 31/12/2026 theo Nghị định 2/2026/NĐ-CP]\n\n3. Cơ quan thuế"}},
		{name: "tam_ngung after its end",
			edges: []LegalAmendEdge{op(LegalOpTamNgung, k3, b2, "2026-01-01", "2026-06-30")},
			prov:  k3, state: LegalStateOriginalInForce, hasnt: []string{"Tạm ngưng"}},
		{name: "het_hieu_luc partial has no end",
			edges: []LegalAmendEdge{op(LegalOpHetHieuLuc, k3, b2, "2026-01-01", "")},
			asOf:  amendAt("2030-01-01"), prov: k3, state: LegalStatePartiallyExpired, gated: true, has: []string{"[Hết hiệu lực từ 01/01/2026 theo Nghị định 2/2026/NĐ-CP]"}},
		{name: "khong_ap_dung with a condition: unknown match",
			edges: []LegalAmendEdge{{Op: LegalOpKhongApDung, Amender: b2, Target: nd68, Provision: k1, Scope: "đối với hộ kinh doanh mới thành lập", EffFrom: amendDay("2026-01-01"), Tier: LegalTierVerified}},
			prov:  k1, state: LegalStateConditionallyNotApp, gated: true, cond: LegalConditionUnknown,
			has: []string{"[Không áp dụng đối với hộ kinh doanh mới thành lập theo Nghị định 2/2026/NĐ-CP, từ 01/01/2026]\n\n1. Nộp hồ sơ"}},
		{name: "khong_ap_dung, condition known not to match",
			edges: []LegalAmendEdge{{Op: LegalOpKhongApDung, Amender: b2, Target: nd68, Provision: k1, Scope: "đối với X", EffFrom: amendDay("2026-01-01"), Tier: LegalTierVerified}},
			opt:   LegalResolveOptions{MatchCondition: func(*LegalAmendEdge) LegalConditionMatch { return LegalConditionNoMatch }},
			prov:  k1, state: LegalStateConditionallyNotApp, gated: false, cond: LegalConditionNoMatch},
		{name: "10.2: amending text outside the allowed KBs",
			edges: []LegalAmendEdge{quote(LegalOpSuaDoi, k1, "1. Bí mật.", private, "2026-01-01", LegalTierVerified)},
			opt:   LegalResolveOptions{AllowedKBs: map[string]bool{"kb-pub": true}},
			prov:  k1, state: LegalStateAmendedTextMissing, gated: true,
			has: []string{"đã bị sửa đổi theo Nghị định 9/2026/NĐ-CP", "không thuộc phạm vi được dùng"}, hasnt: []string{"Bí mật"}},
		{name: "10.2: a phrase's new words are B's text too",
			edges: []LegalAmendEdge{{Op: LegalOpThayCumTu, Amender: private, Target: nd68, Provision: k1, From: "15 ngày", To: "5 ngày", EffFrom: amendDay("2026-01-01"), Tier: LegalTierLegacyParserPhrase}},
			opt:   LegalResolveOptions{AllowedKBs: map[string]bool{"kb-pub": true}},
			prov:  k1, state: LegalStateAmendedTextMissing, hasnt: []string{"5 ngày ["}},
		{name: "sua_doi without quoted text",
			edges: []LegalAmendEdge{quote(LegalOpSuaDoi, k1, "", b1, "2025-07-01", LegalTierUnverified)},
			prov:  k1, state: LegalStateAmendedTextMissing, has: []string{"không có lời văn sửa đổi"}},
		{name: "rejected edge (B's own scope) is ignored",
			edges: []LegalAmendEdge{{Op: LegalOpKhongApDung, Amender: b2, Target: nd68, Provision: k1, EffFrom: amendDay("2026-01-01"), Tier: LegalTierUnverified, Rejected: "phạm vi của B"}},
			prov:  k1, state: LegalStateOriginalInForce},
		{name: "điểm-level edge touches only its điểm",
			edges: []LegalAmendEdge{quote(LegalOpSuaDoi, k2a, "a) tờ khai điện tử;", b1, "2025-07-01", LegalTierVerified)},
			prov:  k2, state: LegalStateOriginalInForce, has: []string{"2. Hồ sơ gồm:"}, chunks: "a) tờ khai điện tử;"},
		{name: "catalogue says amended, no edge of that amender at all",
			opt:  LegalResolveOptions{Catalogue: []LegalCatalogAmendment{{Amender: b3, EffFrom: amendDay("2026-01-01")}}},
			prov: k1, state: LegalStateAmendedTextMissing, gated: true,
			chunks: "trả lời trong 10 ngày.\n\n[LỜI VĂN HIỆN HÀNH CHƯA XÁC MINH: văn bản này đã bị sửa đổi bởi Nghị định 3/2026/NĐ-CP (từ 01/01/2026) theo danh mục; chưa có lời văn sửa đổi]"},
		{name: "catalogue amender with edges elsewhere",
			edges: []LegalAmendEdge{quote(LegalOpSuaDoi, LegalProvisionRef{Article: "9"}, "Điều 9. x", b3, "2026-01-01", LegalTierVerified)},
			opt:   LegalResolveOptions{Catalogue: []LegalCatalogAmendment{{Amender: b3, EffFrom: amendDay("2026-01-01")}}},
			prov:  k1, state: LegalStateUnverified, has: []string{"1. Nộp hồ sơ"}, chunks: "chưa xác định điều khoản này có bị tác động không"},
		{name: "catalogue amender not yet in force",
			opt:  LegalResolveOptions{Catalogue: []LegalCatalogAmendment{{Amender: b3, EffFrom: amendDay("2027-01-01")}}},
			prov: k1, state: LegalStateOriginalInForce},
		{name: "khac → unresolved",
			edges: []LegalAmendEdge{op(LegalOpKhac, k1, b1, "2025-07-01", "")},
			prov:  k1, state: LegalStateUnresolved},
		{name: "tiep_tuc_ap_dung tier 1 is a note",
			edges: []LegalAmendEdge{op(LegalOpTiepTucApDung, k1, b1, "2025-07-01", "2026-12-31")},
			prov:  k1, state: LegalStateOriginalInForce, has: []string{"[Tiếp tục được áp dụng theo Nghị định 1/2025/NĐ-CP, từ 01/07/2025 đến 31/12/2026]"}},
		{name: "bai_bo_cum_tu",
			edges: []LegalAmendEdge{{Op: LegalOpBaiBoCumTu, Amender: b1, Target: nd68, Provision: k1, From: "tại Tổng cục Thuế", EffFrom: amendDay("2025-07-01"), Tier: LegalTierLegacyParserPhrase}},
			prov:  k1, state: LegalStateCurrentVerified, has: []string{"[cụm từ “tại Tổng cục Thuế” đã bị bãi bỏ theo Nghị định 1/2025/NĐ-CP, từ 01/07/2025]"}},
		{name: "dinh_chi",
			edges: []LegalAmendEdge{op(LegalOpDinhChi, k1, b1, "2026-01-01", "")},
			prov:  k1, state: LegalStateNotApplicable, has: []string{"[Bị đình chỉ thi hành từ 01/01/2026 theo Nghị định 1/2025/NĐ-CP]"}},
		{name: "thay_the_van_ban",
			edges: []LegalAmendEdge{op(LegalOpThayTheVanBan, LegalProvisionRef{}, b2, "2026-01-01", "")},
			prov:  k1, state: LegalStateCurrentVerified, chunks: "[Văn bản đã bị thay thế theo Nghị định 2/2026/NĐ-CP, từ 01/01/2026]"},
		{name: "bai_bo_van_ban tier 2",
			edges: []LegalAmendEdge{{Op: LegalOpBaiBoVanBan, Amender: b2, Target: nd68, EffFrom: amendDay("2026-01-01"), Tier: LegalTierUnverified}},
			prov:  k3, state: LegalStateUnverified, chunks: "[LỜI VĂN HIỆN HÀNH CHƯA XÁC MINH: có thể văn bản bị bãi bỏ theo Nghị định 2/2026/NĐ-CP, từ 01/01/2026]\n\n#### Điều 5."},
		{name: "identity problem",
			prov: k1, state: LegalStateUnresolved, chunks: "chưa xác định được văn bản này trong danh mục"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			asOf := tc.asOf
			if asOf.IsZero() {
				asOf = now
			}
			cc := c
			if tc.name == "identity problem" {
				cc.IdentityProblem = "doc_number không khớp"
			}
			edges := append([]LegalAmendEdge(nil), tc.edges...)
			r := ResolveLegalProvisions(cc, edges, asOf, tc.opt)
			u := unitByProv(t, r, tc.prov)
			require.Equal(t, tc.state, u.State, "reasons %v\n%s", u.Reasons, r.ModelContent)
			if tc.under != "" {
				require.Equal(t, tc.under, u.Underlying)
			}
			if tc.cond != "" {
				require.Equal(t, tc.cond, u.Condition)
			}
			if tc.gated || tc.state == LegalStateOriginalInForce || tc.state == LegalStateCurrentVerified {
				require.Equal(t, tc.gated, u.Gated())
			}
			for _, s := range tc.has {
				require.Contains(t, u.Text, s, "unit text")
			}
			for _, s := range tc.hasnt {
				require.NotContains(t, u.Text, s)
			}
			if tc.chunks != "" {
				require.Contains(t, r.ModelContent, tc.chunks)
			}
			if len(tc.edges) == 0 && tc.opt.Catalogue == nil && cc.IdentityProblem == "" {
				require.Equal(t, dieu5, r.ModelContent, "no edge: stored text")
			}
		})
	}
}

func TestLegalUnitsCoverContent(t *testing.T) {
	for _, content := range []string{dieu5, nd68Dieu3, "tiếp đoạn trước\n\nb) điểm b.\n\n5. Khoản năm", "#### Điều 1. A\n\n1. x\n\n#### Điều 2. B\n\n1. y"} {
		units := legalUnits(content, &vietnamese_legal.LegalMetadata{ArticleNos: []string{"4"}, SubdivisionRefs: []string{"khoan:4", "khoan:4/diem:a"}})
		var b strings.Builder
		for _, u := range units {
			b.WriteString(content[u.start:u.end])
		}
		require.Equal(t, content, b.String())
	}
	units := legalUnits("tiếp đoạn trước\n\nb) điểm b.\n\n5. Khoản năm", &vietnamese_legal.LegalMetadata{ArticleNos: []string{"4"}, SubdivisionRefs: []string{"khoan:4", "khoan:4/diem:a", "khoan:5"}})
	require.Equal(t, LegalProvisionRef{Article: "4", Khoan: "4", Diem: "a"}, units[0].prov, "continues the open điểm")
	require.Equal(t, LegalProvisionRef{Article: "4", Khoan: "4", Diem: "b"}, units[1].prov)
	require.Equal(t, LegalProvisionRef{Article: "4", Khoan: "5"}, units[2].prov)
}

// Review B3: a khoản-level phrase change is not "missing" in the units of
// the khoản that do not hold the words, nor in a chunk holding only part
// of the khoản; it is when the whole khoản is here without them.
func TestResolvePhraseScopeWithinKhoan(t *testing.T) {
	a := LegalAmendDoc{CatalogID: "A", Number: "1/2020/NĐ-CP"}
	b := LegalAmendDoc{CatalogID: "B", Number: "2/2021/NĐ-CP", Type: "Nghị định", KnowledgeBaseID: "kb"}
	eff := amendDay("2021-01-01")
	edge := func(from string) []LegalAmendEdge {
		return []LegalAmendEdge{{Op: LegalOpThayCumTu, Amender: b, Target: a, Provision: LegalProvisionRef{Article: "5", Khoan: "2"},
			From: from, To: "Sở Nội vụ", EffFrom: eff, Tier: LegalTierLegacyParserPhrase}}
	}
	at := amendAt("2026-01-01")
	opt := LegalResolveOptions{AllowedKBs: map[string]bool{"kb": true}}

	// the words sit in điểm a: the lead line and điểm b are untouched
	whole := LegalProvisionChunk{ID: "a1", Doc: a, Content: "#### Điều 5. X\n\n1. Khoản một.\n\n2. Hồ sơ gồm:\n\na) Văn bản của Sở Tư pháp;\n\nb) Giấy tờ khác.\n\n3. Khoản ba.",
		Legal: &vietnamese_legal.LegalMetadata{ArticleNos: []string{"5"}}}
	r := ResolveLegalProvisions(whole, edge("Sở Tư pháp"), at, opt)
	require.Equal(t, LegalStateCurrentVerified, r.State, r.ModelContent)
	require.NotContains(t, r.ModelContent, "không thấy")
	require.Contains(t, r.ModelContent, "a) Văn bản của Sở Nội vụ [thay cho “Sở Tư pháp”")

	// a continuation chunk with only điểm c, d of khoản 2: nothing to say
	cont := LegalProvisionChunk{ID: "a2", Doc: a, Content: "c) Hồ sơ bổ sung;\n\nd) Giấy tờ khác.",
		Legal: &vietnamese_legal.LegalMetadata{ArticleNos: []string{"5"}, SubdivisionRefs: []string{"khoan:2", "khoan:2/diem:c", "khoan:2/diem:d"}}}
	r = ResolveLegalProvisions(cont, edge("Sở Tư pháp"), at, opt)
	require.Equal(t, LegalStateOriginalInForce, r.State, r.ModelContent)
	require.Equal(t, cont.Content, r.ModelContent)

	// the whole khoản 2 is here and holds the words nowhere: one note
	r = ResolveLegalProvisions(whole, edge("Sở Xây dựng"), at, opt)
	require.Equal(t, LegalStateUnresolved, r.State)
	require.Equal(t, 1, strings.Count(r.ModelContent, "không thấy cụm từ “Sở Xây dựng” trong khoản 2 Điều 5"), r.ModelContent)
	require.Contains(t, r.ModelContent, "[LỜI VĂN HIỆN HÀNH CHƯA XÁC MINH: không thấy cụm từ “Sở Xây dựng” trong khoản 2 Điều 5 ở bản ngay trước Nghị định 2/2021/NĐ-CP]\n\n2. Hồ sơ gồm:")

	// khoản 2 runs on into the next chunk: absence is not proven
	open := LegalProvisionChunk{ID: "a3", Doc: a, Content: "#### Điều 5. X\n\n1. Khoản một.\n\n2. Hồ sơ gồm:\n\na) Văn bản;",
		Legal: &vietnamese_legal.LegalMetadata{ArticleNos: []string{"5"}}}
	r = ResolveLegalProvisions(open, edge("Sở Xây dựng"), at, opt)
	require.Equal(t, LegalStateOriginalInForce, r.State, r.ModelContent)
}

// Review B3: labels on quoted lines ("> 1.") are another document's text,
// not this document's khoản.
func TestLegalUnitsSkipQuotedLabels(t *testing.T) {
	content := "#### Điều 1. Sửa đổi Nghị định X\n\n1. Sửa đổi khoản 2 Điều 5 như sau:\n\n> “2. Khoản hai mới:\n\n> a) điểm a mới.”.\n\n2. Bãi bỏ Điều 7."
	units := legalUnits(content, &vietnamese_legal.LegalMetadata{ArticleNos: []string{"1"}})
	var provs []LegalProvisionRef
	for _, u := range units {
		provs = append(provs, u.prov)
	}
	require.Equal(t, []LegalProvisionRef{{Article: "1"}, {Article: "1", Khoan: "1"}, {Article: "1", Khoan: "2"}}, provs)
}

// Review S3: entries naming only a document are no rewrite of it; two such
// entries are no CONFLICT.
func TestResolveUnresolvedRewritesNoConflict(t *testing.T) {
	c := LegalProvisionChunk{ID: "a", Content: "#### Điều 3. X\n\n1. Một.\n\n2. Hai.", Legal: &vietnamese_legal.LegalMetadata{ArticleNos: []string{"3"}},
		Doc: LegalAmendDoc{CatalogID: "A", Number: "1/2020/NĐ-CP"}}
	b := LegalAmendDoc{CatalogID: "B", Number: "2/2021/NĐ-CP", Type: "Nghị định", KnowledgeBaseID: "kb"}
	eff := amendDay("2021-01-01")
	edges := []LegalAmendEdge{
		{Op: LegalOpSuaDoi, Amender: b, Target: c.Doc, EffFrom: eff, Tier: LegalTierUnverified, QuoteText: "x", Unresolved: "lời văn trích không mở đầu bằng nhãn"},
		{Op: LegalOpSuaDoi, Amender: b, Target: c.Doc, EffFrom: eff, Tier: LegalTierUnverified, QuoteText: "y", Unresolved: "lời văn trích không mở đầu bằng nhãn"},
	}
	r := ResolveLegalProvisions(c, edges, amendAt("2026-01-01"), LegalResolveOptions{AllowedKBs: map[string]bool{"kb": true}})
	require.Equal(t, LegalStateUnresolved, r.State, r.ModelContent)
	require.NotContains(t, r.ModelContent, "xung đột")
}

// Review: a pending change is shown, not only collected; a long tier-2
// quote is capped in the note.
func TestResolvePendingAndQuoteCap(t *testing.T) {
	c := nd68Chunk(dieu5, "5", "1", "2", "3")
	b := LegalAmendDoc{CatalogID: "b9", Number: "9/2027/NĐ-CP", Type: "Nghị định", KnowledgeBaseID: "kb-pub"}
	long := "1. " + strings.Repeat("rất dài ", 200)
	edges := []LegalAmendEdge{
		{Op: LegalOpSuaDoi, Amender: b, Target: nd68, Provision: LegalProvisionRef{Article: "5", Khoan: "3"}, QuoteText: "3. Mới.", EffFrom: amendDay("2027-01-01"), Tier: LegalTierUnverified},
		{Op: LegalOpSuaDoi, Amender: b, Target: nd68, Provision: LegalProvisionRef{Article: "5", Khoan: "1"}, QuoteText: long, EffFrom: amendDay("2026-01-01"), Tier: LegalTierUnverified},
	}
	r := ResolveLegalProvisions(c, edges, amendAt("2026-10-20"), LegalResolveOptions{})
	require.Contains(t, r.ModelContent, "[Sắp có hiệu lực: khoản 3 Điều 5 được sửa đổi theo Nghị định 9/2027/NĐ-CP, từ 01/01/2027]\n\n3. Cơ quan thuế")
	require.NotContains(t, r.ModelContent, "3. Mới.", "a pending tier-2 text is not shown")
	require.Less(t, len([]rune(r.ModelContent)), len([]rune(dieu5))+legalQuoteRunes+300)
	require.Contains(t, r.ModelContent, " …”]")
}

// Review S5: an amender the catalogue records expiring on or before its
// effective date never applied.
func TestResolveCatalogueNeverInForce(t *testing.T) {
	c := nd68Chunk(dieu5, "5", "1", "2", "3")
	b := LegalAmendDoc{CatalogID: "bx", Number: "7/2025/NĐ-CP", Type: "Nghị định"}
	r := ResolveLegalProvisions(c, nil, amendAt("2026-10-20"), LegalResolveOptions{
		Catalogue: []LegalCatalogAmendment{{Amender: b, EffFrom: amendDay("2025-07-01"), NeverInForce: true}}})
	require.Equal(t, LegalStateUnresolved, r.State)
	require.Contains(t, r.ModelContent, "hết hiệu lực trước hoặc ngay khi có hiệu lực")
}

// NĐ 150/2016 → NĐ 48/2013: one list of khoản for two phrase pairs; a
// listed khoản holding only one of them is no error.
func TestResolveSharedPhraseList(t *testing.T) {
	a := LegalAmendDoc{CatalogID: "A", Number: "48/2013/NĐ-CP"}
	b := LegalAmendDoc{CatalogID: "B", Number: "150/2016/NĐ-CP", Type: "Nghị định", KnowledgeBaseID: "kb"}
	lead := `2. Thay thế cụm từ "Bộ Tư pháp" bằng cụm từ "Văn phòng Chính phủ"; cụm từ "Bộ trưởng Bộ Tư pháp" bằng cụm từ "Bộ trưởng, Chủ nhiệm Văn phòng Chính phủ" tại khoản 1, 2 Điều 1`
	c := LegalProvisionChunk{ID: "a", Doc: a, Content: "#### Điều 1. X\n\n1. Một.\n\n2. Sửa đổi Điều 9: Bộ Tư pháp cho ý kiến.\n\n3. Ba.",
		Legal: &vietnamese_legal.LegalMetadata{ArticleNos: []string{"1"}}}
	edges := []LegalAmendEdge{{Op: LegalOpThayCumTu, Amender: b, Target: a, Provision: LegalProvisionRef{Article: "1", Khoan: "2"},
		From: "Bộ trưởng Bộ Tư pháp", To: "Bộ trưởng, Chủ nhiệm Văn phòng Chính phủ", LeadInText: lead, EffFrom: amendDay("2016-12-15"), Tier: LegalTierLegacyParserPhrase}}
	r := ResolveLegalProvisions(c, edges, amendAt("2026-10-20"), LegalResolveOptions{AllowedKBs: map[string]bool{"kb": true}})
	require.Equal(t, LegalStateOriginalInForce, r.State, r.ModelContent)
}
