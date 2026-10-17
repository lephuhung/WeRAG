//go:build pageindexeval

package service

// Offline quality check of the attachment section picker (PageIndex):
// PI_DOCS (dumped temporary_documents), PI_QUESTIONS (questions with the
// gold chunk), PI_LLM (OpenAI-compatible base URL), PI_MODEL, PI_OUT.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/infrastructure/chunker"
	"github.com/Tencent/WeKnora/internal/types"
)

type piDoc struct {
	ID         string                         `json:"id"`
	FileName   string                         `json:"file_name"`
	TokenCount int                            `json:"token_count"`
	Content    string                         `json:"content"`
	Chunks     []types.TemporaryDocumentChunk `json:"chunks"`
}

type piQuestion struct {
	Doc  string `json:"doc"`
	File string `json:"file"`
	Q    string `json:"q"`
	Gold []int  `json:"gold"`
	Kind string `json:"kind"`
}

func piChat(base, model, system, user string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model": model, "temperature": 0, "max_tokens": 200,
		"messages":             []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}},
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
	})
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Post(base+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Choices []struct {
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("no choices")
	}
	return out.Choices[0].Message.Content, nil
}

func TestPageIndexEval(t *testing.T) {
	var docs []piDoc
	var qs []piQuestion
	raw, _ := os.ReadFile(os.Getenv("PI_DOCS"))
	if err := json.Unmarshal(raw, &docs); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(os.Getenv("PI_QUESTIONS"))
	if err := json.Unmarshal(raw, &qs); err != nil {
		t.Fatal(err)
	}
	byID := map[string]*piDoc{}
	for i := range docs {
		d := &docs[i]
		byID[d.ID] = d
		secs := buildAttachmentSections(d.Content, d.Chunks)
		o, _ := renderAttachmentOutline(secs, attachmentOutlinePickerRunes)
		t.Logf("DOC %s tokens=%d chunks=%d sections=%d\n%s", d.FileName, d.TokenCount, len(d.Chunks), len(secs), o)
	}
	budgets := []int{12000, 3000}
	type row struct {
		Q         piQuestion `json:"q"`
		Budget    int        `json:"budget"`
		Sections  int        `json:"sections"`
		Pick      []int      `json:"pick"`
		Whole     bool       `json:"whole"`
		PickErr   string     `json:"pick_err,omitempty"`
		PickHit   bool       `json:"pick_hit"` // gold chunk inside a picked section
		PickRange []int      `json:"pick_range"`
		PIChunks  []int      `json:"pi_chunks"`
		PITokens  int        `json:"pi_tokens"`
		PIHit     bool       `json:"pi_hit"`
		KWChunks  []int      `json:"kw_chunks"`
		KWTokens  int        `json:"kw_tokens"`
		KWHit     bool       `json:"kw_hit"`
		Latency   float64    `json:"latency_s"`
	}
	var mu sync.Mutex
	var rows []row
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for _, q := range qs {
		d := byID[q.Doc]
		if d == nil {
			continue
		}
		wg.Add(1)
		go func(q piQuestion, d *piDoc) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			secs := buildAttachmentSections(d.Content, d.Chunks)
			var pick attachmentPick
			var perr error
			var lat float64
			if secs != nil {
				o, _ := renderAttachmentOutline(secs, attachmentOutlinePickerRunes)
				system := fmt.Sprintf(attachmentPickerPrompt, attachmentPickMaxSections)
				if os.Getenv("PI_VARIANT") == "lead" {
					o = piLeadOutline(d.Content, secs)
					system = strings.Replace(system, "tiêu đề và độ dài.", "tiêu đề, độ dài và câu mở đầu của phần (sau dấu ↳).", 1)
					system = strings.Replace(system, "Nếu không phần nào liên quan, trả về danh sách rỗng.",
						"Nếu không chắc phần nào, chọn vài phần có khả năng nhất; chỉ trả danh sách rỗng khi câu hỏi không liên quan đến tài liệu.", 1)
				}
				if f := os.Getenv("PI_PROMPT"); f != "" {
					b, _ := os.ReadFile(f)
					system = fmt.Sprintf(string(b), attachmentPickMaxSections)
				}
				st := time.Now()
				reply, err := piChat(os.Getenv("PI_LLM"), os.Getenv("PI_MODEL"), system,
					fmt.Sprintf("Tài liệu: %s\n\nMục lục:\n%s\n\nCâu hỏi: %s", d.FileName, o, q.Q))
				lat = time.Since(st).Seconds()
				if err == nil {
					pick, perr = parseAttachmentPick(reply, len(secs))
				} else {
					perr = err
				}
			} else {
				perr = fmt.Errorf("no outline")
			}
			gold := map[int]bool{}
			for _, g := range q.Gold {
				gold[g] = true
			}
			hit := func(idx []int) bool {
				for _, i := range idx {
					if gold[i] {
						return true
					}
				}
				return false
			}
			tok := func(idx []int) int {
				n := 0
				for _, i := range idx {
					n += d.Chunks[i].TokenCount
				}
				return n
			}
			for _, b := range budgets {
				r := row{Q: q, Budget: b, Sections: len(secs), Pick: pick.Sections, Whole: pick.Whole, Latency: lat}
				if perr != nil {
					r.PickErr = perr.Error()
				}
				for _, id := range pick.Sections {
					s := secs[id-1]
					for i := s.First; i <= s.Last && s.First >= 0; i++ {
						r.PickRange = append(r.PickRange, i)
						if gold[i] {
							r.PickHit = true
						}
					}
				}
				if perr == nil {
					outline, _ := renderAttachmentOutline(secs, attachmentOutlinePromptRunes)
					cb := b - chunker.ApproxTokenCount(outline, chunker.DetectLanguage(outline))
					if cb < b/2 {
						cb = b / 2
					}
					r.PIChunks = packPickedSections(d.Chunks, secs, pick, q.Q, cb)
					r.PITokens, r.PIHit = tok(r.PIChunks), hit(r.PIChunks)
				}
				// keyword baseline with the same budget, inline off
				doc := &types.TemporaryDocument{TokenCount: d.TokenCount, Content: d.Content}
				doc.Chunks, _ = json.Marshal(d.Chunks)
				content, _, _ := selectTemporaryDocumentContentWithLimits(doc, q.Q, b, 0)
				for i, c := range d.Chunks {
					if strings.Contains(content, strings.TrimSpace(c.Content)) {
						r.KWChunks = append(r.KWChunks, i)
					}
				}
				r.KWTokens, r.KWHit = tok(r.KWChunks), hit(r.KWChunks)
				mu.Lock()
				rows = append(rows, r)
				mu.Unlock()
			}
		}(q, d)
	}
	wg.Wait()
	out, _ := json.MarshalIndent(rows, "", " ")
	_ = os.WriteFile(os.Getenv("PI_OUT"), out, 0o644)
}

// piLeadOutline is the outline with the first words of each section's own
// text under its heading.
func piLeadOutline(content string, secs []attachmentSection) string {
	runes := []rune(content)
	var b strings.Builder
	for _, s := range secs {
		fmt.Fprintf(&b, "%s[S%d] %s (~%d token)\n", strings.Repeat("  ", s.Depth), s.ID, clipOutlineTitle(s.Title), s.Tokens)
		end := s.OwnEnd
		if end <= s.Start || end > len(runes) {
			end = s.End
		}
		if end > len(runes) {
			end = len(runes)
		}
		own := string(runes[s.Start:end])
		if i := strings.Index(own, "\n"); i >= 0 && s.Title != "Mở đầu" {
			own = own[i+1:]
		}
		own = strings.Join(strings.Fields(strings.NewReplacer("*", "", "#", "", "|", " ").Replace(own)), " ")
		if r := []rune(own); len(r) > 160 {
			own = string(r[:160]) + "…"
		}
		if own != "" {
			fmt.Fprintf(&b, "%s   ↳ %s\n", strings.Repeat("  ", s.Depth), own)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
