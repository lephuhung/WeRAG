package service

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// Degenerate OCR pages. SenOCR-Vi sometimes loses a page (typically a big
// table) to one token repeated hundreds of times ("TẾ TẾ TẾ …"). The
// docreader cleanup only collapses repeated segments of 10+ characters, so
// such a page reaches the draft. Here it is spotted, read again through the
// Go image OCR path (which read the same page correctly), and when that
// fails too the run is cut and the page is flagged for the reviewer.

const (
	// legalOCRRunMin: this many equal tokens in a row make a page degenerate.
	legalOCRRunMin = 6
	// a page this long whose distinct tokens are under this share of all
	// its tokens is degenerate too
	legalOCRRatioMinChars = 150
	legalOCRRatioMin      = 0.15
	legalOCRRatioMinWords = 20
	// default second-pass OCR endpoint (platform deployment) when neither the
	// platform organization nor the environment names one
	legalOCRDefaultEndpoint = "http://hrag-vllm-ocr:8001/v1"
	legalOCRDefaultModel    = "senocr-vi"
	legalOCRDefaultPenalty  = "1.1"
)

var (
	ocrPageSplitRe = regexp.MustCompile(`<!--\s*page\s+(\d+)\s*-->`)
	ocrTokenRe     = regexp.MustCompile(`\S+`)
	pageImageRe    = regexp.MustCompile(`_page_(\d+)\.(?:jpe?g|png)$`)
)

// LegalOCRReport is what the OCR repair did, stored as
// legal_document_drafts.ocr_report.
type LegalOCRReport struct {
	// PagesReOCR are the pages read a second time (degenerate first pass).
	PagesReOCR []int `json:"ocr_pages_reocr"`
	// DegeneratePages are still degenerate after both passes: their loops
	// were cut and the draft flags them for typing in from the PDF.
	DegeneratePages []int `json:"ocr_degenerate_pages"`
}

// ocrToken is one whitespace-separated token with its normalized form ("" for
// tokens without a letter: table pipes, numbers, punctuation).
type ocrToken struct {
	start, end int
	norm       string
}

func ocrTokens(text string) []ocrToken {
	var out []ocrToken
	for _, loc := range ocrTokenRe.FindAllStringIndex(text, -1) {
		raw := strings.TrimFunc(text[loc[0]:loc[1]], func(r rune) bool {
			return unicode.IsPunct(r) || unicode.IsSymbol(r)
		})
		norm := ""
		if strings.IndexFunc(raw, unicode.IsLetter) >= 0 {
			norm = strings.ToLower(raw)
		}
		out = append(out, ocrToken{start: loc[0], end: loc[1], norm: norm})
	}
	return out
}

// ocrRuns returns the [first, last] token index pairs of every run of at
// least legalOCRRunMin equal word tokens.
func ocrRuns(toks []ocrToken) [][2]int {
	var runs [][2]int
	for i := 0; i < len(toks); {
		j := i
		if toks[i].norm != "" {
			for j+1 < len(toks) && toks[j+1].norm == toks[i].norm {
				j++
			}
			if j-i+1 >= legalOCRRunMin {
				runs = append(runs, [2]int{i, j})
			}
		}
		i = j + 1
	}
	return runs
}

// HasOCRRepetition reports whether text holds a run of legalOCRRunMin or
// more equal words ("TẾ TẾ TẾ TẾ TẾ TẾ").
func HasOCRRepetition(text string) bool { return len(ocrRuns(ocrTokens(text))) > 0 }

// IsDegenerateOCRPage reports whether one page of OCR text is degenerate: a
// run of legalOCRRunMin equal words, or a long page whose distinct words are
// under legalOCRRatioMin of all its words.
func IsDegenerateOCRPage(text string) bool {
	toks := ocrTokens(text)
	if len(ocrRuns(toks)) > 0 {
		return true
	}
	if len([]rune(text)) <= legalOCRRatioMinChars {
		return false
	}
	words, distinct := 0, map[string]bool{}
	for _, t := range toks {
		if t.norm != "" {
			words++
			distinct[t.norm] = true
		}
	}
	return words >= legalOCRRatioMinWords && float64(len(distinct))/float64(words) < legalOCRRatioMin
}

