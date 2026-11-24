package service

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
	"golang.org/x/text/unicode/norm"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Relations read from a document's own text.
//
// vbpl.vn lists the relations of every văn bản quy phạm pháp luật, and the
// catalogue import copies them. A legal feed (dangcongsan.vn, Party
// documents) gives no such list, so a document pushed by a feed stood alone
// in the "Quan hệ văn bản" graph. This file reads the relations off the
// held copy's text instead, with no model call: FindCitations finds every
// số hiệu the document names, the wording of the line in front of each one
// says what the document does to it ("Căn cứ …" → based_on, "thay thế" →
// replaces, "bãi bỏ" → repeals, "sửa đổi, bổ sung" → amends, anything else
// → references), and the cited số hiệu is resolved to a catalogue row. The
// edges are stored under one source label (LegalTextRelationLabel) so a
// rebuild replaces only them and vbpl's rows are never touched.

// LegalTextRelationLabel marks the relations read from a document's text
// (types.LegalRelationSourceText: the catalogue import keeps them).
const LegalTextRelationLabel = types.LegalRelationSourceText

// legalTextRelationSelect reads the top-level text chunks of a held
// document in order; a child chunk repeats its parent's text, a summary
// chunk is the model's words and may invent a số hiệu.
const legalTextRelationSelect = `SELECT c.content FROM chunks c
WHERE c.knowledge_id = ? AND c.deleted_at IS NULL
  AND (c.parent_chunk_id IS NULL OR c.parent_chunk_id = '')
  AND c.chunk_type IN ('text', 'parent_text')
ORDER BY c.chunk_index`

// LegalTextRelationStats reports one run of BuildLegalTextRelations.
type LegalTextRelationStats struct {
	// Documents is how many catalogue documents were read.
	Documents int `json:"documents"`
	// WithoutText is how many of them hold no approved copy to read.
	WithoutText int `json:"without_text"`
	// Citations is how many số hiệu the texts named (self and repeats
	// excluded).
	Citations int `json:"citations"`
	// Unresolved is how many of those name no catalogue row.
	Unresolved int `json:"unresolved"`
	// Relations is how many edges are stored after the run.
	Relations int `json:"relations"`
}

// legalTextRelationWords map the wording in front of a citation to the
// relation. The last word found before the citation on its line wins, so
// "Bãi bỏ Quyết định 1; thay thế Quyết định 2" reads both right.
var legalTextRelationWords = []struct {
	word string
	rel  types.LegalRelation
}{
	{"thay thế", types.LegalRelationReplaces},
	{"bãi bỏ", types.LegalRelationRepeals},
	{"sửa đổi", types.LegalRelationAmends},
	{"bổ sung", types.LegalRelationAmends},
	{"căn cứ", types.LegalRelationBasedOn},
	{"thực hiện", types.LegalRelationBasedOn},
}

// legalTextCitationKinds are the citation kinds a catalogue row can be
// looked up by.
var legalTextCitationKinds = map[vietnamese_legal.CitationKind]bool{
	vietnamese_legal.CitationFull: true, vietnamese_legal.CitationAdmin: true,
	vietnamese_legal.CitationParty: true, vietnamese_legal.CitationStandard: true,
}

// LegalTextCitation is one resolved mention: the cited key, the relation
// the wording gives it and the line it was read from (for tests and logs).
type LegalTextCitation struct {
	Key      string
	Relation types.LegalRelation
	Line     string
}

