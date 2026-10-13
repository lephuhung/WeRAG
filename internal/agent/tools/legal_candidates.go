package tools

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// A legal reference the user remembers only in part — "Nghị định 53" fits
// 25 decrees — is answered with a card of clickable candidates (full số
// hiệu, title, status) instead of a question they would have to type an
// answer to. Picking one re-sends the question with the full number. The
// gate runs in code before the agent (see service.legalCandidatesClarification);
// check_legal_status attaches the same card when its lookup is ambiguous.

// LegalCandidatesType is the display_type (and pipeline tool name) of the card.
const LegalCandidatesType = "legal_candidates"

// legalCandidatesLimit caps the candidates shown; the total is reported.
const legalCandidatesLimit = 12

// LegalReference is a document type and a bare number found in a request.
type LegalReference struct {
	// DocType is the catalogue doc_type ("Nghị định").
	DocType string
	// Number is the bare number ("53").
	Number string
	// Text is the reference as written ("Nghị định số 53"), replaced by the
	// full số hiệu when the question is re-sent.
	Text string
}

var legalTypeWords = []struct{ word, docType string }{
	{"thông tư liên tịch", "Thông tư liên tịch"},
	{"nghị định", "Nghị định"},
	{"nghị quyết", "Nghị quyết"},
	{"thông tư", "Thông tư"},
	{"quyết định", "Quyết định"},
	{"chỉ thị", "Chỉ thị"},
	{"pháp lệnh", "Pháp lệnh"},
	{"luật", "Luật"},
}

// bareLegalRefRe matches "<type> [số] <1-3 digits>"; what follows decides
// whether the number is bare (see ParseBareLegalReference).
var bareLegalRefRe = regexp.MustCompile(`(?i)(thông tư liên tịch|nghị định|nghị quyết|thông tư|quyết định|chỉ thị|pháp lệnh|luật)\s*(?:số\s*)?(\d{1,3})`)

var yearAfterRe = regexp.MustCompile(`^\s*(?:năm\s*)?(?:19|20)\d\d\b`)

// ParseBareLegalReference finds the first document reference whose number
// has no year or suffix ("Nghị định 53", "Thông tư số 01"); false for a
// full số hiệu ("53/2022/NĐ-CP"), a number with its year ("Nghị định 30
// năm 2020") or no reference at all.
func ParseBareLegalReference(query string) (LegalReference, bool) {
	// clients may send decomposed text (ị = i + combining dot): the type
	// words of the pattern are composed
	query = norm.NFC.String(query)
	for _, m := range bareLegalRefRe.FindAllStringSubmatchIndex(query, -1) {
		rest := query[m[1]:]
		if rest != "" {
			c := rest[0]
			if c == '/' || c == '-' || c == '.' || (c >= '0' && c <= '9') {
				continue
			}
			if yearAfterRe.MatchString(rest) {
				continue
			}
		}
		word := strings.ToLower(query[m[2]:m[3]])
		docType := ""
		for _, t := range legalTypeWords {
			if strings.EqualFold(t.word, word) {
				docType = t.docType
				break
			}
		}
		if docType == "" {
			continue
		}
		return LegalReference{DocType: docType, Number: strings.TrimLeft(query[m[4]:m[5]], "0"), Text: query[m[0]:m[1]]}, true
	}
	return LegalReference{}, false
}

// LegalCandidatesCard is the card: the request, the reference and the
// documents it may mean.
type LegalCandidatesCard struct {
	Query      string
	Reference  LegalReference
	Total      int
	Candidates []*types.LegalDocument
}

// legalTopicStopWords are folded words of a question that say nothing
// about which document is meant.
var legalTopicStopWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`quy dinh gi la ve noi dung nhung chinh cua co khong nhu the nao cho toi biet hay xem va voi trong
		theo con hieu luc van ban nay do hoi muon cac mot so dieu khoan diem duoc bi ap dung thi hanh nam bo sung sua doi thay
		ban hanh ngay thang gom may bao nhieu the nao ra sao tom tat noi gi nghi dinh thong tu quyet dinh luat chi thi phap lenh
		lien tich tren duoi ma de den tu khi neu thi nhat moi cu hien hanh con khong het`) {
		legalTopicStopWords[w] = true
	}
}

// topicWords are the folded content words of the question outside the
// reference itself: "an ninh mang" in "Nghị định 53 về an ninh mạng".
func topicWords(query string, ref LegalReference) []string {
	q := foldSearch(strings.Replace(query, ref.Text, " ", 1))
	var out []string
	for _, w := range strings.Fields(q) {
		w = strings.Trim(w, ".,;:?!()\"'“”")
		if len(w) < 3 || legalTopicStopWords[w] || strings.Trim(w, "0123456789/") == "" {
			continue
		}
		out = append(out, w)
	}
	return out
}

