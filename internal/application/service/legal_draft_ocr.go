package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
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
)

var (
	ocrPageSplitRe = regexp.MustCompile(`<!--\s*page\s+(\d+)\s*-->`)
	ocrTokenRe     = regexp.MustCompile(`\S+`)
	pageImageRe    = regexp.MustCompile(`(?:^|[/_])page_(\d+)\.(?:jpe?g|png)$`)
)

// LegalOCRReport is what the OCR repair did, stored as
// legal_document_drafts.ocr_report.
type LegalOCRReport struct {
	// PagesReOCR are the pages read a second time (degenerate first pass).
	PagesReOCR []int `json:"ocr_pages_reocr"`
	// DegeneratePages are still degenerate after both passes: their loops
	// were cut and the draft flags them for typing in from the PDF.
	DegeneratePages []int `json:"ocr_degenerate_pages"`
	// RejectedSamples keeps the start (1500 characters) of the last rejected
	// second-pass text of each page, for diagnosis.
	RejectedSamples map[int]string `json:"rejected_samples,omitempty"`
}

// ocrToken is one whitespace-separated token with its normalized form ("" for
// tokens without a letter: table pipes, numbers, punctuation).
type ocrToken struct {
	start, end int
	norm       string
}

// ocrHTMLTagRe: HTML tags (OCR tables) are not words; "<td></td><td></td>"
// rows would otherwise read as one word repeated.
var ocrHTMLTagRe = regexp.MustCompile(`</?[a-zA-Z][^<>]*>`)

