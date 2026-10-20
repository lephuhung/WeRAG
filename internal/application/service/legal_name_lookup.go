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
	Candidates []LegalNameCandidate // issued before the citing document (or undated), newest first
	Chosen     *types.LegalDocument // set when Resolution is unique
}

// legalLikeEscaper escapes LIKE wildcards (backslash is Postgres's default
// escape character).
var legalLikeEscaper = strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)

// legalNameCandidatesMax bounds the catalogue rows read for one name.
const legalNameCandidatesMax = 500

// LegalNameUndated marks a same-name document without an issue date: it
// cannot be placed in time, so the name stays ambiguous.
const LegalNameUndated = "undated"

// LegalNameQuery is a document cited by name.
type LegalNameQuery struct {
	// Name as read ("Luật Đấu thầu"); it may run on past the name ("Luật
	// Đấu thầu, theo đó") or end with a year ("Luật Đấu thầu 2013").
	Name string
	// Year narrows the candidates to those issued that year ("2013");
	// empty: the year written at the end of Name, if any.
	Year string
	// At is the citing document's issue date; zero: today.
	At time.Time
	// AmenderID is the citing document's catalogue id, if known: a
	// document it is recorded to amend, replace or repeal under exactly
	// that name wins.
	AmenderID string
}

// ResolveLegalDocumentName finds the catalogue document a name cites as
// of q.At. The full name is tried first, then shorter prefixes of it. A
// prefix resolves uniquely only when no catalogue title continues it with
// the word it drops: "Luật Đất đai như sau", "Luật Đầu tư tại Điều 5" name
// Luật Đất đai / Luật Đầu tư, but "Luật Đầu tư theo hình thức …" (a title
// "Luật Đầu tư theo phương thức …" exists) or "Luật Quy hoạch đô thị và
// …" stays ambiguous. A year (q.Year, or ending the name) picks the law of
// that year even when it is no longer in force.
func ResolveLegalDocumentName(ctx context.Context, db *gorm.DB, q LegalNameQuery) (LegalNameMatch, error) {
	prefixes := legalNamePrefixes(q.Name)
	none := LegalNameMatch{Resolution: LegalNameNone}
	if db == nil || len(prefixes) == 0 {
		return none, nil
	}
	at := q.At
	if at.IsZero() {
		at = time.Now()
	}
	year := q.Year
	if year == "" {
		year = legalNameYear(q.Name)
	}
	full := prefixes[0].base
	docType := legalNameType(full)
	db = db.WithContext(ctx)

	if q.AmenderID != "" {
		var targets []*types.LegalDocument
		if err := db.Raw(`SELECT t.* FROM legal_document_relations r
			JOIN legal_documents t ON t.id = r.to_id
			WHERE r.from_id = ? AND r.relation IN ('amends', 'replaces', 'repeals')`, q.AmenderID).
			Scan(&targets).Error; err != nil {
			return none, err
		}
		for _, p := range prefixes {
			var hits []LegalNameCandidate
			keys := map[string]bool{}
			for _, t := range targets {
				if LegalTitleBase(t.Title, t.DocType) == p.base {
					hits = append(hits, LegalNameCandidate{Document: t, State: LegalNameRelation})
					key := t.NumberKey
					if key == "" {
						key = "id:" + t.ID
					}
					keys[key] = true
				}
			}
			if len(hits) == 0 {
				continue
			}
			m := LegalNameMatch{Base: p.base, Resolution: LegalNameAmbiguous, Candidates: hits}
			if len(keys) == 1 && p.safe {
				m.Resolution, m.Chosen = LegalNameUnique, hits[0].Document
			}
			return m, nil
		}
	}

	query := db.Model(&types.LegalDocument{})
	if docType != "" {
		query = query.Where("lower(doc_type) = ?", docType)
	}
	if db.Dialector.Name() == "postgres" {
		// every prefix shares the name's first words; SQLite's lower()
		// folds ASCII only, so its rows are filtered below
		words := strings.Fields(strings.TrimPrefix(full, docType+" "))
		if len(words) > 2 {
			words = words[:2]
		}
		query = query.Where("lower(title) LIKE ?", "%"+legalLikeEscaper.Replace(strings.TrimRight(strings.Join(words, " "), ","))+"%")
	}
	var docs []*types.LegalDocument
	if err := query.Order("issued_on DESC").Limit(legalNameCandidatesMax).Find(&docs).Error; err != nil {
		return none, err
	}
	bases := make([]string, len(docs))
	for i, d := range docs {
		bases[i] = LegalTitleBase(d.Title, d.DocType)
	}
	for _, p := range prefixes {
		var named, same []*types.LegalDocument
		for i, d := range docs {
			if bases[i] == p.base {
				named = append(named, d)
			}
		}
		if len(named) == 0 {
			continue // no law of this name: try a shorter one
		}
		for _, d := range named {
			if d.IssuedOn == nil {
				same = append(same, d) // undated: cannot be ruled out
				continue
			}
			if year != "" && d.IssuedOn.Format("2006") != year {
				continue
			}
			if d.IssuedOn.Before(at) {
				same = append(same, d)
			}
		}
		if len(same) == 0 {
			return LegalNameMatch{Base: p.base, Resolution: LegalNameNone}, nil
		}
		m := resolveByDates(same, at)
		m.Base = p.base
		// a year written with the name picks the law of that year, in
		// force or not (its state stays on the candidate)
		if year != "" && m.Resolution != LegalNameUnique && legalDistinctDocs(same) == 1 {
			m.Resolution, m.Chosen = LegalNameUnique, same[0]
		}
		// a shorter name is safe when no catalogue title continues it with
		// the dropped word: "luật đất đai như" none, "luật đầu tư theo" PPP
		if p.cont != "" && m.Resolution == LegalNameUnique {
			for _, b := range bases {
				if b == p.cont || strings.HasPrefix(b, p.cont+" ") {
					m.Resolution, m.Chosen = LegalNameAmbiguous, nil
					break
				}
			}
		}
		return m, nil
	}
	return none, nil
}