// LegalTextCitations reads the relations a document's text states. ownKey
// is the document's own number key (its header names it; skipped).
func LegalTextCitations(text, ownKey string) []LegalTextCitation {
	text = norm.NFC.String(text)
	runes := []rune(text)
	transitional := legalTextTransitionalLines(runes)
	var out []LegalTextCitation
	at := map[string]int{}
	// prevEnd / prevRel: the citation before this one on the same line; a
	// list ("thay thế Quyết định 1, Quyết định 2 và Quyết định 3") carries
	// its wording to every member.
	prevEnd, prevRel := -1, types.LegalRelationReferences
	// lineNo counts the non-blank lines before scanned, the header test
	lineNo, scanned, blank := 0, 0, true
	for _, c := range vietnamese_legal.FindCitations(text) {
		if !legalTextCitationKinds[c.Kind] || c.Key == "" {
			continue
		}
		lineStart := c.Start
		for lineStart > 0 && runes[lineStart-1] != '\n' {
			lineStart--
		}
		for ; scanned < lineStart; scanned++ {
			switch {
			case runes[scanned] == '\n':
				if !blank {
					lineNo++
				}
				blank = true
			case !unicode.IsSpace(runes[scanned]):
				blank = false
			}
		}
		lineEnd := c.End
		for lineEnd < len(runes) && runes[lineEnd] != '\n' {
			lineEnd++
		}
		from, inList := lineStart, false
		if prevEnd >= lineStart && prevEnd <= c.Start {
			from, inList = prevEnd, true
		}
		gap := string(runes[from:c.Start])
		rel, found := legalTextRelationOf(gap, lineNo < legalTextHeaderLines)
		if !found && inList && legalTextListGap(gap) {
			rel = prevRel
		}
		// a plain mention inside "Điều khoản chuyển tiếp" names the document
		// this one takes over from (resolve keeps it only for a document of
		// the same type issued earlier)
		if rel == types.LegalRelationReferences && transitional[lineStart] {
			rel = types.LegalRelationSucceeds
		}
		prevEnd, prevRel = c.End, rel
		if c.Key == ownKey {
			continue
		}
		cite := LegalTextCitation{Key: c.Key, Relation: rel, Line: strings.TrimSpace(string(runes[lineStart:lineEnd]))}
		// one entry per key: the strongest wording wins over a plain mention
		if i, ok := at[c.Key]; ok {
			if legalTextRelationRank(cite.Relation) < legalTextRelationRank(out[i].Relation) {
				out[i] = cite
			}
			continue
		}
		at[c.Key] = len(out)
		out = append(out, cite)
	}
	return out
}

// legalTextArticleRe matches an article heading line ("Điều 39. Điều khoản
// chuyển tiếp", "#### Điều 39: …"); group 1 is the heading's wording.
var legalTextArticleRe = regexp.MustCompile(`(?i)^[#*\s]*điều\s+\d{1,4}[a-z]?\s*[.:]?\s*(.*)$`)

// legalTextSectionRe matches the start of a higher division or an annex,
// which ends the article before it.
var legalTextSectionRe = regexp.MustCompile(`(?i)^[#*\s]*(chương|mục|phần|phụ lục)\b`)

// legalTextTransitionalLines marks, by the rune offset of its first
// character, every line of a "Điều khoản chuyển tiếp" article (from its
// heading to the next article, division or annex). Such an article names
// the documents whose regime the new one takes over ("hoàn thành việc …
// theo quy định tại Nghị định số 85/2016/NĐ-CP"), with no "thay thế"
// wording for legalTextRelationOf to read.
func legalTextTransitionalLines(runes []rune) map[int]bool {
	out := map[int]bool{}
	in := false
	for start := 0; start < len(runes); {
		end := start
		for end < len(runes) && runes[end] != '\n' {
			end++
		}
		line := strings.TrimSpace(string(runes[start:end]))
		switch {
		case legalTextSectionRe.MatchString(line):
			in = false
		case legalTextArticleRe.MatchString(line):
			heading := strings.ToLower(legalTextArticleRe.FindStringSubmatch(line)[1])
			in = strings.Contains(heading, "chuyển tiếp")
		}
		if in {
			out[start] = true
		}
		start = end + 1
	}
	return out
}

// legalTextListGapRe strips the date that may follow a list member
// ("Quy định số 24-QĐ/TW, ngày 30/7/2021 của Ban Chấp hành Trung ương; Quy
// định số 22-QĐ/TW"); legalTextDropIssuer takes the issuer.
var legalTextListGapRe = regexp.MustCompile(`(?i)ngày\s+\d{1,2}\s*[/-]\s*\d{1,2}\s*[/-]\s*\d{2,4}|` +
	`ngày\s+\d{1,2}\s+tháng\s+\d{1,2}\s+năm\s+\d{4}`)

