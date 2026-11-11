package tools

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// Legal basis check ("Rà soát căn cứ pháp lý") of the document being
// drafted: every document it cites (FindCitations) is looked up in the legal
// catalogue and given a status, without a model. The assistant points out:
// the report lists the citations, the editor may underline them, nothing in
// the document changes. A citation the catalogue does not hold is "not
// found" (chưa rõ), never expired; a citation that may mean several
// catalogue documents is "ambiguous" with its candidates, never one picked
// for the user — also when only one candidate is known but the citation
// does not identify it (a year-less number without its issue date).

// LegalBasisStatus is the status of one cited document.
type LegalBasisStatus string

const (
	LegalBasisInForce         LegalBasisStatus = "in_force"
	LegalBasisAmended         LegalBasisStatus = "amended" // in force, amended by later documents
	LegalBasisPartlyExpired   LegalBasisStatus = "partly_expired"
	LegalBasisExpired         LegalBasisStatus = "expired"
	LegalBasisSuspended       LegalBasisStatus = "suspended"
	LegalBasisNotYetEffective LegalBasisStatus = "not_yet_effective"
	// LegalBasisNeedsReview: held in force (or partly suspended) while a
	// document in force repeals it or ends it in part: which provisions
	// still apply must be checked by hand.
	LegalBasisNeedsReview LegalBasisStatus = "needs_review"
	// LegalBasisUnknown: the catalogue holds the document but states no
	// validity ("Chưa xác định") and nothing in its dates or links decides.
	LegalBasisUnknown   LegalBasisStatus = "unknown"
	LegalBasisAmbiguous LegalBasisStatus = "ambiguous"
	LegalBasisNotFound  LegalBasisStatus = "not_found"
)

// legalBasisOrder is the report order: what needs the drafter's attention
// first.
var legalBasisOrder = map[LegalBasisStatus]int{
	LegalBasisExpired: 0, LegalBasisSuspended: 1, LegalBasisPartlyExpired: 2, LegalBasisNeedsReview: 3,
	LegalBasisNotYetEffective: 4, LegalBasisAmbiguous: 5, LegalBasisAmended: 6, LegalBasisUnknown: 7,
	LegalBasisNotFound: 8, LegalBasisInForce: 9,
}

// LegalBasisMarkedByDefault reports whether the report's own underlines
// (DocumentOps, the "underline all" of a client) cover a status: the
// citations whose validity is in question. Ambiguous, amended, unknown and
// not-found citations are underlined one finding at a time, on request.
func LegalBasisMarkedByDefault(s LegalBasisStatus) bool {
	switch s {
	case LegalBasisExpired, LegalBasisSuspended, LegalBasisPartlyExpired, LegalBasisNeedsReview, LegalBasisNotYetEffective:
		return true
	}
	return false
}

// Why a citation is ambiguous.
const (
	// several catalogue documents carry the number (and date, if written)
	LegalBasisAmbiguitySharedNumber = "shared_number"
	// a type and a bare number, maybe a year: "Nghị định 68", "NĐ 68/2026"
	LegalBasisAmbiguityIncompleteNumber = "incomplete_number"
	// a year-less number ("1461/QĐ-TTg", "18-NQ/TW") with no issue date
	// written after it: such numbers restart every year
	LegalBasisAmbiguityMissingDate = "missing_date"
	// a year-less number whose written issue date no catalogue document of
	// that number has
	LegalBasisAmbiguityDateMismatch = "date_mismatch"
	// a law named with a year that several catalogue documents fit
	LegalBasisAmbiguitySharedName = "shared_name"
	// a law name the catalogue only matches by a shorter title: a guess
	LegalBasisAmbiguityNameGuess = "name_guess"
)

// Relations of a related document, seen from the cited one.
const (
	LegalBasisReplacedBy      = "replaced_by"
	LegalBasisRepealedBy      = "repealed_by" // vbpl files partial repeals here too
	LegalBasisExpiredBy       = "expired_by"
	LegalBasisPartlyExpiredBy = "partly_expired_by"
	LegalBasisSuspendedBy     = "suspended_by"
	LegalBasisAmendedBy       = "amended_by"
)

var legalBasisRelations = []struct {
	rel  types.LegalRelation
	name string
}{
	{types.LegalRelationReplaces, LegalBasisReplacedBy},
	{types.LegalRelationRepeals, LegalBasisRepealedBy},
	{types.LegalRelationExpires, LegalBasisExpiredBy},
	{types.LegalRelationPartiallyExpires, LegalBasisPartlyExpiredBy},
	{types.LegalRelationSuspends, LegalBasisSuspendedBy},
	{types.LegalRelationAmends, LegalBasisAmendedBy},
}

var legalBasisIncoming = func() []types.LegalRelation {
	out := make([]types.LegalRelation, len(legalBasisRelations))
	for i, r := range legalBasisRelations {
		out[i] = r.rel
	}
	return out
}()

const (
	// legalBasisMaxFindings bounds the distinct citations looked up.
	legalBasisMaxFindings = 120
	// legalBasisMaxCandidates bounds the candidates listed for an
	// ambiguous citation (Total says how many there are).
	legalBasisMaxCandidates = 5
	// legalBasisPrefixScan is how many rows of one type a partial number is
	// matched against; more sets CandidatesMore.
	legalBasisPrefixScan = 200
	// legalBasisMaxRelated bounds the related documents of one relation.
	legalBasisMaxRelated = 5
	// legalBasisMaxMarks bounds the underlines of one report.
	legalBasisMaxMarks = 200
)