// CollapseOCRRepetition keeps the first word of every run of
// legalOCRRunMin or more equal words, followed by " …".
func CollapseOCRRepetition(text string) string {
	runs := ocrRuns(ocrTokens(text))
	if len(runs) == 0 {
		return text
	}
	toks := ocrTokens(text)
	var b strings.Builder
	prev := 0
	for _, r := range runs {
		first, last := toks[r[0]], toks[r[1]]
		b.WriteString(text[prev:first.end])
		b.WriteString(" …")
		prev = last.end
	}
	b.WriteString(text[prev:])
	return b.String()
}

// ocrPage is one page of OCR Markdown: the text after its marker.
type ocrPage struct {
	number     int
	start, end int // byte span of the text (after the marker)
}

func splitOCRPages(markdown string) []ocrPage {
	locs := ocrPageSplitRe.FindAllStringSubmatchIndex(markdown, -1)
	pages := make([]ocrPage, 0, len(locs))
	for i, m := range locs {
		n, _ := strconv.Atoi(markdown[m[2]:m[3]])
		end := len(markdown)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		pages = append(pages, ocrPage{number: n, start: m[1], end: end})
	}
	return pages
}

// DegenerateOCRPages lists the pages ("<!-- page N -->" sections) of OCR
// Markdown that are degenerate.
func DegenerateOCRPages(markdown string) []int {
	var out []int
	for _, p := range splitOCRPages(markdown) {
		if IsDegenerateOCRPage(markdown[p.start:p.end]) {
			out = append(out, p.number)
		}
	}
	return out
}

// replaceOCRPages swaps the text of the given pages.
func replaceOCRPages(markdown string, texts map[int]string) string {
	pages := splitOCRPages(markdown)
	var b strings.Builder
	prev := 0
	for _, p := range pages {
		text, ok := texts[p.number]
		if !ok {
			continue
		}
		b.WriteString(markdown[prev:p.start])
		b.WriteString("\n\n" + strings.TrimSpace(text) + "\n\n")
		prev = p.end
	}
	b.WriteString(markdown[prev:])
	return b.String()
}

// LegalDraftPageImages renders the given pages of a PDF (page number → image).
type LegalDraftPageImages func(ctx context.Context, pdf []byte, fileName string, pages []int) (map[int][]byte, error)

// LegalDraftImageOCR reads one page image.
type LegalDraftImageOCR func(ctx context.Context, image []byte, page int) (string, error)

// repairDegeneratePages reads the degenerate pages of a first OCR pass
// again through the image OCR path and swaps in the pages that come back
// clean; pages still degenerate keep their first text with the loops cut.
func (s *LegalDraftService) repairDegeneratePages(
	ctx context.Context, markdown string, pdf []byte, fileName string,
) (string, LegalOCRReport) {
	report := LegalOCRReport{PagesReOCR: []int{}, DegeneratePages: []int{}}
	bad := DegenerateOCRPages(markdown)
	if len(bad) == 0 {
		return markdown, report
	}
	report.PagesReOCR = bad
	logger.Infof(ctx, "[LegalDraft] degenerate OCR on page(s) %v of %s: reading them again", bad, fileName)
	render, read := s.PageImages, s.ImageOCR
	if render == nil {
		render = s.renderPDFPages
	}
	if read == nil {
		read = s.ocrPageImage
	}
	images, err := render(ctx, pdf, fileName, bad)
	if err != nil {
		logger.Warnf(ctx, "[LegalDraft] rendering pages %v of %s: %v", bad, fileName, err)
	}
	texts := map[int]string{}
	pages := splitOCRPages(markdown)
	for _, n := range bad {
		var second string
		if img := images[n]; len(img) > 0 {
			second, err = read(ctx, img, n)
			if err != nil {
				logger.Warnf(ctx, "[LegalDraft] second OCR of page %d of %s: %v", n, fileName, err)
				second = ""
			}
		}
		if strings.TrimSpace(second) != "" && !IsDegenerateOCRPage(second) {
			texts[n] = second
			continue
		}
		report.DegeneratePages = append(report.DegeneratePages, n)
		for _, p := range pages {
			if p.number == n {
				texts[n] = CollapseOCRRepetition(markdown[p.start:p.end])
				break
			}
		}
	}
	logger.Infof(ctx, "[LegalDraft] OCR repair of %s: re-read %v, still degenerate %v", fileName, bad, report.DegeneratePages)
	return replaceOCRPages(markdown, texts), report
}

