//go:build live

package docformat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// Live comparison of the skill evaluation on documents whose judgment
// outcome is known: cell "rules" is production (the judgment rules decide
// the Bảng kiểm in code; the model, thinking on, judges only the items
// they leave undetermined); the "<render>/<thinking>" cells have the model
// judge everything (EvalOptions.ForceLLM) on the full format data
// (RenderData) or the judgment brief (RenderJudgment), thinking on or off.
// Opt-in:
//
//	SSRF_WHITELIST=10.10.0.240 DOCFORMAT_LIVE_BASE_URL=http://10.10.0.240:8000/v1 \
//	DOCFORMAT_LIVE_MODEL=Qwen/Qwen3.6-35B-A3B-FP8 \
//	DOCFORMAT_LIVE_OUT=/tmp/eval.json \
//	go test -tags live -run LiveEvaluation -v -timeout 90m ./internal/docformat/
//
// DOCFORMAT_LIVE_RUNS (default 4) runs per document and cell,
// DOCFORMAT_LIVE_CONCURRENCY (default 4) parallel requests,
// DOCFORMAT_LIVE_CELLS limits the cells ("rules,judgment/on").
//
// Documents: loadLiveDocs (judgment_docs_test.go). A technical or attached
// scope document is evaluated in code without the model in every cell; a
// "Lỗi" entry on it, or a wrong scope, is a false alarm (item 0).

type liveCell struct {
	Render   string // data | judgment
	Thinking bool
	Rules    bool // production: judgment rules, the model only for undetermined items
}

func (c liveCell) String() string {
	if c.Rules {
		return "rules"
	}
	return c.Render + "/" + map[bool]string{true: "on", false: "off"}[c.Thinking]
}

type liveRun struct {
	Cell         string         `json:"cell"`
	Doc          string         `json:"doc"`
	Run          int            `json:"run"`
	Err          string         `json:"error,omitempty"`
	Seconds      float64        `json:"seconds"`
	PromptChars  int            `json:"prompt_chars"`
	PromptTok    int            `json:"prompt_tokens"`
	ComplTok     int            `json:"completion_tokens"`
	Items        map[int]string `json:"items"` // item → loi | warn | dat | unknown
	ErrorLines   int            `json:"error_section_lines"`
	FalseItems   []int          `json:"false_alarm_items,omitempty"`
	Missed       []int          `json:"missed_items,omitempty"`
	Answer       string         `json:"answer"`
	Method       string         `json:"method,omitempty"`
	Undetermined []int          `json:"undetermined,omitempty"`
}

// usageChat records the token usage of the call it wraps.
type usageChat struct {
	inner chat.Chat
	usage types.TokenUsage
}

func (u *usageChat) Chat(ctx context.Context, msgs []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	resp, err := u.inner.Chat(ctx, msgs, opts)
	if resp != nil {
		u.usage = resp.Usage
	}
	return resp, err
}

func (u *usageChat) ChatStream(ctx context.Context, msgs []chat.Message, opts *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return u.inner.ChatStream(ctx, msgs, opts)
}

func (u *usageChat) GetModelName() string { return u.inner.GetModelName() }
func (u *usageChat) GetModelID() string   { return u.inner.GetModelID() }

var numberedLineRe = regexp.MustCompile(`^\d+\.\s`)

// parseChecklist reads the Bảng kiểm statuses of an answer (ParseChecklist):
// loi | warn | dat | unknown, "?" for a line without a status word.
func parseChecklist(answer string) map[int]string {
	items := map[int]string{}
	for n, e := range ParseChecklist(answer) {
		items[n] = map[string]string{JudgeFail: "loi", JudgeWarn: "warn", JudgePass: "dat", JudgeUnknown: "unknown", "": "?"}[e.Status]
	}
	return items
}

// errorSectionLines counts the top-level entries of the "Lỗi" section.
func errorSectionLines(answer string) int {
	in, n := false, 0
	for _, l := range strings.Split(answer, "\n") {
		if mdHeadingRe.MatchString(l) {
			plain := strings.Trim(l, "#*0123456789. ")
			in = strings.HasPrefix(plain, "Lỗi") && !strings.Contains(l, "Bảng kiểm")
			continue
		}
		if in && (strings.HasPrefix(l, "- ") || strings.HasPrefix(l, "* ") || numberedLineRe.MatchString(l)) {
			n++
		}
	}
	return n
}

func envInt(name string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v > 0 {
		return v
	}
	return def
}

