// Package docformat checks the thể thức (format) of Vietnamese
// administrative documents (.docx) against Nghị định 30/2020/NĐ-CP, Phụ lục
// I: it extracts the real layout (fonts, sizes, emphasis, alignment, zones,
// page setup), assigns every text unit to an NĐ30 component — by an LLM
// when one is given, else by a positional heuristic — and evaluates the
// rule set of the document type.
//
// docformat/ (Python) is the reference implementation and evaluation
// harness; testdata/parity holds its outputs and parity_test.go keeps the
// two in agreement.
package docformat

import (
	"context"
	"fmt"
	"strings"
)

// Message is one chat turn sent to the labeling model.
type Message struct {
	Role    string
	Content string
}

// Completer runs one chat completion and returns the model's text. The
// service layer adapts a workspace chat model to it.
type Completer interface {
	Complete(ctx context.Context, messages []Message) (string, error)
}

// Segmenter modes.
const (
	SegmenterAuto      = "auto"      // labels if given, else LLM if given, else heuristic
	SegmenterHeuristic = "heuristic" // positional rules only
	SegmenterLLM       = "llm"
	SegmenterLabels    = "labels"
)

// Options configure Check.
type Options struct {
	// DocumentType forces a rule set ("cong_van", "quyet_dinh", ...);
	// "" or "auto" uses the detected type.
	DocumentType string
	Segmenter    string
	// Labels is a labeling answer produced elsewhere (e.g. by an agent).
	Labels *Reply
	// LLM labels the components when set.
	LLM Completer
	// ModelName is reported in the segmentation info.
	ModelName  string
	SourceName string
}

// Segmentation info as reported.
type SegmentationInfo struct {
	Method string `json:"method"` // heuristic | llm | labels
	Model  string `json:"model,omitempty"`
	Error  string `json:"error,omitempty"`
	Notes  string `json:"notes,omitempty"`
	*LabelDiagnostics
}

// DocumentTypeInfo tells which rule set was applied and why.
type DocumentTypeInfo struct {
	Requested string `json:"requested"`
	Detected  string `json:"detected"`
	Used      string `json:"used"`
	RuleSet   string `json:"rule_set"`
}

// Summary counts check statuses.
type Summary struct {
	Pass int `json:"pass"`
	Fail int `json:"fail"`
	Warn int `json:"warn"`
	Skip int `json:"skip"`
}

// Report is the full result of a check.
type Report struct {
	Source       string                `json:"source"`
	OK           bool                  `json:"ok"`
	Error        string                `json:"error,omitempty"`
	DocumentType *DocumentTypeInfo     `json:"document_type,omitempty"`
	Segmentation *SegmentationInfo     `json:"segmentation,omitempty"`
	Summary      *Summary              `json:"summary,omitempty"`
	Sections     []*Section            `json:"sections,omitempty"`
	Components   map[string]*Component `json:"components,omitempty"`
	Checks       []CheckResult         `json:"checks,omitempty"`
	// Format is the measured formatting of every component, line by line —
	// the data the evaluation skills judge.
	Format []ComponentFormat `json:"format,omitempty"`
	// Evaluation is the skill-based judgment written by a reasoning model
	// (EvaluateWithSkills), when one was requested.
	Evaluation string `json:"evaluation,omitempty"`
	// Skills are the evaluation skills of the document's type.
	Skills []string `json:"skills,omitempty"`
}

// LineFormat is one line of a component with its measured formatting.
type LineFormat struct {
	Para   int      `json:"para"`
	Text   string   `json:"text"`
	Zone   string   `json:"zone"`
	Align  string   `json:"align"`
	Font   *string  `json:"font,omitempty"`
	SizePt *float64 `json:"size_pt,omitempty"`
	Bold   bool     `json:"bold"`
	Italic bool     `json:"italic"`
}

// ComponentFormat is one component's lines, in document order.
type ComponentFormat struct {
	Key     string       `json:"key"`
	Name    string       `json:"name"`
	Lines   []LineFormat `json:"lines"`
	Omitted int          `json:"omitted,omitempty"` // body lines left out
}

// maxBodyLines caps the nội dung lines carried in the format data: the
// body's formatting is checked by the rules; the skills need its opening
// and closing, not every paragraph.
const maxBodyLines = 12

// bodyTailLines of the capped body are its last lines.
const bodyTailLines = 4

// ExtractFormat lists each found component's lines with their measured
// formatting, in NĐ30 reading order. A tab-split line reports its own side.
func ExtractFormat(l *Layout, seg *Segmentation) []ComponentFormat {
	var out []ComponentFormat
	for _, def := range Components {
		comp, ok := seg.Components[def.Key]
		if !ok || !comp.Found {
			continue
		}
		cf := ComponentFormat{Key: def.Key, Name: ComponentName(def.Key)}
		for i, idx := range comp.Paras {
			if idx < 0 || idx >= len(l.Paragraphs) {
				continue
			}
			p := l.Paragraphs[idx]
			zone := p.Zone
			if i < len(comp.Zones) {
				zone = comp.Zones[i]
			}
			lf := LineFormat{Para: idx, Text: strings.TrimSpace(p.Text), Zone: zone, Align: p.Alignment,
				Font: p.FontName, SizePt: p.SizePt, Bold: isTrue(p.Bold), Italic: isTrue(p.Italic)}
			if p.Zone == ZoneSplit && (zone == ZoneLeft || zone == ZoneRight) {
				side := p.LeftProps
				lf.Text = p.LeftText
				if zone == ZoneRight {
					side, lf.Text = p.RightProps, p.RightText
				}
				if side.Present {
					lf.Font, lf.SizePt = side.FontName, side.SizePt
					lf.Bold, lf.Italic = isTrue(side.Bold), isTrue(side.Italic)
				}
			}
			if lf.Text == "" {
				continue
			}
			if def.Key == "noi_dung" && runeLen(lf.Text) > 200 {
				// mark the cut so a clipped word is not read as a typo
				lf.Text = truncateRunes(lf.Text, 200) + "…"
			}
			cf.Lines = append(cf.Lines, lf)
		}
		if def.Key == "noi_dung" && len(cf.Lines) > maxBodyLines {
			// keep the opening and the closing ("Trên đây là …", "./.")
			head, tail := maxBodyLines-bodyTailLines, bodyTailLines
			cf.Omitted = len(cf.Lines) - head - tail
			cf.Lines = append(cf.Lines[:head:head], cf.Lines[len(cf.Lines)-tail:]...)
		}
		if len(cf.Lines) > 0 {
			out = append(out, cf)
		}
	}
	return out
}

