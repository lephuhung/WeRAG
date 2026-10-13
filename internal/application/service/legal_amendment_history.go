package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// Amendment history appended to legal answers ("Lịch sử sửa đổi"): for the
// retrieved provisions an answer relies on, the rule before and after a
// change, with the amending document and its effective date. It is built
// from the amendment metadata stored at ingest (vbpl converter, chunk
// legal.amends) and the knowledge metadata (doc_number, eff_from), so the
// user always sees both texts and the dates are never inferred by a model.

const (
	legalHistoryMaxLines   = 4
	legalHistoryQuoteRunes = 260
	legalHistoryTimeout    = 4 * time.Second
	// legalHistoryCandidates bounds the amending chunks read per provision.
	legalHistoryCandidates = 40
)

var legalDocNumberRe = regexp.MustCompile(`\d+/\d{4}/[A-Za-zĐđ0-9\-]+`)

// legalHistoryChunk is a retrieved chunk with its document's identity.
type legalHistoryChunk struct {
	ID          string
	KnowledgeID string
	Content     string
	Legal       *vietnamese_legal.LegalMetadata
	Doc         legalHistoryDoc
}

type legalHistoryDoc struct {
	ExternalID string // vbpl id = legal_documents.id
	Number     string // "75/2019/NĐ-CP"
	IssueDate  string // "2026-03-31"
	Type       string // "Nghị định"
	EffFrom    string // "2026-05-20"
}

func (d legalHistoryDoc) label() string {
	if d.Type == "" {
		return d.Number
	}
	return d.Type + " " + d.Number
}

// legalHistoryLine is one provision's change.
type legalHistoryLine struct {
	key  string // amended doc number + provision, for dedupe
	text string
}

