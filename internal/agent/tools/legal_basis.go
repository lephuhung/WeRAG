package tools

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/docformat/docxedit"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// Legal basis check ("Rà soát căn cứ pháp lý") of the document being
// drafted: every document it cites (FindCitations) is looked up in the legal
// catalogue and given a status, without a model. The assistant points out:
// the report lists the citations, the editor may underline them, nothing in
// the document changes. A citation the catalogue does not hold is "not
// found" (chưa rõ), never expired; a number several catalogue documents
// share is "ambiguous" with its candidates, never one picked for the user.

// LegalBasisStatus is the status of one cited document.
type LegalBasisStatus string

const (
	LegalBasisInForce         LegalBasisStatus = "in_force"
	LegalBasisAmended         LegalBasisStatus = "amended" // in force, amended by later documents
	LegalBasisPartlyExpired   LegalBasisStatus = "partly_expired"
	LegalBasisExpired         LegalBasisStatus = "expired"
	LegalBasisSuspended       LegalBasisStatus = "suspended"
	LegalBasisNotYetEffective LegalBasisStatus = "not_yet_effective"
	// LegalBasisUnknown: the catalogue holds the document but states no
	// validity ("Chưa xác định") and nothing in its dates or links decides.
	LegalBasisUnknown   LegalBasisStatus = "unknown"
	LegalBasisAmbiguous LegalBasisStatus = "ambiguous"
	LegalBasisNotFound  LegalBasisStatus = "not_found"
)

// legalBasisOrder is the report order: what needs the drafter's attention
// first.
var legalBasisOrder = map[LegalBasisStatus]int{
	LegalBasisExpired: 0, LegalBasisSuspended: 1, LegalBasisPartlyExpired: 2, LegalBasisNotYetEffective: 3,
	LegalBasisAmbiguous: 4, LegalBasisAmended: 5, LegalBasisUnknown: 6, LegalBasisNotFound: 7, LegalBasisInForce: 8,
}

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
	// legalBasisPrefixScan is how many rows a partial number is matched
	// against (Candidates of the catalogue service reads as many).
	legalBasisPrefixScan = 200
	// legalBasisMaxRelated bounds the related documents of one relation.
	legalBasisMaxRelated = 5
	// legalBasisMaxMarks bounds the underlines of one check.
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
	// 2013", "Nghị định 68".
	Citation string `json:"citation"`
	// Kind is the FindCitations kind (full, admin, party, standard,
	// partial, name).
	Kind    string           `json:"kind"`
	DocType string           `json:"doc_type,omitempty"`
	Status  LegalBasisStatus `json:"status"`
	// Document is the catalogue document, nil when ambiguous or not found.
	Document *LegalBasisDocument `json:"document,omitempty"`
	// Candidates of an ambiguous citation (newest first), at most
	// legalBasisMaxCandidates of CandidatesTotal.
	Candidates      []LegalBasisDocument `json:"candidates,omitempty"`
	CandidatesTotal int                  `json:"candidates_total,omitempty"`
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
	// Incomplete: the citation lacks the year or code of its number
	// ("Nghị định 68", "NĐ 68/2026").
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
	// DocumentOps underline every place of the findings that need a look
	// (all but in_force); the client applies them only when the user asks.
	DocumentOps []DocumentOp `json:"document_ops"`
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
	FindByNumberPrefix(ctx context.Context, key string, limit int) ([]*types.LegalDocument, error)
	ListLinks(ctx context.Context, id string, outgoing, incoming []types.LegalRelation) ([]types.LegalRelationLink, error)
}

