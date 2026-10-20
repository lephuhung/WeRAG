package service

import (
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// Amendment edges (plan 2026-10-19-legal-amendment-kg §4.3–§4.7, §4.12):
// one operation document B makes on document A — a provision of A or A as
// a whole — with when it applies and how sure we are. Edges are source
// agnostic: today the only source is the vbpl converter's chunk metadata
// (legal.amends, loaded by LoadLegalAmendEdges); the Qwen extraction and
// the Neo4j projection of later steps produce the same type. The resolver
// (ResolveLegalProvisions) turns the edges touching one retrieved chunk of
// A into a state and a text per khoản / điểm.
//
// They live in package service, next to the phrase amendments and the
// amendment history they replace in step 11: the resolver reuses their
// unexported helpers (findPhrase, applyPhraseChanges, provisionText …) and
// the loader the catalogue lookups (ResolveLegalDocumentName).

// LegalAmendOp is what B does to A (plan §4.3).
type LegalAmendOp string

const (
	LegalOpSuaDoi        LegalAmendOp = "sua_doi"          // rewrite a provision (quoted new text)
	LegalOpBoSung        LegalAmendOp = "bo_sung"          // add a provision (NewLabel, quoted text)
	LegalOpThayCumTu     LegalAmendOp = "thay_cum_tu"      // replace words From → To
	LegalOpBaiBoCumTu    LegalAmendOp = "bai_bo_cum_tu"    // remove words From
	LegalOpBaiBo         LegalAmendOp = "bai_bo"           // repeal a provision
	LegalOpThayTheVanBan LegalAmendOp = "thay_the_van_ban" // replace document A
	LegalOpBaiBoVanBan   LegalAmendOp = "bai_bo_van_ban"   // repeal document A
	LegalOpDinhChi       LegalAmendOp = "dinh_chi"         // suspend by order (đình chỉ)
	LegalOpKhongApDung   LegalAmendOp = "khong_ap_dung"    // does not apply (to Scope, in [EffFrom, EffTo))
	LegalOpTamNgung      LegalAmendOp = "tam_ngung"        // suspended in [EffFrom, EffTo)
	LegalOpHetHieuLuc    LegalAmendOp = "het_hieu_luc"     // expires from EffFrom, no end
	LegalOpTiepTucApDung LegalAmendOp = "tiep_tuc_ap_dung" // transition: still applies
	LegalOpKhac          LegalAmendOp = "khac"             // anything else: read B
)

// LegalAmendTier is how far an edge may be trusted (plan §3.3).
type LegalAmendTier string

const (
	// LegalTierVerified (tier 1) edges replace the wording: two measured
	// agreeing sources (§6.1) on a path that passed gate §6.2. None yet.
	LegalTierVerified LegalAmendTier = "tier1"
	// LegalTierUnverified (tier 2): one checked source, or a path not yet
	// through the gate; the provision is UNVERIFIED and the answer gate
	// applies (§4.11).
	LegalTierUnverified LegalAmendTier = "tier2"
	// LegalTierLegacyParserPhrase is INTERIM (until the Qwen extraction
	// exists): a phrase change read by the vbpl converter. It replaces the
	// words as LegalCurrentWording always did, labelled with its source,
	// and counts as verified for the wording. Listed in plan §3.3.
	LegalTierLegacyParserPhrase LegalAmendTier = "legacy_parser_phrase"
)

// verifiedWording reports whether the edge's change is applied to the
// text the model reads instead of only noted.
func (t LegalAmendTier) verifiedWording() bool {
	return t == LegalTierVerified || t == LegalTierLegacyParserPhrase
}

// Edge sources.
const (
	LegalAmendSourceParser    = "vbpl_parser" // chunks.metadata.legal.amends
	LegalAmendSourceCatalogue = "catalogue"   // legal_document_relations
	LegalAmendSourceChain     = "chain"       // derived: C amends B's amending provision
)

// LegalAmendDoc identifies a document of an edge: its catalogue row and,
// for the amender, the chunk the operation is written in.
type LegalAmendDoc struct {
	CatalogID       string // legal_documents.id (= vbpl id)
	Number          string // "141/2026/NĐ-CP"
	Type            string // "Nghị định"
	KnowledgeID     string
	KnowledgeBaseID string
	ChunkID         string
}

func (d LegalAmendDoc) label() string {
	return legalHistoryDoc{Number: d.Number, Type: d.Type}.label()
}

// LegalProvisionRef is a provision: Điều Article, khoản Khoan, điểm Diem;
// an empty Article is the whole document, an empty Khoan the whole Điều.
type LegalProvisionRef struct {
	Article string `json:"article,omitempty"`
	Khoan   string `json:"khoan,omitempty"`
	Diem    string `json:"diem,omitempty"`
}

func (p LegalProvisionRef) normalized() LegalProvisionRef {
	return LegalProvisionRef{strings.ToLower(strings.TrimSpace(p.Article)),
		strings.ToLower(strings.TrimSpace(p.Khoan)), strings.ToLower(strings.TrimSpace(p.Diem))}
}

// covers reports whether p holds q (a whole Điều holds its khoản).
func (p LegalProvisionRef) covers(q LegalProvisionRef) bool {
	switch {
	case p.Article == "":
		return true
	case p.Article != q.Article:
		return false
	case p.Khoan == "":
		return true
	case p.Khoan != q.Khoan:
		return false
	}
	return p.Diem == "" || p.Diem == q.Diem
}

func (p LegalProvisionRef) key() string { return p.Article + "|" + p.Khoan + "|" + p.Diem }

// String renders "điểm a khoản 2 Điều 5"; "văn bản" for the whole document.
func (p LegalProvisionRef) String() string {
	if p.Article == "" {
		return "toàn văn bản"
	}
	return vietnamese_legal.AmendRef{Article: p.Article, Khoan: p.Khoan, Diem: p.Diem}.String()
}

// label is the label a quoted text of p starts with: "Điều 5.", "2.", "a)".
func (p LegalProvisionRef) label() string {
	switch {
	case p.Diem != "":
		return p.Diem + ")"
	case p.Khoan != "":
		return p.Khoan + "."
	case p.Article != "":
		return "điều " + p.Article + "."
	}
	return ""
}

// LegalAmendDate is a date that may be unknown. Only an effective date is
// one: an issue date alone leaves it unknown (plan §4.7).
type LegalAmendDate struct {
	At    time.Time
	Known bool
}

func legalDay(s string, loc *time.Location) LegalAmendDate {
	if loc == nil {
		loc = time.UTC
	}
	t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(s), loc)
	if err != nil {
		return LegalAmendDate{}
	}
	return LegalAmendDate{At: t, Known: true}
}

