package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// The answer path reads the current wording and the amendment history
// through the resolver (legal_amend_resolver.go) when the platform switch
// LegalWordingResolverSetting is on (default off until reviewed). Off — or
// on any error, panic or timeout of the resolver for a call — the previous
// code (LegalCurrentWording, LegalAmendmentHistory) answers that call, so
// turning the switch off restores the old behaviour at once.
//
// INTERIM: plan §4.7 says a failing graph marks every legal provision
// UNVERIFIED ("chưa kiểm tra được lời văn hiện hành"). Until the answer
// gate (step 4) exists, a resolver failure falls back to the previous
// phrase-only wording instead, which is what users had before.

// LegalWordingResolverSetting is the system setting switching the legal
// current wording and amendment history to the amendment resolver.
const LegalWordingResolverSetting = "legal.current_wording.resolver"

// legalResolverTimeout is the resolver's share of legalHistoryTimeout:
// the old path still has time to answer when it falls back.
const legalResolverTimeout = legalHistoryTimeout / 2

// SetLegalWordingSettings gives the agents the settings service the
// resolver switch (LegalWordingResolverSetting) is read from.
func (s *agentService) SetLegalWordingSettings(settings interfaces.SystemSettingService) {
	s.legalSettings = settings
}

// legalResolverDefault is the switch's value when unset: off until the
// re-review passes.
const legalResolverDefault = false

// legalResolverOn reads the switch per call (the settings service caches);
// the default when no settings service is wired.
func legalResolverOn(ctx context.Context, settings interfaces.SystemSettingService) bool {
	if settings == nil {
		return legalResolverDefault
	}
	return settings.GetBool(ctx, LegalWordingResolverSetting, "", legalResolverDefault)
}

// legalWordingChoice runs the resolver when on, else — and when it fails —
// the old lookup.
func legalWordingChoice(ctx context.Context, on bool,
	resolved func() (map[string]string, error), old func() map[string]string,
) map[string]string {
	if !on {
		return old()
	}
	out, err := legalRecover(resolved)
	if err != nil {
		logger.Warnf(ctx, "[LegalWording] resolver failed, using the phrase-only wording: %v", err)
		return old()
	}
	return out
}

// legalRecover runs fn, turning a panic into an error.
func legalRecover[T any](fn func() (T, error)) (out T, err error) {
	defer func() {
		if p := recover(); p != nil {
			var zero T
			out, err = zero, fmt.Errorf("panic: %v", p)
		}
	}()
	return fn()
}

// legalHistoryChoice is legalWordingChoice for the history block.
func legalHistoryChoice(ctx context.Context, on bool, resolved func() (string, error), old func() string) string {
	if !on {
		return old()
	}
	out, err := legalRecover(resolved)
	if err != nil {
		logger.Warnf(ctx, "[LegalHistory] resolver failed, using the previous history: %v", err)
		return old()
	}
	return out
}

// legalAllowedKBs are the KBs whose amending text a question may read
// (plan §10.2): its own search scope plus the public platform KBs (public
// and owned by no tenant, as access.IsPlatformPublicKB).
func legalAllowedKBs(ctx context.Context, db *gorm.DB, scope []string) ([]string, map[string]bool, error) {
	var public []string
	if err := db.WithContext(ctx).Raw(`SELECT id FROM knowledge_bases WHERE deleted_at IS NULL AND visibility = ? AND owner_tenant_id = 0`,
		string(types.KBVisibilityPublic)).Scan(&public).Error; err != nil {
		return nil, nil, fmt.Errorf("public KBs: %w", err)
	}
	set := map[string]bool{}
	var list []string
	for _, id := range append(append([]string(nil), scope...), public...) {
		if id != "" && !set[id] {
			set[id] = true
			list = append(list, id)
		}
	}
	return list, set, nil
}