// legalTextIssuerStops end an issuer's name after "của": what follows is
// about the document, not who issued it ("của Bộ Giáo dục và Đào tạo về
// việc sửa đổi …", "của Bộ Chính trị khoá XI về chức năng").
var legalTextIssuerStops = map[string]bool{
	"về": true, "việc": true, "kèm": true, "theo": true, "tại": true, "trong": true, "quy": true,
	"hướng": true, "khoá": true, "khóa": true, "đã": true, "được": true, "để": true, "nhằm": true, "là": true,
	"có": true, "sửa": true, "bổ": true, "thay": true, "bãi": true,
}

var legalTextIssuerRe = regexp.MustCompile(`(?i)của\s+`)

// legalTextDropIssuer removes "của <issuer>" from a lower-cased gap: the
// words after "của" up to a separator or a stop word.
func legalTextDropIssuer(s string) string {
	var b strings.Builder
	for {
		loc := legalTextIssuerRe.FindStringIndex(s)
		if loc == nil {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:loc[0]])
		b.WriteByte(' ')
		rest := s[loc[1]:]
		// consume the issuer's words
		i := 0
		for i < len(rest) {
			j := i
			for j < len(rest) && !strings.ContainsRune(" \t,;.:()", rune(rest[j])) {
				j++
			}
			if j == i {
				break
			}
			if legalTextIssuerStops[rest[i:j]] {
				break
			}
			i = j
			for i < len(rest) && (rest[i] == ' ' || rest[i] == '\t') {
				i++
			}
		}
		s = rest[i:]
	}
}

var legalTextListGapSplitRe = regexp.MustCompile(`[\s,;:()]+`)

// legalTextListGapWords are the words that may sit between two members of
// one list of documents: "và", "các", the type word, "số". A two-word type
// is listed by its words in order.
var legalTextListGapWords = map[string]bool{
	"và": true, "các": true, "số": true, "ký": true, "hiệu": true,
	"luật": true, "bộ": true, "pháp": true, "lệnh": true, "nghị": true, "định": true, "quyết": true, "quy": true,
	"chế": true, "chỉ": true, "thị": true, "kết": true, "luận": true, "hướng": true, "dẫn": true, "thông": true,
	"tư": true, "báo": true, "kế": true, "hoạch": true, "công": true, "văn": true, "chương": true, "trình": true,
	"đề": true, "án": true, "điều": true, "lệ": true, "liên": true, "tịch": true,
}

// legalTextListGap reports whether gap only separates two members of one
// list of documents; anything else is other wording.
func legalTextListGap(gap string) bool {
	rest := legalTextDropIssuer(legalTextListGapRe.ReplaceAllString(strings.ToLower(norm.NFC.String(gap)), " "))
	for _, w := range legalTextListGapSplitRe.Split(rest, -1) {
		if w != "" && !legalTextListGapWords[w] {
			return false
		}
	}
	return true
}

// legalTextHeaderLines is how many lines of text the title takes: an
// amending document names what it amends in its title ("Quy định … sửa
// đổi, bổ sung một số điều của Quy định số 24-QĐ/TW"), which the held copy
// opens with. Blank lines do not count.
const legalTextHeaderLines = 2

// legalTextActionRelations are the relations only the document itself can
// state ("Quyết định này thay thế …"); a mention of someone else's act
// ("Ban Tổ chức Trung ương sửa đổi, bổ sung Hướng dẫn số …", "vừa được bổ
// sung theo Luật số …") is a plain reference.
var legalTextActionRelations = map[types.LegalRelation]bool{
	types.LegalRelationReplaces: true, types.LegalRelationRepeals: true, types.LegalRelationAmends: true,
}

var legalTextClauseSplitRe = regexp.MustCompile(`[;.:]`)