// LegalAmendmentHistory returns the "Lịch sử sửa đổi" block for the chunks
// an agent retrieved, limited to documents the answer names and to the
// knowledge bases kbIDs; "" when nothing applies.
func LegalAmendmentHistory(
	ctx context.Context, db *gorm.DB, kbIDs, chunkIDs []string, answer string, now time.Time,
) string {
	if db == nil || len(kbIDs) == 0 || len(chunkIDs) == 0 || strings.TrimSpace(answer) == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, legalHistoryTimeout)
	defer cancel()
	h := &legalHistory{amended: map[string]string{}, ctx: ctx, db: db.WithContext(ctx), kbIDs: kbIDs, answer: answer, now: now}
	chunks, err := h.loadChunks(chunkIDs)
	if err != nil {
		logger.Warnf(ctx, "[LegalHistory] load chunks: %v", err)
		return ""
	}
	var lines []legalHistoryLine
	seen := map[string]bool{}
	add := func(l *legalHistoryLine) {
		if l != nil && !seen[l.key] && len(lines) < legalHistoryMaxLines {
			seen[l.key] = true
			lines = append(lines, *l)
		}
	}
	for _, c := range chunks {
		if len(c.Legal.Amends) > 0 {
			for _, a := range c.Legal.Amends {
				add(h.forward(c, a))
			}
			continue
		}
		for _, l := range h.backward(c) {
			add(l)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n---\n**Lịch sử sửa đổi**\n")
	for _, l := range lines {
		b.WriteString("- " + l.text + "\n")
	}
	return b.String()
}

type legalHistory struct {
	// amended caches amendedNumber by amending document and name.
	amended map[string]string
	ctx     context.Context
	db      *gorm.DB
	kbIDs   []string
	answer  string
	now     time.Time
}

type legalHistoryRow struct {
	ID          string
	KnowledgeID string
	Content     string
	Metadata    []byte
	KMetadata   []byte
}

const legalHistorySelect = `SELECT c.id, c.knowledge_id, c.content, c.metadata, k.metadata AS k_metadata
FROM chunks c JOIN knowledges k ON k.id = c.knowledge_id
WHERE c.deleted_at IS NULL AND k.deleted_at IS NULL AND c.knowledge_base_id IN ?`

// loadChunks reads the retrieved chunks that carry legal structure.
func (h *legalHistory) loadChunks(ids []string) ([]legalHistoryChunk, error) {
	var rows []legalHistoryRow
	if err := h.db.Raw(legalHistorySelect+` AND c.id IN ?`, h.kbIDs, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	order := make(map[string]int, len(ids))
	for i, id := range ids {
		order[id] = i
	}
	chunks := parseLegalHistoryRows(rows)
	sort.SliceStable(chunks, func(i, j int) bool { return order[chunks[i].ID] < order[chunks[j].ID] })
	return chunks, nil
}

func parseLegalHistoryRows(rows []legalHistoryRow) []legalHistoryChunk {
	var out []legalHistoryChunk
	for _, r := range rows {
		var meta types.DocumentChunkMetadata
		if json.Unmarshal(r.Metadata, &meta) != nil || meta.Legal == nil || len(meta.Legal.ArticleNos) == 0 {
			continue
		}
		var km map[string]interface{}
		_ = json.Unmarshal(r.KMetadata, &km)
		str := func(k string) string { v, _ := km[k].(string); return strings.TrimSpace(v) }
		doc := legalHistoryDoc{ExternalID: str("external_id"), Number: str("doc_number"), IssueDate: str("issue_date"), Type: str("doc_type"), EffFrom: str("eff_from")}
		if doc.Number == "" {
			continue
		}
		out = append(out, legalHistoryChunk{
			ID: r.ID, KnowledgeID: r.KnowledgeID, Content: r.Content, Legal: meta.Legal, Doc: doc,
		})
	}
	return out
}

// forward: the chunk is amending text — show the rule it replaced.
func (h *legalHistory) forward(c legalHistoryChunk, a vietnamese_legal.AmendRef) *legalHistoryLine {
	old := h.amendedNumber(c.Doc, a.Document)
	if old == "" || a.Article == "" || !(mentionsDoc(h.answer, old) || h.names(c.Doc.Number, c.ID)) {
		return nil
	}
	prov := vietnamese_legal.AmendRef{Article: a.Article, Khoan: a.Khoan, Diem: a.Diem}.String()
	if !answerUsesProvision(h.answer, a.Article, a.Khoan, quotedProvision(c.Content, a.Khoan)) {
		return nil
	}
	oldDoc := legalHistoryDoc{Number: old}
	var before string
	if oc, ok := h.provisionChunk(old, a.Article, a.Khoan); ok {
		oldDoc = oc.Doc
		before = provisionText(oc.Content, a.Khoan)
	}
	text := fmt.Sprintf("%s %s: được sửa đổi bởi %s%s.", prov, oldDoc.label(), c.Doc.label(), h.dateText(c.Doc))
	if before != "" {
		text += " Quy định trước đây: “" + before + "”"
	}
	return &legalHistoryLine{key: old + "|" + prov, text: text}
}

// backward: the chunk is a provision of a document that a newer one
// amended — show the current rule.
func (h *legalHistory) backward(c legalHistoryChunk) []*legalHistoryLine {
	if !h.names(c.Doc.Number, c.ID) || len(c.Legal.ArticleNos) != 1 {
		return nil
	}
	article := c.Legal.ArticleNos[0]
	filter, _ := json.Marshal([]map[string]string{{"article": article}})
	var rows []legalHistoryRow
	// Common article numbers are amended in many documents: first those
	// naming this document's số hiệu, then those naming a document by title.
	if err := h.db.Raw(legalHistorySelect+` AND c.knowledge_id <> ?
		AND c.metadata->'legal'->'amends' @> ?::jsonb
		ORDER BY strpos(c.metadata->'legal'->>'amends', ?) > 0 DESC,
			c.metadata->'legal'->>'amends' !~ '[0-9]+/[0-9]{4}' DESC
		LIMIT ?`,
		h.kbIDs, c.KnowledgeID, string(filter), c.Doc.Number, legalHistoryCandidates).Scan(&rows).Error; err != nil {
		logger.Warnf(h.ctx, "[LegalHistory] amending chunks: %v", err)
		return nil
	}
	khoans := map[string]bool{}
	for _, k := range c.Legal.KhoanNos {
		khoans[k] = true
	}
	// newest amendment per provision
	type cand struct {
		amender legalHistoryDoc
		amend   vietnamese_legal.AmendRef
		content string
	}
	best := map[string]cand{}
	for _, ac := range parseLegalHistoryRows(rows) {
		for _, a := range ac.Legal.Amends {
			if a.Article != article || h.amendedNumber(ac.Doc, a.Document) != c.Doc.Number {
				continue
			}
			if a.Khoan != "" && len(khoans) > 0 && !khoans[a.Khoan] {
				continue
			}
			if !answerUsesProvision(h.answer, a.Article, a.Khoan, provisionText(c.Content, a.Khoan)) {
				continue
			}
			prov := vietnamese_legal.AmendRef{Article: a.Article, Khoan: a.Khoan, Diem: a.Diem}.String()
			if b, ok := best[prov]; ok && b.amender.EffFrom >= ac.Doc.EffFrom {
				continue
			}
			best[prov] = cand{ac.Doc, a, ac.Content}
		}
	}
	provs := make([]string, 0, len(best))
	for p := range best {
		provs = append(provs, p)
	}
	sort.Strings(provs)
	var lines []*legalHistoryLine
	for _, prov := range provs {
		b := best[prov]
		text := fmt.Sprintf("%s %s: đã được sửa đổi bởi %s%s.", prov, c.Doc.label(), b.amender.label(), h.dateText(b.amender))
		if now := quotedProvision(b.content, b.amend.Khoan); now != "" {
			text += " Quy định mới: “" + now + "”"
		}
		lines = append(lines, &legalHistoryLine{key: c.Doc.Number + "|" + prov, text: text})
	}
	return lines
}

// provisionChunk finds, in the searched KBs, the chunk holding Điều article
// (and khoản khoan) of document number.
func (h *legalHistory) provisionChunk(number, article, khoan string) (legalHistoryChunk, bool) {
	q := legalHistorySelect + ` AND k.metadata->>'doc_number' = ? AND jsonb_exists(c.metadata->'legal'->'article_nos', ?)`
	args := []interface{}{h.kbIDs, number, article}
	if khoan != "" {
		q += ` AND jsonb_exists(c.metadata->'legal'->'khoan_nos', ?)`
		args = append(args, khoan)
	}
	var rows []legalHistoryRow
	if err := h.db.Raw(q+` LIMIT 1`, args...).Scan(&rows).Error; err != nil || len(rows) == 0 {
		return legalHistoryChunk{}, false
	}
	cs := parseLegalHistoryRows(rows)
	if len(cs) == 0 {
		return legalHistoryChunk{}, false
	}
	return cs[0], true
}

// dateText is the amending document's effective date, else — when vbpl
// gives none — its issue date, worded as such.
func (h *legalHistory) dateText(d legalHistoryDoc) string {
	if t := h.effText(d.EffFrom); t != "" {
		return t
	}
	if t, err := time.Parse("2006-01-02", d.IssueDate); err == nil {
		return ", ban hành ngày " + t.Format("02/01/2006")
	}
	return ""
}

func (h *legalHistory) effText(effFrom string) string {
	t, err := time.ParseInLocation("2006-01-02", effFrom, h.now.Location())
	if err != nil {
		return ""
	}
	if t.After(h.now) {
		return ", sẽ có hiệu lực từ " + t.Format("02/01/2006")
	}
	return ", có hiệu lực từ " + t.Format("02/01/2006")
}

// names reports whether the answer names document number or cites its
// chunk chunkID (a citation tag may stand in for the số hiệu, as in "Luật
// Giáo dục 2019 <kb … chunk_id=…/>").
func (h *legalHistory) names(number, chunkID string) bool {
	return mentionsDoc(h.answer, number) || (chunkID != "" && strings.Contains(h.answer, `chunk_id="`+chunkID+`"`))
}

// mentionsDoc reports whether text names document number ("75/2019"
// matches "Nghị định 75/2019/NĐ-CP").
func mentionsDoc(text, number string) bool {
	parts := strings.SplitN(number, "/", 3)
	if len(parts) < 2 {
		return false
	}
	short := parts[0] + "/" + parts[1]
	for i := 0; ; {
		j := strings.Index(text[i:], short)
		if j < 0 {
			return false
		}
		j += i
		// not part of a longer number ("175/2019")
		if j == 0 || text[j-1] < '0' || text[j-1] > '9' {
			return true
		}
		i = j + len(short)
	}
}

// legalFigureRe finds the figures an answer repeats from a provision:
// amounts ("20.000.000"), rates ("5%") and periods ("03 tháng").
var (
	legalFigureRe = regexp.MustCompile(`\d{1,3}(?:\.\d{3})+|\d+(?:,\d+)?\s*%|\d+\s*(?:ngày|tháng|năm|tuổi|giờ)`)
	legalAmountRe = regexp.MustCompile(`\d{1,3}(?:\.\d{3})+`)
)

// answerUsesProvision reports whether the answer relies on a provision:
// it names "khoản K Điều A", repeats the provision's leading figures
// ("03 tháng" also as "3 tháng"), or — for a provision without figures —
// repeats a run of its wording.
func answerUsesProvision(answer, article, khoan, text string) bool {
	low := strings.ToLower(answer)
	dieu := "điều " + strings.ToLower(article)
	if khoan != "" {
		k := "khoản " + strings.ToLower(khoan)
		for _, form := range []string{k + " " + dieu, k + ", " + dieu, dieu + " " + k, dieu + ", " + k} {
			if strings.Contains(low, form) {
				return true
			}
		}
	}
	// A provision with a fine counts only when its leading amounts — "từ X
	// đồng đến Y đồng" — are all repeated: one shared bound or the
	// boilerplate around it is not enough (khoản 3 "30–50 triệu" next to
	// khoản 2 "20–30 triệu", both "đối với bên bị điều tra…").
	if amounts := legalAmountRe.FindAllString(text, 2); len(amounts) > 0 {
		return repeatsAll(answer, amounts)
	}
	// Other figures (ages, periods, rates) or the wording.
	if figures := legalFigureRe.FindAllString(text, 2); len(figures) > 0 && repeatsAll(answer, figures) {
		return true
	}
	return sharesPhrase(low, strings.ToLower(text), legalPhraseWords)
}

func repeatsAll(answer string, figures []string) bool {
	for _, f := range figures {
		if !strings.Contains(answer, f) && !strings.Contains(answer, strings.TrimLeft(f, "0")) {
			return false
		}
	}
	return true
}

// legalPhraseWords is the run of words an answer must share with a
// provision's wording to count as quoting it.
const legalPhraseWords = 8

// sharesPhrase reports whether a and b share n consecutive words.
func sharesPhrase(a, b string, n int) bool {
	words := strings.FieldsFunc(b, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(words) < n {
		return false
	}
	norm := " " + strings.Join(strings.FieldsFunc(a, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }), " ") + " "
	for i := 0; i+n <= len(words); i++ {
		if strings.Contains(norm, " "+strings.Join(words[i:i+n], " ")+" ") {
			return true
		}
	}
	return false
}

var (
	khoanStartRe = regexp.MustCompile(`(?m)^\s*(?:> )?(\d+[a-zđ]?)\.\s`)
	headingRe    = regexp.MustCompile(`(?m)^#+ .*$`)
)

// provisionText is the khoản of an article chunk ("" = the whole article
// text), trimmed to a quote.
func provisionText(content, khoan string) string {
	content = headingRe.ReplaceAllString(content, "")
	if khoan == "" {
		return shortQuote(content)
	}
	locs := khoanStartRe.FindAllStringSubmatchIndex(content, -1)
	for i, l := range locs {
		if content[l[2]:l[3]] != khoan {
			continue
		}
		end := len(content)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		return shortQuote(content[l[1]:end])
	}
	return ""
}

// quotedProvision is the amending text quoted for khoản khoan ("“2. …”").
func quotedProvision(content, khoan string) string {
	for _, q := range strings.Split(content, "“")[1:] {
		q = strings.SplitN(q, "”", 2)[0]
		if khoan == "" {
			return shortQuote(q)
		}
		if m := khoanStartRe.FindStringSubmatchIndex(q); m != nil && m[0] == 0 && q[m[2]:m[3]] == khoan {
			return shortQuote(q[m[1]:])
		}
	}
	return ""
}

func shortQuote(s string) string {
	s = strings.Join(strings.Fields(strings.ReplaceAll(s, "> ", " ")), " ")
	if utf8.RuneCountInString(s) <= legalHistoryQuoteRunes {
		return s
	}
	r := []rune(s)[:legalHistoryQuoteRunes]
	if k := strings.LastIndexByte(string(r), ' '); k > legalHistoryQuoteRunes/2 {
		return string(r)[:k] + "…"
	}
	return string(r) + "…"
}

// legalHistoryTarget is a catalogue document an amendment may name.
type legalHistoryTarget struct {
	DocumentNumber string
	Title          string
}

// amendedNumber is the số hiệu of the document an amendment names: written
// in the text ("Nghị định số 75/2019/NĐ-CP"), else — when the text names it
// only by title ("Luật Giáo dục") — the catalogue document of exactly that
// title (not "Luật Giáo dục đại học"): one the amending document is
// recorded to amend, else the latest one issued before it and not fully
// expired.
func (h *legalHistory) amendedNumber(amender legalHistoryDoc, name string) string {
	if n := legalDocNumberRe.FindString(name); n != "" {
		return n
	}
	name = strings.ToLower(strings.Join(strings.Fields(name), " "))
	if name == "" {
		return ""
	}
	key := amender.Number + "|" + name
	if n, ok := h.amended[key]; ok {
		return n
	}
	n := h.lookupAmended(amender, name)
	h.amended[key] = n
	return n
}

func (h *legalHistory) lookupAmended(amender legalHistoryDoc, name string) string {
	var targets []legalHistoryTarget
	if amender.ExternalID != "" {
		if err := h.db.Raw(`SELECT t.document_number, t.title FROM legal_document_relations r
			JOIN legal_documents t ON t.id = r.to_id
			WHERE r.from_id = ? AND r.relation IN ('amends', 'replaces', 'repeals')`, amender.ExternalID).
			Scan(&targets).Error; err != nil {
			logger.Warnf(h.ctx, "[LegalHistory] catalogue targets: %v", err)
		}
	}
	for _, t := range targets {
		if titleBase(t.Title) == name {
			return t.DocumentNumber
		}
	}
	issued := amender.IssueDate
	if issued == "" {
		issued = "9999-12-31"
	}
	targets = nil
	if err := h.db.Raw(`SELECT document_number, title FROM legal_documents
		WHERE lower(title) LIKE ? AND coalesce(status, '') NOT LIKE 'Hết hiệu lực toàn bộ%'
		AND issued_on < ? ORDER BY issued_on DESC LIMIT 10`, name+" số %", issued).
		Scan(&targets).Error; err != nil {
		logger.Warnf(h.ctx, "[LegalHistory] catalogue by title: %v", err)
		return ""
	}
	for _, t := range targets {
		if titleBase(t.Title) == name {
			return t.DocumentNumber
		}
	}
	return ""
}

// titleBase is a catalogue title without its số hiệu, lower-cased: "Luật
// Giáo dục số 43/2019/QH14" → "luật giáo dục".
func titleBase(title string) string {
	title = strings.ToLower(strings.Join(strings.Fields(title), " "))
	if i := strings.Index(title, " số "); i > 0 {
		return title[:i]
	}
	return title
}
