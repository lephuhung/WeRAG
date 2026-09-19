// entity.go is the Go port of AIRAG's deterministic legal entity layer
// (backend/app/services/kg/legal_kg_service.py): entity canonicalization,
// type forcing, generic/junk guards, self-reference folding, person composite
// keys and document-header metadata parsing.
//
// Everything here is pure code — no LLM, no I/O. The goal is that entity
// merge keys converge regardless of how the extraction model spelled a name
// ("UBND tỉnh" vs "UBND Tỉnh Nghệ An", "53/2022/nđ-cp" vs "53/2022/NĐ-CP"),
// and that non-specific junk never becomes a graph node.
package vietnamese_legal

import (
	"fmt"
	"regexp"
	"strings"
)

// Legal entity types — the closed vocabulary the extraction prompt is
// constrained to (AIRAG LEGAL_KG_SYSTEM_PROMPT).
const (
	EntityTypeArticle      = "Article"
	EntityTypeDocument     = "Document"
	EntityTypeOrganization = "Organization"
	EntityTypePerson       = "Person"
	EntityTypeTask         = "Task"
	EntityTypeLocation     = "Location"
)

// Legal relation types (AIRAG LEGAL_KG_SYSTEM_PROMPT + personnel variant).
const (
	RelCanCu          = "CAN_CU"
	RelVienDan        = "VIEN_DAN"
	RelSuaDoi         = "SUA_DOI"
	RelThayThe        = "THAY_THE"
	RelBaiBo          = "BAI_BO"
	RelChuTri         = "CHU_TRI"
	RelPhoiHop        = "PHOI_HOP"
	RelChiuTrachNhiem = "CHIU_TRACH_NHIEM"
	RelPartOf         = "PART_OF"
	RelReferences     = "REFERENCES"
	RelKy             = "KY"
	RelBoNhiem        = "BO_NHIEM"
	RelMienNhiem      = "MIEN_NHIEM"
	RelDieuDong       = "DIEU_DONG"
	RelNghiHuu        = "NGHI_HUU"
	RelKhenThuong     = "KHEN_THUONG"
	RelKyLuat         = "KY_LUAT"
	RelPheDuyet       = "PHE_DUYET"
	RelLienQuan       = "LIEN_QUAN"
	RelBanHanhBoi     = "BAN_HANH_BOI"
	RelBanHanhTai     = "BAN_HANH_TAI"
	RelTrucThuoc      = "TRUC_THUOC"
	RelThuocTinh      = "THUOC_TINH"
)

var legalRelationTypes = map[string]bool{
	RelCanCu: true, RelVienDan: true, RelSuaDoi: true, RelThayThe: true,
	RelBaiBo: true, RelChuTri: true, RelPhoiHop: true, RelChiuTrachNhiem: true,
	RelPartOf: true, RelReferences: true, RelKy: true,
	RelBoNhiem: true, RelMienNhiem: true, RelDieuDong: true, RelNghiHuu: true,
	RelKhenThuong: true, RelKyLuat: true, RelPheDuyet: true, RelLienQuan: true,
	RelBanHanhBoi: true, RelBanHanhTai: true, RelTrucThuoc: true, RelThuocTinh: true,
}

// IsLegalRelationType reports whether rel is in the closed legal relation
// vocabulary (case-insensitive).
func IsLegalRelationType(rel string) bool {
	return legalRelationTypes[strings.ToUpper(strings.TrimSpace(rel))]
}

var legalEntityTypes = map[string]bool{
	EntityTypeArticle: true, EntityTypeDocument: true, EntityTypeOrganization: true,
	EntityTypePerson: true, EntityTypeTask: true, EntityTypeLocation: true,
}

// IsLegalEntityType reports whether etype is in the closed legal entity
// vocabulary (case-insensitive, canonicalized to the exported casing).
func IsLegalEntityType(etype string) bool {
	return legalEntityTypes[CanonicalEntityType(etype)]
}

// CanonicalEntityType maps a raw LLM entity type to the canonical casing,
// or "" when it is not a legal entity type.
func CanonicalEntityType(etype string) string {
	switch strings.ToLower(strings.TrimSpace(etype)) {
	case "article":
		return EntityTypeArticle
	case "document":
		return EntityTypeDocument
	case "organization", "organisation", "org":
		return EntityTypeOrganization
	case "person":
		return EntityTypePerson
	case "task":
		return EntityTypeTask
	case "location", "place":
		return EntityTypeLocation
	}
	return ""
}