func (d LegalAmendDate) text() string {
	if !d.Known {
		return "chưa rõ ngày"
	}
	return d.At.Format("02/01/2006")
}

// iso is the date as legalHistoryDoc keeps it ("" when unknown).
func (d LegalAmendDate) iso() string {
	if !d.Known {
		return ""
	}
	return d.At.Format("2006-01-02")
}

// LegalRuneSpan is [Start, End) in runes of the amender chunk's content as
// stored (the chunk offset convention).
type LegalRuneSpan struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// LegalAmendEdge is one operation of B on A.
type LegalAmendEdge struct {
	Op        LegalAmendOp
	Amender   LegalAmendDoc // B, with the chunk holding the operation
	Target    LegalAmendDoc // A (CatalogID / Number)
	Provision LegalProvisionRef
	// Origin is where in B the operation is written (Điều / khoản of B), to
	// follow C → B → A.
	Origin LegalProvisionRef
	// From / To are the words of a phrase operation (To "" = removed).
	From, To string
	// NewLabel is the label a bo_sung adds under Provision ("15", "c",
	// "5a" for a Điều); After is the label it goes after, when written.
	NewLabel, After string
	// Quote is the quoted new text in B's chunk (sua_doi / bo_sung);
	// QuoteText its text without the "> " markers.
	Quote     *LegalRuneSpan
	QuoteText string
	// Scope is the condition of a khong_ap_dung ("đối với …").
	Scope string
	// EffFrom / EffTo bound when the edge applies; EffTo is only read for
	// interval operations (tam_ngung, dinh_chi, khong_ap_dung,
	// tiep_tuc_ap_dung): an amender expiring later never reverts a change.
	EffFrom, EffTo LegalAmendDate
	Tier           LegalAmendTier
	Sources        []string
	// LeadIn is B's sentence introducing the operation; ChunkHash the
	// hash of B's chunk content the spans refer to.
	LeadIn     *LegalRuneSpan
	LeadInText string
	ChunkHash  string
	// TextWithheld: B's chunk is outside the KBs the question may read
	// (plan §10.2) — the edge's words (Quote, To) must not reach the
	// model; the provision is AMENDED_TEXT_MISSING.
	TextWithheld bool
	// Unresolved is why the edge cannot be placed or checked (identity,
	// date, validation): the provisions it may touch are UNRESOLVED.
	Unresolved string
	// Rejected is why the edge is not an operation on A at all ("Nghị
	// định này không áp dụng …"); it is kept for review, the resolver
	// ignores it.
	Rejected string
}