// LegalBasisDocument is a catalogue document as the report shows it. Dates
// are YYYY-MM-DD ("" when unknown).
type LegalBasisDocument struct {
	ID             string `json:"id"`
	DocumentNumber string `json:"document_number"`
	DocType        string `json:"doc_type"`
	Title          string `json:"title"`
	Issuer         string `json:"issuer,omitempty"`
	IssuedOn       string `json:"issued_on,omitempty"`
	EffectiveOn    string `json:"effective_on,omitempty"`
	ExpiredOn      string `json:"expired_on,omitempty"`
	// CatalogStatus is the catalogue's own wording ("Còn hiệu lực", …).
	CatalogStatus string `json:"catalog_status"`
	// Status is this document's status by the same rules as a finding's
	// (set on candidates and related documents too).
	Status LegalBasisStatus `json:"status"`
	// Unverified: a row from a legal feed no SuperAdmin has checked yet.
	Unverified bool `json:"unverified,omitempty"`
}

// LegalBasisRelated is a document acting on the cited one.
type LegalBasisRelated struct {
	Relation string `json:"relation"`
	LegalBasisDocument
}

// LegalBasisPosition is one place a citation is written: the paragraph (as
// docxedit numbers them), rune offsets in its text, the text as written and
// the editor address of that text (anchor + 1-based occurrence in the
// paragraph), the same contract as a mark op.
type LegalBasisPosition struct {
	Paragraph      int       `json:"paragraph"`
	Start          int       `json:"start"`
	End            int       `json:"end"`
	Text           string    `json:"text"`
	Anchor         *OpAnchor `json:"anchor"`
	TextOccurrence int       `json:"text_occurrence"`
}

// LegalBasisFinding is one cited document with all the places it is cited.
type LegalBasisFinding struct {
	// Key identifies the finding within a report.
	Key string `json:"key"`
	// Citation is the normalised reference: "30/2020/NĐ-CP", "Luật Đất đai
	// 2013", "Nghị định 68", "1461/QĐ-TTg ngày 20/11/2023".
	Citation string `json:"citation"`
	// Kind is the FindCitations kind (full, admin, party, standard,
	// partial, name).
	Kind    string           `json:"kind"`
	DocType string           `json:"doc_type,omitempty"`
	Status  LegalBasisStatus `json:"status"`
	// IssuedOn is the issue date written after a year-less number
	// (YYYY-MM-DD), the part of its identity the number lacks.
	IssuedOn string `json:"issued_on,omitempty"`
	// Document is the catalogue document, nil when ambiguous or not found.
	Document *LegalBasisDocument `json:"document,omitempty"`
	// Ambiguity says why an ambiguous citation is (LegalBasisAmbiguity*).
	Ambiguity string `json:"ambiguity,omitempty"`
	// Candidates of an ambiguous citation (newest first), at most
	// legalBasisMaxCandidates of CandidatesTotal; CandidatesMore: the
	// catalogue holds more than were counted.
	Candidates      []LegalBasisDocument `json:"candidates,omitempty"`
	CandidatesTotal int                  `json:"candidates_total,omitempty"`
	CandidatesMore  bool                 `json:"candidates_more,omitempty"`
	// Related: documents replacing, repealing, ending, suspending or
	// amending Document (newest first per relation).
	Related []LegalBasisRelated `json:"related,omitempty"`
	// DerivedFrom is set when Status is expired although the catalogue
	// status says otherwise: the number of the document already in force
	// that replaced or ended this one (check_legal_status derivedExpiry).
	DerivedFrom      string `json:"derived_from,omitempty"`
	DerivedExpiredOn string `json:"derived_expired_on,omitempty"`
	// MaybeExpired: the catalogue holds the document partly expired, but a
	// document in force replaces or ends it: perhaps wholly.
	MaybeExpired bool `json:"maybe_expired,omitempty"`
	// ReviewBy: the documents in force that repeal or partly end a
	// document the catalogue holds in force (Status needs_review).
	ReviewBy []string `json:"review_by,omitempty"`
	// PartialSuspension: the catalogue says "Ngưng hiệu lực một phần".
	PartialSuspension bool `json:"partial_suspension,omitempty"`
	// Unsuitable: the portal flags the document "Không còn phù hợp" (no
	// longer suitable): needs review, not expired.
	Unsuitable bool `json:"unsuitable,omitempty"`
	// StaleStatus: the catalogue says "Chưa có hiệu lực" although the
	// effective date has passed; the date was trusted.
	StaleStatus bool `json:"stale_status,omitempty"`
	// Incomplete: the citation lacks part of its identity: the year or
	// code of its number ("Nghị định 68", "NĐ 68/2026") or the issue date
	// of a year-less number.
	Incomplete bool `json:"incomplete,omitempty"`
	// Note says the status in one Vietnamese sentence (for clients that
	// show text, e.g. the Word add-in).
	Note      string               `json:"note"`
	Positions []LegalBasisPosition `json:"positions"`
}

// LegalBasisReport is the result of one legal basis check.
type LegalBasisReport struct {
	DocumentID string              `json:"document_id"`
	Revision   int                 `json:"revision"`
	CheckedAt  time.Time           `json:"checked_at"`
	Findings   []LegalBasisFinding `json:"findings"`
	// Summary counts findings by status.
	Summary map[LegalBasisStatus]int `json:"summary"`
	// DocumentOps underline every place of the findings whose validity is
	// in question (LegalBasisMarkedByDefault), at most legalBasisMaxMarks;
	// the client applies them only when the user asks.
	DocumentOps []DocumentOp `json:"document_ops"`
	// MarksTruncated: more places than legalBasisMaxMarks.
	MarksTruncated bool `json:"marks_truncated,omitempty"`
	// Skipped counts citations deliberately not checked: laws named
	// without a year (they mean the law in force, nothing to check), the
	// document's own number ("Số: …") and non-normative paperwork numbers
	// the catalogue never holds ("45/KH-UBND", "12/2025/TTr-STP").
	Skipped LegalBasisSkipped `json:"skipped"`
	// Truncated: more distinct citations than legalBasisMaxFindings.
	Truncated bool `json:"truncated,omitempty"`
	// Unsaved: the editor's latest changes could not be saved first; the
	// check read the last saved version.
	Unsaved bool `json:"unsaved,omitempty"`
}