// ---------------------------------------------------------------------------
// Date normalization (port of normalize_date)
// ---------------------------------------------------------------------------

var legalDatePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(\d{1,2})[/\-.](\d{1,2})[/\-.](\d{4})`), // DD/MM/YYYY or D/M/YYYY
	regexp.MustCompile(`(\d{4})[/\-.](\d{1,2})[/\-.](\d{1,2})`), // YYYY/MM/DD
}

var bareYearRe = regexp.MustCompile(`^\d{4}$`)

// NormalizeLegalDate normalizes any date-like string to DD/MM/YYYY.
// Returns "không xác định" on failure or bare-year ambiguity.
func NormalizeLegalDate(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "không xác định"
	}
	if m := legalDatePatterns[0].FindStringSubmatch(raw); m != nil {
		return fmt.Sprintf("%02d/%02d/%s", atoiSafe(m[1]), atoiSafe(m[2]), m[3])
	}
	if m := legalDatePatterns[1].FindStringSubmatch(raw); m != nil {
		return fmt.Sprintf("%02d/%02d/%s", atoiSafe(m[3]), atoiSafe(m[2]), m[1])
	}
	if bareYearRe.MatchString(raw) {
		return "không xác định"
	}
	return raw
}

// BuildPersonCompositeKey builds a disambiguated Person entity key from raw
// LLM output (port of build_person_composite_key).
// Priority: ngày sinh → CCCD/thẻ đảng → đơn vị → 'không xác định'.
func BuildPersonCompositeKey(name string, props map[string]string) string {
	name = strings.TrimSpace(name)
	if strings.Contains(name, "(") && strings.Contains(name, ")") {
		return name
	}
	if ngaySinh := props["ngay_sinh"]; ngaySinh != "" {
		if normalized := NormalizeLegalDate(ngaySinh); normalized != "không xác định" {
			return name + " (" + normalized + ")"
		}
	}
	if cccd := props["cccd"]; cccd != "" {
		return name + " (" + cccd + ")"
	}
	if soThe := props["so_the_dang"]; soThe != "" {
		return name + " (" + soThe + ")"
	}
	if donVi := props["don_vi_moi"]; donVi != "" {
		return name + " (" + donVi + ")"
	}
	if donVi := props["don_vi_cu"]; donVi != "" {
		return name + " (" + donVi + ")"
	}
	return name + " (không xác định)"
}

// ---------------------------------------------------------------------------
// Organization / Document canonicalization (port of normalize_org_name)
// ---------------------------------------------------------------------------

// vnParticles are lowercase particles that should NOT be title-cased.
var vnParticles = map[string]bool{
	"và": true, "của": true, "tại": true, "trong": true, "từ": true,
	"về": true, "theo": true, "với": true, "để": true, "có": true,
	"cho": true, "khi": true, "là": true, "trên": true, "đến": true,
	"qua": true, "sau": true, "thành": true, "ra": true, "vào": true,
	"tới": true, "bởi": true, "nếu": true, "mà": true, "hay": true,
	"hoặc": true,
}

// NormalizeOrgName is the canonical normalization for Organization/Document
// entity names. LLMs emit the same org with different capitalizations
// ("Sở Thông tin và Truyền thông" / "Sở Thông Tin và Truyền thông" /
// "SỞ THÔNG TIN VÀ TRUYỀN THÔNG") — all fold to one canonical form so the
// graph merge key converges.
func NormalizeOrgName(name string) string {
	name = strings.ReplaceAll(name, "#", "")
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return name
	}
	words := strings.Split(strings.ToLower(name), " ")
	for i, w := range words {
		if i == 0 || !vnParticles[w] {
			words[i] = capitalizeWord(w)
		}
	}
	return strings.Join(words, " ")
}

// capitalizeWord upper-cases the first rune of an already-lowercased word.
func capitalizeWord(w string) string {
	if w == "" {
		return w
	}
	r := []rune(w)
	r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	return string(r)
}

// ---------------------------------------------------------------------------
// Legal-document / article-ref classification (port of _is_* / _force_*)
// ---------------------------------------------------------------------------

// DocNumberPattern matches a Vietnamese legal document number (số hiệu)
// e.g. "29/2018/QH14", "01/2023/NĐ-CP". Two constraints beyond AIRAG's
// original:
//   - The suffix may contain the Vietnamese letter "Đ"/"đ" (NĐ-CP, QĐ-…),
//     which is NOT in the ASCII A-Z range — it MUST be in the class, else the
//     match truncates "85/2016/NĐ-CP" to "85/2016/N".
//   - The suffix must contain at least one letter, so a bare date inside a
//     Person composite key ("Nguyễn Văn B (01/01/1980)") is never mistaken
//     for a số hiệu.
var DocNumberPattern = regexp.MustCompile(`\d+/\d+/[A-Za-z0-9Đđ\-]*[A-Za-zĐđ][A-Za-z0-9Đđ\-]*`)

var legalDocPrefixRe = regexp.MustCompile(
	`(?i)^(Luật|Bộ luật|Nghị định|Thông tư|Quyết định|Chỉ thị|Nghị quyết|Hiến pháp|Pháp lệnh)\b`,
)

// Names opening with a structural-part keyword are Article/clause REFERENCES,
// not documents — even when they embed a số hiệu (e.g. "Khoản 4 Điều 3 Nghị
// định 53/2022/NĐ-CP"). They must be typed Article so the số-hiệu Document
// key never collapses a clause reference onto the document root.
var articleRefPrefixRe = regexp.MustCompile(
	`(?i)^(Điều|Khoản|Điểm|Mục|Chương|Phần)\b`,
)

// IsArticleRefName reports whether a name opens with a structural-part
// keyword (Điều/Khoản/Điểm/Mục/Chương/Phần).
func IsArticleRefName(name string) bool {
	return name != "" && articleRefPrefixRe.MatchString(strings.TrimSpace(name))
}

// IsLegalDocName reports whether a raw entity name denotes a legal document —
// it carries a số hiệu (e.g. "117/2025/QH15") or opens with a law-type prefix
// ("Luật", "Nghị định", …) — and is NOT a clause reference. Such entities
// MUST be typed Document regardless of how the LLM classified them.
func IsLegalDocName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || IsArticleRefName(name) {
		return false
	}
	return DocNumberPattern.MatchString(name) || legalDocPrefixRe.MatchString(name)
}

// ForceLegalType overrides an LLM-assigned type for legal-structure names:
// clause references (Điều/Khoản/…) → Article; laws/decrees (số hiệu or law
// prefix) → Document. Anything else keeps its original type.
func ForceLegalType(name, etype string) string {
	if IsArticleRefName(name) {
		return EntityTypeArticle
	}
	// Person composite keys ("Tên (ngày sinh/CCCD)") must never be
	// re-typed — the parenthesized qualifier is an identifier, not a doc ref.
	if c := CanonicalEntityType(etype); c != EntityTypeDocument &&
		c != EntityTypePerson && IsLegalDocName(name) {
		return EntityTypeDocument
	}
	if canonical := CanonicalEntityType(etype); canonical != "" {
		return canonical
	}
	return etype
}

// ---------------------------------------------------------------------------
// Entity merge key (port of normalize_entity_id)
// ---------------------------------------------------------------------------

// NormalizeEntityID returns the canonical merge key for an entity:
//   - Document → the embedded số hiệu when present (so "Nghị định 53/2022/NĐ-CP
//     (Chính phủ, 2022)", "53/2022/NĐ-CP" and "53/2022/nđ-cp" all merge onto
//     ONE node); otherwise NormalizeOrgName of the full name.
//   - Organization, Article, Location → NormalizeOrgName (case-folded canonical)
//   - Person → unchanged (Person uses the composite key for disambiguation)
//   - Task → whitespace-normalized only
func NormalizeEntityID(name, entityType string) string {
	name = strings.Join(strings.Fields(strings.TrimSpace(name)), " ")
	if entityType == EntityTypeDocument {
		if m := DocNumberPattern.FindString(name); m != "" {
			return NormalizeDocumentNumber(m)
		}
		return NormalizeOrgName(name)
	}
	switch entityType {
	case EntityTypeOrganization, EntityTypeArticle, EntityTypeLocation:
		return NormalizeOrgName(name)
	}
	return name
}

// NormalizeDocumentNumber canonicalizes a số hiệu: whitespace collapsed and
// the type suffix upper-cased so "53/2022/nđ-cp" and "53/2022/NĐ-CP" merge.
func NormalizeDocumentNumber(num string) string {
	return strings.ToUpper(strings.Join(strings.Fields(strings.TrimSpace(num)), " "))
}

// DocAliasBase is the canonical form for doc-identity comparison: normalize +
// drop a trailing "(issuer, year)" suffix.
func DocAliasBase(s string) string {
	return strings.TrimSpace(
		trailingParenRe.ReplaceAllString(NormalizeOrgName(s), ""))
}

var trailingParenRe = regexp.MustCompile(`\s*\([^)]+\)\s*$`)

// IsDocRootAlias reports whether name IS the current document itself — it
// matches the structured docName or the document title (EXACT match after
// normalization + dropping a trailing "(issuer, year)" suffix).
//
// Exact match only — never substring — so a sub-part of the title such as
// "Luật An ninh mạng" (a DIFFERENT document the decree details) is not
// swallowed.
func IsDocRootAlias(name, docName, docTitle string) bool {
	n := DocAliasBase(name)
	if n == "" {
		return false
	}
	for _, ref := range []string{docName, docTitle} {
		if ref != "" && n == DocAliasBase(ref) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Self-reference detection (port of _SELF_REF_PATTERNS)
// ---------------------------------------------------------------------------

var selfRefRe = regexp.MustCompile(
	`(?i)^(văn bản này|quyết định này|nghị định này|thông tư này|` +
		`luật này|bộ luật này|pháp lệnh này|nghị quyết này|chỉ thị này|quy định này)\s*$`,
)

// IsSelfReferenceName reports whether name is a bare self-reference to the
// enclosing document ("văn bản này", "Nghị định này", …).
func IsSelfReferenceName(name string) bool {
	return selfRefRe.MatchString(strings.TrimSpace(name))
}

// HasTrailingSelfRef reports whether name ends with the " này" coreference
// marker ("Luật này", "Quyết định này" — port of the `endswith(" này")`
// checks in _store_extraction).
func HasTrailingSelfRef(name string) bool {
	low := strings.ToLower(strings.TrimSpace(name))
	return low == "này" || strings.HasSuffix(low, " này")
}

// ---------------------------------------------------------------------------
// Generic / junk entity guard (port of _is_generic_or_junk)
// ---------------------------------------------------------------------------
// LLMs keep emitting a small, FINITE set of non-specific "organizations"
// ("Bộ", "Cơ quan nhà nước", "các … có liên quan"), form-template
// placeholders ("(tên đơn vị đề nghị)") and form names ("Mẫu số 02", "Tờ
// trình"), no matter how strongly the prompt forbids them. They get
// re-materialised as nodes from relation endpoints. CONSERVATIVE by design —
// a precise stoplist + tight patterns — so real (even long, comma-listed)
// names like "Doanh nghiệp viễn thông, doanh nghiệp cung cấp dịch vụ …" are
// NOT dropped.

var genericOrgExact = map[string]bool{
	"bộ": true, "các bộ": true, "bộ trưởng": true, "các bộ trưởng": true,
	"cơ quan": true, "các cơ quan": true, "cơ quan ngang bộ": true,
	"cơ quan thuộc chính phủ": true, "cơ quan nhà nước": true,
	"cơ quan, tổ chức nhà nước": true, "đơn vị": true, "các đơn vị": true,
	"doanh nghiệp": true, "các doanh nghiệp": true, "tổ chức": true,
	"các tổ chức": true, "tổ chức chính trị": true, "cá nhân": true,
	"tổ chức, cá nhân": true, "chủ quản hệ thống thông tin": true,
	"thủ trưởng cơ quan ngang bộ":        true,
	"thủ trưởng cơ quan thuộc chính phủ": true,
}

var genericOrgPat = regexp.MustCompile(
	`(?i)(có liên quan$|^các\s+(cơ quan|tổ chức|bộ|ngành|đơn vị|doanh nghiệp|cá nhân)\b)`,
)

var junkPlaceholderPat = regexp.MustCompile(
	`(?i)\(\s*(tên|chủ quản|đơn vị|cơ quan)\b[^)]*\)`,
)

// No trailing \b — RE2 word boundaries are ASCII-based and do not fire after
// a non-ASCII letter such as "ố".
var junkFormPat = regexp.MustCompile(
	`(?i)^\s*(mẫu số|tờ trình|biểu mẫu|đơn đề nghị)`,
)

// IsGenericOrJunkEntity reports whether name is a non-specific / template
// entity that must NOT become a graph node. Placeholder & form names are
// junk for ANY type; the generic-org stoplist/pattern only applies to
// Organization (or unknown) types so a clause ref or document with an
// incidental match is never dropped.
func IsGenericOrJunkEntity(name, entityType string) bool {
	n := strings.TrimSpace(name)
	if n == "" {
		return true
	}
	if junkPlaceholderPat.MatchString(n) || junkFormPat.MatchString(n) {
		return true
	}
	if entityType == "" || CanonicalEntityType(entityType) == EntityTypeOrganization {
		low := strings.ToLower(n)
		return genericOrgExact[low] || genericOrgPat.MatchString(low)
	}
	return false
}

// CleanEntityName strips markdown/list decoration an LLM may prepend
// ("# ", "- ", "* ") and collapses whitespace.
func CleanEntityName(name string) string {
	return strings.TrimSpace(
		regexp.MustCompile(`^[#*\-\s]+`).ReplaceAllString(strings.TrimSpace(name), ""))
}