// resolveLegalChunks loads and resolves the chunks as of now, within the
// resolver's time budget.
func resolveLegalChunks(ctx context.Context, db *gorm.DB, kbIDs, chunkIDs []string, now time.Time,
) (*LegalAmendLoad, map[string]LegalResolution, error) {
	list, set, err := legalAllowedKBs(ctx, db, kbIDs)
	if err != nil {
		return nil, nil, err
	}
	load, err := LoadLegalAmendments(ctx, db, kbIDs, chunkIDs, list, now.Location())
	if err != nil {
		return nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	// TODO(legal-amend): the date a question asks about ("năm 2025",
	// "trước đây") instead of today (plan §4.7).
	return load, load.Resolve(now, set), nil
}

// LegalCurrentWordingResolved is LegalCurrentWording through the
// resolver: by chunk id, the text the model reads for each retrieved legal
// chunk whose wording or state differs from the stored text. An error
// (including the timeout) means no answer: the caller falls back.
func LegalCurrentWordingResolved(ctx context.Context, db *gorm.DB, kbIDs, chunkIDs []string, now time.Time) (map[string]string, error) {
	if db == nil || len(kbIDs) == 0 || len(chunkIDs) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, legalResolverTimeout)
	defer cancel()
	load, res, err := resolveLegalChunks(ctx, db, kbIDs, chunkIDs, now)
	if err != nil {
		return nil, err
	}
	// a document's chunk-level notes are shown once per result set, on its
	// first chunk
	noted := map[string]bool{}
	out := map[string]string{}
	for _, c := range load.Chunks {
		r, ok := res[c.ID]
		if !ok {
			continue
		}
		text := r.Body
		if len(r.Notes) > 0 {
			key := legalDocKey(c.Doc) + "|" + strings.Join(r.Notes, "|")
			if !noted[key] {
				noted[key] = true
				text += legalNotesBlock(r.Notes)
			}
		}
		if len(r.Info) > 0 {
			key := legalDocKey(c.Doc) + "|info|" + strings.Join(r.Info, "|")
			if !noted[key] {
				noted[key] = true
				text += legalInfoBlock(r.Info)
			}
		}
		if text != c.Content {
			out[c.ID] = text
		}
	}
	return out, nil
}