// LegalBasisSkipped counts the citations a check left out, by reason.
type LegalBasisSkipped struct {
	NamesWithoutYear int `json:"names_without_year"`
	OwnNumber        int `json:"own_number"`
	Paperwork        int `json:"paperwork"`
}

// LegalBasisCatalog is the part of the catalogue repository the check
// reads (interfaces.LegalCatalogRepository has it).
type LegalBasisCatalog interface {
	FindByNumberKey(ctx context.Context, key string, limit int) ([]*types.LegalDocument, error)
	// FindByNumberPrefixTypes is FindByNumberPrefix limited to documents
	// of the given types (doc_type, case-insensitive).
	FindByNumberPrefixTypes(ctx context.Context, key string, docTypes []string, limit int) ([]*types.LegalDocument, error)
	ListLinks(ctx context.Context, id string, outgoing, incoming []types.LegalRelation) ([]types.LegalRelationLink, error)
}

// LegalNameResolution is what a LegalNameResolver made of a law named by
// title and year: the one document it names (Chosen), or the candidates
// when several may be meant; neither when the catalogue has none. Guess:
// the candidates match only a shorter form of the name.
type LegalNameResolution struct {
	Chosen     *types.LegalDocument
	Candidates []*types.LegalDocument
	Guess      bool
}

// LegalNameResolver resolves a law named with a year ("Luật Đất đai",
// "2013") in the catalogue as of today.
type LegalNameResolver func(ctx context.Context, name, year string) (LegalNameResolution, error)

// ReadLegalBasisParagraphs returns the paragraph texts of a workspace
// document (the stored file, as the format check reads it).
func ReadLegalBasisParagraphs(ctx context.Context, src DocumentWorkspaceSource, sessionID, documentID string) ([]string, *types.DocumentWorkspace, error) {
	content, ws, err := readWorkspaceDocument(ctx, src, sessionID, documentID)
	if err != nil {
		return nil, nil, err
	}
	doc, err := docxedit.Open(content)
	if err != nil {
		return nil, nil, err
	}
	paras := doc.Paragraphs()
	texts := make([]string, len(paras))
	for i, p := range paras {
		texts[i] = p.Text
	}
	return texts, ws, nil
}

// ownNumberRe is the "Số:" a document writes its own number after.
var ownNumberRe = regexp.MustCompile(`(?i)^\s*số\s*[:.]?\s*$`)

// The issue date written after a year-less number: "ngày 20/11/2023",
// "ngày 20-11-2023", "ngày 20 tháng 11 năm 2023", optionally introduced by
// "(", "ký" / "ban hành" or "của <cơ quan>" — within issueDateWindow runes
// of the number and before the next citation. Spaces include the no-break
// space Word writes. A date before the number is not read.
const issueDateWindow = 60

var (
	issueDateRe = regexp.MustCompile(`(?i)ngày[\s\p{Zs}]+(?:(\d{1,2})[\s\p{Zs}]*[/.\-][\s\p{Zs}]*(\d{1,2})[\s\p{Zs}]*[/.\-][\s\p{Zs}]*((?:19|20)\d\d)|(\d{1,2})[\s\p{Zs}]+tháng[\s\p{Zs}]+(\d{1,2})[\s\p{Zs}]+năm[\s\p{Zs}]+((?:19|20)\d\d))`)
	// what may stand between the number and "ngày": nothing, "(", "ký" /
	// "ban hành", or "của <cơ quan>" — an agency phrase without a comma or
	// semicolon inside (one may end it)
	issueDateLeadRe = regexp.MustCompile(`(?i)^[\s\p{Zs},(]*(?:(?:được[\s\p{Zs}]+)?(?:ký|ban[\s\p{Zs}]+hành)[\s\p{Zs}]+|của[\s\p{Zs}]+[^,;.()\n]{1,50}?[\s\p{Zs}]*,?[\s\p{Zs}]+)?$`)
	// a year written right after an undated number: "năm 2023", "(2023)"
	undatedYearAfterRe = regexp.MustCompile(`(?i)^[\s\p{Zs},(]*(?:năm[\s\p{Zs}]+)?((?:19|20)\d\d)(?:[^\p{N}]|$)`)
)

// issueDateLeadWords are words that, in the text between a number and a
// date, show the date is not its issue date ("có hiệu lực từ ngày …",
// "hết hiệu lực ngày …", "biên bản họp ngày …", "căn cứ … ngày …").
var issueDateLeadWords = []string{"hiệu lực", "từ", "họp", "về", "đến", "biên bản", "căn cứ"}