// legalTextRelationOf reads the wording in front of a citation; found is
// false when no word applies (a plain mention). The last word before the
// citation wins. An action word counts when the document is its subject
// (the clause before it ends in "này"), when it opens the clause ("Bãi bỏ
// Quyết định …", "Điều 3. Thay thế …") or when the citation sits in the
// header; "thực hiện" counts only when it opens the clause ("Thực hiện Kết
// luận số …, Ban … hướng dẫn"); "căn cứ" always does.
func legalTextRelationOf(before string, header bool) (rel types.LegalRelation, found bool) {
	s := strings.ToLower(norm.NFC.String(before))
	type hit struct {
		at, end int
		rel     types.LegalRelation
		word    string
	}
	var hits []hit
	for _, w := range legalTextRelationWords {
		for from := 0; ; {
			at := strings.Index(s[from:], w.word)
			if at < 0 {
				break
			}
			at += from
			end := at + len(w.word)
			from = end
			// a whole word: "theo" must not match "theo dõi" nor "kiểm theo"
			if (at > 0 && legalTextWordRune(rune(s[at-1]))) || (end < len(s) && legalTextWordRune(rune(s[end]))) {
				continue
			}
			hits = append(hits, hit{at, end, w.rel, w.word})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].at > hits[j].at })
	for _, h := range hits {
		if h.rel == types.LegalRelationBasedOn && h.word == "căn cứ" {
			return h.rel, true
		}
		// the clause in front of the word
		parts := legalTextClauseSplitRe.Split(s[:h.at], -1)
		clause := strings.TrimSpace(parts[len(parts)-1])
		opens := legalTextListGap(clause) || legalTextClauseHeadRe.MatchString(clause)
		if legalTextActionRelations[h.rel] {
			if opens || header || strings.HasSuffix(clause, " này") || clause == "này" {
				return h.rel, true
			}
			continue
		}
		if opens {
			return h.rel, true
		}
	}
	return types.LegalRelationReferences, false
}

// legalTextClauseHeadRe is a clause number or bullet with nothing after it.
var legalTextClauseHeadRe = regexp.MustCompile(`^(?:[-+•*]|\d{1,3}\.?|[a-zđ]\)|điều\s+\d{1,3}\.?|khoản\s+\d{1,3}\.?)?$`)

func legalTextWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r >= 0x80 }

// legalTextRelationRank: lower is stronger; references is the weakest.
func legalTextRelationRank(r types.LegalRelation) int {
	if n, ok := legalRelationRank[r]; ok {
		return n
	}
	return 100
}

// legalTextRelationBuilder resolves and stores the text relations of
// catalogue documents.
type legalTextRelationBuilder struct {
	db *gorm.DB
}

// BuildLegalTextRelations reads the text relations of the catalogue
// documents docIDs (all documents of source when docIDs is empty) and
// replaces their LegalTextRelationLabel edges. Only documents holding an
// approved copy in a knowledge base have text to read; the others count in
// WithoutText and keep whatever edges they had.
func BuildLegalTextRelations(ctx context.Context, db *gorm.DB, source string, docIDs []string) (*LegalTextRelationStats, error) {
	if db == nil {
		return nil, fmt.Errorf("database is not available")
	}
	b := &legalTextRelationBuilder{db: db}
	stats := &LegalTextRelationStats{}
	var docs []*types.LegalDocument
	q := db.WithContext(ctx).Model(&types.LegalDocument{})
	switch {
	case len(docIDs) > 0:
		q = q.Where("id IN ?", docIDs)
	case strings.TrimSpace(source) != "":
		q = q.Where("source = ?", source)
	default:
		return nil, fmt.Errorf("a source or document ids are required")
	}
	if err := q.Order("id").Find(&docs).Error; err != nil {
		return nil, err
	}
	stats.Documents = len(docs)
	for _, d := range docs {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		text, err := b.heldText(ctx, d.ID)
		if err != nil {
			return stats, err
		}
		if strings.TrimSpace(text) == "" {
			stats.WithoutText++
			continue
		}
		cites := LegalTextCitations(text, d.NumberKey)
		stats.Citations += len(cites)
		rels, unresolved, err := b.resolve(ctx, d, cites)
		if err != nil {
			return stats, err
		}
		stats.Unresolved += unresolved
		if err := b.store(ctx, d.ID, rels); err != nil {
			return stats, err
		}
		stats.Relations += len(rels)
	}
	return stats, nil
}

