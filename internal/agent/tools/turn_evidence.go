package tools

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// A calculation tool's legal figures (a threshold, a rate) are passed by
// the model, read from the documents it looked up. Qwen passed the 100
// triệu threshold of Thông tư 40/2021/TT-BTC (partly expired, its threshold
// silently superseded) although Nghị định 68/2026/NĐ-CP, newer and higher,
// said 01 tỷ in the same turn's results. The tool therefore asks which
// document a figure comes from and checks it against what this turn's tools
// returned: the document must be among them, say that figure, and no newer
// document of the same or a higher rank among them may say another.

type turnStepsKey struct{}

// WithTurnSteps puts the turn's finished steps in ctx for the tools of the
// round (the engine sets it before running them).
func WithTurnSteps(ctx context.Context, steps []types.AgentStep) context.Context {
	return context.WithValue(ctx, turnStepsKey{}, steps)
}

// TurnStepsFromContext is what WithTurnSteps stored; nil outside a turn.
func TurnStepsFromContext(ctx context.Context) []types.AgentStep {
	s, _ := ctx.Value(turnStepsKey{}).([]types.AgentStep)
	return s
}

// LegalDocIdentity is a knowledge document's legal identity (its metadata).
type LegalDocIdentity struct {
	Number    string // "68/2026/NĐ-CP"
	Type      string // "Nghị định"
	IssueDate string // "2026-03-05"
	Status    string // "Còn hiệu lực", "Hết hiệu lực toàn bộ", …
}

// LegalDocIdentityFunc returns the identity of the given knowledge ids
// (those without a số hiệu left out).
type LegalDocIdentityFunc func(ctx context.Context, knowledgeIDs []string) map[string]LegalDocIdentity

// evidenceTextsByKnowledge collects, by knowledge id, the texts this turn's
// successful tool results returned: every list of rows that carry a
// knowledge_id (search results, read chunks, sections), the text the model
// read (model_content) before the stored one.
func evidenceTextsByKnowledge(steps []types.AgentStep) map[string][]string {
	out := map[string][]string{}
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch x := v.(type) {
		case []map[string]interface{}:
			for _, row := range x {
				walk(row)
			}
		case []interface{}:
			for _, e := range x {
				walk(e)
			}
		case map[string]interface{}:
			id, _ := x["knowledge_id"].(string)
			text, _ := x["model_content"].(string)
			if text == "" {
				text, _ = x["content"].(string)
			}
			if id != "" && text != "" {
				out[id] = append(out[id], text)
			}
			for k, e := range x {
				if k != "model_content" && k != "content" {
					walk(e)
				}
			}
		}
	}
	for _, s := range steps {
		for _, tc := range s.ToolCalls {
			if tc.Result != nil && tc.Result.Success {
				walk(tc.Result.Data)
			}
		}
	}
	return out
}

// legalRank orders documents by legal force from the số hiệu suffix:
// law > UBTVQH resolution > decree > PM decision > circular > other.
func legalRank(number string) int {
	n := strings.ToUpper(number)
	// a consolidated text ranks as what it consolidates, told by who
	// consolidates: the National Assembly office a law, the Government
	// office a decree, a ministry a circular
	if strings.Contains(n, "/VBHN-") {
		switch {
		case strings.Contains(n, "VBHN-VPQH"):
			return 6
		case strings.Contains(n, "VBHN-VPCP"):
			return 4
		}
		return 2
	}
	switch {
	case strings.Contains(n, "/UBTVQH"):
		return 5
	case strings.Contains(n, "/QH"):
		return 6
	case strings.Contains(n, "NĐ-CP") || strings.Contains(n, "ND-CP"):
		return 4
	case strings.Contains(n, "QĐ-TTG") || strings.Contains(n, "QD-TTG"):
		return 3
	case strings.Contains(n, "/TT-") || strings.Contains(n, "/TTLT-"):
		return 2
	}
	return 1
}