// ---------------------------------------------------------------------------
// Document header / preamble parsing (port of parse_document_meta +
// extract_preamble + is_personnel_document + the CAN_CU regex pre-filter)
// ---------------------------------------------------------------------------

var preambleEndRe = regexp.MustCompile(
	`(?i)(QUYẾT ĐỊNH:|QUY ĐỊNH:|THÔNG TƯ:|CHỈ THỊ:|CỬ\s+ÔNG|ĐIỀU 1\b)`,
)

var ngayBanHanhRe = regexp.MustCompile(
	`(?i)ngày\s+(\d{1,2})\s+tháng\s+(\d{1,2})\s+năm\s+(\d{4})`,
)

var canCuRe = regexp.MustCompile(`(?i)Căn cứ\s+(.+?)(?:;|\n|$)`)

// PersonnelDocumentTriggers mark a document as a personnel decision
// (bổ nhiệm, điều động, khen thưởng…) — port of PERSON_DOCUMENT_TRIGGERS.
var PersonnelDocumentTriggers = []string{
	"bổ nhiệm", "bổ nhiêm", "miễn nhiệm", "điều động", "điều chuyển",
	"nghỉ hưu", "khen thưởng", "kỷ luật", "kỉ luật", "phê duyệt danh sách",
	"tiếp nhận và bổ nhiệm", "hưu trí", "thôi việc",
}

