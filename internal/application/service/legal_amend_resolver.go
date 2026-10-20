package service

import (
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// The current-wording resolver (plan §4.7, §4.12): given one retrieved
// chunk of document A, the edges touching A and a reference date, it
// gives each khoản / điểm of the chunk a state and the text the model is
// shown. Pure: no database, no clock.
//
// Rules:
//   - an edge applies from its effective date; an issue date alone is an
//     unknown date → UNRESOLVED, never assumed in force;
//   - edges apply in order of effective date; a phrase's From is checked
//     against the version right before its amender only while every
//     earlier edge resolved, otherwise the provision is UNRESOLVED;
//   - one document's changes of one date apply at once (renumbering
//     "khoản 5" → "khoản 6", "khoản 4" → "khoản 5" does not chain);
//   - same provision, same date, different content → CONFLICT;
//   - an amender expiring later reverts nothing: only newer edges change
//     the text; tam_ngung / dinh_chi / khong_ap_dung have their own end,
//     het_hieu_luc (partial) has none;
//   - tier-1 (and, interim, legacy parser phrase) changes are applied to
//     the text; any other is shown inside "[LỜI VĂN HIỆN HÀNH CHƯA XÁC
//     MINH: …]" around the stored text.

// LegalProvisionChunk is a retrieved chunk of A with A's identity.
type LegalProvisionChunk struct {
	ID              string
	KnowledgeID     string
	KnowledgeBaseID string
	Content         string
	Legal           *vietnamese_legal.LegalMetadata
	Doc             LegalAmendDoc
	// IdentityProblem is set when A could not be mapped to one catalogue
	// row (plan §4.13): every provision is UNRESOLVED.
	IdentityProblem string
}

// LegalCatalogAmendment is a catalogue relation "B amends A" (vbpl), with
// B's effective date: a provision no edge of B touches cannot be called
// ORIGINAL_IN_FORCE.
type LegalCatalogAmendment struct {
	Amender LegalAmendDoc
	EffFrom LegalAmendDate
	// NeverInForce: the catalogue records B expiring on or before the day
	// it takes effect, so its changes may never have applied.
	NeverInForce bool
}

// LegalResolveOptions are the resolver's inputs besides the chunk.
type LegalResolveOptions struct {
	// AllowedKBs, when set, are the KBs whose amending text the model may
	// read (plan §10.2); an edge from another KB is AMENDED_TEXT_MISSING.
	AllowedKBs map[string]bool
	// Catalogue lists the catalogue's "amends A" relations.
	Catalogue []LegalCatalogAmendment
	// MatchCondition compares a khong_ap_dung condition with the question;
	// nil: always unknown.
	// TODO(legal-amend step 4): deterministic matcher against the question.
	MatchCondition func(e *LegalAmendEdge) LegalConditionMatch
}

// LegalProvisionResult is the verdict on one khoản / điểm of the chunk.
type LegalProvisionResult struct {
	Provision LegalProvisionRef
	State     LegalProvisionState
	// Underlying is the state the provision would have if its tier-2 edges
	// were accepted (what review would make of it).
	Underlying LegalProvisionState
	Condition  LegalConditionMatch // CONDITIONALLY_NOT_APPLICABLE only
	Original   string              // stored text of the unit
	Text       string              // what the model is shown
	Reasons    []string            // why the state is not ORIGINAL / CURRENT
	Notes      []string            // pending changes (shown as "[Sắp có hiệu lực: …]")
	Applied    []*LegalAmendEdge   // edges that took effect
	Added      bool                // a provision bo_sung adds, not stored in A
}

// Gated reports whether the answer gate applies (§4.11): any state but
// ORIGINAL / CURRENT, except a condition known not to match.
func (r LegalProvisionResult) Gated() bool {
	if r.State == LegalStateConditionallyNotApp && r.Condition == LegalConditionNoMatch {
		return false
	}
	return r.State.Gated()
}

// LegalResolution is the resolver's result for one chunk.
type LegalResolution struct {
	ChunkID string
	AsOf    time.Time
	State   LegalProvisionState // the most severe unit state
	Units   []LegalProvisionResult
	// ModelContent is the chunk as the model reads it: Body, then the
	// chunk-level notes (after the text, so a content head shows the text).
	ModelContent string
	// Body is the units' texts; Notes the chunk-level notes (catalogue,
	// identity), which a result set shows once per document.
	Body  string
	Notes []string
	// Ignored are the rejected edges (kept for review).
	Ignored []*LegalAmendEdge
}

// legalUnit is a byte range [start, end) of the chunk and its provision.
type legalUnit struct {
	prov       LegalProvisionRef
	start, end int
	// labelled: the unit starts at its own label (not a continuation)
	labelled bool
}

var legalDieuHeadRe = regexp.MustCompile(`(?m)^[ \t]*(?:#{1,6}[ \t]*)?(?:[*_]{1,3}[ \t]*)?Điều[ \t]+(\d+[a-zA-Zđ]?)[ \t]*[*_]{0,3}[ \t]*[.:]`)

// legalUnits splits content at its Điều headings and khoản / điểm labels
// (the marks provisionScope reads), so the units cover the content exactly.
// Labels on quoted lines ("> 1. …", another document's text quoted in
// this one) are not this document's structure.
func legalUnits(content string, meta *vietnamese_legal.LegalMetadata) []legalUnit {
	type mark struct {
		at, kind int
		label    string
	}
	quoted := func(labelAt int) bool {
		line := content[strings.LastIndexByte(content[:labelAt], '\n')+1 : labelAt]
		return strings.HasPrefix(strings.TrimSpace(line), ">")
	}
	var marks []mark
	for _, m := range legalDieuHeadRe.FindAllStringSubmatchIndex(content, -1) {
		marks = append(marks, mark{m[0], 0, strings.ToLower(content[m[2]:m[3]])})
	}
	for _, m := range khoanStartRe.FindAllStringSubmatchIndex(content, -1) {
		if !quoted(m[2]) {
			marks = append(marks, mark{m[0], 1, strings.ToLower(content[m[2]:m[3]])})
		}
	}
	for _, m := range diemStartRe.FindAllStringSubmatchIndex(content, -1) {
		if !quoted(m[2]) {
			marks = append(marks, mark{m[0], 2, strings.ToLower(content[m[2]:m[3]])})
		}
	}
	sort.SliceStable(marks, func(i, j int) bool {
		if marks[i].at != marks[j].at {
			return marks[i].at < marks[j].at
		}
		return marks[i].kind < marks[j].kind
	})
	var cur LegalProvisionRef
	if meta != nil && len(meta.ArticleNos) == 1 {
		cur.Article = strings.ToLower(meta.ArticleNos[0])
		// a chunk continuing an article starts inside the khoản / điểm
		// open at its first character
		if (len(marks) == 0 || marks[0].kind != 0) && len(meta.SubdivisionRefs) > 0 {
			if p, ok := parseSubdivisionRef(cur.Article, meta.SubdivisionRefs[0]); ok {
				cur = p
				if p.Diem == "" && len(meta.SubdivisionRefs) > 1 {
					if q, ok := parseSubdivisionRef(cur.Article, meta.SubdivisionRefs[1]); ok && q.Khoan == p.Khoan && q.Diem != "" {
						cur = q
					}
				}
			}
		}
	}
	var units []legalUnit
	start, labelled := 0, false
	for _, m := range marks {
		if m.at > start {
			units = append(units, legalUnit{cur, start, m.at, labelled})
		}
		labelled = true
		switch m.kind {
		case 0:
			cur = LegalProvisionRef{Article: m.label}
		case 1:
			cur = LegalProvisionRef{Article: cur.Article, Khoan: m.label}
		default:
			cur = LegalProvisionRef{Article: cur.Article, Khoan: cur.Khoan, Diem: m.label}
		}
		start = m.at
	}
	units = append(units, legalUnit{cur, start, len(content), labelled})
	return units
}

// unitRun is the state of one unit while edges apply.
type unitRun struct {
	u          legalUnit
	idx        int
	chunk      *chunkRun
	orig       string
	state      LegalProvisionState
	underlying LegalProvisionState
	cond       LegalConditionMatch
	reasons    []string
	notes      []string
	markers    []string // tier-1 notes of operations that keep the text
	applied    []*LegalAmendEdge
	// plain is the text as amended so far, without notes, for checks
	plain    string
	resolved bool
	// replace is the last verified replacement (sua_doi / bai_bo …);
	// display are the verified phrase edges after it (pending ones too,
	// for applyPhraseChanges' notes)
	replace *LegalAmendEdge
	display []*LegalAmendEdge
	removed bool
}

// chunkRun is what the units of one chunk share.
type chunkRun struct {
	// missing gives, for a khoản / điểm phrase edge whose words no unit of
	// its provision holds while the chunk holds the whole provision, the
	// unit that reports it (the provision's first)
	missing map[*LegalAmendEdge]int
}

func (r *unitRun) raise(state LegalProvisionState, reason string) {
	r.state = worseState(r.state, state)
	if reason != "" && !containsString(r.reasons, reason) {
		r.reasons = append(r.reasons, reason)
	}
}

func (r *unitRun) effect(state LegalProvisionState) {
	r.underlying = worseState(r.underlying, state)
}

// ResolveLegalProvisions resolves one chunk of A as of asOf.
func ResolveLegalProvisions(c LegalProvisionChunk, edges []LegalAmendEdge, asOf time.Time, opt LegalResolveOptions) LegalResolution {
	res := LegalResolution{ChunkID: c.ID, AsOf: asOf, State: LegalStateOriginalInForce}
	var live []*LegalAmendEdge
	for i := range edges {
		e := &edges[i]
		if e.Rejected != "" {
			res.Ignored = append(res.Ignored, e)
			continue
		}
		if e.Target.CatalogID != "" && c.Doc.CatalogID != "" && e.Target.CatalogID != c.Doc.CatalogID {
			continue
		}
		if opt.AllowedKBs != nil && e.carriesText() && !opt.AllowedKBs[e.Amender.KnowledgeBaseID] {
			e.TextWithheld = true
		}
		e.Provision = e.Provision.normalized()
		live = append(live, e)
	}
	units := legalUnits(c.Content, c.Legal)
	cr := &chunkRun{missing: legalMissingPhrases(c.Content, units, live)}
	runs := make([]*unitRun, len(units))
	for i, u := range units {
		orig := c.Content[u.start:u.end]
		runs[i] = &unitRun{u: u, idx: i, chunk: cr, orig: orig, plain: orig, resolved: true,
			state: LegalStateOriginalInForce, underlying: LegalStateOriginalInForce}
	}
	var adds []*LegalAmendEdge
	for _, e := range live {
		if e.Op == LegalOpBoSung && e.NewLabel != "" {
			adds = append(adds, e)
		}
	}
	for _, r := range runs {
		var mine []*LegalAmendEdge
		for _, e := range live {
			if e.Op == LegalOpBoSung && e.NewLabel != "" {
				continue
			}
			if r.u.prov.Article == "" && e.Provision.Article != "" {
				continue
			}
			if e.Provision.covers(r.u.prov) {
				mine = append(mine, e)
			}
		}
		resolveUnit(r, mine, asOf, opt)
	}

	// a provision written by several units (a Điều replaced whole) is
	// shown once, at its first unit in the chunk
	head := map[*LegalAmendEdge]int{}
	for i, r := range runs {
		if r.replace != nil {
			if _, ok := head[r.replace]; !ok {
				head[r.replace] = i
			}
		}
	}

	// catalogue relations without an edge here
	chunkNotes := legalCatalogueNotes(c, runs, live, asOf, opt.Catalogue)
	if c.IdentityProblem != "" {
		reason := "chưa xác định được văn bản này trong danh mục (" + c.IdentityProblem + ")"
		for _, r := range runs {
			r.raise(LegalStateUnresolved, reason)
		}
		chunkNotes = append([]string{reason}, chunkNotes...)
	}

	shown := map[string]bool{}
	var b strings.Builder
	for i, r := range runs {
		text := unitText(r, i, head, asOf, chunkNotes, shown)
		b.WriteString(text)
		res.Units = append(res.Units, LegalProvisionResult{
			Provision: r.u.prov, State: r.state, Underlying: r.underlying, Condition: r.cond,
			Original: r.orig, Text: text, Reasons: r.reasons, Notes: r.notes, Applied: r.applied,
		})
		res.State = worseState(res.State, r.state)
		// provisions bo_sung adds after the last unit of their parent
		for _, add := range addedAfter(runs, i, adds) {
			ar := resolveAdded(add, adds, asOf)
			if ar == nil {
				continue
			}
			for cur := b.String(); !strings.HasSuffix(cur, "\n\n"); cur += "\n" {
				b.WriteString("\n")
			}
			b.WriteString(ar.Text)
			res.Units = append(res.Units, *ar)
			res.State = worseState(res.State, ar.State)
		}
	}
	res.Body, res.Notes = b.String(), chunkNotes
	res.ModelContent = res.Body + legalNotesBlock(chunkNotes)
	return res
}

// legalNotesBlock renders chunk-level notes, after the text.
func legalNotesBlock(notes []string) string {
	if len(notes) == 0 {
		return ""
	}
	return "\n\n[LỜI VĂN HIỆN HÀNH CHƯA XÁC MINH: " + strings.Join(notes, "; ") + "]"
}

// legalManyPhrases reports a lead-in changing several phrases over one
// list of provisions ("cụm từ “A” bằng “B”; cụm từ “C” bằng “D” tại khoản
// 1, 2 …"): each phrase need not be in every provision listed.
func legalManyPhrases(leadIn string) bool {
	return strings.Count(leadIn, "“")+strings.Count(leadIn, `"`)/2 > 2
}

// legalMissingPhrases finds the khoản / điểm phrase edges whose words are
// in no unit of their provision although the chunk holds the provision
// whole (its own label and a later unit outside it): the version before
// the amender cannot be what the edge says. Words an edge's change brings
// in are not counted missing.
func legalMissingPhrases(content string, units []legalUnit, live []*LegalAmendEdge) map[*LegalAmendEdge]int {
	out := map[*LegalAmendEdge]int{}
	for _, e := range live {
		if !e.phrase() || e.Provision.Khoan == "" || legalManyPhrases(e.LeadInText) {
			continue
		}
		brought := false
		for _, o := range live {
			if o != e && (len(findPhrase(o.To, e.From)) > 0 || len(findPhrase(o.QuoteText, e.From)) > 0) {
				brought = true
				break
			}
		}
		if brought {
			continue
		}
		first, last, startsHere, found := -1, -1, false, false
		for i, u := range units {
			if u.prov.Article == "" || !e.Provision.covers(u.prov) {
				continue
			}
			if first < 0 {
				first = i
			}
			last = i
			if u.labelled && u.prov == e.Provision {
				startsHere = true
			}
			if len(findPhrase(content[u.start:u.end], e.From)) > 0 {
				found = true
			}
		}
		endsHere := last >= 0 && last+1 < len(units) && units[last+1].prov.Article != ""
		if first >= 0 && startsHere && endsHere && !found {
			out[e] = first
		}
	}
	return out
}

// resolveUnit applies the edges covering one unit in date order.
func resolveUnit(r *unitRun, edges []*LegalAmendEdge, asOf time.Time, opt LegalResolveOptions) {
	var applicable, unknown []*LegalAmendEdge
	for _, e := range edges {
		switch {
		case !e.EffFrom.Known:
			unknown = append(unknown, e)
		case e.EffFrom.At.After(asOf):
			if e.phrase() && e.Tier.verifiedWording() && !e.TextWithheld && e.Unresolved == "" {
				r.display = append(r.display, e) // applyPhraseChanges notes it as pending
			} else if !e.phrase() || len(findPhrase(r.orig, e.From)) > 0 {
				r.notes = append(r.notes, legalOpPhrase(e)+" theo "+e.Amender.label()+", từ "+e.EffFrom.text())
			}
		case e.interval() && e.EffTo.Known && !asOf.Before(e.EffTo.At):
			// its interval ended: nothing to show
		default:
			applicable = append(applicable, e)
		}
	}
	sort.SliceStable(applicable, func(i, j int) bool {
		a, b := applicable[i], applicable[j]
		if !a.EffFrom.At.Equal(b.EffFrom.At) {
			return a.EffFrom.At.Before(b.EffFrom.At)
		}
		return a.Amender.Number < b.Amender.Number
	})
	for i := 0; i < len(applicable); {
		j := i
		for j < len(applicable) && applicable[j].EffFrom.At.Equal(applicable[i].EffFrom.At) {
			j++
		}
		applyGroup(r, applicable[i:j], opt)
		i = j
	}
	// an edge without an effective date: UNRESOLVED where it may apply
	for _, e := range unknown {
		if e.phrase() && len(findPhrase(r.orig, e.From)) == 0 && len(findPhrase(r.plain, e.From)) == 0 {
			continue
		}
		r.raise(LegalStateUnresolved, legalOpPhrase(e)+" theo "+e.Amender.label()+": chưa rõ ngày hiệu lực (chỉ có ngày ban hành hoặc không có ngày)")
		r.resolved = false
	}
}

// legalGroupKey is the scope an edge writes, for conflicts: two edges of
// one date with the same key and different content conflict.
func legalGroupKey(e *LegalAmendEdge) (key, content string, ok bool) {
	switch {
	case e.phrase():
		return "p|" + e.Provision.key() + "|" + foldSpaces(e.From), foldSpaces(e.To), true
	case e.Op == LegalOpSuaDoi || e.Op == LegalOpBoSung:
		return "t|" + e.Provision.key(), "q|" + foldSpaces(e.QuoteText), true
	case e.Op == LegalOpBaiBo:
		return "t|" + e.Provision.key(), "bai_bo", true
	}
	return "", "", false
}

// applyGroup applies the edges of one effective date at once.
func applyGroup(r *unitRun, group []*LegalAmendEdge, opt LegalResolveOptions) {
	base := r.plain
	priorResolved := r.resolved
	seen := map[string]*LegalAmendEdge{}
	for _, e := range group {
		key, content, ok := legalGroupKey(e)
		if !ok {
			continue
		}
		if e.phrase() && len(findPhrase(base, e.From)) == 0 && len(findPhrase(r.orig, e.From)) == 0 {
			continue
		}
		if !e.phrase() && e.Unresolved != "" {
			continue // already UNRESOLVED; its text is no rival
		}
		if prev, dup := seen[key]; dup {
			_, prevContent, _ := legalGroupKey(prev)
			if prevContent != content {
				r.raise(LegalStateConflict, "xung đột cùng ngày "+e.EffFrom.text()+": "+legalOpPhrase(prev)+" theo "+prev.Amender.label()+
					" và "+legalOpPhrase(e)+" theo "+e.Amender.label())
				r.resolved = false
			}
			continue
		}
		seen[key] = e
	}
	var phrases []*LegalAmendEdge
	for _, e := range group {
		via := " theo " + e.Amender.label() + ", từ " + e.EffFrom.text()
		src := ""
		if s := e.sourceText(); s != "" {
			src = " (nguồn: " + s + ")"
		}
		if e.phrase() {
			found := len(findPhrase(base, e.From)) > 0
			if !found && len(findPhrase(r.orig, e.From)) == 0 {
				// the words are elsewhere; report them missing once, when
				// the whole provision is here and holds them nowhere
				if at, ok := r.chunk.missing[e]; ok && at == r.idx && priorResolved && e.Unresolved == "" {
					r.raise(LegalStateUnresolved, "không thấy cụm từ “"+e.From+"” trong "+e.Provision.String()+" ở bản ngay trước "+e.Amender.label())
					r.resolved = false
				}
				continue
			}
			switch {
			case e.Unresolved != "":
				r.raise(LegalStateUnresolved, legalOpPhrase(e)+via+": "+e.Unresolved)
				r.resolved = false
				continue
			case !priorResolved:
				r.raise(LegalStateUnresolved, legalOpPhrase(e)+via+": chưa giải được các sửa đổi trước đó nên không kiểm được cụm từ")
				r.resolved = false
				continue
			case !found:
				r.raise(LegalStateUnresolved, "không thấy cụm từ “"+e.From+"” trong "+r.u.prov.String()+" ở bản ngay trước "+e.Amender.label())
				r.resolved = false
				continue
			case e.TextWithheld:
				r.raise(LegalStateAmendedTextMissing, e.Provision.String()+" bị sửa đổi cụm từ “"+e.From+"”"+via+"; lời văn của văn bản sửa đổi không thuộc phạm vi được dùng")
				r.resolved = false
				continue
			}
			r.applied = append(r.applied, e)
			phrases = append(phrases, e)
			if e.Tier.verifiedWording() {
				r.display = append(r.display, e)
				r.raise(LegalStateCurrentVerified, "")
			} else {
				r.raise(LegalStateUnverified, legalOpPhrase(e)+via+src+", chưa xác minh")
			}
			r.effect(LegalStateCurrentVerified)
			continue
		}
		if e.Unresolved != "" {
			r.raise(LegalStateUnresolved, legalOpPhrase(e)+via+": "+e.Unresolved)
			r.resolved = false
			continue
		}
		switch e.Op {
		case LegalOpSuaDoi, LegalOpBoSung:
			switch {
			case e.TextWithheld:
				r.raise(LegalStateAmendedTextMissing, e.Provision.String()+" đã bị sửa đổi"+via+"; lời văn sửa đổi không thuộc phạm vi được dùng")
				r.resolved = false
				continue
			case strings.TrimSpace(e.QuoteText) == "":
				r.raise(LegalStateAmendedTextMissing, e.Provision.String()+" đã bị sửa đổi"+via+"; không có lời văn sửa đổi")
				r.resolved = false
				continue
			}
			r.applied = append(r.applied, e)
			r.plain, r.removed = e.QuoteText, false
			r.effect(LegalStateCurrentVerified)
			if e.Tier.verifiedWording() {
				r.replace, r.display = e, nil
				r.raise(LegalStateCurrentVerified, "")
			} else {
				r.raise(LegalStateUnverified, e.Provision.String()+" có thể đã được sửa đổi"+via+src+
					". Lời văn mới theo nguồn này: “"+legalCapQuote(e.QuoteText)+"”")
			}
		case LegalOpBaiBo, LegalOpBaiBoVanBan, LegalOpThayTheVanBan:
			r.applied = append(r.applied, e)
			r.plain, r.removed = "", true
			r.effect(LegalStateCurrentVerified)
			if e.Tier.verifiedWording() {
				r.replace, r.display = e, nil
				r.raise(LegalStateCurrentVerified, "")
			} else {
				r.raise(LegalStateUnverified, "có thể "+legalOpPhrase(e)+via+src)
			}
		case LegalOpHetHieuLuc, LegalOpTamNgung, LegalOpDinhChi, LegalOpKhongApDung, LegalOpTiepTucApDung:
			r.applied = append(r.applied, e)
			state, marker := legalIntervalMarker(e)
			if state == LegalStateConditionallyNotApp {
				r.cond = LegalConditionUnknown
				if opt.MatchCondition != nil {
					r.cond = opt.MatchCondition(e)
				}
			}
			if state != "" {
				r.effect(state)
			}
			if e.Tier.verifiedWording() {
				r.markers = append(r.markers, marker)
				if state != "" {
					r.raise(state, "")
				}
			} else {
				r.raise(LegalStateUnverified, "có thể: "+strings.Trim(marker, "[]")+src)
			}
		default:
			r.raise(LegalStateUnresolved, legalOpPhrase(e)+via+": thao tác khác, cần đọc văn bản sửa đổi")
			r.resolved = false
		}
	}
	if len(phrases) > 0 && !r.removed {
		r.plain = replacePhrasesAtOnce(r.plain, phrases)
	}
}

// legalQuoteRunes caps B's new text quoted in an unverified note.
const legalQuoteRunes = 400

// legalCapQuote shortens an unverified new text for a note.
func legalCapQuote(s string) string {
	r := []rune(s)
	if len(r) <= legalQuoteRunes {
		return s
	}
	cut := string(r[:legalQuoteRunes])
	if k := strings.LastIndexByte(cut, ' '); k > len(cut)/2 {
		cut = cut[:k]
	}
	return cut + " …"
}

// replacePhrasesAtOnce replaces each edge's From with its To in one pass.
func replacePhrasesAtOnce(text string, edges []*LegalAmendEdge) string {
	type span struct {
		s, e int
		to   string
	}
	sort.SliceStable(edges, func(i, j int) bool { return len(edges[i].From) > len(edges[j].From) })
	var spans []span
	for _, e := range edges {
		for _, loc := range findPhrase(text, e.From) {
			clash := false
			for _, sp := range spans {
				if loc[0] < sp.e && sp.s < loc[1] {
					clash = true
					break
				}
			}
			if !clash {
				spans = append(spans, span{loc[0], loc[1], e.To})
			}
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].s < spans[j].s })
	var b strings.Builder
	last := 0
	for _, sp := range spans {
		b.WriteString(text[last:sp.s])
		b.WriteString(sp.to)
		last = sp.e
	}
	b.WriteString(text[last:])
	return b.String()
}