// LegalNameResolution is what a LegalNameResolver made of a law named by
// title and year: the one document it names (Chosen), or the candidates
// when several may be meant; neither when the catalogue has none.
type LegalNameResolution struct {
	Chosen     *types.LegalDocument
	Candidates []*types.LegalDocument
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

// legalBasisGroup collects the places of one distinct citation.
type legalBasisGroup struct {
	key       string
	cit       vietnamese_legal.Citation
	positions []LegalBasisPosition
}

// CheckLegalBasis checks the citations of paras (a document's paragraph
// texts) against the catalogue as of now. names may be nil: laws named
// with a year are then not checked either.
func CheckLegalBasis(ctx context.Context, paras []string, catalog LegalBasisCatalog, names LegalNameResolver, now time.Time) (*LegalBasisReport, error) {
	report := &LegalBasisReport{CheckedAt: now, Summary: map[LegalBasisStatus]int{}}
	texts := make([]string, len(paras))
	copy(texts, paras)
	vdoc := &virtualDoc{texts: texts, ids: make([]int, len(texts)), next: -2}
	for i := range vdoc.ids {
		vdoc.ids[i] = i
	}
	var groups []*legalBasisGroup
	byKey := map[string]*legalBasisGroup{}
	for pi, text := range paras {
		if strings.TrimSpace(text) == "" {
			continue
		}
		runes := []rune(text)
		var anchor *OpAnchor
		for _, c := range vietnamese_legal.FindCitations(text) {
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
			if anchor == nil {
				anchor = vdoc.anchor(pi)
			}
			pos := LegalBasisPosition{
				Paragraph: pi, Start: c.Start, End: c.End, Text: c.Raw, Anchor: anchor,
				TextOccurrence: 1 + strings.Count(string(runes[:c.Start]), c.Raw),
			}
			g := byKey[key]
			if g == nil {
				if len(groups) >= legalBasisMaxFindings {
					report.Truncated = true
					continue
				}
				g = &legalBasisGroup{key: key, cit: c}
				byKey[key] = g
				groups = append(groups, g)
			}
			g.positions = append(g.positions, pos)
		}
	}
	c := &legalBasisChecker{catalog: catalog, names: names, now: now, docs: map[string]*LegalBasisDocument{}}
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
	for _, f := range findings {
		report.Summary[f.Status]++
		if f.Status == LegalBasisInForce {
			continue
		}
		for _, p := range f.Positions {
			if len(report.DocumentOps) >= legalBasisMaxMarks {
				break
			}
			report.DocumentOps = append(report.DocumentOps, p.MarkOp())
		}
	}
	if findings == nil {
		findings = []LegalBasisFinding{}
	}
	if report.DocumentOps == nil {
		report.DocumentOps = []DocumentOp{}
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

type legalBasisChecker struct {
	catalog LegalBasisCatalog
	names   LegalNameResolver
	now     time.Time
	// docs caches classified documents by id (a related document is often
	// cited too)
	docs map[string]*LegalBasisDocument
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
		return f, c.candidates(ctx, f, res.Candidates)
	case vietnamese_legal.CitationPartial:
		f.Citation = strings.TrimSpace(cit.DocType + " " + cit.Number)
		f.Incomplete = true
		docs, err := c.catalog.FindByNumberPrefix(ctx, vietnamese_legal.CatalogNumberKey(cit.Number), legalBasisPrefixScan)
		if err != nil {
			return nil, err
		}
		want := vietnamese_legal.DocTypeClass(cit.DocType)
		var same []*types.LegalDocument
		for _, d := range docs {
			if vietnamese_legal.DocTypeClass(d.DocType) == want {
				same = append(same, d)
			}
		}
		// with its year the number is one document unless the catalogue
		// holds several; without it ("Nghị định 68") it never is
		if cit.Year != "" && legalDistinctNumbers(same) == 1 {
			return f, c.resolve(ctx, f, same[0])
		}
		return f, c.candidates(ctx, f, same)
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
	if len(exact) == 1 {
		return f, c.resolve(ctx, f, exact[0])
	}
	return f, c.candidates(ctx, f, exact)
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
		f.Status = LegalBasisNotFound
		f.Note = legalBasisNote(f)
		return nil
	}
	sort.SliceStable(docs, func(i, j int) bool { return legalDateAfter(docs[i].IssuedOn, docs[j].IssuedOn) })
	f.Status = LegalBasisAmbiguous
	f.CandidatesTotal = len(docs)
	for i, d := range docs {
		if i == legalBasisMaxCandidates {
			break
		}
		doc, _, err := c.classify(ctx, d)
		if err != nil {
			return err
		}
		f.Candidates = append(f.Candidates, *doc)
	}
	f.Note = legalBasisNote(f)
	return nil
}

// resolve fills a finding whose citation names one catalogue document.
func (c *legalBasisChecker) resolve(ctx context.Context, f *LegalBasisFinding, d *types.LegalDocument) error {
	doc, v, err := c.classify(ctx, d)
	if err != nil {
		return err
	}
	f.Document = doc
	f.Status = doc.Status
	f.Related = v.related
	f.DerivedFrom, f.DerivedExpiredOn = v.derivedFrom, v.derivedOn
	f.MaybeExpired = v.maybeExpired
	if f.DocType == "" {
		f.DocType = d.DocType
	}
	f.Note = legalBasisNote(f)
	return nil
}

// legalBasisVerdict is what classify reads beyond the document's status.
type legalBasisVerdict struct {
	related      []LegalBasisRelated
	derivedFrom  string
	derivedOn    string
	maybeExpired bool
}

// classify states a catalogue document's status as of c.now from its
// catalogue status, dates and incoming links (check_legal_status's rules:
// a stale "Còn hiệu lực" yields to a replacing document already in force).
func (c *legalBasisChecker) classify(ctx context.Context, d *types.LegalDocument) (*LegalBasisDocument, legalBasisVerdict, error) {
	var v legalBasisVerdict
	links, err := c.catalog.ListLinks(ctx, d.ID, nil, legalBasisIncoming)
	if err != nil {
		return nil, v, err
	}
	rep := &types.LegalStatusReport{Document: d, Links: links}
	doc := legalBasisDocument(d)
	doc.Status = ClassifyLegalDocument(rep, c.now)
	if by, on := derivedExpiry(rep, c.now); by != nil {
		if partlyExpired(d) {
			v.maybeExpired = true
		} else {
			v.derivedFrom, v.derivedOn = legalOr(by.DocumentNumber, by.Title), on.Format("2006-01-02")
		}
	}
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
			od.Status = ClassifyLegalDocument(&types.LegalStatusReport{Document: o}, c.now)
			v.related = append(v.related, LegalBasisRelated{Relation: r.name, LegalBasisDocument: od})
		}
	}
	return &doc, v, nil
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

// legalDayAfter reports whether t falls on a later calendar day than at,
// each read in its own location (catalogue dates are UTC midnights).
func legalDayAfter(t, at time.Time) bool { return t.Format("2006-01-02") > at.Format("2006-01-02") }

// ClassifyLegalDocument states a catalogue document's status as of now:
//   - expired: the catalogue says "Hết hiệu lực toàn bộ", its expiry date
//     has passed, or a document already in force replaces or ends it while
//     the catalogue still holds it in force (derivedExpiry);
//   - suspended: "Ngưng hiệu lực" / "Tạm ngưng hiệu lực";
//   - not yet effective: "Chưa có hiệu lực" or an effective date ahead;
//   - partly expired: "Hết hiệu lực một phần";
//   - in force / amended: "Còn hiệu lực", amended when a document amends it;
//   - unknown: no status stated ("Chưa xác định") and nothing above.
//
// rep.Links are the document's incoming links (may be empty).
func ClassifyLegalDocument(rep *types.LegalStatusReport, now time.Time) LegalBasisStatus {
	d := rep.Document
	status := strings.ToLower(strings.Join(strings.Fields(d.Status), " "))
	switch {
	case strings.HasPrefix(status, "hết hiệu lực toàn bộ"),
		d.ExpiredOn != nil && !d.ExpiredOn.IsZero() && !legalDayAfter(*d.ExpiredOn, now):
		return LegalBasisExpired
	case strings.Contains(status, "ngưng hiệu lực"):
		return LegalBasisSuspended
	}
	if by, _ := derivedExpiry(rep, now); by != nil {
		if partlyExpired(d) {
			return LegalBasisPartlyExpired
		}
		return LegalBasisExpired
	}
	switch {
	case status == "chưa có hiệu lực",
		d.EffectiveOn != nil && !d.EffectiveOn.IsZero() && legalDayAfter(*d.EffectiveOn, now):
		return LegalBasisNotYetEffective
	case strings.HasPrefix(status, "hết hiệu lực một phần"):
		return LegalBasisPartlyExpired
	case status == "còn hiệu lực":
		for _, l := range rep.Links {
			if l.Incoming && l.Relation == types.LegalRelationAmends {
				return LegalBasisAmended
			}
		}
		return LegalBasisInForce
	}
	return LegalBasisUnknown
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

// legalBasisNote words a finding's status in one sentence.
func legalBasisNote(f *LegalBasisFinding) string {
	related := func(rels ...string) string {
		var names []string
		for _, r := range f.Related {
			for _, want := range rels {
				if r.Relation == want {
					names = append(names, legalBasisLabel(&r.LegalBasisDocument))
				}
			}
		}
		return strings.Join(names, ", ")
	}
	var b strings.Builder
	switch f.Status {
	case LegalBasisNotFound:
		b.WriteString("Chưa có trong danh mục văn bản pháp luật: chưa rõ hiệu lực, cần tự kiểm tra.")
		return b.String()
	case LegalBasisAmbiguous:
		if f.Incomplete {
			b.WriteString("Viện dẫn chưa đủ số hiệu; có ")
		} else {
			b.WriteString("Có ")
		}
		b.WriteString(strconv.Itoa(f.CandidatesTotal) + " văn bản trong danh mục khớp, cần ghi rõ văn bản nào (số hiệu đầy đủ, cơ quan ban hành).")
		return b.String()
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
		if s := related(LegalBasisReplacedBy, LegalBasisRepealedBy, LegalBasisExpiredBy); s != "" {
			b.WriteString("; được thay thế/bãi bỏ bởi " + s)
		}
		b.WriteString(".")
	case LegalBasisSuspended:
		b.WriteString(label + " đang bị ngưng hiệu lực")
		if s := related(LegalBasisSuspendedBy); s != "" {
			b.WriteString(" bởi " + s)
		}
		b.WriteString(".")
	case LegalBasisPartlyExpired:
		b.WriteString(label + " đã hết hiệu lực một phần")
		if f.MaybeExpired {
			b.WriteString(", có thể đã hết hiệu lực toàn bộ")
		}
		if s := related(LegalBasisReplacedBy, LegalBasisRepealedBy, LegalBasisExpiredBy, LegalBasisPartlyExpiredBy); s != "" {
			b.WriteString(" (" + s + ")")
		}
		b.WriteString("; cần kiểm tra điều, khoản được viện dẫn.")
	case LegalBasisNotYetEffective:
		b.WriteString(label + " chưa có hiệu lực")
		if d.EffectiveOn != "" {
			b.WriteString(", có hiệu lực từ ngày " + legalBasisDay(d.EffectiveOn))
		}
		b.WriteString(".")
	case LegalBasisAmended:
		b.WriteString(label + " còn hiệu lực, đã được sửa đổi, bổ sung bởi " + related(LegalBasisAmendedBy) + ".")
	case LegalBasisInForce:
		b.WriteString(label + " còn hiệu lực.")
	default:
		b.WriteString(label + " có trong danh mục nhưng chưa xác định được hiệu lực.")
	}
	switch f.Status {
	case LegalBasisExpired, LegalBasisPartlyExpired:
	default:
		// a replacing document not in force yet (or without a date)
		if s := related(LegalBasisReplacedBy, LegalBasisExpiredBy); s != "" {
			b.WriteString(" Đã có văn bản thay thế chưa có hiệu lực hoặc chưa rõ ngày hiệu lực: " + s + ".")
		}
	}
	if d.Unverified {
		b.WriteString(" Dòng danh mục chưa được đối chiếu.")
	}
	return b.String()
}