// LabelWithLLM asks the model to label the task's units, retrying once
// with a corrective turn when the answer is not JSON.
func LabelWithLLM(ctx context.Context, llm Completer, task *Task) (*Reply, error) {
	msgs := []Message{{Role: "system", Content: SystemPrompt}, {Role: "user", Content: task.UserMessage()}}
	text, err := llm.Complete(ctx, msgs)
	if err != nil {
		return nil, err
	}
	reply, perr := ParseReply(text)
	if perr == nil {
		return reply, nil
	}
	msgs = append(msgs,
		Message{Role: "assistant", Content: text},
		Message{Role: "user", Content: "Trả lời lại CHỈ bằng một JSON object hợp lệ theo đúng định dạng đã yêu cầu."})
	if text, err = llm.Complete(ctx, msgs); err != nil {
		return nil, err
	}
	return ParseReply(text)
}

func segmentDoc(ctx context.Context, l *Layout, opts Options) (*Segmentation, *SegmentationInfo, error) {
	heur := Segment(l)
	mode := strings.ToLower(strings.TrimSpace(opts.Segmenter))
	if mode == "" {
		mode = SegmenterAuto
	}
	switch mode {
	case SegmenterAuto, SegmenterHeuristic, SegmenterLLM, SegmenterLabels:
	default:
		return nil, nil, fmt.Errorf("segmenter must be one of auto, heuristic, llm, labels")
	}
	if opts.Labels != nil && (mode == SegmenterAuto || mode == SegmenterLabels) {
		seg, diag := ApplyLabels(l, opts.Labels, PrepareTask(l, heur))
		return seg, &SegmentationInfo{Method: SegmenterLabels, LabelDiagnostics: diag}, nil
	}
	if mode == SegmenterLabels {
		return nil, nil, fmt.Errorf("segmenter 'labels' requires labels")
	}
	if mode == SegmenterHeuristic {
		return heur, &SegmentationInfo{Method: SegmenterHeuristic}, nil
	}
	if opts.LLM == nil {
		info := &SegmentationInfo{Method: SegmenterHeuristic}
		if mode == SegmenterLLM {
			info.Error = "no chat model available — heuristic used"
		}
		return heur, info, nil
	}
	task := PrepareTask(l, heur)
	reply, err := LabelWithLLM(ctx, opts.LLM, task)
	if err != nil {
		// an LLM failure degrades to the heuristic instead of failing
		return heur, &SegmentationInfo{Method: SegmenterHeuristic, Error: "LLM: " + err.Error()}, nil
	}
	seg, diag := ApplyLabels(l, reply, task)
	return seg, &SegmentationInfo{Method: SegmenterLLM, Model: opts.ModelName, Notes: reply.Notes, LabelDiagnostics: diag}, nil
}

// Check runs the whole pipeline on .docx bytes.
func Check(ctx context.Context, content []byte, opts Options) *Report {
	l := InspectDocx(content)
	if len(l.Errors) > 0 && len(l.Paragraphs) == 0 {
		return &Report{Source: opts.SourceName, Error: strings.Join(l.Errors, "; ")}
	}
	seg, info, err := segmentDoc(ctx, l, opts)
	if err != nil {
		return &Report{Source: opts.SourceName, Error: err.Error()}
	}
	requested := strings.ToLower(strings.TrimSpace(opts.DocumentType))
	if requested == "" {
		requested = "auto"
	}
	used := requested
	if requested == "auto" || requested == "detect" {
		used = seg.DetectedType
	}
	rulesName := "base"
	for _, t := range AvailableTypes() {
		if t == used {
			rulesName = used
			break
		}
	}
	rs, err := LoadRuleSet(rulesName)
	if err != nil {
		return &Report{Source: opts.SourceName, Error: err.Error()}
	}
	checks := Evaluate(l, seg, rs)
	sum := &Summary{}
	for _, c := range checks {
		switch c.Status {
		case StatusPass:
			sum.Pass++
		case StatusFail:
			sum.Fail++
		case StatusWarn:
			sum.Warn++
		case StatusSkip:
			sum.Skip++
		}
	}
	label := rs.Label
	if label == "" {
		label = rulesName
	}
	return &Report{
		Source: opts.SourceName, OK: true,
		DocumentType: &DocumentTypeInfo{Requested: requested, Detected: seg.DetectedType, Used: used, RuleSet: label},
		Segmentation: info, Summary: sum, Sections: l.Sections,
		Components: seg.Components, Checks: checks,
		Format: ExtractFormat(l, seg),
		Skills: skillNamesFor(used),
	}
}

func skillNamesFor(docType string) []string {
	var out []string
	for _, s := range SkillsFor(docType) {
		out = append(out, s.Name)
	}
	return out
}