// legalOpPhrase words an operation: "cụm từ “A” được thay bằng “B”".
func legalOpPhrase(e *LegalAmendEdge) string {
	p := e.Provision.String()
	switch e.Op {
	case LegalOpThayCumTu:
		return "cụm từ “" + e.From + "” được thay bằng “" + e.To + "” tại " + p
	case LegalOpBaiBoCumTu:
		return "cụm từ “" + e.From + "” bị bãi bỏ tại " + p
	case LegalOpSuaDoi:
		return p + " được sửa đổi"
	case LegalOpBoSung:
		if e.NewLabel != "" {
			return legalAddedProvision(e.Provision, e.NewLabel).String() + " được bổ sung"
		}
		return p + " được bổ sung"
	case LegalOpBaiBo:
		return p + " bị bãi bỏ"
	case LegalOpBaiBoVanBan:
		return "văn bản bị bãi bỏ"
	case LegalOpThayTheVanBan:
		return "văn bản bị thay thế"
	case LegalOpDinhChi:
		return p + " bị đình chỉ thi hành"
	case LegalOpTamNgung:
		return p + " bị tạm ngưng hiệu lực"
	case LegalOpHetHieuLuc:
		return p + " hết hiệu lực"
	case LegalOpKhongApDung:
		if e.Scope != "" {
			return p + " không áp dụng " + e.Scope
		}
		return p + " không áp dụng"
	case LegalOpTiepTucApDung:
		return p + " tiếp tục được áp dụng"
	}
	return p + " bị tác động (" + string(e.Op) + ")"
}

