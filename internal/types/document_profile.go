package types

import (
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// Document profile statuses: the same values as the format check's, so the
// chat header shows both with one vocabulary.
const (
	DocumentProfileQueued  = "queued"
	DocumentProfileRunning = "running"
	DocumentProfileReady   = "ready"
	DocumentProfileFailed  = "failed"
)

// Units of a profile's section ranges: paragraph indexes of a target (the
// [i] of read_document_outline and rewrite_paragraphs), chunk or line
// indexes of a source (the [i] of read_document_outline on a source).
const (
	DocumentProfileUnitParagraph = "paragraph"
	DocumentProfileUnitChunk     = "chunk"
	DocumentProfileUnitLine      = "line"
)

// Bounds that keep a profile a card, not a second copy of the document.
const (
	DocumentProfileMaxKeyPoints  = 5
	DocumentProfileMaxQuestions  = 3
	DocumentProfileMaxSections   = 40
	DocumentProfileMaxEntities   = 12
	documentProfileMaxLineRunes  = 300
	documentProfileMaxTitleRunes = 120
	documentProfileMaxShortRunes = 160
)

// DocumentProfile is the card of one document of a document-assistant
// session (a target or a source): its identity (số ký hiệu, cơ quan ban
// hành, ngày, loại, trích yếu), what it says (gist, key points, topics,
// entities) and its sections with their index ranges, so a turn that names
// no document can still tell which one and which part a question is about
// without carrying every text. The identity and the section list are read
// without a model; the rest comes from one thinking-off model call over the
// section headings and their first paragraphs.
type DocumentProfile struct {
	DocumentNumber   string                   `json:"document_number,omitempty"`
	Issuer           string                   `json:"issuer,omitempty"`
	Date             string                   `json:"date,omitempty"`
	DocType          string                   `json:"doc_type,omitempty"`
	DocTypeCode      string                   `json:"doc_type_code,omitempty"`
	Subject          string                   `json:"subject,omitempty"`
	Gist             string                   `json:"gist,omitempty"`
	KeyPoints        []string                 `json:"key_points,omitempty"`
	Sections         []DocumentProfileSection `json:"sections,omitempty"`
	Topics           []string                 `json:"topics,omitempty"`
	Entities         *DocumentProfileEntities `json:"entities,omitempty"`
	TypicalQuestions []string                 `json:"typical_questions,omitempty"`
	// Unit names what Sections' From/To count (DocumentProfileUnit*).
	Unit string `json:"unit,omitempty"`

	// TextHash is the sha256 of the plain text the profile was made from
	// (not of the file: a formatting-only save keeps the profile).
	TextHash    string     `json:"text_hash,omitempty"`
	Model       string     `json:"model,omitempty"`
	GeneratedAt *time.Time `json:"generated_at,omitempty"`
	Status      string     `json:"status"`
	// Stale is set when the document was edited after the profile was made
	// (computed when the profile is shown, never stored as true).
	Stale bool   `json:"stale,omitempty"`
	Error string `json:"error,omitempty"`

	// Role, Revision and SaveCount are the workspace row the profile
	// describes; StartedAt is when the current run was queued or started.
	Role      string    `json:"role,omitempty"`
	Revision  int       `json:"revision"`
	SaveCount int       `json:"save_count"`
	StartedAt time.Time `json:"started_at"`
}

// DocumentProfileSection is one part of a document (Căn cứ, Chương, Điều,
// Mục, Phụ lục, a numbered heading) with the inclusive range of paragraph
// (or chunk) indexes it covers.
type DocumentProfileSection struct {
	Title   string `json:"title"`
	From    int    `json:"from"`
	To      int    `json:"to"`
	Summary string `json:"summary,omitempty"`
}

// DocumentProfileEntities are the names and numbers a question may quote:
// units (cơ quan, đơn vị), cited document numbers, dates and figures
// (amounts, quantities; for a source, the tables of figures and where they
// are).
type DocumentProfileEntities struct {
	Units        []string `json:"units,omitempty"`
	CitedNumbers []string `json:"cited_numbers,omitempty"`
	Dates        []string `json:"dates,omitempty"`
	Figures      []string `json:"figures,omitempty"`
}

// InProgress reports a profile that is queued or running.
func (p *DocumentProfile) InProgress() bool {
	return p != nil && (p.Status == DocumentProfileQueued || p.Status == DocumentProfileRunning)
}

// Describes reports whether the profile was made from this version of ws.
func (p *DocumentProfile) Describes(ws *DocumentWorkspace) bool {
	return p != nil && ws != nil && p.Revision == ws.Revision && p.SaveCount == ws.SaveCount &&
		(p.Role == "" || p.Role == DocumentRoleOf(ws))
}

// DocumentRoleOf is ws's role, a row without one being a target.
func DocumentRoleOf(ws *DocumentWorkspace) string {
	if ws.IsSource() {
		return DocumentWorkspaceRoleSource
	}
	return DocumentWorkspaceRoleTarget
}

// Public is the copy shown to the client: no hash, and Stale set when ws
// was edited since the profile was made.
func (p *DocumentProfile) Public(ws *DocumentWorkspace) *DocumentProfile {
	if p == nil {
		return nil
	}
	out := *p
	out.TextHash = ""
	out.Stale = p.Status == DocumentProfileReady && ws != nil && !p.Describes(ws)
	return &out
}

// Normalize trims and bounds every field the model wrote, resolving the
// document type to its canonical Vietnamese name and slug, as
// KnowledgeProfile.Normalize does.
func (p *DocumentProfile) Normalize() *DocumentProfile {
	if p == nil {
		return nil
	}
	p.DocumentNumber = vietnamese_legal.NormalizeOwnDocumentNumber(p.DocumentNumber)
	p.Issuer = truncateRunes(collapseWhitespace(p.Issuer), documentProfileMaxShortRunes)
	p.Date = truncateRunes(collapseWhitespace(p.Date), 40)
	p.DocType = truncateRunes(collapseWhitespace(p.DocType), knowledgeProfileMaxDocTypeRunes)
	if d := vietnamese_legal.DocTypeByName(p.DocType); d != nil {
		p.DocType, p.DocTypeCode = d.Name, d.Slug
	} else if d := vietnamese_legal.DocTypeBySlug(strings.TrimSpace(p.DocTypeCode)); d != nil {
		p.DocType, p.DocTypeCode = d.Name, d.Slug
	} else {
		p.DocTypeCode = ""
	}
	p.Subject = truncateRunes(collapseWhitespace(p.Subject), documentProfileMaxLineRunes)
	p.Gist = truncateRunes(collapseWhitespace(p.Gist), documentProfileMaxLineRunes)
	p.KeyPoints = boundList(p.KeyPoints, DocumentProfileMaxKeyPoints, documentProfileMaxLineRunes)
	p.Topics = NormalizeTopicList(p.Topics, KnowledgeProfileMaxTopics)
	p.TypicalQuestions = boundList(p.TypicalQuestions, DocumentProfileMaxQuestions, documentProfileMaxShortRunes)
	if len(p.Sections) > DocumentProfileMaxSections {
		p.Sections = p.Sections[:DocumentProfileMaxSections]
	}
	for i := range p.Sections {
		s := &p.Sections[i]
		s.Title = truncateRunes(collapseWhitespace(s.Title), documentProfileMaxTitleRunes)
		s.Summary = truncateRunes(collapseWhitespace(s.Summary), documentProfileMaxLineRunes)
	}
	if e := p.Entities; e != nil {
		e.Units = boundList(e.Units, DocumentProfileMaxEntities, documentProfileMaxShortRunes)
		e.CitedNumbers = boundList(e.CitedNumbers, DocumentProfileMaxEntities, documentProfileMaxShortRunes)
		e.Dates = boundList(e.Dates, DocumentProfileMaxEntities, documentProfileMaxShortRunes)
		e.Figures = boundList(e.Figures, DocumentProfileMaxEntities, documentProfileMaxShortRunes)
		if len(e.Units)+len(e.CitedNumbers)+len(e.Dates)+len(e.Figures) == 0 {
			p.Entities = nil
		}
	}
	return p
}

// boundList trims, deduplicates (case-insensitively) and bounds a list.
func boundList(items []string, limit, runes int) []string {
	seen := map[string]bool{}
	var out []string
	for _, it := range items {
		it = truncateRunes(collapseWhitespace(it), runes)
		key := strings.ToLower(it)
		if it == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, it)
		if len(out) >= limit {
			break
		}
	}
	return out
}