// issuedOn is the document's issue date ("2026-03-05"), else its year from
// the số hiệu ("2026"); comparable as strings only between equal lengths,
// so a year stands for its first day.
func issuedOn(id LegalDocIdentity) string {
	if len(id.IssueDate) >= 10 {
		return id.IssueDate[:10]
	}
	if m := numberYearRe.FindStringSubmatch(id.Number); m != nil {
		return m[1] + "-01-01"
	}
	return ""
}

var numberYearRe = regexp.MustCompile(`/((?:19|20)\d\d)/`)

var (
	// notes the system adds to a provision: the old words of an amendment,
	// history, unverified wording — not what the provision says now
	evidenceNoteRe = regexp.MustCompile(`\[[^\[\]]*\]`)
	// a revenue threshold as the law words it: "… doanh thu năm từ 01 tỷ
	// đồng trở xuống … không phải nộp / không chịu thuế …"
	thresholdAmountRe  = regexp.MustCompile(`(?i)(?:từ\s+(\d[\d.,]*)\s*(tỷ|triệu|nghìn|ngàn)?\s*đồng(?:\s*/\s*năm)?\s+trở\s+xuống|không\s+quá\s+(\d[\d.,]*)\s*(tỷ|triệu|nghìn|ngàn)?\s*đồng)`)
	thresholdSubjectRe = regexp.MustCompile(`(?i)doanh\s+thu`)
	thresholdExemptRe  = regexp.MustCompile(`(?i)không\s+phải\s+nộp|không\s+chịu\s+thuế|không\s+thuộc\s+diện\s+chịu`)
	sentenceSplitRe    = regexp.MustCompile(`[.;\n]\s`)
)

// revenueThresholds are the amounts (đồng) a text states as the revenue up
// to which no tax is due, with the system notes removed.
func revenueThresholds(text string) []float64 {
	text = evidenceNoteRe.ReplaceAllString(text, " ")
	var out []float64
	for _, sentence := range sentenceSplitRe.Split(text, -1) {
		if !thresholdSubjectRe.MatchString(sentence) || !thresholdExemptRe.MatchString(sentence) {
			continue
		}
		for _, m := range thresholdAmountRe.FindAllStringSubmatch(sentence, -1) {
			digits, unit := m[1], m[2]
			if digits == "" {
				digits, unit = m[3], m[4]
			}
			if v, ok := vndAmount(digits, unit); ok && !containsAmount(out, v) {
				out = append(out, v)
			}
		}
	}
	return out
}