// legalIntervalMarker is the state and note of an operation that keeps
// the text: tạm ngưng, đình chỉ, không áp dụng, hết hiệu lực, tiếp tục.
func legalIntervalMarker(e *LegalAmendEdge) (LegalProvisionState, string) {
	by := " theo " + e.Amender.label()
	until := ""
	if e.EffTo.Known {
		until = " đến " + e.EffTo.text()
	}
	switch e.Op {
	case LegalOpHetHieuLuc:
		return LegalStatePartiallyExpired, "[Hết hiệu lực từ " + e.EffFrom.text() + by + "]"
	case LegalOpTamNgung:
		return LegalStateNotApplicable, "[Tạm ngưng hiệu lực từ " + e.EffFrom.text() + until + by + "]"
	case LegalOpDinhChi:
		return LegalStateNotApplicable, "[Bị đình chỉ thi hành từ " + e.EffFrom.text() + until + by + "]"
	case LegalOpKhongApDung:
		if strings.TrimSpace(e.Scope) != "" {
			return LegalStateConditionallyNotApp, "[Không áp dụng " + strings.TrimSpace(e.Scope) + by + ", từ " + e.EffFrom.text() + until + "]"
		}
		return LegalStateNotApplicable, "[Không áp dụng từ " + e.EffFrom.text() + until + by + "]"
	}
	return "", "[Tiếp tục được áp dụng" + by + ", từ " + e.EffFrom.text() + until + "]"
}