func ocrTokens(text string) []ocrToken {
	if strings.Contains(text, "<") {
		// same length, so token offsets still index text
		text = ocrHTMLTagRe.ReplaceAllStringFunc(text, func(tag string) string { return strings.Repeat(" ", len(tag)) })
	}
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

// LegalDraftImageOCR reads one page image; attempt counts from 1 (the
// second attempt uses a stronger repetition penalty).
type LegalDraftImageOCR func(ctx context.Context, image []byte, page, attempt int) (string, error)

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
		accepted := false
		img := images[n]
		// attempt 1: the organization's OCR settings, as an image attachment
		// is read; attempt 2: the same image with a stronger repetition penalty
		for attempt := 1; attempt <= legalOCRAttempts && len(img) > 0; attempt++ {
			second, err = read(ctx, img, n, attempt)
			if err != nil {
				logger.Warnf(ctx, "[LegalDraft] second OCR of page %d of %s (attempt %d): %v", n, fileName, attempt, err)
				second = ""
			}
			second = normalizeSecondPassText(second)
			rejected := strings.TrimSpace(second) == "" || IsDegenerateOCRPage(second)
			logger.Infof(ctx, "[LegalDraft] second OCR of page %d of %s (attempt %d): %d chars, image %d bytes, rejected=%v",
				n, fileName, attempt, len([]rune(second)), len(img), rejected)
			if !rejected {
				accepted = true
				logger.Infof(ctx, "[LegalDraft] page %d of %s: attempt %d won", n, fileName, attempt)
				break
			}
			if second != "" {
				token, count := longestOCRRun(second)
				logger.Infof(ctx, "[LegalDraft] rejected OCR of page %d (attempt %d): longest run %q ×%d; starts %q",
					n, attempt, token, count, truncateDraftRunes(second, 400))
				if report.RejectedSamples == nil {
					report.RejectedSamples = map[int]string{}
				}
				report.RejectedSamples[n] = truncateDraftRunes(second, 1500)
			}
		}
		if accepted {
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

// legalOCRAttempts is how many times a degenerate page's image is read.
const legalOCRAttempts = 2

// Repetition penalties of the second pass. SenOCR-Vi loops on big table
// pages without one and reads them with 1.1; the readers only send a
// penalty that is set explicitly, so the second pass always sets one.
const (
	legalOCRPenalty      = "1.1"
	legalOCRRetryPenalty = "1.2"
)

// secondPassPenalty is the repetition penalty of an attempt: the platform
// organization's own value (else 1.1) first; then 1.1 when the first
// attempt used another value, else 1.2.
func secondPassPenalty(configured string, attempt int) string {
	configured = strings.TrimSpace(configured)
	first := configured
	if first == "" {
		first = legalOCRPenalty
	}
	if attempt <= 1 {
		return first
	}
	if first != legalOCRPenalty {
		return legalOCRPenalty
	}
	return legalOCRRetryPenalty
}

// normalizeSecondPassText puts an image OCR answer in the first pass's
// shape: Markdown tables instead of the model's HTML tables, "\n" endings.
func normalizeSecondPassText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.TrimSpace(docparser.NormalizeHTMLTables(text))
}

// longestOCRRun returns the word of the longest run of equal words and its length.
func longestOCRRun(text string) (string, int) {
	toks := ocrTokens(text)
	best, bestN := "", 0
	for i := 0; i < len(toks); {
		j := i
		for j+1 < len(toks) && toks[i].norm != "" && toks[j+1].norm == toks[i].norm {
			j++
		}
		if toks[i].norm != "" && j-i+1 > bestN {
			best, bestN = text[toks[i].start:toks[i].end], j-i+1
		}
		i = j + 1
	}
	return best, bestN
}

func truncateDraftRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// legalDraftMaxPageImageBytes caps one page image fetched from storage.
const legalDraftMaxPageImageBytes = 32 << 20

// LegalDraftStorageImage fetches a page image docreader left in shared
// storage (ImageRef.StorageKey) instead of sending it inline.
type LegalDraftStorageImage func(ctx context.Context, key string) ([]byte, error)

// renderPDFPages renders a PDF's pages through docreader's builtin pdf
// parser in force-scanned mode (pdf_force_scanned): every page comes back as
// an image "…_page_N.jpg" and nothing is OCRed. Force-scanned matters: a scan
// with a garbled text layer is read as text by the builtin router (only the
// vietnamese_legal parser reclassifies it), which would leave its pages
// without an image. Images come inline (ImageData) or, when docreader put
// them in shared storage, from their StorageKey.
func (s *LegalDraftService) renderPDFPages(ctx context.Context, pdf []byte, fileName string, pages []int) (map[int][]byte, error) {
	if s.reader == nil {
		return nil, fmt.Errorf("docreader is not connected")
	}
	overrides := map[string]string{"pdf_force_scanned": "true"}
	reader, err := docparser.NewReader(ctx, docparser.BuiltinEngineName, "pdf", false,
		docparser.ReaderDeps{Remote: s.reader, Overrides: overrides})
	if err != nil {
		return nil, err
	}
	res, err := reader.Read(ctx, &types.ReadRequest{
		FileContent: pdf, FileName: fileName, FileType: "pdf", ParserEngine: docparser.BuiltinEngineName,
		ParserEngineOverrides: overrides,
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
	return s.pickPageImages(ctx, res.ImageRefs, pages, fileName), nil
}

// pickPageImages keeps the images of the wanted pages from a render result,
// matching "…_page_N.jpg" on Filename or OriginalRef ("images/…" or bare),
// and logs where each wanted page's bytes came from.
func (s *LegalDraftService) pickPageImages(ctx context.Context, refs []types.ImageRef, pages []int, fileName string) map[int][]byte {
	want := map[int]bool{}
	for _, n := range pages {
		want[n] = true
	}
	out := map[int][]byte{}
	source := map[int]string{}
	matched := 0
	fetch := s.StorageImage
	if fetch == nil {
		fetch = s.fetchStorageImage
	}
	for _, ref := range refs {
		n := 0
		for _, name := range []string{ref.Filename, ref.OriginalRef} {
			if m := pageImageRe.FindStringSubmatch(strings.TrimSpace(name)); m != nil {
				n, _ = strconv.Atoi(m[1])
				break
			}
		}
		if !want[n] || len(out[n]) > 0 {
			continue
		}
		matched++
		switch {
		case len(ref.ImageData) > 0:
			out[n], source[n] = ref.ImageData, "inline"
		case strings.TrimSpace(ref.StorageKey) != "":
			data, err := fetch(ctx, strings.TrimSpace(ref.StorageKey))
			if err != nil || len(data) == 0 {
				logger.Warnf(ctx, "[LegalDraft] page %d of %s: fetching its image from storage failed: %v", n, fileName, err)
				source[n] = "storage failed"
				continue
			}
			out[n], source[n] = data, "storage"
		default:
			source[n] = "ref without bytes"
		}
	}
	var parts []string
	for _, n := range pages {
		where := source[n]
		if where == "" {
			where = "none"
		}
		parts = append(parts, fmt.Sprintf("%d:%s", n, where))
		if len(out[n]) == 0 {
			logger.Warnf(ctx, "[LegalDraft] no page image for page %d of %s (%s)", n, fileName, where)
		}
	}
	logger.Infof(ctx, "[LegalDraft] render of %s returned %d image ref(s), %d matched the wanted pages; %s",
		fileName, len(refs), matched, strings.Join(parts, ", "))
	return out
}

// fetchStorageImage reads a StorageKey: a storage reference (local://,
// minio://, …) through the file service, an http(s) URL through the
// SSRF-safe client.
func (s *LegalDraftService) fetchStorageImage(ctx context.Context, key string) ([]byte, error) {
	if !strings.HasPrefix(key, "http://") && !strings.HasPrefix(key, "https://") {
		if s.files == nil {
			return nil, fmt.Errorf("no file service for %s", key)
		}
		rc, err := s.files.GetFile(ctx, key)
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(io.LimitReader(rc, legalDraftMaxPageImageBytes))
	}
	if err := utils.ValidateURLForSSRF(key); err != nil {
		return nil, fmt.Errorf("storage URL blocked by SSRF policy: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, key, nil)
	if err != nil {
		return nil, err
	}
	client := utils.NewSSRFSafeHTTPClient(utils.SSRFSafeHTTPClientConfig{Timeout: 60 * time.Second, MaxRedirects: 3})
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("storage answered HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, legalDraftMaxPageImageBytes))
}

// ocrPageImage reads one page image with the Go paddleocr_vl reader in
// OpenAI mode (one /chat/completions call), the path that read the pages
// the docreader pass looped on.
func (s *LegalDraftService) ocrPageImage(ctx context.Context, image []byte, page, attempt int) (string, error) {
	overrides := s.secondPassOverrides(ctx)
	overrides["paddleocr_vl_repetition_penalty"] = secondPassPenalty(overrides["paddleocr_vl_repetition_penalty"], attempt)
	reader := docparser.NewPaddleOCRVLReader(overrides, nil)
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

// secondPassOverrides resolves the image OCR settings exactly as an image
// attachment does (the organization's ParserEngineConfig.ToOverridesMap()),
// for the platform organization; only when it names no endpoint/model,
// DOCREADER_VN_OCR_URL/MODEL, then the platform deployment's SenOCR-Vi. No
// repetition penalty is added: the reader's own default applies unless the
// organization configured one.
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
	return out
}