// vndAmount reads "01" + "tỷ", "500" + "triệu", "1.000.000.000" + "".
func vndAmount(digits, unit string) (float64, bool) {
	scale := 1.0
	switch strings.ToLower(unit) {
	case "tỷ":
		scale = 1e9
	case "triệu":
		scale = 1e6
	case "nghìn", "ngàn":
		scale = 1e3
	}
	if scale == 1 {
		digits = strings.NewReplacer(".", "", ",", "").Replace(digits)
	} else {
		digits = strings.NewReplacer(".", "", ",", ".").Replace(digits) // "2,5 tỷ"
	}
	v, err := strconv.ParseFloat(digits, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v * scale, true
}

func containsAmount(list []float64, v float64) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func amountsText(list []float64) string {
	parts := make([]string, len(list))
	for i, v := range list {
		parts[i] = formatVNDPlain(v)
	}
	return strings.Join(parts, " / ")
}

// formatVNDPlain writes 1000000000 as "1.000.000.000 đ".
func formatVNDPlain(v float64) string {
	s := strconv.FormatFloat(v, 'f', 0, 64)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	return b.String() + " đ"
}

// checkThresholdBasis checks the threshold value the model passed against
// its basis document and this turn's evidence; nil when it holds or cannot
// be checked (no identity lookup, no steps).
// basisParam names the tool parameter holding the số hiệu, label the
// figure ("ngưỡng doanh thu").
func checkThresholdBasis(ctx context.Context, identity LegalDocIdentityFunc, basisParam, label, basis string, value float64) error {
	steps := TurnStepsFromContext(ctx)
	if identity == nil || steps == nil {
		return nil
	}
	texts := evidenceTextsByKnowledge(steps)
	ids := make([]string, 0, len(texts))
	for id := range texts {
		ids = append(ids, id)
	}
	docs := identity(ctx, ids)
	type evDoc struct {
		id     LegalDocIdentity
		values []float64
	}
	byKey := map[string]*evDoc{}
	for kid, d := range docs {
		key := vietnamese_legal.CatalogNumberKey(d.Number)
		if key == "" {
			continue
		}
		e := byKey[key]
		if e == nil {
			e = &evDoc{id: d}
			byKey[key] = e
		}
		for _, t := range texts[kid] {
			for _, v := range revenueThresholds(t) {
				if !containsAmount(e.values, v) {
					e.values = append(e.values, v)
				}
			}
		}
	}
	cited := byKey[vietnamese_legal.CatalogNumberKey(basis)]
	if cited == nil {
		return fmt.Errorf("Văn bản %s ghi ở %s không có trong kết quả tra cứu đã trả về ở các bước trước của lượt này "+
			"(lệnh tra cứu gọi cùng lúc với công cụ tính chưa được tính). "+
			"Tra văn bản quy định %s (search_knowledge hoặc read_document) trước, rồi gọi lại với đúng số hiệu văn bản đã đọc", basis, basisParam, label)
	}
	if cited.id.Status == "Hết hiệu lực toàn bộ" {
		return fmt.Errorf("Văn bản %s đã hết hiệu lực toàn bộ; tra văn bản đang có hiệu lực quy định %s", cited.id.Number, label)
	}
	if len(cited.values) > 0 && !containsAmount(cited.values, value) {
		return fmt.Errorf("Văn bản %s ghi %s là %s, khác %s đã truyền; dùng đúng số trong văn bản",
			cited.id.Number, label, amountsText(cited.values), formatVNDPlain(value))
	}
	// the cited document states no figure ("mức quy định của Chính phủ", an
	// amending decree): the value must come from an evidence document that
	// states it, and the newer-document rule runs from that one
	anchor := cited
	if len(cited.values) == 0 {
		anchor = nil
		for _, e := range byKey {
			if !containsAmount(e.values, value) || e.id.Status == "Hết hiệu lực toàn bộ" {
				continue
			}
			if anchor == nil || legalRank(e.id.Number) > legalRank(anchor.id.Number) ||
				(legalRank(e.id.Number) == legalRank(anchor.id.Number) && issuedOn(e.id) > issuedOn(anchor.id)) {
				anchor = e
			}
		}
		if anchor == nil {
			return fmt.Errorf("Văn bản %s không ghi con số %s, và không văn bản nào trong kết quả tra cứu của lượt này ghi %s. "+
				"Ghi ở %s số hiệu văn bản nêu con số đó", cited.id.Number, label, formatVNDPlain(value), basisParam)
		}
	}
	// a newer document of the same or a higher rank that says another figure
	var newer *evDoc
	citedRank, citedDate := legalRank(anchor.id.Number), issuedOn(anchor.id)
	for _, e := range byKey {
		if e == anchor || len(e.values) == 0 || containsAmount(e.values, value) || e.id.Status == "Hết hiệu lực toàn bộ" {
			continue
		}
		date := issuedOn(e.id)
		if legalRank(e.id.Number) < citedRank || date == "" || citedDate == "" || date <= citedDate {
			continue
		}
		if newer == nil || date > issuedOn(newer.id) {
			newer = e
		}
	}
	if newer != nil {
		return fmt.Errorf("Văn bản %s (ban hành %s) mới hơn và có hiệu lực pháp lý không thấp hơn %s, ghi %s là %s. "+
			"Dùng %s theo văn bản hiện hành và ghi số hiệu đó ở %s",
			newer.id.Number, issuedOn(newer.id), anchor.id.Number, label, amountsText(newer.values), label, basisParam)
	}
	return nil
}
