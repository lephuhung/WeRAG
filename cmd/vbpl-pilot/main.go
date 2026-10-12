// Command vbpl-pilot converts a sample of crawled vbpl.vn HTML files with
// internal/vbpl and writes, per document, the Markdown, the block map and
// the planned chunks, plus a summary for reviewing the converter before a
// bulk ingest.
//
//	go run ./cmd/vbpl-pilot -html /home/orca/data/vbpl/html -ids ids.txt -out /tmp/pilot
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/net/html"

	"github.com/Tencent/WeKnora/internal/vbpl"
)

type docReport struct {
	ID         string     `json:"id"`
	Structured bool       `json:"structured"`
	Stats      vbpl.Stats `json:"stats"`
	Warnings   []string   `json:"warnings,omitempty"`
	Chunks     int        `json:"chunks"`
	SplitUnits int        `json:"split_units"`
	MaxChunk   int        `json:"max_chunk"`
	Tiny       int        `json:"tiny_chunks"` // < 60 runes
	Coverage   float64    `json:"coverage"`    // markdown letters / html letters
}

func main() {
	htmlDir := flag.String("html", "/home/orca/data/vbpl/html", "directory of <id>.html")
	idsFile := flag.String("ids", "", "file with one id per line")
	outDir := flag.String("out", "", "output directory")
	max := flag.Int("max", 2500, "max chunk size in runes")
	write := flag.Bool("write", true, "write per-document outputs (summary.json is always written)")
	flag.Parse()
	if *idsFile == "" || *outDir == "" {
		log.Fatal("-ids and -out are required")
	}
	f, err := os.Open(*idsFile)
	if err != nil {
		log.Fatal(err)
	}
	var ids []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if t := strings.TrimSpace(sc.Text()); t != "" {
			ids = append(ids, t)
		}
	}
	f.Close()
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatal(err)
	}

	var reports []docReport
	for _, id := range ids {
		raw, err := os.ReadFile(filepath.Join(*htmlDir, id+".html"))
		if err != nil {
			log.Printf("%s: %v", id, err)
			continue
		}
		doc, err := vbpl.ConvertBytes(raw)
		if err != nil {
			log.Printf("%s: %v", id, err)
			continue
		}
		chunks := vbpl.Plan(doc, *max)
		rep := docReport{ID: id, Structured: doc.Structured, Stats: doc.Stats, Warnings: doc.Warnings, Chunks: len(chunks)}
		runes := []rune(doc.Markdown)
		for _, c := range chunks {
			if c.Content != string(runes[c.Start:c.End]) {
				log.Fatalf("%s: chunk content is not the markdown slice", id)
			}
			n := c.End - c.Start
			if n > rep.MaxChunk {
				rep.MaxChunk = n
			}
			if n < 60 {
				rep.Tiny++
			}
			if c.Part == 1 {
				rep.SplitUnits++
			}
		}
		rep.Coverage = ratio(letters(doc.Markdown), htmlLetters(raw))
		reports = append(reports, rep)

		if !*write {
			continue
		}
		_ = os.WriteFile(filepath.Join(*outDir, id+".md"), []byte(doc.Markdown), 0o644)
		var sb strings.Builder
		for i, c := range chunks {
			fmt.Fprintf(&sb, "===== chunk %d [%s] part=%d runes=%d art=%s khoan=%v quoted=%v ids=%s %v\n# %s\n%s\n\n",
				i, c.Kind, c.Part, c.End-c.Start, c.ArticleNo, c.KhoanNos, c.Quoted, c.ArticleID, c.IDs, c.ContextHeader(), c.Content)
		}
		_ = os.WriteFile(filepath.Join(*outDir, id+".chunks.txt"), []byte(sb.String()), 0o644)
		bj, _ := json.MarshalIndent(doc, "", " ")
		_ = os.WriteFile(filepath.Join(*outDir, id+".blocks.json"), bj, 0o644)
	}

	sort.Slice(reports, func(i, j int) bool { return reports[i].Coverage < reports[j].Coverage })
	sj, _ := json.MarshalIndent(reports, "", " ")
	_ = os.WriteFile(filepath.Join(*outDir, "summary.json"), sj, 0o644)

	var structured, chunks, split, tiny, warn int
	for _, r := range reports {
		if r.Structured {
			structured++
		}
		chunks += r.Chunks
		split += r.SplitUnits
		tiny += r.Tiny
		if len(r.Warnings) > 0 {
			warn++
		}
	}
	fmt.Printf("docs=%d structured=%d chunks=%d split_units=%d tiny=%d docs_with_warnings=%d\n",
		len(reports), structured, chunks, split, tiny, warn)
}

func letters(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			n++
		}
	}
	return n
}

// htmlLetters counts letters/digits of the visible HTML text.
func htmlLetters(raw []byte) int {
	z := html.NewTokenizer(strings.NewReader(string(raw)))
	n, skip := 0, 0
	for {
		switch z.Next() {
		case html.ErrorToken:
			return n
		case html.StartTagToken:
			name, _ := z.TagName()
			if s := string(name); s == "style" || s == "script" || s == "head" {
				skip++
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			if s := string(name); (s == "style" || s == "script" || s == "head") && skip > 0 {
				skip--
			}
		case html.TextToken:
			if skip == 0 {
				n += letters(string(z.Text()))
			}
		}
	}
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 1
	}
	return float64(a) / float64(b)
}