// unitText is what the model reads for a unit.
func unitText(r *unitRun, i int, head map[*LegalAmendEdge]int, asOf time.Time, chunkNotes []string, shown map[string]bool) string {
	body := r.orig
	if r.replace != nil && head[r.replace] != i {
		// the rest of a provision shown whole at its first unit
		body = ""
	} else if r.replace != nil {
		e := r.replace
		lead, rest, tail := splitSpace(r.orig)
		via := e.Amender.label() + ", từ " + e.EffFrom.text()
		if r.removed {
			what := "[Văn bản đã bị bãi bỏ theo "
			switch e.Op {
			case LegalOpBaiBo:
				what = "[" + e.Provision.String() + " đã bị bãi bỏ theo "
			case LegalOpThayTheVanBan:
				what = "[Văn bản đã bị thay thế theo "
			}
			body = lead + what + via + "]" + tail
		} else {
			text := applyPhraseChanges(e.QuoteText, phraseChangesFor(r.display, r.u.prov), asOf)
			body = lead + "[Lời văn hiện hành theo " + via + "]\n\n" + text + "\n\n[trước " + e.EffFrom.text() + ": " + shortQuote(headingRe.ReplaceAllString(rest, "")) + "]" + tail
		}
	} else if len(r.display) > 0 {
		body = applyPhraseChanges(r.orig, phraseChangesFor(r.display, r.u.prov), asOf)
	}
	var pre []string
	pre = append(pre, r.markers...)
	for _, n := range r.notes {
		if !shown["pending|"+n] {
			shown["pending|"+n] = true
			pre = append(pre, "[Sắp có hiệu lực: "+n+"]")
		}
	}
	if r.state.Gated() && r.state != LegalStateCurrentVerified {
		// a reason is shown once per chunk, at the first unit it concerns
		// (an edge on a khoản covers its điểm too)
		var own []string
		for _, reason := range ownReasons(r.reasons, chunkNotes) {
			if shown[reason] {
				continue
			}
			shown[reason] = true
			own = append(own, reason)
		}
		if len(own) > 0 {
			pre = append([]string{"[LỜI VĂN HIỆN HÀNH CHƯA XÁC MINH: " + strings.Join(own, "; ") + "]"}, pre...)
		}
	}
	if len(pre) == 0 {
		return body
	}
	lead, rest, tail := splitSpace(body)
	if rest == "" {
		return lead + strings.Join(pre, "\n") + "\n\n" + tail
	}
	return lead + strings.Join(pre, "\n") + "\n\n" + rest + tail
}

