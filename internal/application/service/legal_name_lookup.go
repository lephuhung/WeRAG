package service

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
)

// A document cited by name only ("Luật Đấu thầu", 16.5 % of amendment
// targets) is the catalogue document of that type and exact title that was
// in force when the citing document B was issued. In force is read from
// the dates (issued_on, effective_on, expired_on), never from the status
// column, which goes stale: Luật An ninh mạng 24/2018/QH14 still says
// "Còn hiệu lực" although 116/2025/QH15 replaced it on 01/07/2026. A newer
// law of the same name in force at that date supersedes the older one; one
// issued but not yet in force makes the reference ambiguous.

// LegalNameResolution is how a name resolved.
type LegalNameResolution string

const (
	LegalNameUnique    LegalNameResolution = "unique"
	LegalNameAmbiguous LegalNameResolution = "ambiguous"
	LegalNameNone      LegalNameResolution = "none"
)

// Candidate states at the citing document's issue date.
const (
	LegalNameInForce    = "in_force"
	LegalNamePending    = "pending"    // issued, not yet in force
	LegalNameSuperseded = "superseded" // in force by its dates, a newer same-name law is too
	LegalNameExpired    = "expired"
	LegalNameRelation   = "relation" // the citing document is recorded to amend / replace / repeal it
)

// LegalNameCandidate is one catalogue document a name may mean.
type LegalNameCandidate struct {
	Document *types.LegalDocument
	State    string
}

// LegalNameMatch is the outcome of ResolveLegalDocumentName.
type LegalNameMatch struct {
	// Base is the normalised title matched ("luật đấu thầu"): the name, or
	// a shorter prefix of it when the reader ran past its end.
	Base       string
	Resolution LegalNameResolution
	Candidates []LegalNameCandidate // issued before the citing document, newest first
	Chosen     *types.LegalDocument // set when Resolution is unique
}

// legalLikeEscaper escapes LIKE wildcards (backslash is Postgres's default
// escape character).
var legalLikeEscaper = strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)

// legalNameCandidatesMax bounds the catalogue rows read for one name.
const legalNameCandidatesMax = 200

// ResolveLegalDocumentName finds the catalogue document a name cites as
// of at, the issue date of the citing document (zero: today). amenderID,
// when set, is the citing document's catalogue id: a document it is
// recorded to amend, replace or repeal under exactly that name wins.
func ResolveLegalDocumentName(
	ctx context.Context, db *gorm.DB, name string, at time.Time, amenderID string,
) (LegalNameMatch, error) {
	bases := legalNameBases(name)
	if db == nil || len(bases) == 0 {
		return LegalNameMatch{Resolution: LegalNameNone}, nil
	}
	if at.IsZero() {
		at = time.Now()
	}
	docType := legalNameType(bases[0])
	db = db.WithContext(ctx)

	if amenderID != "" {
		var targets []*types.LegalDocument
		if err := db.Raw(`SELECT t.* FROM legal_document_relations r
			JOIN legal_documents t ON t.id = r.to_id
			WHERE r.from_id = ? AND r.relation IN ('amends', 'replaces', 'repeals')`, amenderID).
			Scan(&targets).Error; err != nil {
			return LegalNameMatch{Resolution: LegalNameNone}, err
		}
		for _, base := range bases {
			var hits []LegalNameCandidate
			keys := map[string]bool{}
			for _, t := range targets {
				if LegalTitleBase(t.Title, t.DocType) == base {
					hits = append(hits, LegalNameCandidate{Document: t, State: LegalNameRelation})
					keys[t.NumberKey] = true
				}
			}
			if len(keys) == 1 {
				return LegalNameMatch{Base: base, Resolution: LegalNameUnique, Candidates: hits, Chosen: hits[0].Document}, nil
			}
			if len(hits) > 0 {
				return LegalNameMatch{Base: base, Resolution: LegalNameAmbiguous, Candidates: hits}, nil
			}
		}
	}

	q := db.Model(&types.LegalDocument{}).Where("issued_on IS NOT NULL")
	if docType != "" {
		q = q.Where("lower(doc_type) = ?", docType)
	}
	if db.Dialector.Name() == "postgres" {
		// the shortest base is in every longer one; SQLite's lower() folds
		// ASCII only, so its rows are filtered below
		short := bases[len(bases)-1]
		q = q.Where("lower(title) LIKE ?", "%"+legalLikeEscaper.Replace(strings.TrimPrefix(short, docType+" "))+"%")
	}
	var docs []*types.LegalDocument
	if err := q.Order("issued_on DESC").Limit(legalNameCandidatesMax).Find(&docs).Error; err != nil {
		return LegalNameMatch{Resolution: LegalNameNone}, err
	}
	for _, base := range bases {
		var same []*types.LegalDocument
		for _, d := range docs {
			if d.IssuedOn != nil && d.IssuedOn.Before(at) && LegalTitleBase(d.Title, d.DocType) == base {
				same = append(same, d)
			}
		}
		if len(same) > 0 {
			m := resolveByDates(same, at)
			m.Base = base
			return m, nil
		}
	}
	return LegalNameMatch{Resolution: LegalNameNone}, nil
}