// renderPDFPages renders a PDF's pages through docreader's builtin pdf
// parser, which returns each scanned page as an image ("…_page_N.jpg")
// without OCRing it, and keeps the wanted pages.
func (s *LegalDraftService) renderPDFPages(ctx context.Context, pdf []byte, fileName string, pages []int) (map[int][]byte, error) {
	if s.reader == nil {
		return nil, fmt.Errorf("docreader is not connected")
	}
	reader, err := docparser.NewReader(ctx, docparser.BuiltinEngineName, "pdf", false, docparser.ReaderDeps{Remote: s.reader})
	if err != nil {
		return nil, err
	}
	res, err := reader.Read(ctx, &types.ReadRequest{
		FileContent: pdf, FileName: fileName, FileType: "pdf", ParserEngine: docparser.BuiltinEngineName,
	})
	if err != nil {
		return nil, err
	}
	if res == nil || res.Error != "" {
		msg := "empty result"
		if res != nil {
			msg = res.Error
		}
		return nil, fmt.Errorf("%s", msg)
	}
	want := map[int]bool{}
	for _, n := range pages {
		want[n] = true
	}
	out := map[int][]byte{}
	for _, ref := range res.ImageRefs {
		for _, name := range []string{ref.Filename, ref.OriginalRef} {
			if m := pageImageRe.FindStringSubmatch(name); m != nil {
				if n, _ := strconv.Atoi(m[1]); want[n] && len(ref.ImageData) > 0 {
					out[n] = ref.ImageData
				}
				break
			}
		}
	}
	return out, nil
}

// ocrPageImage reads one page image with the Go paddleocr_vl reader in
// OpenAI mode (one /chat/completions call), the path that read the pages
// the docreader pass looped on.
func (s *LegalDraftService) ocrPageImage(ctx context.Context, image []byte, page int) (string, error) {
	reader := docparser.NewPaddleOCRVLReader(s.secondPassOverrides(ctx), nil)
	res, err := reader.Read(ctx, &types.ReadRequest{
		FileContent: image, FileName: fmt.Sprintf("page_%d.jpg", page), FileType: "jpg",
	})
	if err != nil {
		return "", err
	}
	if res == nil || res.Error != "" {
		msg := "empty result"
		if res != nil {
			msg = res.Error
		}
		return "", fmt.Errorf("%s", msg)
	}
	return res.MarkdownContent, nil
}

// secondPassOverrides resolves the image OCR endpoint like an image
// attachment does (the organization's paddleocr_vl_* parser settings),
// for the platform organization; then DOCREADER_VN_OCR_URL/MODEL; then the
// platform deployment's SenOCR-Vi. The repetition penalty defaults to 1.1.
func (s *LegalDraftService) secondPassOverrides(ctx context.Context) map[string]string {
	out := map[string]string{}
	if s.tenants != nil {
		if tenant, err := s.tenants.GetTenantByID(ctx, types.PlatformTenantID); err == nil && tenant != nil &&
			tenant.ParserEngineConfig != nil {
			for k, v := range tenant.ParserEngineConfig.ToOverridesMap() {
				if strings.HasPrefix(k, "paddleocr_vl_") {
					out[k] = v
				}
			}
		}
	}
	if out["paddleocr_vl_endpoint"] == "" {
		out["paddleocr_vl_endpoint"] = firstNonEmpty(os.Getenv("DOCREADER_VN_OCR_URL"), legalOCRDefaultEndpoint)
		out["paddleocr_vl_api"] = "openai"
	}
	if out["paddleocr_vl_model"] == "" {
		out["paddleocr_vl_model"] = firstNonEmpty(os.Getenv("DOCREADER_VN_OCR_MODEL"), legalOCRDefaultModel)
	}
	if out["paddleocr_vl_repetition_penalty"] == "" {
		out["paddleocr_vl_repetition_penalty"] = legalOCRDefaultPenalty
	}
	return out
}