// ownReasons drops the reasons the chunk-level note already gives.
func ownReasons(reasons, chunkNotes []string) []string {
	var out []string
	for _, r := range reasons {
		if !containsString(chunkNotes, r) {
			out = append(out, r)
		}
	}
	return out
}

// splitSpace cuts s into leading space, body and trailing space.
func splitSpace(s string) (lead, body, tail string) {
	body = strings.TrimLeftFunc(s, unicode.IsSpace)
	lead = s[:len(s)-len(body)]
	trimmed := strings.TrimRightFunc(body, unicode.IsSpace)
	return lead, trimmed, body[len(trimmed):]
}

// phraseChangesFor turns verified phrase edges into applyPhraseChanges'
// changes for a unit: the scope the unit already is is dropped, a deeper
// one (a khoản inside a replaced Điều) kept.
func phraseChangesFor(edges []*LegalAmendEdge, u LegalProvisionRef) []legalPhraseChange {
	var out []legalPhraseChange
	for _, e := range edges {
		ch := legalPhraseChange{From: e.From, To: e.To, Amender: legalHistoryDoc{
			ExternalID: e.Amender.CatalogID, Number: e.Amender.Number, Type: e.Amender.Type, EffFrom: e.EffFrom.iso(),
		}}
		if u.Khoan == "" {
			ch.Khoan, ch.Diem = e.Provision.Khoan, e.Provision.Diem
		} else if u.Diem == "" {
			ch.Diem = e.Provision.Diem
		}
		out = append(out, ch)
	}
	return out
}