// LegalAmendmentHistoryResolved is LegalAmendmentHistory with the
// provisions of the retrieved chunks read through the resolver (the
// edges and dates the wording uses); the chunks that are themselves
// amending text keep the forward lines. An error means no answer.
func LegalAmendmentHistoryResolved(
	ctx context.Context, db *gorm.DB, kbIDs, chunkIDs []string, answer string, now time.Time,
) (string, error) {
	if db == nil || len(kbIDs) == 0 || len(chunkIDs) == 0 || strings.TrimSpace(answer) == "" {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(ctx, legalResolverTimeout)
	defer cancel()
	h := &legalHistory{amended: map[string]string{}, phrases: map[string]*phraseChange{}, ctx: ctx, db: db.WithContext(ctx), kbIDs: kbIDs, answer: answer, now: now}
	chunks, err := h.loadChunks(chunkIDs)
	if err != nil {
		return "", fmt.Errorf("load chunks: %w", err)
	}
	var lines []legalHistoryLine
	seen := map[string]bool{}
	add := func(l *legalHistoryLine) {
		if l != nil && !seen[l.key] && len(lines) < legalHistoryMaxLines {
			seen[l.key] = true
			lines = append(lines, *l)
		}
	}
	var named []legalHistoryChunk
	var namedIDs []string
	for _, c := range chunks {
		if len(c.Legal.Amends) > 0 {
			for _, a := range c.Legal.Amends {
				add(h.forward(c, a))
			}
			continue
		}
		if h.names(c.Doc.Number, c.ID) {
			named = append(named, c)
			namedIDs = append(namedIDs, c.ID)
		}
	}
	if len(named) > 0 {
		load, _, err := resolveLegalChunks(ctx, db, kbIDs, namedIDs, now)
		if err != nil {
			return "", err
		}
		byID := map[string]LegalProvisionChunk{}
		for _, c := range load.Chunks {
			byID[c.ID] = c
		}
		for _, c := range named {
			pc, ok := byID[c.ID]
			if !ok {
				continue
			}
			for _, l := range h.resolvedLines(c, pc, load.Edges[legalDocKey(pc.Doc)]) {
				add(l)
			}
		}
		for _, l := range h.phraseLines() {
			add(&l)
		}
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if len(lines) > 0 {
		var bases []string
		for _, l := range lines {
			bases = append(bases, strings.SplitN(l.key, "|", 2)[0])
		}
		if expired := fullyExpiredDocs(ctx, h.db, bases); len(expired) > 0 {
			kept := lines[:0]
			for _, l := range lines {
				if _, gone := expired[vietnamese_legal.CatalogNumberKey(strings.SplitN(l.key, "|", 2)[0])]; !gone {
					kept = append(kept, l)
				}
			}
			lines = kept
		}
	}
	if len(lines) == 0 {
		return "", nil
	}
	var b strings.Builder
	b.WriteString("\n\n---\n**Lịch sử sửa đổi**\n")
	for _, l := range lines {
		b.WriteString("- " + l.text + "\n")
	}
	return b.String(), nil
}

// resolvedLines are the history lines of a named chunk c of A from the
// edges on A: the newest rewrite per provision the answer relies on
// (as backward did), and the phrase changes the answer repeats (as
// backwardPhrases did, collected for phraseLines). Dates are effective
// dates only; an edge without one says so.
func (h *legalHistory) resolvedLines(c legalHistoryChunk, pc LegalProvisionChunk, edges []LegalAmendEdge) []*legalHistoryLine {
	units := legalUnits(pc.Content, pc.Legal)
	inChunk := func(p LegalProvisionRef) bool {
		for _, u := range units {
			if u.prov.Article != "" && p.covers(u.prov) {
				return true
			}
		}
		return false
	}
	best := map[string]*LegalAmendEdge{}
	var out []*legalHistoryLine
	for i := range edges {
		e := &edges[i]
		if e.Rejected != "" || (e.Target.CatalogID != "" && pc.Doc.CatalogID != "" && e.Target.CatalogID != pc.Doc.CatalogID) {
			continue
		}
		prov := e.Provision
		if e.Op == LegalOpBoSung && e.NewLabel != "" {
			prov = legalAddedProvision(e.Provision, e.NewLabel)
			if !inChunk(e.Provision) {
				continue
			}
		} else if !inChunk(prov) {
			continue
		}
		if e.phrase() {
			if !e.EffFrom.Known || e.EffFrom.At.After(h.now) || len(findPhrase(c.Content, e.From)) == 0 || !answerUsesPhrase(h.answer, e.From) {
				continue
			}
			if e.TextWithheld {
				// §10.2: B's new words stay out, as in the wording
				out = append(out, &legalHistoryLine{key: c.Doc.Number + "|phrase-withheld|" + e.Amender.Number + "|" + e.From,
					text: fmt.Sprintf("%s %s: cụm từ “%s” đã được sửa đổi bởi %s%s (lời văn mới không thuộc phạm vi được dùng).",
						prov.String(), c.Doc.label(), e.From, e.Amender.label(), h.effText(e.EffFrom.iso()))})
				continue
			}
			h.addPhrase(c.Doc, legalHistoryDoc{ExternalID: e.Amender.CatalogID, Number: e.Amender.Number, Type: e.Amender.Type, EffFrom: e.EffFrom.iso()},
				vietnamese_legal.AmendRef{Article: prov.Article, Khoan: prov.Khoan, Diem: prov.Diem, Phrase: true, From: e.From, To: e.To})
			continue
		}
		if e.Op != LegalOpSuaDoi && e.Op != LegalOpBoSung {
			continue
		}
		if !answerUsesProvision(h.answer, prov.Article, prov.Khoan, provisionText(c.Content, prov.Khoan)) {
			continue
		}
		key := prov.key()
		if b, ok := best[key]; ok && (!e.EffFrom.Known || (b.EffFrom.Known && !e.EffFrom.At.After(b.EffFrom.At))) {
			continue
		}
		best[key] = e
	}
	keys := make([]string, 0, len(best))
	for k := range best {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		e := best[k]
		prov := e.Provision
		verb := "đã được sửa đổi"
		if e.Op == LegalOpBoSung && e.NewLabel != "" {
			prov, verb = legalAddedProvision(e.Provision, e.NewLabel), "đã được bổ sung"
		}
		date := ", chưa rõ ngày hiệu lực"
		if e.EffFrom.Known {
			date = h.effText(e.EffFrom.iso())
		}
		text := fmt.Sprintf("%s %s: %s bởi %s%s.", prov.String(), c.Doc.label(), verb, e.Amender.label(), date)
		if !e.Tier.verifiedWording() {
			text += " (Chưa xác minh.)"
		}
		if e.QuoteText != "" && !e.TextWithheld {
			text += " Quy định mới: “" + shortQuote(e.QuoteText) + "”"
		}
		out = append(out, &legalHistoryLine{key: c.Doc.Number + "|" + prov.String(), text: text})
	}
	return out
}