// issueDateLeadOK reports whether lead (the text from the number to
// "ngày") introduces the number's issue date.
func issueDateLeadOK(lead string) bool {
	if !issueDateLeadRe.MatchString(lead) {
		return false
	}
	words := " " + strings.Join(strings.FieldsFunc(strings.ToLower(lead), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ") + " "
	for _, w := range issueDateLeadWords {
		if strings.Contains(words, " "+w+" ") {
			return false
		}
	}
	return true
}

// yearAfter is a year written right after an undated year-less number
// ("15/QĐ-UBND năm 2023"), "" when none.
func yearAfter(rest string) string {
	if m := undatedYearAfterRe.FindStringSubmatch(rest); m != nil {
		return m[1]
	}
	return ""
}

// issueDateAfter is the issue date (YYYY-MM-DD) written after a citation
// in rest (the text from the citation's end to the next citation), ""
// when none (or not a real date).
func issueDateAfter(rest string) string {
	loc := issueDateRe.FindStringSubmatchIndex(rest)
	if loc == nil || utf8.RuneCountInString(rest[:loc[0]]) > issueDateWindow || !issueDateLeadOK(rest[:loc[0]]) {
		return ""
	}
	group := func(i int) string {
		if loc[2*i] < 0 {
			return ""
		}
		return rest[loc[2*i]:loc[2*i+1]]
	}
	d, mo, y := group(1), group(2), group(3)
	if d == "" {
		d, mo, y = group(4), group(5), group(6)
	}
	day, _ := strconv.Atoi(d)
	month, _ := strconv.Atoi(mo)
	year, _ := strconv.Atoi(y)
	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if t.Day() != day || int(t.Month()) != month {
		return ""
	}
	return t.Format("2006-01-02")
}

// yearlessKind reports a citation kind whose number has no year: such
// numbers restart every year ("1461/QĐ-TTg", "18-NQ/TW").
func yearlessKind(k vietnamese_legal.CitationKind) bool {
	return k == vietnamese_legal.CitationAdmin || k == vietnamese_legal.CitationParty
}

// legalBasisGroup collects the places of one distinct citation.
type legalBasisGroup struct {
	key       string
	cit       vietnamese_legal.Citation
	issued    string // issue date after a year-less number
	year      string // or only a year after it ("năm 2023")
	positions []LegalBasisPosition
}

// paragraphAnchors returns the editor anchor of every paragraph, the
// normalised text computed once (OpAnchor occurrences counted in one pass).
func paragraphAnchors(paras []string) []*OpAnchor {
	out := make([]*OpAnchor, len(paras))
	seen := map[string]int{}
	for i, p := range paras {
		t := anchorText(p)
		seen[t]++
		out[i] = &OpAnchor{Text: t, Occurrence: seen[t]}
	}
	return out
}

// CheckLegalBasis checks the citations of paras (a document's paragraph
// texts) against the catalogue as of now. names may be nil: laws named
// with a year are then not checked either.
func CheckLegalBasis(ctx context.Context, paras []string, catalog LegalBasisCatalog, names LegalNameResolver, now time.Time) (*LegalBasisReport, error) {
	report := &LegalBasisReport{CheckedAt: now, Summary: map[LegalBasisStatus]int{}}
	anchors := paragraphAnchors(paras)
	// first pass: every citation to check, with the issue date written
	// after a year-less number
	type mention struct {
		key    string
		issued string
		year   string // a year written right after an undated number
		cit    vietnamese_legal.Citation
		pos    LegalBasisPosition
	}
	var mentions []mention
	dates := map[string]map[string]bool{} // year-less key → distinct dates
	for pi, text := range paras {
		if strings.TrimSpace(text) == "" {
			continue
		}
		runes := []rune(text)
		cits := vietnamese_legal.FindCitations(text)
		for ci, c := range cits {
			if c.Kind == vietnamese_legal.CitationName && names == nil {
				continue
			}
			if c.Kind != vietnamese_legal.CitationName && ownNumberRe.MatchString(string(runes[:c.Start])) {
				report.Skipped.OwnNumber++
				continue
			}
			key, skip := legalBasisKey(c)
			switch skip {
			case "name":
				report.Skipped.NamesWithoutYear++
				continue
			case "drop":
				continue
			}
			m := mention{key: key, cit: c, pos: LegalBasisPosition{
				Paragraph: pi, Start: c.Start, End: c.End, Text: c.Raw, Anchor: anchors[pi],
				TextOccurrence: 1 + strings.Count(string(runes[:c.Start]), c.Raw),
			}}
			if yearlessKind(c.Kind) {
				next := len(runes)
				if ci+1 < len(cits) {
					next = cits[ci+1].Start
				}
				rest := string(runes[c.End:max(c.End, next)])
				if m.issued = issueDateAfter(rest); m.issued != "" {
					if dates[key] == nil {
						dates[key] = map[string]bool{}
					}
					dates[key][m.issued] = true
				} else {
					m.year = yearAfter(rest)
				}
			}
			mentions = append(mentions, m)
		}
	}
	// second pass: group. An undated year-less mention is the dated one
	// only when the draft dates that number once; with several dates it
	// stays its own (missing date) finding.
	var groups []*legalBasisGroup
	byKey := map[string]*legalBasisGroup{}
	for _, m := range mentions {
		key, issued := m.key, m.issued
		if yearlessKind(m.cit.Kind) && issued == "" && len(dates[key]) == 1 {
			for d := range dates[key] {
				// a year written with the mention must be the date's
				if m.year == "" || strings.HasPrefix(d, m.year+"-") {
					issued = d
				}
			}
		}
		year := ""
		switch {
		case issued != "":
			key += "|" + issued
		case m.year != "":
			year = m.year
			key += "|year:" + year
		}
		g := byKey[key]
		if g == nil {
			if len(groups) >= legalBasisMaxFindings {
				report.Truncated = true
				continue
			}
			g = &legalBasisGroup{key: key, cit: m.cit, issued: issued, year: year}
			byKey[key] = g
			groups = append(groups, g)
		}
		g.positions = append(g.positions, m.pos)
	}
	c := &legalBasisChecker{catalog: catalog, names: names, now: now, docs: map[string]*legalClassified{}}
	var findings []LegalBasisFinding
	for _, g := range groups {
		f, err := c.finding(ctx, g)
		if err != nil {
			return nil, err
		}
		if f == nil {
			report.Skipped.Paperwork++
			continue
		}
		findings = append(findings, *f)
	}
	findings = mergeLegalBasisFindings(findings)
	sort.SliceStable(findings, func(i, j int) bool {
		return legalBasisOrder[findings[i].Status] < legalBasisOrder[findings[j].Status]
	})
	report.DocumentOps = []DocumentOp{}
	for _, f := range findings {
		report.Summary[f.Status]++
		if !LegalBasisMarkedByDefault(f.Status) {
			continue
		}
		for _, p := range f.Positions {
			if len(report.DocumentOps) >= legalBasisMaxMarks {
				report.MarksTruncated = true
				break
			}
			report.DocumentOps = append(report.DocumentOps, p.MarkOp())
		}
	}
	if findings == nil {
		findings = []LegalBasisFinding{}
	}
	report.Findings = findings
	return report, nil
}

// MarkOp is the underline of the position.
func (p LegalBasisPosition) MarkOp() DocumentOp {
	op := DocumentOp{Op: OpMark, Anchor: p.Anchor, Text: p.Text, Style: "underline"}
	if p.TextOccurrence > 1 {
		op.TextOccurrence = p.TextOccurrence
	}
	return op
}

// legalBasisKey is the dedupe key of a citation; skip is "name" for a law
// named without a year and "drop" for one never checked (a law named
// inside an amending law's title, a partial reference without a type).
func legalBasisKey(c vietnamese_legal.Citation) (key, skip string) {
	switch c.Kind {
	case vietnamese_legal.CitationName:
		if c.Role == vietnamese_legal.CitationRoleInAmendingTitle {
			return "", "drop"
		}
		if c.Year == "" {
			return "", "name"
		}
		return "name:" + strings.ToLower(strings.Join(strings.Fields(c.Name), " ")) + "|" + c.Year, ""
	case vietnamese_legal.CitationPartial:
		if c.DocType == "" {
			return "", "drop"
		}
		return "partial:" + vietnamese_legal.DocTypeClass(c.DocType) + "|" + c.Number, ""
	}
	if c.Key == "" {
		return "", "drop"
	}
	return "number:" + c.Key, ""
}

// legalClassified is a classified catalogue document.
type legalClassified struct {
	doc     LegalBasisDocument
	class   legalClassification
	related []LegalBasisRelated
}

type legalBasisChecker struct {
	catalog LegalBasisCatalog
	names   LegalNameResolver
	now     time.Time
	// docs caches classified documents by id (a candidate or a related
	// document is often cited too)
	docs map[string]*legalClassified
}

// legalDocTypeVariants are the catalogue doc types of one DocTypeClass.
func legalDocTypeVariants(docType string) []string {
	switch vietnamese_legal.DocTypeClass(docType) {
	case "luật":
		return []string{"Luật", "Bộ luật"}
	case "thông tư":
		return []string{"Thông tư", "Thông tư liên tịch"}
	case "nghị quyết":
		return []string{"Nghị quyết", "Nghị quyết liên tịch"}
	}
	return []string{docType}
}

// finding looks one citation up and classifies it; nil for paperwork the
// catalogue never holds.
func (c *legalBasisChecker) finding(ctx context.Context, g *legalBasisGroup) (*LegalBasisFinding, error) {
	cit := g.cit
	f := &LegalBasisFinding{Key: g.key, Kind: string(cit.Kind), DocType: cit.DocType, Positions: g.positions}
	switch cit.Kind {
	case vietnamese_legal.CitationName:
		f.Citation = strings.TrimSpace(legalNameOnly(cit) + " " + cit.Year)
		res, err := c.names(ctx, legalNameOnly(cit), cit.Year)
		if err != nil {
			return nil, err
		}
		if res.Chosen != nil {
			return f, c.resolve(ctx, f, res.Chosen)
		}
		f.Ambiguity = LegalBasisAmbiguitySharedName
		if res.Guess {
			f.Ambiguity = LegalBasisAmbiguityNameGuess
		}
		return f, c.candidates(ctx, f, res.Candidates)
	case vietnamese_legal.CitationPartial:
		f.Citation = strings.TrimSpace(cit.DocType + " " + cit.Number)
		f.Incomplete = true
		docs, err := c.catalog.FindByNumberPrefixTypes(ctx, vietnamese_legal.CatalogNumberKey(cit.Number),
			legalDocTypeVariants(cit.DocType), legalBasisPrefixScan+1)
		if err != nil {
			return nil, err
		}
		if len(docs) > legalBasisPrefixScan {
			docs, f.CandidatesMore = docs[:legalBasisPrefixScan], true
		}
		// with its year the number is one document unless the catalogue
		// holds several; without it ("Nghị định 68") it never is
		if cit.Year != "" && !f.CandidatesMore && legalDistinctNumbers(docs) == 1 {
			return f, c.resolve(ctx, f, docs[0])
		}
		f.Ambiguity = LegalBasisAmbiguityIncompleteNumber
		return f, c.candidates(ctx, f, docs)
	}
	f.Citation = cit.Number
	docs, err := c.catalog.FindByNumberKey(ctx, cit.Key, legalBasisPrefixScan)
	if err != nil {
		return nil, err
	}
	// FindByNumberKey falls back to a prefix match: keep the exact key
	exact := docs[:0:0]
	for _, d := range docs {
		if d.NumberKey == cit.Key || vietnamese_legal.CatalogNumberKey(d.DocumentNumber) == cit.Key {
			exact = append(exact, d)
		}
	}
	if len(exact) == 0 && !legalBasisNormative(cit) {
		return nil, nil
	}
	if yearlessKind(cit.Kind) {
		return f, c.yearless(ctx, f, g.issued, g.year, exact)
	}
	if len(exact) == 1 {
		return f, c.resolve(ctx, f, exact[0])
	}
	f.Ambiguity = LegalBasisAmbiguitySharedNumber
	return f, c.candidates(ctx, f, exact)
}

// yearless resolves a number without a year by the issue date written
// after it (year: only a year was written, which narrows the candidates)
// after it, the exact day of the catalogue's issued_on: a number like
// "1461/QĐ-TTg" restarts every year, so a lone catalogue row of that
// number is not the cited document unless the dates agree. Without a
// date, or with one no row has, the citation stays ambiguous (the year
// alone is not taken: two rows of a number in one year happen, and a
// wrong day more likely names a document the catalogue lacks).
func (c *legalBasisChecker) yearless(ctx context.Context, f *LegalBasisFinding, issued, year string, docs []*types.LegalDocument) error {
	if issued == "" {
		f.Incomplete = true
		f.Ambiguity = LegalBasisAmbiguityMissingDate
		if year != "" {
			// a written year narrows the candidates, never picks one
			f.Citation += " năm " + year
			var same []*types.LegalDocument
			for _, d := range docs {
				if d.IssuedOn != nil && d.IssuedOn.Format("2006") == year {
					same = append(same, d)
				}
			}
			if len(same) > 0 {
				docs = same
			}
		}
		return c.candidates(ctx, f, docs)
	}
	f.IssuedOn = issued
	f.Citation += " ngày " + legalBasisDay(issued)
	var same []*types.LegalDocument
	for _, d := range docs {
		if d.IssuedOn != nil && d.IssuedOn.Format("2006-01-02") == issued {
			same = append(same, d)
		}
	}
	switch {
	case len(same) == 1:
		return c.resolve(ctx, f, same[0])
	case len(same) > 1:
		f.Ambiguity = LegalBasisAmbiguitySharedNumber
		return c.candidates(ctx, f, same)
	}
	f.Ambiguity = LegalBasisAmbiguityDateMismatch
	return c.candidates(ctx, f, docs)
}

// legalNameOnly is the law's name without the year the reader appended.
func legalNameOnly(c vietnamese_legal.Citation) string {
	name := strings.Join(strings.Fields(c.Name), " ")
	if c.Year != "" {
		name = strings.TrimSpace(strings.TrimSuffix(name, c.Year))
		name = strings.TrimSpace(strings.TrimSuffix(name, " năm"))
	}
	return name
}

// legalBasisNormative reports whether a numbered citation the catalogue
// lacks is still worth reporting: a normative type code, a year-less
// number of a decision or directive ("1461/QĐ-TTg"), a Party document or
// a technical standard. An agency's own paperwork ("45/KH-UBND",
// "12/2025/TTr-STP") never is in the catalogue.
func legalBasisNormative(c vietnamese_legal.Citation) bool {
	switch c.Kind {
	case vietnamese_legal.CitationParty, vietnamese_legal.CitationStandard:
		return true
	case vietnamese_legal.CitationFull:
		return vietnamese_legal.IsNormativeDocumentNumber(c.Number)
	case vietnamese_legal.CitationAdmin:
		parts := strings.SplitN(c.Key, "/", 2)
		if len(parts) != 2 {
			return false
		}
		code := parts[1]
		if i := strings.IndexAny(code, "-/"); i >= 0 {
			code = code[:i]
		}
		switch code {
		case "QD", "CT", "NQ", "ND", "TT", "TTLT":
			return true
		}
	}
	return false
}

// legalDistinctNumbers counts documents by number key.
func legalDistinctNumbers(docs []*types.LegalDocument) int {
	keys := map[string]bool{}
	for _, d := range docs {
		k := d.NumberKey
		if k == "" {
			k = "id:" + d.ID
		}
		keys[k] = true
	}
	return len(keys)
}

// candidates fills an ambiguous (or, with none, not found) finding.
func (c *legalBasisChecker) candidates(ctx context.Context, f *LegalBasisFinding, docs []*types.LegalDocument) error {
	if len(docs) == 0 {
		f.Status, f.Ambiguity, f.CandidatesMore, f.Incomplete = LegalBasisNotFound, "", false, false
		f.Note = legalBasisNote(f, c.now)
		return nil
	}
	sort.SliceStable(docs, func(i, j int) bool { return legalDateAfter(docs[i].IssuedOn, docs[j].IssuedOn) })
	f.Status = LegalBasisAmbiguous
	f.CandidatesTotal = len(docs)
	for i, d := range docs {
		if i == legalBasisMaxCandidates {
			break
		}
		cl, err := c.classify(ctx, d)
		if err != nil {
			return err
		}
		f.Candidates = append(f.Candidates, cl.doc)
	}
	f.Note = legalBasisNote(f, c.now)
	return nil
}

// resolve fills a finding whose citation names one catalogue document.
func (c *legalBasisChecker) resolve(ctx context.Context, f *LegalBasisFinding, d *types.LegalDocument) error {
	cl, err := c.classify(ctx, d)
	if err != nil {
		return err
	}
	doc := cl.doc
	f.Document = &doc
	f.Status = doc.Status
	f.Related = cl.related
	if by := cl.class.derivedBy; by != nil {
		if f.Status == LegalBasisPartlyExpired {
			f.MaybeExpired = true
		}
		f.DerivedFrom = legalOr(by.DocumentNumber, by.Title)
		if cl.class.derivedOn != nil {
			f.DerivedExpiredOn = cl.class.derivedOn.Format("2006-01-02")
		}
	}
	for _, o := range cl.class.reviewBy {
		f.ReviewBy = append(f.ReviewBy, legalShortLabel(o))
	}
	f.PartialSuspension, f.StaleStatus, f.Unsuitable = cl.class.partialSuspension, cl.class.staleNotYet, cl.class.unsuitable
	if f.DocType == "" {
		f.DocType = d.DocType
	}
	f.Note = legalBasisNote(f, c.now)
	return nil
}

// classify states a catalogue document's status as of c.now from its
// catalogue status, dates and incoming links (classifyLegal).
func (c *legalBasisChecker) classify(ctx context.Context, d *types.LegalDocument) (*legalClassified, error) {
	if cl, ok := c.docs[d.ID]; ok {
		return cl, nil
	}
	links, err := c.catalog.ListLinks(ctx, d.ID, nil, legalBasisIncoming)
	if err != nil {
		return nil, err
	}
	cl := &legalClassified{doc: legalBasisDocument(d)}
	cl.class = classifyLegal(&types.LegalStatusReport{Document: d, Links: links}, c.now)
	cl.doc.Status = cl.class.status
	for _, r := range legalBasisRelations {
		var others []*types.LegalDocument
		for _, l := range links {
			if l.Incoming && l.Relation == r.rel && l.Other != nil {
				others = append(others, l.Other)
			}
		}
		sort.SliceStable(others, func(i, j int) bool { return legalDateAfter(others[i].IssuedOn, others[j].IssuedOn) })
		for i, o := range others {
			if i == legalBasisMaxRelated {
				break
			}
			od := legalBasisDocument(o)
			// the related document's own links are not read: its status
			// comes from its catalogue row and dates
			od.Status = ClassifyLegalDocument(&types.LegalStatusReport{Document: o}, c.now)
			cl.related = append(cl.related, LegalBasisRelated{Relation: r.name, LegalBasisDocument: od})
		}
	}
	c.docs[d.ID] = cl
	return cl, nil
}

func legalBasisDocument(d *types.LegalDocument) LegalBasisDocument {
	day := func(t *time.Time) string {
		if t == nil || t.IsZero() {
			return ""
		}
		return t.Format("2006-01-02")
	}
	return LegalBasisDocument{
		ID: d.ID, DocumentNumber: d.DocumentNumber, DocType: d.DocType, Title: d.Title, Issuer: d.Issuer,
		IssuedOn: day(d.IssuedOn), EffectiveOn: day(d.EffectiveOn), ExpiredOn: day(d.ExpiredOn),
		CatalogStatus: d.Status, Unverified: d.Unverified(),
	}
}

// mergeLegalBasisFindings joins findings that resolved to the same
// catalogue document ("Luật Đất đai 2024" and "31/2024/QH15"): one finding,
// every place, under the first citation.
func mergeLegalBasisFindings(fs []LegalBasisFinding) []LegalBasisFinding {
	out := fs[:0:0]
	at := map[string]int{}
	for _, f := range fs {
		if f.Document == nil {
			out = append(out, f)
			continue
		}
		if i, ok := at[f.Document.ID]; ok {
			out[i].Positions = append(out[i].Positions, f.Positions...)
			sort.SliceStable(out[i].Positions, func(a, b int) bool {
				pa, pb := out[i].Positions[a], out[i].Positions[b]
				return pa.Paragraph < pb.Paragraph || pa.Paragraph == pb.Paragraph && pa.Start < pb.Start
			})
			continue
		}
		at[f.Document.ID] = len(out)
		out = append(out, f)
	}
	return out
}

// legalBasisLabel names a document: "Nghị định 30/2020/NĐ-CP".
func legalBasisLabel(d *LegalBasisDocument) string {
	if d.DocumentNumber == "" {
		return strings.TrimSpace(d.Title)
	}
	return strings.TrimSpace(d.DocType + " " + d.DocumentNumber)
}

func legalBasisDay(s string) string {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return s
	}
	return t.Format("02/01/2006")
}

// legalBasisNote words a finding's status in Vietnamese.
func legalBasisNote(f *LegalBasisFinding, now time.Time) string {
	related := func(rels ...string) []LegalBasisRelated {
		var out []LegalBasisRelated
		for _, r := range f.Related {
			for _, want := range rels {
				if r.Relation == want {
					out = append(out, r)
				}
			}
		}
		return out
	}
	names := func(rs []LegalBasisRelated) string {
		var out []string
		for _, r := range rs {
			out = append(out, legalBasisLabel(&r.LegalBasisDocument))
		}
		return strings.Join(out, ", ")
	}
	var b strings.Builder
	switch f.Status {
	case LegalBasisNotFound:
		if f.IssuedOn != "" {
			return "Chưa có trong danh mục văn bản pháp luật (không có văn bản số này ban hành ngày " + legalBasisDay(f.IssuedOn) +
				"): chưa rõ hiệu lực, cần tự kiểm tra."
		}
		return "Chưa có trong danh mục văn bản pháp luật: chưa rõ hiệu lực, cần tự kiểm tra."
	case LegalBasisAmbiguous:
		n := strconv.Itoa(f.CandidatesTotal)
		if f.CandidatesMore {
			n = "hơn " + n
		}
		switch f.Ambiguity {
		case LegalBasisAmbiguityMissingDate:
			return "Số hiệu này không có năm và được đánh lại hằng năm; viện dẫn chưa ghi ngày ban hành nên chưa xác định được văn bản (" +
				n + " văn bản cùng số trong danh mục). Cần ghi rõ ngày ban hành."
		case LegalBasisAmbiguityDateMismatch:
			return "Danh mục không có văn bản số này ban hành ngày " + legalBasisDay(f.IssuedOn) + " (có " + n +
				" văn bản cùng số, ngày khác): chưa rõ hiệu lực, cần kiểm tra số hiệu và ngày ban hành."
		case LegalBasisAmbiguityIncompleteNumber:
			return "Viện dẫn chưa đủ số hiệu (thiếu năm hoặc ký hiệu); có " + n + " văn bản trong danh mục khớp, cần ghi số hiệu đầy đủ."
		case LegalBasisAmbiguityNameGuess:
			return "Tên văn bản không khớp hẳn tên nào trong danh mục; có thể là văn bản dưới đây, cần ghi số hiệu đầy đủ."
		case LegalBasisAmbiguitySharedName:
			return "Có " + n + " văn bản trong danh mục cùng tên và năm này, cần ghi số hiệu đầy đủ."
		}
		return "Có " + n + " văn bản trong danh mục cùng số hiệu này, cần ghi rõ văn bản nào (cơ quan ban hành, ngày ban hành)."
	}
	d := f.Document
	label := legalBasisLabel(d)
	switch f.Status {
	case LegalBasisExpired:
		b.WriteString(label + " đã hết hiệu lực")
		if on := legalOr(d.ExpiredOn, f.DerivedExpiredOn); on != "" {
			b.WriteString(" từ ngày " + legalBasisDay(on))
		}
		if f.DerivedFrom != "" {
			b.WriteString(" (suy ra vì " + f.DerivedFrom + " đã có hiệu lực; danh mục chưa cập nhật trạng thái)")
		}
		if s := names(related(LegalBasisReplacedBy, LegalBasisRepealedBy, LegalBasisExpiredBy)); s != "" {
			b.WriteString("; được thay thế/bãi bỏ bởi " + s)
		}
		b.WriteString(".")
	case LegalBasisSuspended:
		b.WriteString(label + " đang bị ngưng hiệu lực")
		if s := names(related(LegalBasisSuspendedBy)); s != "" {
			b.WriteString(" bởi " + s)
		}
		b.WriteString(".")
	case LegalBasisPartlyExpired:
		b.WriteString(label + " đã hết hiệu lực một phần")
		if f.MaybeExpired {
			b.WriteString(", có thể đã hết hiệu lực toàn bộ (" + f.DerivedFrom + " thay thế hoặc làm hết hiệu lực văn bản này")
			if f.DerivedExpiredOn != "" {
				b.WriteString(" từ ngày " + legalBasisDay(f.DerivedExpiredOn))
			}
			b.WriteString(")")
		}
		if s := names(related(LegalBasisRepealedBy, LegalBasisPartlyExpiredBy)); s != "" {
			b.WriteString("; liên quan: " + s)
		}
		b.WriteString("; cần kiểm tra điều, khoản được viện dẫn.")
	case LegalBasisNeedsReview:
		switch {
		case f.Unsuitable:
			b.WriteString(label + " được vbpl.vn ghi \"Không còn phù hợp\"")
			if len(f.ReviewBy) > 0 {
				b.WriteString("; có văn bản bãi bỏ toàn bộ hoặc một phần: " + strings.Join(f.ReviewBy, ", "))
			}
			b.WriteString("; cần kiểm tra trước khi viện dẫn.")
		case f.PartialSuspension:
			b.WriteString(label + " bị ngưng hiệu lực một phần")
			if s := names(related(LegalBasisSuspendedBy)); s != "" {
				b.WriteString(" bởi " + s)
			}
			b.WriteString("; cần kiểm tra điều, khoản được viện dẫn.")
		case len(f.ReviewBy) > 0:
			lead := " được danh mục ghi còn hiệu lực"
			if st := strings.ToLower(strings.TrimSpace(d.CatalogStatus)); st == "" || st == "chưa xác định" {
				lead = " chưa được danh mục ghi tình trạng hiệu lực"
			}
			b.WriteString(label + lead + ", nhưng có văn bản bãi bỏ toàn bộ hoặc một phần: " +
				strings.Join(f.ReviewBy, ", ") + "; cần kiểm tra điều, khoản được viện dẫn.")
		default:
			b.WriteString(label + " có tình trạng hiệu lực một phần (" + d.CatalogStatus + "); cần kiểm tra điều, khoản được viện dẫn.")
		}
	case LegalBasisNotYetEffective:
		b.WriteString(label + " chưa có hiệu lực")
		if d.EffectiveOn != "" {
			b.WriteString(", có hiệu lực từ ngày " + legalBasisDay(d.EffectiveOn))
		}
		b.WriteString(".")
	case LegalBasisAmended:
		b.WriteString(label + " còn hiệu lực, đã được sửa đổi, bổ sung bởi " + names(related(LegalBasisAmendedBy)) + ".")
	case LegalBasisInForce:
		b.WriteString(label + " còn hiệu lực.")
	default:
		b.WriteString(label + " có trong danh mục nhưng chưa xác định được hiệu lực.")
	}
	if f.StaleStatus {
		b.WriteString(" Danh mục còn ghi \"Chưa có hiệu lực\" nhưng ngày có hiệu lực (" + legalBasisDay(d.EffectiveOn) + ") đã qua.")
	}
	switch f.Status {
	case LegalBasisExpired, LegalBasisPartlyExpired:
	default:
		// a replacing document: pending (no date, or one ahead) or, when
		// the status does not show it, already in force
		var pending, inForce []string
		for _, r := range related(LegalBasisReplacedBy, LegalBasisExpiredBy) {
			on, err := time.Parse("2006-01-02", r.EffectiveOn)
			switch {
			case err != nil:
				pending = append(pending, legalBasisLabel(&r.LegalBasisDocument)+" (chưa rõ ngày hiệu lực)")
			case legalDayAfter(on, now):
				pending = append(pending, legalBasisLabel(&r.LegalBasisDocument)+" (có hiệu lực từ ngày "+legalBasisDay(r.EffectiveOn)+")")
			default:
				inForce = append(inForce, legalBasisLabel(&r.LegalBasisDocument)+" (đã có hiệu lực từ ngày "+legalBasisDay(r.EffectiveOn)+")")
			}
		}
		if len(pending) > 0 {
			b.WriteString(" Sắp bị thay thế bởi " + strings.Join(pending, ", ") + ".")
		}
		if len(inForce) > 0 {
			b.WriteString(" Có văn bản thay thế đã có hiệu lực: " + strings.Join(inForce, ", ") + "; cần kiểm tra.")
		}
	}
	if d.Unverified {
		b.WriteString(" Dòng danh mục chưa được đối chiếu.")
	}
	return b.String()
}