// addedAfter lists the bo_sung edges whose new provision goes after unit
// i: the last unit of the parent in the chunk (or the unit After names).
func addedAfter(runs []*unitRun, i int, adds []*LegalAmendEdge) []*LegalAmendEdge {
	var out []*LegalAmendEdge
	for _, e := range adds {
		parent := e.Provision
		if parent.Article == "" {
			// a new Điều goes after the one it follows: After, else the
			// number it extends ("6a" after Điều 6)
			parent.Article = strings.ToLower(e.After)
			if parent.Article == "" {
				parent.Article = strings.TrimRight(strings.ToLower(e.NewLabel), "abcdđeghiklmnopqrstuvxy")
			}
			if parent.Article == "" || parent.Article == strings.ToLower(e.NewLabel) {
				continue
			}
		}
		last := -1
		for j, r := range runs {
			if r.u.prov.Article != "" && parent.covers(r.u.prov) {
				last = j
			}
		}
		if last != i {
			continue
		}
		out = append(out, e)
	}
	return out
}

// resolveAdded is the unit a bo_sung adds; nil when not yet in force.
func resolveAdded(e *LegalAmendEdge, adds []*LegalAmendEdge, asOf time.Time) *LegalProvisionResult {
	if e.EffFrom.Known && e.EffFrom.At.After(asOf) {
		return nil
	}
	prov := legalAddedProvision(e.Provision, e.NewLabel)
	res := &LegalProvisionResult{Provision: prov, Added: true, Underlying: LegalStateCurrentVerified, Applied: []*LegalAmendEdge{e}}
	via := " theo " + e.Amender.label() + ", từ " + e.EffFrom.text()
	src := ""
	if s := e.sourceText(); s != "" {
		src = " (nguồn: " + s + ")"
	}
	for _, o := range adds {
		if o != e && o.EffFrom == e.EffFrom && legalAddedProvision(o.Provision, o.NewLabel) == prov && foldSpaces(o.QuoteText) != foldSpaces(e.QuoteText) {
			res.State = LegalStateConflict
			res.Reasons = append(res.Reasons, "xung đột cùng ngày: "+prov.String()+" được bổ sung khác nhau theo "+o.Amender.label()+" và "+e.Amender.label())
		}
	}
	switch {
	case res.State == LegalStateConflict:
	case !e.EffFrom.Known:
		res.State = LegalStateUnresolved
		res.Reasons = append(res.Reasons, prov.String()+" được bổ sung theo "+e.Amender.label()+": chưa rõ ngày hiệu lực")
	case e.Unresolved != "":
		res.State = LegalStateUnresolved
		res.Reasons = append(res.Reasons, prov.String()+" có thể được bổ sung"+via+": "+e.Unresolved)
	case e.TextWithheld || strings.TrimSpace(e.QuoteText) == "":
		res.State = LegalStateAmendedTextMissing
		res.Reasons = append(res.Reasons, prov.String()+" đã được bổ sung"+via+"; lời văn bổ sung không có hoặc không thuộc phạm vi được dùng")
	case e.Tier.verifiedWording():
		res.State = LegalStateCurrentVerified
		res.Text = "[Bổ sung" + via + "]\n\n" + e.QuoteText + "\n"
		return res
	default:
		res.State = LegalStateUnverified
		res.Reasons = append(res.Reasons, prov.String()+" có thể đã được bổ sung"+via+src+". Lời văn bổ sung theo nguồn này: “"+legalCapQuote(e.QuoteText)+"”")
	}
	res.Text = "[LỜI VĂN HIỆN HÀNH CHƯA XÁC MINH: " + strings.Join(res.Reasons, "; ") + "]\n"
	return res
}