// BuildLegalCandidates resolves a bare reference in query through the
// catalogue: nil when the query has none, the catalogue knows at most one
// document for it, or the question's own topic words single one out.
func BuildLegalCandidates(ctx context.Context, svc interfaces.LegalCatalogService, query string) (*LegalCandidatesCard, error) {
	if svc == nil {
		return nil, nil
	}
	query = norm.NFC.String(query)
	ref, ok := ParseBareLegalReference(query)
	if !ok {
		return nil, nil
	}
	docs, total, err := svc.Candidates(ctx, ref.Number, ref.DocType, legalCandidatesLimit)
	if err != nil {
		return nil, err
	}
	if total < 2 {
		return nil, nil
	}
	if words := topicWords(query, ref); len(words) > 0 {
		var narrowed []*types.LegalDocument
		for _, d := range docs {
			title := foldSearch(d.Title)
			for _, w := range words {
				if strings.Contains(title, w) {
					narrowed = append(narrowed, d)
					break
				}
			}
		}
		switch len(narrowed) {
		case 0:
		case 1:
			return nil, nil
		default:
			docs, total = narrowed, len(narrowed)
		}
	}
	return &LegalCandidatesCard{Query: query, Reference: ref, Total: total, Candidates: docs}, nil
}

// LegalCandidatesFromDocs builds the card from documents a tool already
// found (check_legal_status with several matches).
func LegalCandidatesFromDocs(query, reference string, docs []*types.LegalDocument) *LegalCandidatesCard {
	if len(docs) < 2 {
		return nil
	}
	query = norm.NFC.String(query)
	ref, ok := ParseBareLegalReference(query)
	if !ok || !strings.Contains(ref.Text, strings.TrimSpace(reference)) {
		ref = LegalReference{Number: strings.TrimSpace(reference), Text: strings.TrimSpace(reference)}
	}
	return &LegalCandidatesCard{Query: query, Reference: ref, Total: len(docs), Candidates: docs}
}

// Resend is the question to send when doc is picked: the reference as
// written becomes the full số hiệu.
func (c *LegalCandidatesCard) Resend(doc *types.LegalDocument) string {
	full := strings.TrimSpace(doc.DocumentNumber)
	if c.Reference.DocType != "" {
		full = c.Reference.DocType + " " + full
	}
	if c.Reference.Text != "" && strings.Contains(c.Query, c.Reference.Text) {
		return strings.Replace(c.Query, c.Reference.Text, full, 1)
	}
	return strings.TrimSpace(c.Query) + " (" + full + ")"
}

// Data is the card payload (display_type legal_candidates).
func (c *LegalCandidatesCard) Data() map[string]interface{} {
	cands := make([]map[string]interface{}, 0, len(c.Candidates))
	for _, d := range c.Candidates {
		cands = append(cands, map[string]interface{}{
			"document_number": d.DocumentNumber,
			"title":           d.Title,
			"doc_type":        d.DocType,
			"issuer":          d.Issuer,
			"issued_on":       legalISODate(d.IssuedOn),
			"status":          d.Status,
			"resend":          c.Resend(d),
		})
	}
	return map[string]interface{}{
		"display_type": LegalCandidatesType,
		"query":        c.Query,
		"reference":    c.Reference.Text,
		"doc_type":     c.Reference.DocType,
		"number":       c.Reference.Number,
		"total":        c.Total,
		"candidates":   cands,
	}
}

// Message is the question shown above the choices; it lists them in text
// too, so a client without the list (Word add-in) can still answer by
// number or số hiệu.
func (c *LegalCandidatesCard) Message() string {
	var b strings.Builder
	fmt.Fprintf(&b, "“%s” bạn đang nói đến văn bản nào? Danh mục có %d văn bản mang số này", c.Reference.Text, c.Total)
	if c.Total > len(c.Candidates) {
		fmt.Fprintf(&b, " (mới nhất %d)", len(c.Candidates))
	}
	b.WriteString(":\n")
	for i, d := range c.Candidates {
		fmt.Fprintf(&b, "%d. %s", i+1, d.DocumentNumber)
		if t := strings.TrimSpace(d.Title); t != "" {
			b.WriteString(" — " + t)
		}
		if st := strings.TrimSpace(d.Status); st != "" {
			b.WriteString(" (" + st + ")")
		}
		b.WriteString("\n")
	}
	b.WriteString("Chọn một văn bản ở trên để tiếp tục, hoặc nêu thêm năm hay lĩnh vực.")
	return b.String()
}

func legalISODate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}

func legalYear(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006")
}