func TestLiveEvaluation(t *testing.T) {
	base, name := os.Getenv("DOCFORMAT_LIVE_BASE_URL"), os.Getenv("DOCFORMAT_LIVE_MODEL")
	if base == "" || name == "" {
		t.Skip("DOCFORMAT_LIVE_BASE_URL / DOCFORMAT_LIVE_MODEL not set")
	}
	model, err := chat.NewChat(&chat.ChatConfig{
		Source: types.ModelSourceRemote, BaseURL: base, ModelName: name,
		ModelID: "live", Provider: os.Getenv("DOCFORMAT_LIVE_PROVIDER"),
		APIKey: os.Getenv("DOCFORMAT_LIVE_API_KEY"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	docs := loadLiveDocs(t)
	runs := envInt("DOCFORMAT_LIVE_RUNS", 4)
	conc := envInt("DOCFORMAT_LIVE_CONCURRENCY", 4)
	cells := []liveCell{{Render: "judgment", Thinking: true, Rules: true}}
	if want := os.Getenv("DOCFORMAT_LIVE_CELLS"); want != "" && !strings.Contains(","+want+",", ",rules,") {
		cells = nil
	}
	for _, render := range []string{"data", "judgment"} {
		for _, thinking := range []bool{true, false} {
			c := liveCell{Render: render, Thinking: thinking}
			if want := os.Getenv("DOCFORMAT_LIVE_CELLS"); want != "" && !strings.Contains(","+want+",", ","+c.String()+",") {
				continue
			}
			cells = append(cells, c)
		}
	}
	renders := map[string]func(*Report) string{"data": RenderData, "judgment": RenderJudgment}

	type job struct {
		cell liveCell
		doc  liveDoc
		run  int
	}
	var jobs []job
	// interleave cells so a cut-short run still covers all of them
	for r := 0; r < runs; r++ {
		for _, d := range docs {
			for _, c := range cells {
				jobs = append(jobs, job{c, d, r})
			}
		}
	}
	results := make([]liveRun, len(jobs))
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	var done int
	var mu sync.Mutex
	for i, j := range jobs {
		wg.Add(1)
		go func(i int, j job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			uc := &usageChat{inner: model}
			ev := ChatEvaluatorWithThinking(uc, j.cell.Thinking)
			opts := EvalOptions{Render: renders[j.cell.Render], ForceLLM: !j.cell.Rules}
			// each run judges its own copy: EvaluateWithSkills records
			// r.Judgment
			rep := *j.doc.Report
			chars := 0
			if !j.cell.Rules {
				for _, m := range EvaluationMessages(&rep, opts) {
					chars += len([]rune(m.Content))
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			start := time.Now()
			answer, err := EvaluateWithSkillsOpts(ctx, ev, &rep, opts)
			res := liveRun{Cell: j.cell.String(), Doc: j.doc.Name, Run: j.run, Seconds: time.Since(start).Seconds(),
				PromptChars: chars, PromptTok: uc.usage.PromptTokens, ComplTok: uc.usage.CompletionTokens, Answer: answer}
			if rep.Judgment != nil {
				res.Method, res.Undetermined = rep.Judgment.Method, rep.Judgment.Undetermined
				if rep.Judgment.Error != "" {
					res.Err = "llm: " + rep.Judgment.Error
				}
			}
			if err != nil {
				res.Err = err.Error()
			} else if j.doc.Scope != ScopeFull {
				// technical / attached: no Bảng kiểm; a "Lỗi" entry or a
				// wrong scope is a false alarm (item 0)
				res.ErrorLines = errorSectionLines(answer)
				if res.ErrorLines > 0 || rep.Scope == nil || rep.Scope.Used != j.doc.Scope {
					res.FalseItems = []int{0}
				}
			} else {
				res.Items = parseChecklist(answer)
				res.ErrorLines = errorSectionLines(answer)
				for n := 1; n <= 6; n++ {
					got := res.Items[n] == "loi"
					want := contains(j.doc.Errors, n)
					switch {
					case contains(j.doc.Any, n):
					case got && !want:
						res.FalseItems = append(res.FalseItems, n)
					case want && !got:
						res.Missed = append(res.Missed, n)
					}
				}
			}
			results[i] = res
			mu.Lock()
			done++
			t.Logf("[%d/%d] %s %s #%d: %.1fs items=%v fa=%v miss=%v llm=%v %s", done, len(jobs), res.Cell, res.Doc, res.Run,
				res.Seconds, res.Items, res.FalseItems, res.Missed, res.Undetermined, res.Err)
			mu.Unlock()
		}(i, j)
	}
	wg.Wait()

	// per cell metrics
	type agg struct {
		runs, failed, unparsed, okRuns int
		correctRuns, faRuns, faItems   int
		expected, missed               int
		errLinesCorrect                int
		secs, compl, chars             float64
		faByItem                       [7]int
		noLLM                          int
		llmByItem                      [7]int
	}
	byCell := map[string]*agg{}
	docCell := map[string]map[string][2]int{} // doc → cell → {false alarm items, missed}
	for _, r := range results {
		a := byCell[r.Cell]
		if a == nil {
			a = &agg{}
			byCell[r.Cell] = a
		}
		a.runs++
		if r.Err != "" {
			a.failed++
			continue
		}
		var d liveDoc
		for _, x := range docs {
			if x.Name == r.Doc {
				d = x
			}
		}
		a.okRuns++
		if r.Method == "rules" {
			a.noLLM++
		}
		for _, n := range r.Undetermined {
			a.llmByItem[n]++
		}
		a.secs += r.Seconds
		a.compl += float64(r.ComplTok)
		a.chars += float64(r.PromptChars)
		if len(r.Items) < 6 && d.Scope == ScopeFull {
			a.unparsed++
		}
		if len(d.Errors) == 0 {
			a.correctRuns++
			if len(r.FalseItems) > 0 {
				a.faRuns++
			}
			a.errLinesCorrect += r.ErrorLines
		}
		a.faItems += len(r.FalseItems)
		for _, n := range r.FalseItems {
			a.faByItem[n]++
		}
		a.expected += len(d.Errors)
		for _, n := range d.Errors {
			if contains(d.Any, n) {
				a.expected--
			}
		}
		a.missed += len(r.Missed)
		if docCell[r.Doc] == nil {
			docCell[r.Doc] = map[string][2]int{}
		}
		v := docCell[r.Doc][r.Cell]
		v[0] += len(r.FalseItems)
		v[1] += len(r.Missed)
		docCell[r.Doc][r.Cell] = v
	}

	var b strings.Builder
	nCorrect := 0
	for _, d := range docs {
		if len(d.Errors) == 0 {
			nCorrect++
		}
	}
	fmt.Fprintf(&b, "\nModel %s, %d documents (%d correct, %d with judgment errors), %d runs per cell\n\n",
		name, len(docs), nCorrect, len(docs)-nCorrect, runs)
	b.WriteString("| cell | runs ok/total | false-alarm runs on correct docs | false-alarm items (all docs) | FA by item 0-6 (0 = scoped doc) | missed / expected | \"## Lỗi\" lines on correct docs (mean) | checklist unparsed | mean latency s | mean completion tok | mean prompt chars | runs without LLM | items sent to LLM 1-6 |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	summary := map[string]any{}
	for _, c := range cells {
		a := byCell[c.String()]
		if a == nil {
			continue
		}
		ok := float64(max(a.okRuns, 1))
		fa := fmt.Sprintf("%d/%d/%d/%d/%d/%d/%d", a.faByItem[0], a.faByItem[1], a.faByItem[2], a.faByItem[3], a.faByItem[4], a.faByItem[5], a.faByItem[6])
		llm := fmt.Sprintf("%d/%d/%d/%d/%d/%d", a.llmByItem[1], a.llmByItem[2], a.llmByItem[3], a.llmByItem[4], a.llmByItem[5], a.llmByItem[6])
		noLLM := "-"
		if c.Rules {
			noLLM = fmt.Sprintf("%d/%d", a.noLLM, a.okRuns)
		}
		fmt.Fprintf(&b, "| %s | %d/%d | %d/%d | %d | %s | %d/%d | %.2f | %d | %.2f | %.0f | %.0f | %s | %s |\n",
			c, a.okRuns, a.runs, a.faRuns, a.correctRuns, a.faItems, fa, a.missed, a.expected,
			float64(a.errLinesCorrect)/float64(max(a.correctRuns, 1)), a.unparsed,
			a.secs/ok, a.compl/ok, a.chars/ok, noLLM, llm)
		summary[c.String()] = map[string]any{
			"runs": a.runs, "ok_runs": a.okRuns, "failed": a.failed, "false_alarm_runs": a.faRuns,
			"correct_runs": a.correctRuns, "false_alarm_items": a.faItems, "false_alarm_by_item": a.faByItem[:],
			"missed": a.missed, "expected": a.expected, "unparsed": a.unparsed,
			"mean_seconds": a.secs / ok, "runs_without_llm": a.noLLM, "llm_by_item": a.llmByItem[1:], "mean_completion_tokens": a.compl / ok, "mean_prompt_chars": a.chars / ok,
		}
	}
	b.WriteString("\nPer document (false-alarm items / missed items, summed over runs):\n\n| document | expected Lỗi | note |")
	for _, c := range cells {
		fmt.Fprintf(&b, " %s |", c)
	}
	b.WriteString("\n|---|---|---|" + strings.Repeat("---|", len(cells)) + "\n")
	for _, d := range docs {
		fmt.Fprintf(&b, "| %s | %v | %s |", d.Name, d.Errors, d.Note)
		for _, c := range cells {
			v := docCell[d.Name][c.String()]
			fmt.Fprintf(&b, " %d / %d |", v[0], v[1])
		}
		b.WriteString("\n")
	}
	t.Log(b.String())
	fmt.Println(b.String())

	if out := os.Getenv("DOCFORMAT_LIVE_OUT"); out != "" {
		skeletons := map[string]string{}
		for _, d := range docs {
			skeletons[d.Name] = d.Report.Skeleton
		}
		raw, _ := json.MarshalIndent(map[string]any{
			"model": name, "runs_per_cell": runs, "summary": summary, "runs": results,
			"skeletons": skeletons, "skills_digest": SkillsDigest(),
		}, "", " ")
		if err := os.WriteFile(out, raw, 0o644); err != nil {
			t.Error(err)
		}
	}
}