// LegalTextRelationsOfKnowledge re-reads the text relations of the catalogue
// document knowledgeID is the approved copy of, once the knowledge's text
// chunks are written (a first parse, a re-parse, a replaced file): the
// ingest hooks run before parsing has written them. A knowledge that is no
// approved copy (most of them) costs one primary-key lookup on
// legal_document_links and returns "", nil, nil; so does a nil db. docID is
// the document read.
func LegalTextRelationsOfKnowledge(ctx context.Context, db *gorm.DB, knowledgeID string,
) (docID string, stats *LegalTextRelationStats, err error) {
	if db == nil || knowledgeID == "" {
		return "", nil, nil
	}
	var ids []string
	if err := db.WithContext(ctx).Model(&types.LegalDocumentLink{}).
		Where("knowledge_id = ? AND review_status = ? AND legal_document_id <> ''", knowledgeID, types.LegalReviewApproved).
		Limit(1).Pluck("legal_document_id", &ids).Error; err != nil {
		return "", nil, err
	}
	if len(ids) == 0 {
		return "", nil, nil
	}
	stats, err = BuildLegalTextRelations(ctx, db, "", ids)
	return ids[0], stats, err
}

// SetLegalCatalogDB lets the parse re-read the text relations of the legal
// document a knowledge is the approved copy of (container wiring).
func (s *knowledgeService) SetLegalCatalogDB(db *gorm.DB) { s.legalDB = db }

// legalTextRelationsAfterParse runs LegalTextRelationsOfKnowledge once
// processChunks has written the knowledge's chunks. Best effort: a failure
// is logged and never fails the parse (the nightly rebuild catches up).
func (s *knowledgeService) legalTextRelationsAfterParse(ctx context.Context, knowledgeID string) {
	if s == nil || s.legalDB == nil {
		return
	}
	docID, stats, err := LegalTextRelationsOfKnowledge(ctx, s.legalDB, knowledgeID)
	if err != nil {
		logger.Warnf(ctx, "[LegalText] text relations after parsing %s (document %q): %v", knowledgeID, docID, err)
		return
	}
	if stats != nil {
		logger.Infof(ctx, "[LegalText] text relations of %s after parsing %s: %d cited, %d unresolved, %d stored",
			docID, knowledgeID, stats.Citations, stats.Unresolved, stats.Relations)
	}
}

// heldText is the text of the document's approved copy (the latest link
// when several knowledge bases hold one), "" when none.
func (b *legalTextRelationBuilder) heldText(ctx context.Context, docID string) (string, error) {
	var kids []string
	err := b.db.WithContext(ctx).Raw(`SELECT l.knowledge_id FROM legal_document_links l
		JOIN knowledges k ON k.id = l.knowledge_id AND k.deleted_at IS NULL
		WHERE l.legal_document_id = ? AND l.review_status = ?
		ORDER BY l.updated_at DESC`, docID, types.LegalReviewApproved).Scan(&kids).Error
	if err != nil || len(kids) == 0 {
		return "", err
	}
	var parts []string
	if err := b.db.WithContext(ctx).Raw(legalTextRelationSelect, kids[0]).Scan(&parts).Error; err != nil {
		return "", err
	}
	return strings.Join(parts, "\n"), nil
}

// resolve turns the citations into edges from doc. A key held by several
// catalogue rows (a số hiệu is not unique) goes to the row of the same
// source first, then to the newest row issued no later than doc; a key
// nobody holds is counted unresolved.
func (b *legalTextRelationBuilder) resolve(ctx context.Context, doc *types.LegalDocument, cites []LegalTextCitation,
) ([]types.LegalDocumentRelation, int, error) {
	if len(cites) == 0 {
		return nil, 0, nil
	}
	keys := make([]string, 0, len(cites))
	for _, c := range cites {
		keys = append(keys, c.Key)
	}
	var rows []*types.LegalDocument
	if err := b.db.WithContext(ctx).Where("number_key IN ?", keys).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	byKey := map[string][]*types.LegalDocument{}
	for _, r := range rows {
		byKey[r.NumberKey] = append(byKey[r.NumberKey], r)
	}
	now := time.Now()
	var out []types.LegalDocumentRelation
	unresolved := 0
	for _, c := range cites {
		target := legalTextPickTarget(doc, byKey[c.Key])
		if target == nil || target.ID == doc.ID {
			if target == nil {
				unresolved++
			}
			continue
		}
		rel := c.Relation
		if rel == types.LegalRelationSucceeds && !legalTextSucceeds(doc, target) {
			rel = types.LegalRelationReferences
		}
		// vbpl.vn lists a document's relations itself (căn cứ, dẫn chiếu,
		// thay thế …): of its documents only what the portal cannot know is
		// kept, so the graph is not doubled with text mentions.
		if doc.Source == types.LegalDocumentSourceVBPL && rel != types.LegalRelationSucceeds {
			continue
		}
		out = append(out, types.LegalDocumentRelation{FromID: doc.ID, ToID: target.ID, Relation: rel,
			SourceLabel: LegalTextRelationLabel, CreatedAt: now})
	}
	return out, unresolved, nil
}