// LegalHeaderMeta is the deterministic document-metadata parse of the header
// region (port of parse_document_meta).
type LegalHeaderMeta struct {
	SoHieu        string // document number (số hiệu)
	NgayBanHanh   string // DD/MM/YYYY
	CoQuanBanHanh string // first all-caps line near the top
	DocumentName  string // human-readable document name
}

// ParseLegalHeaderMeta extracts document metadata from the top of a legal
// document. Only the first ~2000 chars are scanned (the header area).
func ParseLegalHeaderMeta(text string) LegalHeaderMeta {
	header := text
	if len(header) > 2000 {
		header = header[:2000]
	}
	var meta LegalHeaderMeta
	meta.SoHieu = RecoverDocumentNumber(header)
	if m := ngayBanHanhRe.FindStringSubmatch(header); m != nil {
		meta.NgayBanHanh = fmt.Sprintf("%02d/%02d/%s",
			atoiSafe(m[1]), atoiSafe(m[2]), m[3])
	}
	for _, line := range strings.Split(header, "\n") {
		line = strings.TrimSpace(line)
		if len([]rune(line)) > 5 && line == strings.ToUpper(line) &&
			strings.IndexFunc(line, func(r rune) bool {
				return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
					r > 127 // Vietnamese letters
			}) >= 0 {
			meta.CoQuanBanHanh = line
			break
		}
	}
	switch {
	case meta.SoHieu != "":
		meta.DocumentName = meta.SoHieu
	case meta.CoQuanBanHanh != "":
		if len(meta.CoQuanBanHanh) > 80 {
			meta.DocumentName = meta.CoQuanBanHanh[:80]
		} else {
			meta.DocumentName = meta.CoQuanBanHanh
		}
	default:
		meta.DocumentName = "Văn bản không xác định"
	}
	return meta
}