// legalDistinctDocs counts documents by number key (by id when a row has
// none).
func legalDistinctDocs(docs []*types.LegalDocument) int {
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

// resolveByDates states each same-name document at the date at and picks
// the one in force: the newest by effective (else issue) date among those
// in force. The name is ambiguous when a document is pending (issued, not
// yet in force — also a newer one without an effective date while an
// older one is in force), undated, or when two are in force from the same
// date under different numbers.
func resolveByDates(docs []*types.LegalDocument, at time.Time) LegalNameMatch {
	since := func(d *types.LegalDocument) time.Time {
		if d.EffectiveOn != nil {
			return *d.EffectiveOn
		}
		return *d.IssuedOn
	}
	sort.SliceStable(docs, func(i, j int) bool {
		a, b := docs[i].IssuedOn, docs[j].IssuedOn
		if a == nil || b == nil {
			return a == nil && b != nil
		}
		return a.After(*b)
	})
	m := LegalNameMatch{Resolution: LegalNameNone}
	states := make([]string, len(docs))
	for i, d := range docs {
		switch {
		case d.ExpiredOn != nil && !d.ExpiredOn.After(at):
			states[i] = LegalNameExpired
		case d.IssuedOn == nil:
			states[i] = LegalNameUndated
		case d.EffectiveOn != nil && d.EffectiveOn.After(at):
			states[i] = LegalNamePending
		default:
			states[i] = LegalNameInForce
		}
	}
	// a newer document with no effective date while an older one is in
	// force may not be in force yet
	for i, d := range docs {
		if states[i] != LegalNameInForce || d.EffectiveOn != nil {
			continue
		}
		for j, o := range docs {
			if j != i && states[j] == LegalNameInForce && o.IssuedOn.Before(*d.IssuedOn) {
				states[i] = LegalNamePending
				break
			}
		}
	}
	var inForce []*types.LegalDocument
	unsure := false
	for i, d := range docs {
		switch states[i] {
		case LegalNameInForce:
			inForce = append(inForce, d)
		case LegalNamePending, LegalNameUndated:
			unsure = true
		}
	}
	sort.SliceStable(inForce, func(i, j int) bool { return since(inForce[i]).After(since(inForce[j])) })
	var current []*types.LegalDocument
	for _, d := range inForce {
		if len(current) == 0 || since(d).Equal(since(current[0])) && d.NumberKey != current[0].NumberKey {
			current = append(current, d)
		}
	}
	for i, d := range docs {
		if states[i] == LegalNameInForce && !containsDoc(current, d) {
			states[i] = LegalNameSuperseded
		}
		m.Candidates = append(m.Candidates, LegalNameCandidate{Document: d, State: states[i]})
	}
	switch {
	case unsure || len(current) > 1:
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

var (
	// legalTitleNumberCutRe is where a title's số hiệu starts: "… số 43/2019/QH14".
	legalTitleNumberCutRe = regexp.MustCompile(`\s+số\s*:?\s*\d`)
	// legalTitleEndYearRe is a year closing a title: "… công tư 2020", "… năm 2013".
	legalTitleEndYearRe = regexp.MustCompile(`\s+(?:năm\s+)?((?:19|20)\d\d)$`)
)

// LegalTitleBase is a catalogue title without its số hiệu or closing
// year, lower-cased and with its type in front: "Luật Giáo dục số
// 43/2019/QH14" → "luật giáo dục"; "Luật Đầu tư theo phương thức đối tác
// công tư 2020" → "luật đầu tư theo phương thức đối tác công tư"; "An ninh
// mạng" of type Luật → "luật an ninh mạng". docType may be empty.
func LegalTitleBase(title, docType string) string {
	t := strings.ToLower(strings.Join(strings.Fields(norm.NFC.String(title)), " "))
	if loc := legalTitleNumberCutRe.FindStringIndex(t); loc != nil && loc[0] > 0 {
		t = t[:loc[0]]
	}
	t = strings.TrimRight(t, " .,;:")
	if loc := legalTitleEndYearRe.FindStringIndex(t); loc != nil && loc[0] > 0 {
		t = strings.TrimRight(t[:loc[0]], " .,;:")
	}
	if typ := strings.ToLower(strings.Join(strings.Fields(docType), " ")); typ != "" && t != typ && !strings.HasPrefix(t, typ+" ") {
		t = typ + " " + t
	}
	return t
}

// legalNameYear is the year a name ends with ("Luật Đấu thầu 2013"), or "".
func legalNameYear(name string) string {
	t := strings.ToLower(strings.Join(strings.Fields(norm.NFC.String(name)), " "))
	if loc := legalTitleNumberCutRe.FindStringIndex(t); loc != nil && loc[0] > 0 {
		t = t[:loc[0]]
	}
	if m := legalTitleEndYearRe.FindStringSubmatch(strings.TrimRight(t, " .,;:")); m != nil {
		return m[1]
	}
	return ""
}

// legalNamePrefix is a base to look up; safe when what it drops from the
// name is read-on text, not words the name may hold.
type legalNamePrefix struct {
	base string
	safe bool
	// cont is the prefix with the first dropped word ("luật đầu tư
	// theo"): a catalogue title starting so means the word may belong to
	// the name.
	cont string
}

// legalReadOn are the words a clause after a name starts with: dropping
// them never shortens the name itself.
var legalReadOn = map[string]bool{
	"có": true, "được": true, "đã": true, "đang": true, "sẽ": true, "thì": true, "là": true,
	"này": true, "nêu": true, "quy": true, "hướng": true, "sửa": true, "bổ": true, "bãi": true,
	"thay": true, "ngày": true, "số": true, "năm": true, "hiện": true, "hết": true,
	"như": true, "sau": true, "các": true,
}

// legalNamePrefixes is the name's base, then its shorter word prefixes
// down to the type and one word, longest first. Statically, a prefix is
// safe when the words it drops start after a comma ("Luật Đấu thầu, theo
// đó") or with a clause word ("… có hiệu lực", "… như sau"); against the
// catalogue, when no title starts with the prefix and the dropped word
// (ResolveLegalDocumentName).
func legalNamePrefixes(name string) []legalNamePrefix {
	base := LegalTitleBase(name, "")
	if base == "" {
		return nil
	}
	words := strings.Fields(base)
	min := 2
	if t := legalNameType(base); t != "" {
		min = len(strings.Fields(t)) + 1
	}
	if len(words) < min {
		return nil
	}
	out := []legalNamePrefix{{base: base, safe: true}}
	for k := len(words) - 1; k >= min; k-- {
		p := strings.TrimRight(strings.Join(words[:k], " "), ",")
		if p == out[len(out)-1].base {
			continue
		}
		safe := strings.HasSuffix(words[k-1], ",") || legalReadOn[strings.Trim(words[k], ",")]
		out = append(out, legalNamePrefix{base: p, safe: safe, cont: strings.Join(words[:k+1], " ")})
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