// legalTextSucceeds reports whether doc can take over from target: the same
// kind of document ("Nghị định" → "Nghị định"; a decree does not succeed
// the law it names), issued no earlier than target (an unknown date passes),
// on the same subject (the titles share a pair of consecutive words once
// the type and boilerplate are dropped: "hệ thống thông tin"). A
// transitional clause also names documents it merely applies ("theo Luật
// Tổ chức chính quyền địa phương …"), which the subject test drops.
func legalTextSucceeds(doc, target *types.LegalDocument) bool {
	if !strings.EqualFold(strings.TrimSpace(doc.DocType), strings.TrimSpace(target.DocType)) {
		return false
	}
	if doc.IssuedOn != nil && target.IssuedOn != nil && target.IssuedOn.After(*doc.IssuedOn) {
		return false
	}
	return legalTextSameSubject(doc.Title, target.Title)
}

// legalTextTitleStop are the words of a title that say nothing about its
// subject: the document type, "quy định chi tiết … thi hành", numbers.
var legalTextTitleStop = map[string]bool{
	"luật": true, "bộ": true, "pháp": true, "lệnh": true, "nghị": true, "định": true, "quyết": true, "thông": true,
	"tư": true, "liên": true, "tịch": true, "chỉ": true, "thị": true, "quy": true, "chế": true, "văn": true, "bản": true,
	"hợp": true, "nhất": true, "số": true, "ban": true, "hành": true, "về": true, "việc": true, "của": true, "và": true,
	"các": true, "một": true, "điều": true, "chi": true, "tiết": true, "hướng": true, "dẫn": true, "thi": true,
	"biện": true, "sửa": true, "đổi": true, "bổ": true, "sung": true, "thay": true, "thế": true, "bãi": true, "bỏ": true,
	"theo": true, "tại": true, "trong": true, "đối": true, "với": true, "cho": true, "từ": true, "đến": true,
	"chính": true, "phủ": true, "thủ": true, "tướng": true, "năm": true, "ngày": true, "tháng": true,
}

var legalTextTitleSplitRe = regexp.MustCompile(`[^\p{L}]+`)

// legalTextTitleNumberRe is a số hiệu or year in a title ("331/2026/NĐ-CP",
// "QH14"): dropped whole, so its letters do not become subject words.
var legalTextTitleNumberRe = regexp.MustCompile(`\S*\d\S*`)

// legalTextTitleBigrams is the set of consecutive subject-word pairs of a
// title (the stop words removed first, so "an toàn hệ thống" and "an ninh
// mạng đối với hệ thống" both hold "hệ thống").
func legalTextTitleBigrams(title string) map[string]bool {
	var words []string
	title = legalTextTitleNumberRe.ReplaceAllString(norm.NFC.String(title), " ")
	for _, w := range legalTextTitleSplitRe.Split(strings.ToLower(title), -1) {
		if w != "" && !legalTextTitleStop[w] {
			words = append(words, w)
		}
	}
	out := map[string]bool{}
	for i := 1; i < len(words); i++ {
		out[words[i-1]+" "+words[i]] = true
	}
	return out
}

