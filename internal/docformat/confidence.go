package docformat

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/logger"
)

// When the positional heuristic already found every component the
// document's type requires, in a shape that leaves nothing to decide, the
// model is not asked (Confident). Otherwise the model labels only the head
// and tail blocks (PrepareTask) and, when a required component is still
// missing, the block it belongs to is widened into the body (LabelChunked).

// MethodHeuristicConfident is SegmentationInfo.Method when the heuristic
// passed the confidence gate and no model was called.
const MethodHeuristicConfident = "heuristic-confident"

// luuRe: the "- Lưu: VT, ..." line that closes a Nơi nhận list.
var luuRe = regexp.MustCompile(`^[-–—+•*]?\s*LUU\b`)

// headComponents sit in the head block; tailComponents in the tail block.
var (
	headComponents = map[string]bool{"quoc_hieu": true, "tieu_ngu": true, "co_quan_chu_quan": true,
		"co_quan_ban_hanh": true, "so_ky_hieu": true, "dia_danh_ngay_thang": true, "trich_yeu": true,
		"tham_quyen_ban_hanh": true, "can_cu": true, "kinh_gui": true, "do_mat": true, "do_khan": true,
		"chi_dan_luu_hanh": true}
	tailComponents = map[string]bool{"signature": true, "chuc_danh": true, "nguoi_ky": true,
		"noi_nhan": true, "ky_hieu_soan_thao": true}
)

// requiredComponents of a detected type: its rule set's, plus thẩm quyền
// and căn cứ for a quyết định / nghị quyết.
func requiredComponents(docType string) []string {
	var out []string
	if rs, err := LoadRuleSet(ruleSetName(docType)); err == nil {
		out = append(out, rs.RequiredComponents...)
	}
	if docType == "quyet_dinh" || docType == "nghi_quyet" {
		out = append(out, "tham_quyen_ban_hanh", "can_cu")
	}
	return out
}

// missingRequired lists the required components seg lacks; a required
// signature also needs both chức danh and người ký.
func missingRequired(seg *Segmentation) []string {
	var out []string
	seen := map[string]bool{}
	for _, k := range requiredComponents(seg.DetectedType) {
		if seen[k] {
			continue
		}
		seen[k] = true
		if !seg.Comp(k).Found {
			out = append(out, k)
		}
		if k == "signature" {
			for _, part := range []string{"chuc_danh", "nguoi_ky"} {
				if !seg.Comp(part).Found {
					out = append(out, part)
				}
			}
		}
	}
	return out
}

// Confident reports whether the heuristic segmentation can stand without
// the model, and else why not: a known type; every required component
// found; no header line left unassigned; a signature with chức danh and
// người ký; a Nơi nhận closed by its "Lưu" line; cơ quan lines split by
// their weight, not guessed from wording.
func Confident(seg *Segmentation) (bool, string) {
	if seg.DetectedType == "unknown" {
		return false, "loại văn bản chưa rõ"
	}
	if m := missingRequired(seg); len(m) > 0 {
		return false, "thiếu " + strings.Join(m, ", ")
	}
	for _, k := range []string{"header_other", "header_left_other", "header_right_other"} {
		if seg.Comp(k).Found {
			return false, "dòng đầu văn bản chưa gán: " + k
		}
	}
	if hasString(requiredComponents(seg.DetectedType), "noi_nhan") {
		closed := false
		for _, t := range seg.Comp("noi_nhan").Texts {
			closed = closed || luuRe.MatchString(fold(t))
		}
		if !closed {
			return false, "nơi nhận không có dòng Lưu"
		}
	}
	if seg.agencyGuessed {
		return false, "cơ quan chủ quản / ban hành cùng kiểu chữ"
	}
	if seg.tailBody {
		return false, "dòng nội dung trong khối ký / nơi nhận"
	}
	return true, ""
}

// LabelStats records how a document was labelled.
type LabelStats struct {
	// Gate is why the heuristic was not trusted on its own ("" when it was
	// not consulted or passed).
	Gate        string `json:"gate,omitempty"`
	LLMCalls    int    `json:"llm_calls"`
	Widenings   int    `json:"widenings,omitempty"`
	PromptChars int    `json:"prompt_chars,omitempty"`
	UnitsSent   int    `json:"units_sent,omitempty"`
	UnitsTotal  int    `json:"units_total,omitempty"`
}

// countingCompleter counts the calls and prompt characters of a Completer.
type countingCompleter struct {
	llm   Completer
	stats *LabelStats
}

func (c countingCompleter) Complete(ctx context.Context, msgs []Message) (string, error) {
	c.stats.LLMCalls++
	for _, m := range msgs {
		c.stats.PromptChars += utf8.RuneCountInString(m.Content)
	}
	return c.llm.Complete(ctx, msgs)
}

// LabelChunked labels the task with the model and, while a required
// component is missing and the task can grow, widens the block it belongs
// to (head forward, tail back) by widenStep units and labels the wider task
// again — at most maxWidenings times. A failed widened call keeps the last
// answer. stats is filled in.
func LabelChunked(ctx context.Context, llm Completer, l *Layout, task *Task, stats *LabelStats) (*Segmentation, *Reply, *LabelDiagnostics, error) {
	counted := countingCompleter{llm, stats}
	stats.UnitsTotal = len(task.All)
	reply, err := LabelWithLLM(ctx, counted, task)
	if err != nil {
		return nil, nil, nil, err
	}
	stats.UnitsSent = len(task.Units)
	seg, diag := ApplyLabels(l, reply, task)
	for stats.Widenings < maxWidenings {
		missing := missingRequired(seg)
		wider := task.Widen(missing)
		if wider == nil {
			break
		}
		stats.Widenings++
		logger.Infof(ctx, "docformat: widening %d/%d (head +%d, tail +%d), missing %s: %d → %d units sent",
			stats.Widenings, maxWidenings, wider.headExtra, wider.tailExtra, strings.Join(missing, ","),
			len(task.Units), len(wider.Units))
		r2, err := LabelWithLLM(ctx, counted, wider)
		if err != nil {
			logger.Warnf(ctx, "docformat: widened labeling failed, keeping the previous answer: %v", err)
			break
		}
		task, reply = wider, r2
		stats.UnitsSent = len(task.Units)
		seg, diag = ApplyLabels(l, reply, task)
	}
	return seg, reply, diag, nil
}

func hasString(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
