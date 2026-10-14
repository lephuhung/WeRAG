// Command offline scores the gate's typo decisions on typo_gate_set.jsonl
// without the backend: it calls the spell-check model and the chat model
// directly and runs the same evidence functions as the gate. A word read
// without asking is "wrong" only when it differs from gold (accepted tone
// placements aside); the live scorer is ../run.py.
//
//	go run ./scripts/typo_gate_eval/offline scripts/typo_gate_eval/typo_gate_set.jsonl
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/abbreviation"
)

var (
	spellURL   = env("SPELL_URL", "http://10.10.0.241:8003/v1/chat/completions")
	spellModel = env("SPELL_MODEL", "spellcheck-gemma4-e4b-v32")
	chatURL    = env("CHAT_URL", "http://10.10.0.240:8000/v1/chat/completions")
	chatModel  = env("CHAT_MODEL", "Qwen/Qwen3.6-35B-A3B-FP8")
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// sameReading compares readings ignoring case and old/new tone placement
// (hủy / huỷ).
func sameReading(a, b string) bool {
	return strings.EqualFold(a, b) || abbreviation.StripMarks(a) == abbreviation.StripMarks(b) && toneOf(a) == toneOf(b)
}

func toneOf(s string) string {
	var t []rune
	for _, r := range norm.NFD.String(s) {
		if strings.ContainsRune("\u0300\u0301\u0303\u0309\u0323", r) {
			t = append(t, r)
		}
	}
	return string(t)
}

type row struct {
	Kind, Query, Word, Gold string
	OOV                     bool `json:"oov"`
}

func call(url, model string, msgs []map[string]string, jsonFmt bool) string {
	body := map[string]any{"model": model, "temperature": 0.01, "max_tokens": 400, "messages": msgs,
		"chat_template_kwargs": map[string]any{"enable_thinking": false}}
	if jsonFmt {
		body["response_format"] = map[string]string{"type": "json_object"}
	}
	b, _ := json.Marshal(body)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var out struct {
		Choices []struct{ Message struct{ Content string } }
	}
	json.NewDecoder(resp.Body).Decode(&out)
	if len(out.Choices) == 0 {
		return ""
	}
	return out.Choices[0].Message.Content
}

func main() {
	src, _ := os.ReadFile("internal/application/service/abbreviation.go")
	rp := regexp.MustCompile("(?s)const abbreviationRewritePrompt = `(.*?)`").FindSubmatch(src)[1]
	sp := tools.DefaultSpellcheckPrompt
	shortForms := strings.Fields("UBND HĐND TTHC BHXH BHYT CCCD GPLX CNTT ATTT VBQPPL BMNN NCKH KTXH CBCC TNMT GDĐT NĐ TT QĐ CSDL PCCC CP TP")
	f, _ := os.Open(os.Args[1])
	var rows []row
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r row
		json.Unmarshal(sc.Bytes(), &r)
		rows = append(rows, r)
	}
	type res struct {
		r       row
		verdict map[string]string
	}
	out := make([]res, len(rows))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i, r := range rows {
		wg.Add(1)
		go func(i int, r row) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			v := map[string]string{}
			known := map[string]bool{}
			for _, s := range shortForms {
				known[strings.ToLower(s)] = true
			}
			var suspects []string
			for _, w := range abbreviation.SuspectWords(r.Query) {
				if !known[strings.ToLower(w)] {
					suspects = append(suspects, w)
				}
			}
			if len(suspects) == 0 {
				out[i] = res{r, v}
				return
			}
			spell := map[string]string{}
			g := call(spellURL, spellModel,
				[]map[string]string{{"role": "system", "content": string(sp)}, {"role": "user", "content": "Đoạn cần kiểm tra:\n[0] " + r.Query + "\n"}}, false)
			var gf struct {
				Findings []struct{ Wrong, Correct string }
			}
			json.Unmarshal([]byte(g), &gf)
			var pairs [][2]string
			for _, x := range gf.Findings {
				pairs = append(pairs, abbreviation.TypoWords(x.Wrong, x.Correct)...)
			}
			for _, t := range abbreviation.TypoSuggestions(r.Query, pairs) {
				spell[strings.ToLower(t.Wrong)] = t.Correct
			}
			q := call(chatURL, chatModel,
				[]map[string]string{{"role": "system", "content": string(rp)}, {"role": "user", "content": "Câu hỏi: " + r.Query + "\nTừ nghi gõ sai: " + strings.Join(suspects, ", ")}}, true)
			var qo struct{ Corrected string }
			if a, b := strings.Index(q, "{"), strings.LastIndex(q, "}"); a >= 0 && b > a {
				json.Unmarshal([]byte(q[a:b+1]), &qo)
			}
			var readings map[string]abbreviation.RewriteReading
			if qo.Corrected != "" {
				readings = abbreviation.RewriteReadings(r.Query, qo.Corrected, suspects)
			}
			for _, w := range suspects {
				k := strings.ToLower(w)
				rr := readings[k]
				e := abbreviation.TypoEvidence{Word: w, Spellcheck: spell[k], Rewrite: rr.Text, RewriteRan: qo.Corrected != "",
					RewriteAbbreviation: rr.Abbreviation, Dictionary: abbreviation.NearShortForm(w, shortForms)}
				if rr.Text != "" && abbreviation.NewAbbreviationIn(rr.Text, known) {
					e.Rewrite, e.RewriteAbbreviation = "", true
				}
				e.Decoded, _ = abbreviation.DecodeTyping(w)
				e.Unmarked = abbreviation.UnmarkedReading(w, e.Rewrite, e.Spellcheck)
				verdict, corr, opts := abbreviation.DecideTypo(e)
				switch verdict {
				case abbreviation.TypoCorrect:
					v[k] = "AUTO:" + corr
				case abbreviation.TypoAsk:
					v[k] = "ASK:" + strings.Join(opts, "|")
				case abbreviation.TypoAbbreviation:
					v[k] = "ABBR"
				case abbreviation.TypoNone:
					v[k] = "NONE"
				}
			}
			out[i] = res{r, v}
		}(i, r)
	}
	wg.Wait()
	type agg struct{ n, auto, autoOK, wrong, ask, abbr, none int }
	stats := map[string]*agg{}
	for _, o := range out {
		kind := o.r.Kind
		if kind == "typo" {
			if o.r.OOV {
				kind = "typo(oov)"
			} else {
				kind = "typo(word)"
			}
		}
		a := stats[kind]
		if a == nil {
			a = &agg{}
			stats[kind] = a
		}
		a.n++
		w := strings.ToLower(o.r.Word)
		anyAuto, anyAsk, anyAbbr := false, false, false
		for k, v := range o.verdict {
			switch {
			case strings.HasPrefix(v, "AUTO:"):
				anyAuto = true
				if k == w && o.r.Kind == "typo" && sameReading(strings.TrimPrefix(v, "AUTO:"), o.r.Gold) {
					a.autoOK++
				} else if k != w && abbreviation.UnmarkedReading(k, strings.TrimPrefix(v, "AUTO:")) != "" {
					// an unmarked word given its marks (the source question was typed without them)
				} else {
					a.wrong++
					fmt.Printf("WRONG AUTO [%s] %q %s=%s (gold %q)\n", o.r.Kind, o.r.Query, k, v, o.r.Gold)
				}
			case strings.HasPrefix(v, "ASK"):
				anyAsk = true
			case v == "ABBR":
				anyAbbr = true
			}
		}
		if anyAuto {
			a.auto++
		}
		if anyAsk {
			a.ask++
			fmt.Printf("ask  [%s] %q %v\n", o.r.Kind, o.r.Query, o.verdict)
		}
		if anyAbbr {
			a.abbr++
		}
		if len(o.verdict) == 0 || (!anyAuto && !anyAsk && !anyAbbr) {
			a.none++
		}
	}
	fmt.Println("\nkind          n  auto_ok  wrong_auto  ask_typo  abbr_ask  nothing")
	for _, k := range []string{"typo(oov)", "typo(word)", "abbr", "name", "clean"} {
		if a := stats[k]; a != nil {
			fmt.Printf("%-11s %3d  %7d  %10d  %8d  %8d  %7d\n", k, a.n, a.autoOK, a.wrong, a.ask, a.abbr, a.none)
		}
	}
}