// legalTextSameSubject reports whether two titles share a subject-word pair;
// a title left without one (nothing but boilerplate) passes.
func legalTextSameSubject(a, b string) bool {
	ba, bb := legalTextTitleBigrams(a), legalTextTitleBigrams(b)
	if len(ba) == 0 || len(bb) == 0 {
		return true
	}
	for k := range ba {
		if bb[k] {
			return true
		}
	}
	return false
}

// legalTextPickTarget chooses among the rows sharing a cited key.
func legalTextPickTarget(doc *types.LegalDocument, rows []*types.LegalDocument) *types.LegalDocument {
	if len(rows) == 0 {
		return nil
	}
	cands := make([]*types.LegalDocument, 0, len(rows))
	for _, r := range rows {
		if r.ID != doc.ID {
			cands = append(cands, r)
		}
	}
	if len(cands) == 0 {
		return nil
	}
	issued := func(d *types.LegalDocument) time.Time {
		if d.IssuedOn == nil {
			return time.Time{}
		}
		return *d.IssuedOn
	}
	sort.SliceStable(cands, func(i, j int) bool {
		a, c := cands[i], cands[j]
		if (a.Source == doc.Source) != (c.Source == doc.Source) {
			return a.Source == doc.Source
		}
		// a document cites what already exists: rows issued after it last
		ai, ci := issued(a), issued(c)
		if doc.IssuedOn != nil {
			aLate, cLate := ai.After(*doc.IssuedOn), ci.After(*doc.IssuedOn)
			if aLate != cLate {
				return !aLate
			}
		}
		if !ai.Equal(ci) {
			return ai.After(ci)
		}
		return a.ID < c.ID
	})
	return cands[0]
}

// store brings the text edges of docID to rels by difference in one
// transaction: an edge no longer read is deleted, a new one inserted, an
// unchanged one left (its created_at stays). An edge the catalogue import
// already holds (same from, to, relation) is left as vbpl's.
func (b *legalTextRelationBuilder) store(ctx context.Context, docID string, rels []types.LegalDocumentRelation) error {
	return b.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var have []types.LegalDocumentRelation
		if err := tx.Select("from_id", "to_id", "relation").
			Where("from_id = ? AND source_label = ?", docID, LegalTextRelationLabel).Find(&have).Error; err != nil {
			return err
		}
		type key struct {
			to  string
			rel types.LegalRelation
		}
		want := make(map[key]bool, len(rels))
		var insert []types.LegalDocumentRelation
		for _, r := range rels {
			k := key{r.ToID, r.Relation}
			if !want[k] {
				want[k] = true
				insert = append(insert, r)
			}
		}
		held := make(map[key]bool, len(have))
		for _, h := range have {
			k := key{h.ToID, h.Relation}
			held[k] = true
			if want[k] {
				continue
			}
			if err := tx.Where("from_id = ? AND to_id = ? AND relation = ? AND source_label = ?",
				docID, h.ToID, h.Relation, LegalTextRelationLabel).Delete(&types.LegalDocumentRelation{}).Error; err != nil {
				return err
			}
		}
		fresh := insert[:0]
		for _, r := range insert {
			if !held[key{r.ToID, r.Relation}] {
				fresh = append(fresh, r)
			}
		}
		if len(fresh) == 0 {
			return nil
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&fresh).Error
	})
}

// textRelations reads the text relations of one freshly ingested document;
// best effort, the rebuild endpoint catches up on a failure.
func (s *LegalPushService) textRelations(ctx context.Context, docID string) {
	if s == nil || s.db == nil || docID == "" {
		return
	}
	stats, err := BuildLegalTextRelations(ctx, s.db, "", []string{docID})
	if err != nil {
		logger.Warnf(ctx, "[LegalPush] text relations of %s: %v", docID, err)
		return
	}
	logger.Infof(ctx, "[LegalPush] text relations of %s: %d cited, %d unresolved, %d stored",
		docID, stats.Citations, stats.Unresolved, stats.Relations)
}

// RebuildTextRelations reads the text relations of every catalogue document
// of source (SuperAdmin backfill).
func (s *LegalPushService) RebuildTextRelations(ctx context.Context, source string) (*LegalTextRelationStats, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("database is not available")
	}
	return BuildLegalTextRelations(ctx, s.db, source, nil)
}