// legalCatalogueNotes marks the units no edge of a catalogue amender
// covers: AMENDED_TEXT_MISSING when that amender has no edge on A at all
// (its text is not held or not usable), UNVERIFIED when its edges touch
// other provisions only, UNRESOLVED when its date is unknown. The notes
// are returned once for the chunk.
func legalCatalogueNotes(c LegalProvisionChunk, runs []*unitRun, live []*LegalAmendEdge, asOf time.Time, cat []LegalCatalogAmendment) []string {
	var notes []string
	for _, ca := range cat {
		if ca.EffFrom.Known && ca.EffFrom.At.After(asOf) {
			continue
		}
		var mine []*LegalAmendEdge
		for _, e := range live {
			if ca.Amender.CatalogID != "" && e.Amender.CatalogID == ca.Amender.CatalogID {
				mine = append(mine, e)
			}
		}
		var state LegalProvisionState
		var note string
		switch {
		case ca.NeverInForce:
			state, note = LegalStateUnresolved, "danh mục ghi văn bản này bị sửa đổi bởi "+ca.Amender.label()+" nhưng văn bản đó hết hiệu lực trước hoặc ngay khi có hiệu lực"
		case !ca.EffFrom.Known:
			state, note = LegalStateUnresolved, "danh mục ghi văn bản này bị sửa đổi bởi "+ca.Amender.label()+", chưa rõ ngày hiệu lực"
		case len(mine) == 0:
			state, note = LegalStateAmendedTextMissing, "văn bản này đã bị sửa đổi bởi "+ca.Amender.label()+" (từ "+ca.EffFrom.text()+") theo danh mục; chưa có lời văn sửa đổi"
		default:
			state, note = LegalStateUnverified, "danh mục ghi văn bản này bị sửa đổi bởi "+ca.Amender.label()+" (từ "+ca.EffFrom.text()+"); chưa xác định điều khoản này có bị tác động không"
		}
		hit := false
		for _, r := range runs {
			if strings.TrimSpace(r.orig) == "" {
				continue
			}
			covered := false
			for _, e := range mine {
				if (r.u.prov.Article != "" || e.Provision.Article == "") && e.Provision.covers(r.u.prov) {
					covered = true
					break
				}
				if e.Op == LegalOpBoSung && e.NewLabel != "" && r.u.prov.Article != "" && e.Provision.covers(r.u.prov) {
					covered = true
					break
				}
			}
			if !covered {
				r.raise(state, note)
				hit = true
			}
		}
		if hit {
			notes = append(notes, note)
		}
	}
	return notes
}