// resolveByDates states each same-name document at the date at and picks
// the one in force: the newest by effective (else issue) date among those
// in force; a pending one makes the name ambiguous, as do two in force
// from the same date under different numbers.
func resolveByDates(docs []*types.LegalDocument, at time.Time) LegalNameMatch {
	since := func(d *types.LegalDocument) time.Time {
		if d.EffectiveOn != nil {
			return *d.EffectiveOn
		}
		return *d.IssuedOn
	}
	sort.SliceStable(docs, func(i, j int) bool { return docs[i].IssuedOn.After(*docs[j].IssuedOn) })
	m := LegalNameMatch{Resolution: LegalNameNone}
	var inForce []*types.LegalDocument
	pending := false
	for _, d := range docs {
		state := LegalNameInForce
		switch {
		case d.ExpiredOn != nil && !d.ExpiredOn.After(at):
			state = LegalNameExpired
		case d.EffectiveOn != nil && d.EffectiveOn.After(at):
			state = LegalNamePending
			pending = true
		default:
			inForce = append(inForce, d)
		}
		m.Candidates = append(m.Candidates, LegalNameCandidate{Document: d, State: state})
	}
	sort.SliceStable(inForce, func(i, j int) bool { return since(inForce[i]).After(since(inForce[j])) })
	var current []*types.LegalDocument
	for _, d := range inForce {
		if len(current) == 0 || since(d).Equal(since(current[0])) && d.NumberKey != current[0].NumberKey {
			current = append(current, d)
		}
	}
	for i := range m.Candidates {
		if c := &m.Candidates[i]; c.State == LegalNameInForce && !containsDoc(current, c.Document) {
			c.State = LegalNameSuperseded
		}
	}
	switch {
	case pending || len(current) > 1:
		m.Resolution = LegalNameAmbiguous
	case len(current) == 1:
		m.Resolution, m.Chosen = LegalNameUnique, current[0]
	}
	return m
}

func containsDoc(docs []*types.LegalDocument, d *types.LegalDocument) bool {
	for _, x := range docs {
		if x == d {
			return true
		}
	}
	return false
}

// legalTitleNumberCutRe is where a title's số hiệu starts: "… số 43/2019/QH14".
var legalTitleNumberCutRe = regexp.MustCompile(`\s+số\s*:?\s*\d`)

// LegalTitleBase is a catalogue title without its số hiệu, lower-cased
// and with its type in front: "Luật Giáo dục số 43/2019/QH14" → "luật
// giáo dục"; "An ninh mạng" of type Luật → "luật an ninh mạng". docType
// may be empty.
func LegalTitleBase(title, docType string) string {
	t := strings.ToLower(strings.Join(strings.Fields(norm.NFC.String(title)), " "))
	if loc := legalTitleNumberCutRe.FindStringIndex(t); loc != nil && loc[0] > 0 {
		t = t[:loc[0]]
	}
	t = strings.TrimRight(t, " .,;:")
	if typ := strings.ToLower(strings.Join(strings.Fields(docType), " ")); typ != "" && t != typ && !strings.HasPrefix(t, typ+" ") {
		t = typ + " " + t
	}
	return t
}

// legalNameCutRe marks where a name read from running text may have run
// past its end: before a comma or a joining word.
var legalNameCutRe = regexp.MustCompile(`,| (?:của|và|về|đối với|trong|theo|tại|cho|với) `)

// legalNameBases is the name's base, then the shorter bases it may have
// run past ("luật đấu thầu, theo đó" → "luật đấu thầu"), longest first.
// "Luật Giáo dục đại học" has no cut point, so it never falls back to
// "Luật Giáo dục".
func legalNameBases(name string) []string {
	base := LegalTitleBase(name, "")
	if base == "" {
		return nil
	}
	out := []string{base}
	locs := legalNameCutRe.FindAllStringIndex(base, -1)
	for i := len(locs) - 1; i >= 0; i-- {
		if p := strings.TrimRight(base[:locs[i][0]], " ,"); strings.Contains(p, " ") && p != out[len(out)-1] {
			out = append(out, p)
		}
	}
	// a base that is only a type ("luật") names nothing
	if legalNameType(out[len(out)-1]) == out[len(out)-1] {
		out = out[:len(out)-1]
	}
	return out
}

// legalNameType is the lower-cased type a base starts with, or "".
func legalNameType(base string) string {
	for _, t := range legalTitleTypes {
		if base == t || strings.HasPrefix(base, t+" ") {
			return t
		}
	}
	return ""
}
