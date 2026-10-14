package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// Validity note appended to answers ("Lưu ý hiệu lực"): every số hiệu the
// answer cites that the legal catalogue marks fully expired, unless the
// answer already says so next to it. On the VLSP 2025 questions the agent
// presented about 10 of 174 such documents as the rule in force (Nghị định
// 27/2015/NĐ-CP, 13/2023/NĐ-CP…); a prompt rule cannot be relied on, the
// catalogue status can.

const (
	legalValidityStatusExpired = "Hết hiệu lực toàn bộ"
	legalValidityTimeout       = 3 * time.Second
	legalValidityMaxDocs       = 12
	legalValidityMaxLines      = 4
	// legalValidityWindow is how far (bytes) around a mention an expiry
	// remark counts as the answer's own warning.
	legalValidityWindow = 160
)

var legalExpiryRemarkRe = regexp.MustCompile(`(?i)hết hiệu lực|không còn hiệu lực|thay thế|bãi bỏ|ngưng hiệu lực`)

// legalCatalogStatus is a catalogue row of one số hiệu.
type legalCatalogStatus struct {
	ID             string
	DocumentNumber string
	DocType        string
	Status         string
	ExpiredOn      *time.Time
}

// fullyExpiredDocs returns, by catalogue number key, the documents of
// numbers whose every catalogue entry is fully expired (số hiệu are not
// unique: one in-force match keeps the number out).
func fullyExpiredDocs(ctx context.Context, db *gorm.DB, numbers []string) map[string]legalCatalogStatus {
	keys := map[string]bool{}
	var list []string
	for _, n := range numbers {
		if k := vietnamese_legal.CatalogNumberKey(n); k != "" && !keys[k] {
			keys[k] = true
			list = append(list, k)
		}
	}
	if len(list) == 0 {
		return nil
	}
	var rows []struct {
		ID             string
		DocumentNumber string
		DocType        string
		Status         string
		ExpiredOn      *time.Time
		NumberKey      string
	}
	if err := db.WithContext(ctx).Raw(`SELECT id, document_number, doc_type, status, expired_on, number_key
		FROM legal_documents WHERE number_key IN ?`, list).Scan(&rows).Error; err != nil {
		logger.Warnf(ctx, "[LegalValidity] catalogue lookup: %v", err)
		return nil
	}
	out := map[string]legalCatalogStatus{}
	live := map[string]bool{}
	for _, r := range rows {
		if r.Status != legalValidityStatusExpired {
			live[r.NumberKey] = true
			continue
		}
		if _, ok := out[r.NumberKey]; !ok {
			out[r.NumberKey] = legalCatalogStatus{ID: r.ID, DocumentNumber: r.DocumentNumber,
				DocType: r.DocType, Status: r.Status, ExpiredOn: r.ExpiredOn}
		}
	}
	for k := range live {
		delete(out, k)
	}
	return out
}

// LegalValidityNote returns the "Lưu ý hiệu lực" block for answer, or "".
func LegalValidityNote(ctx context.Context, db *gorm.DB, answer string) string {
	if db == nil || strings.TrimSpace(answer) == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, legalValidityTimeout)
	defer cancel()
	// an earlier generated block is not the answer's own text
	body := answer
	if i := strings.Index(body, "\n---\n**Lịch sử sửa đổi**"); i >= 0 {
		body = body[:i]
	}
	var numbers []string
	for _, loc := range legalDocNumberRe.FindAllStringIndex(body, -1) {
		if n := body[loc[0]:loc[1]]; len(numbers) < legalValidityMaxDocs {
			numbers = append(numbers, n)
		}
	}
	expired := fullyExpiredDocs(ctx, db, numbers)
	if len(expired) == 0 {
		return ""
	}
	var lines []string
	done := map[string]bool{}
	for _, n := range numbers {
		k := vietnamese_legal.CatalogNumberKey(n)
		doc, ok := expired[k]
		if !ok || done[k] || len(lines) >= legalValidityMaxLines {
			continue
		}
		done[k] = true
		if remarksExpiry(body, n) {
			continue
		}
		lines = append(lines, validityLine(ctx, db, doc))
	}
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n---\n**Lưu ý hiệu lực**\n")
	for _, l := range lines {
		b.WriteString("- " + l + "\n")
	}
	b.WriteString("Cần đối chiếu với văn bản đang có hiệu lực trước khi áp dụng.\n")
	return b.String()
}

// remarksExpiry reports whether a mention of number has an expiry remark
// ("đã hết hiệu lực", "được thay thế bởi…") close to it.
func remarksExpiry(text, number string) bool {
	for idx := 0; ; {
		i := strings.Index(text[idx:], number)
		if i < 0 {
			return false
		}
		i += idx
		from, to := i-legalValidityWindow, i+len(number)+legalValidityWindow
		if from < 0 {
			from = 0
		}
		if to > len(text) {
			to = len(text)
		}
		for from > 0 && !isRuneStart(text[from]) {
			from--
		}
		for to < len(text) && !isRuneStart(text[to]) {
			to++
		}
		if legalExpiryRemarkRe.MatchString(text[from:to]) {
			return true
		}
		idx = i + len(number)
	}
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// validityLine words one expired document with its date and the in-force
// documents that replaced or repealed it.
func validityLine(ctx context.Context, db *gorm.DB, doc legalCatalogStatus) string {
	label := strings.TrimSpace(doc.DocType + " " + doc.DocumentNumber)
	text := label + " đã hết hiệu lực toàn bộ"
	if doc.ExpiredOn != nil && !doc.ExpiredOn.IsZero() {
		text += " từ ngày " + doc.ExpiredOn.Format("02/01/2006")
	}
	var succ []struct {
		DocType        string
		DocumentNumber string
	}
	if err := db.WithContext(ctx).Raw(`SELECT DISTINCT d.doc_type, d.document_number, d.issued_on
		FROM legal_document_relations r JOIN legal_documents d ON d.id = r.from_id
		WHERE r.to_id = ? AND r.relation IN ('replaces', 'repeals')
		  AND d.status IN ('Còn hiệu lực', 'Hết hiệu lực một phần')
		ORDER BY d.issued_on DESC NULLS LAST LIMIT 2`, doc.ID).Scan(&succ).Error; err == nil && len(succ) > 0 {
		var names []string
		for _, s := range succ {
			names = append(names, strings.TrimSpace(s.DocType+" "+s.DocumentNumber))
		}
		text += fmt.Sprintf(" (được thay thế/bãi bỏ bởi %s)", strings.Join(names, ", "))
	}
	return text + "."
}