// ExtractPreamble returns the preamble block (before QUYẾT ĐỊNH: / ĐIỀU 1).
func ExtractPreamble(text string) string {
	if m := preambleEndRe.FindStringIndex(text); m != nil {
		return strings.TrimSpace(text[:m[0]])
	}
	if len(text) > 1500 {
		return strings.TrimSpace(text[:1500])
	}
	return strings.TrimSpace(text)
}

// IsPersonnelDocument reports whether the document is a personnel decision —
// trigger keywords in the first 500 chars.
func IsPersonnelDocument(text string) bool {
	head := text
	if len([]rune(head)) > 500 {
		head = string([]rune(head)[:500])
	}
	head = strings.ToLower(head)
	for _, trigger := range PersonnelDocumentTriggers {
		if strings.Contains(head, trigger) {
			return true
		}
	}
	return false
}

// ExtractPreambleCanCu extracts the legal-basis citation list from a preamble
// using the deterministic regex pass (AIRAG tries this regex first and only
// falls back to an LLM when it misses — the deterministic layer keeps the
// regex part only).
func ExtractPreambleCanCu(preamble string) []string {
	var out []string
	for _, m := range canCuRe.FindAllStringSubmatch(preamble, -1) {
		c := strings.TrimRight(strings.TrimSpace(m[1]), ";,. ")
		if len([]rune(c)) > 5 {
			out = append(out, c)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Document-level context for legal graph extraction
// ---------------------------------------------------------------------------

// LegalDocContext carries the document-level identity that every per-chunk
// extraction task needs: the canonical root-node name, the metadata injected
// into the extraction prompt, and the preamble CAN_CU list recovered at
// enqueue time (so workers never need a second read).
type LegalDocContext struct {
	// IsLegal marks the document as Vietnamese legal/administrative —
	// chunk tasks use the legal extraction path only when true.
	IsLegal bool `json:"is_legal"`
	// IsPersonnel selects the personnel-decision prompt variant.
	IsPersonnel bool `json:"is_personnel,omitempty"`
	// DocumentNumber is the document's own số hiệu ("53/2022/NĐ-CP").
	DocumentNumber string `json:"document_number,omitempty"`
	// Title is the knowledge title (display + doc-root alias target).
	Title string `json:"title,omitempty"`
	// IssuingAgency is the cơ quan ban hành (used for BAN_HANH_BOI).
	IssuingAgency string `json:"issuing_agency,omitempty"`
	// PublishedDate is the issue date DD/MM/YYYY.
	PublishedDate string `json:"published_date,omitempty"`
	// RootName is the canonical name of the document root node — số hiệu
	// when known, else the normalized title. Self-references fold onto it.
	RootName string `json:"root_name,omitempty"`
	// CanCu lists the preamble legal-basis citations (deterministic regex
	// extraction); injected as CAN_CU edges by the header chunk's task.
	CanCu []string `json:"can_cu,omitempty"`
}

// BuildLegalDocContext computes the document-level legal context once, at
// enqueue time, from the header chunk's text plus the knowledge title /
// filename. headerText should be the content of the chunk whose StartAt == 0
// ("" when unavailable); hasLegalStructureHint is true when any chunk of the
// document already carries legal subdivision metadata from the chunker.
func BuildLegalDocContext(headerText, title, fileName string, hasLegalStructureHint bool) *LegalDocContext {
	meta := ParseLegalHeaderMeta(headerText)

	docNum := meta.SoHieu
	agency := meta.CoQuanBanHanh
	date := meta.NgayBanHanh

	// Fall back to the knowledge title / file name when the header parse
	// found nothing — titles in legal KBs are typically "Nghị định 13/2024/…"
	// or at least "Nghị định 13 năm 2024".
	ref := ParseReference(title)
	if docNum == "" {
		docNum = ref.DocumentNumber
	}
	if agency == "" {
		agency = ref.IssuingAgencyText
	}
	if date == "" && ref.Year != "" {
		date = ref.Year
	}

	isLegal := hasLegalStructureHint ||
		HasLegalStructure(headerText) ||
		docNum != "" ||
		(ref.DocTypeSlug != "" && ref.NumberRaw != "") ||
		IsPersonnelDocument(headerText) ||
		IsPersonnelDocument(title)

	rootName := ""
	switch {
	case docNum != "":
		rootName = NormalizeDocumentNumber(docNum)
	case title != "":
		rootName = NormalizeOrgName(title)
	case fileName != "":
		rootName = NormalizeOrgName(strings.TrimSuffix(fileName, fileExt(fileName)))
	}

	return &LegalDocContext{
		IsLegal:        isLegal,
		IsPersonnel:    IsPersonnelDocument(headerText) || IsPersonnelDocument(title),
		DocumentNumber: docNum,
		Title:          title,
		IssuingAgency:  agency,
		PublishedDate:  date,
		RootName:       rootName,
		CanCu:          ExtractPreambleCanCu(ExtractPreamble(headerText)),
	}
}

// fileExt returns the lower-cased extension of a file name (".pdf"), or "".
func fileExt(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i:]
	}
	return ""
}