// carriesText reports whether the edge's change is B's words.
func (e *LegalAmendEdge) carriesText() bool {
	switch e.Op {
	case LegalOpSuaDoi, LegalOpBoSung:
		return true
	case LegalOpThayCumTu:
		return e.To != ""
	}
	return false
}

// interval reports whether EffTo bounds the edge.
func (e *LegalAmendEdge) interval() bool {
	switch e.Op {
	case LegalOpTamNgung, LegalOpDinhChi, LegalOpKhongApDung, LegalOpTiepTucApDung:
		return true
	}
	return false
}

func (e *LegalAmendEdge) phrase() bool {
	return e.Op == LegalOpThayCumTu || e.Op == LegalOpBaiBoCumTu
}

// sourceText names the edge's sources for a note.
func (e *LegalAmendEdge) sourceText() string {
	var names []string
	for _, s := range e.Sources {
		switch s {
		case LegalAmendSourceParser:
			names = append(names, "bộ đọc vbpl")
		case LegalAmendSourceCatalogue:
			names = append(names, "danh mục vbpl")
		case LegalAmendSourceChain:
			names = append(names, "suy ra qua văn bản sửa đổi")
		default:
			names = append(names, s)
		}
	}
	return strings.Join(names, ", ")
}

// LegalProvisionState is the resolver's verdict on one khoản / điểm (§4.12).
type LegalProvisionState string

const (
	LegalStateOriginalInForce     LegalProvisionState = "ORIGINAL_IN_FORCE"
	LegalStateCurrentVerified     LegalProvisionState = "CURRENT_VERIFIED"
	LegalStateUnverified          LegalProvisionState = "UNVERIFIED"
	LegalStateUnresolved          LegalProvisionState = "UNRESOLVED"
	LegalStateConflict            LegalProvisionState = "CONFLICT"
	LegalStateAmendedTextMissing  LegalProvisionState = "AMENDED_TEXT_MISSING"
	LegalStateNotApplicable       LegalProvisionState = "NOT_APPLICABLE_IN_PERIOD"
	LegalStatePartiallyExpired    LegalProvisionState = "PARTIALLY_EXPIRED"
	LegalStateConditionallyNotApp LegalProvisionState = "CONDITIONALLY_NOT_APPLICABLE"
)

// legalStateRank orders states, the most severe last: a unit takes the
// most severe state any of its edges gives it.
var legalStateRank = map[LegalProvisionState]int{
	LegalStateOriginalInForce:     0,
	LegalStateCurrentVerified:     1,
	LegalStateConditionallyNotApp: 2,
	LegalStateNotApplicable:       3,
	LegalStatePartiallyExpired:    4,
	LegalStateUnverified:          5,
	LegalStateAmendedTextMissing:  6,
	LegalStateUnresolved:          7,
	LegalStateConflict:            8,
}

func worseState(a, b LegalProvisionState) LegalProvisionState {
	if legalStateRank[b] > legalStateRank[a] {
		return b
	}
	return a
}

// Gated reports whether the answer gate (§4.11) applies to the state.
func (s LegalProvisionState) Gated() bool {
	return s != LegalStateOriginalInForce && s != LegalStateCurrentVerified
}

// LegalConditionMatch is how a khong_ap_dung condition compares with the
// question (§4.12).
type LegalConditionMatch string

const (
	LegalConditionMatches LegalConditionMatch = "match"
	LegalConditionNoMatch LegalConditionMatch = "no_match"
	LegalConditionUnknown LegalConditionMatch = "unknown"
)
