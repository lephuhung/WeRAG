package docparser

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/searchutil"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
)

const paddleOCRVLTimeout = 1000 * time.Second // large scanned PDFs can take a while

// PaddleOCRVLReader calls a self-hosted OCR service. Two wire protocols are
// supported, selected per-tenant:
//
//   - "layout" (PaddleX pipeline server, default): POST {endpoint}/layout-parsing
//     with base64 file → synchronous JSON with per-page markdown + inline
//     base64 images.
//   - "openai" (OpenAI-compatible vision OCR, e.g. a vLLM serving of
//     SenOCR-Vi or Unlimited-OCR): PDFs are delegated to the docreader service
//     which renders each page and calls {endpoint}/chat/completions per page
//     (engine "openai_ocr" on the docreader side); standalone image files are
//     OCR'd directly here with a single chat-completions call.
//
// Mode is auto-detected: an endpoint whose path ends in "/v1" selects
// "openai"; anything else selects "layout". paddleocr_vl_api overrides it.
type PaddleOCRVLReader struct {
	endpoint      string
	apiMode       string
	ocrModel      string
	ocrAPIKey     string
	ocrPrompt     string
	ocrVllmXargs  string
	ocrRepPenalty float64
	// ocrRepPenaltySet reports whether the tenant explicitly configured
	// paddleocr_vl_repetition_penalty; only then is it forwarded to the
	// docreader (which otherwise falls back to its own env default).
	ocrRepPenaltySet bool
	useSeal          bool
	useChart         bool
	remote           interfaces.DocReader
}

// NewPaddleOCRVLReader creates a reader from ParserEngineOverrides.
// remote is the docreader client used for OpenAI-mode PDF rendering (may be nil).
func NewPaddleOCRVLReader(overrides map[string]string, remote interfaces.DocReader) *PaddleOCRVLReader {
	// Default 1.05 breaks SenOCR-Vi's degenerate loops (instruction-template
	// echoes run to max_tokens otherwise); set the override to "1" or "0"
	// for endpoints that reject the repetition_penalty extension.
	rp, rpSet := 1.05, false
	if raw := strings.TrimSpace(overrides["paddleocr_vl_repetition_penalty"]); raw != "" {
		rpSet = true
		if v, err := strconv.ParseFloat(raw, 64); err == nil {
			rp = v
		}
	}
	return &PaddleOCRVLReader{
		endpoint:         strings.TrimRight(overrides["paddleocr_vl_endpoint"], "/"),
		apiMode:          resolvePaddleOCRVLAPIMode(overrides),
		ocrModel:         strings.TrimSpace(overrides["paddleocr_vl_model"]),
		ocrAPIKey:        overrides["paddleocr_vl_api_key"],
		ocrPrompt:        overrides["paddleocr_vl_prompt"],
		ocrVllmXargs:     strings.TrimSpace(overrides["paddleocr_vl_vllm_xargs"]),
		ocrRepPenalty:    rp,
		ocrRepPenaltySet: rpSet,
		useSeal:          parseBoolOr(overrides["paddleocr_vl_use_seal_recognition"], true),
		useChart:         parseBoolOr(overrides["paddleocr_vl_use_chart_recognition"], false),
		remote:           remote,
	}
}

// resolvePaddleOCRVLAPIMode picks the wire protocol: an explicit
// paddleocr_vl_api=layout|openai wins; otherwise a "/v1"-suffixed endpoint
// (OpenAI convention) selects openai and anything else selects layout.
func resolvePaddleOCRVLAPIMode(overrides map[string]string) string {
	switch strings.ToLower(strings.TrimSpace(overrides["paddleocr_vl_api"])) {
	case "layout", "paddlex":
		return "layout"
	case "openai", "vllm":
		return "openai"
	}
	if strings.HasSuffix(strings.TrimRight(overrides["paddleocr_vl_endpoint"], "/"), "/v1") {
		return "openai"
	}
	return "layout"
}

func isImageFileType(req *types.ReadRequest) bool {
	ft := strings.ToLower(strings.TrimPrefix(req.FileType, "."))
	if ft == "" {
		ft = strings.TrimPrefix(strings.ToLower(filepath.Ext(req.FileName)), ".")
	}
	switch ft {
	case "jpg", "jpeg", "png", "gif", "bmp", "tiff", "tif", "webp":
		return true
	}
	return false
}

func (c *PaddleOCRVLReader) Read(ctx context.Context, req *types.ReadRequest) (*types.ReadResult, error) {
	if c.endpoint == "" {
		return &types.ReadResult{Error: "PaddleOCR-VL endpoint is not configured"}, nil
	}
	if err := utils.ValidateURLForSSRF(c.endpoint); err != nil {
		return &types.ReadResult{Error: fmt.Sprintf("PaddleOCR-VL endpoint blocked by SSRF policy: %v", err)}, nil
	}

	if c.apiMode == "openai" {
		return c.readOpenAI(ctx, req)
	}

	content := req.FileContent
	if len(content) == 0 {
		return &types.ReadResult{Error: "no file content provided"}, nil
	}

	logger.Infof(context.Background(), "[PaddleOCR-VL] Parsing file=%s size=%d via %s",
		req.FileName, len(content), c.endpoint)

	mdContent, imagesB64, err := c.callLayoutParsing(ctx, req, content)
	if err != nil {
		return nil, fmt.Errorf("PaddleOCR-VL layout-parsing: %w", err)
	}

	// PaddleOCR-VL renders tables as styled HTML (per-cell text-align), which
	// wastes tokens and defeats the chunker's table-protection logic. Convert
	// them to Markdown tables (or strip layout attributes when conversion is
	// not possible) before downstream processing.
	mdContent = NormalizeHTMLTables(mdContent)

	imageRefs, mdContent := c.processImages(mdContent, imagesB64)
	mdContent, imageRefs = ensureOriginalImageRef(req, mdContent, imageRefs)

	logger.Infof(context.Background(), "[PaddleOCR-VL] Parsed successfully, markdown=%d chars, images=%d",
		len(mdContent), len(imageRefs))

	return &types.ReadResult{
		MarkdownContent: mdContent,
		ImageRefs:       imageRefs,
	}, nil
}

// paddleOCRVLRecognitionParams returns the recognition / page-restructuring
// parameters shared by the self-hosted (/layout-parsing, top-level body) and
// cloud (optionalPayload) request bodies. Keeping both identical ensures the
// self-hosted engine reproduces the cloud output: cross-page table merging,
// multi-level heading reconstruction, header/footer stripping, and the same
// sampling / resolution settings used by the AI Studio service.
func paddleOCRVLRecognitionParams(useSeal, useChart bool) map[string]interface{} {
	return map[string]interface{}{
		"markdownIgnoreLabels": []string{
			"header", "header_image", "footer", "footer_image",
			"number", "footnote", "aside_text",
		},
		"useDocOrientationClassify": false,
		"useDocUnwarping":           false,
		"useLayoutDetection":        true,
		"useChartRecognition":       useChart,
		"useSealRecognition":        useSeal,
		"useOcrForImageBlock":       false,
		"mergeTables":               true,
		"relevelTitles":             true,
		"restructurePages":          true,
		"layoutShapeMode":           "auto",
		"promptLabel":               "ocr",
		"layoutNms":                 true,
		"repetitionPenalty":         1,
		"temperature":               0,
		"topP":                      1,
		"minPixels":                 147384,
		"maxPixels":                 2822400,
	}
}

// fileTypeCode maps a request to the PaddleOCR-VL fileType field:
// 0 = PDF, 1 = image (including TIFF).
func fileTypeCode(req *types.ReadRequest) int {
	ft := strings.ToLower(strings.TrimPrefix(req.FileType, "."))
	if ft == "" {
		ft = strings.TrimPrefix(strings.ToLower(filepath.Ext(req.FileName)), ".")
	}
	if ft == "pdf" {
		return 0
	}
	return 1
}

// paddleOCRVLResponse mirrors the relevant fields of the PaddleX serving
// /layout-parsing response. The service returns one entry per page.
type paddleOCRVLResponse struct {
	ErrorCode int    `json:"errorCode"`
	ErrorMsg  string `json:"errorMsg"`
	Result    struct {
		LayoutParsingResults []struct {
			Markdown struct {
				Text   string            `json:"text"`
				Images map[string]string `json:"images"`
			} `json:"markdown"`
		} `json:"layoutParsingResults"`
	} `json:"result"`
}

func (c *PaddleOCRVLReader) callLayoutParsing(
	ctx context.Context, req *types.ReadRequest, content []byte,
) (string, map[string]string, error) {
	payload := paddleOCRVLRecognitionParams(c.useSeal, c.useChart)
	payload["file"] = base64.StdEncoding.EncodeToString(content)
	payload["fileType"] = fileTypeCode(req)
	payload["visualize"] = false

	body, err := json.Marshal(payload)
	if err != nil {
		return "", nil, fmt.Errorf("marshal payload: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.endpoint+"/layout-parsing", bytes.NewReader(body),
	)
	if err != nil {
		return "", nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := utils.NewSSRFSafeHTTPClient(utils.SSRFSafeHTTPClientConfig{
		Timeout:      paddleOCRVLTimeout,
		MaxRedirects: 5,
	})
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", nil, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("PaddleOCR-VL API status %d: %s", resp.StatusCode, string(respBody))
	}

	var result paddleOCRVLResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", nil, fmt.Errorf("decode response: %w", err)
	}
	if result.ErrorCode != 0 {
		return "", nil, fmt.Errorf("PaddleOCR-VL error %d: %s", result.ErrorCode, result.ErrorMsg)
	}

	pages := result.Result.LayoutParsingResults
	if len(pages) == 0 {
		logger.Errorf(context.Background(), "[PaddleOCR-VL] response has no layoutParsingResults")
		return "", nil, nil
	}

	// Merge per-page markdown and image dicts into one document.
	texts := make([]string, 0, len(pages))
	images := make(map[string]string)
	for _, p := range pages {
		if t := strings.TrimSpace(p.Markdown.Text); t != "" {
			texts = append(texts, p.Markdown.Text)
		}
		for path, data := range p.Markdown.Images {
			if _, ok := images[path]; !ok {
				images[path] = data
			}
		}
	}

	logger.Infof(context.Background(), "[PaddleOCR-VL] parsed %d page(s), images=%d", len(pages), len(images))
	return strings.Join(texts, "\n\n"), images, nil
}

// processImages decodes the inline base64 images returned by PaddleOCR-VL and
// builds ImageRef entries, matching them against references in the markdown.
func (c *PaddleOCRVLReader) processImages(
	mdContent string, imagesB64 map[string]string,
) ([]types.ImageRef, string) {
	var refs []types.ImageRef

	for ipath, b64Str := range imagesB64 {
		matchedRefs := mineruImageOriginalRefs(mdContent, ipath)
		if len(matchedRefs) == 0 {
			continue
		}

		var imgBytes []byte
		var ext string
		if m := b64DataURIPattern.FindStringSubmatch(b64Str); len(m) == 3 {
			ext = m[1]
			decoded, err := base64.StdEncoding.DecodeString(m[2])
			if err != nil {
				logger.Errorf(context.Background(), "[PaddleOCR-VL] decode base64 image %s: %v", ipath, err)
				continue
			}
			imgBytes = decoded
		} else {
			decoded, err := base64.StdEncoding.DecodeString(b64Str)
			if err != nil {
				logger.Errorf(context.Background(), "[PaddleOCR-VL] decode raw base64 image %s: %v", ipath, err)
				continue
			}
			imgBytes = decoded
			ext = strings.TrimPrefix(filepath.Ext(ipath), ".")
			if ext == "" {
				ext = "png"
			}
		}

		mimeType := mime.TypeByExtension("." + ext)
		if mimeType == "" {
			mimeType = "image/png"
		}

		for _, originalRef := range matchedRefs {
			refs = append(refs, types.ImageRef{
				Filename:    ipath,
				OriginalRef: originalRef,
				MimeType:    mimeType,
				ImageData:   imgBytes,
			})
		}
	}

	return refs, mdContent
}

// readOpenAI routes an OpenAI-compatible OCR endpoint (vLLM /chat/completions).
// PDFs are delegated to the docreader's openai_ocr engine which renders each
// page with pdfium and OCRs per page; standalone images are OCR'd here with a
// single chat-completions call.
func (c *PaddleOCRVLReader) readOpenAI(ctx context.Context, req *types.ReadRequest) (*types.ReadResult, error) {
	if isImageFileType(req) {
		md, err := c.openAIOCRImage(ctx, req.FileContent)
		if err != nil {
			return &types.ReadResult{Error: fmt.Sprintf("OCR request failed: %v", err)}, nil
		}
		return &types.ReadResult{
			MarkdownContent: md,
			ImageRefs:       []types.ImageRef{},
			Metadata:        map[string]string{"conversion_engine": "openai_ocr"},
		}, nil
	}

	if c.remote == nil {
		return &types.ReadResult{Error: "docreader service unavailable; required for OpenAI OCR of PDF files"}, nil
	}
	// paddleocr_vl_vllm_xargs defaults off: SenOCR-Vi / PaddleOCR-VL endpoints
	// run no NGram logits processor, so Unlimited-OCR's per-request extras
	// would be meaningless. Set it to 1 only for engines that still serve
	// Unlimited-OCR.
	xargs := c.ocrVllmXargs
	if xargs == "" {
		xargs = "0"
	}
	overrides := map[string]string{
		"openai_ocr_url":        c.endpoint,
		"openai_ocr_model":      c.ocrModel,
		"openai_ocr_api_key":    c.ocrAPIKey,
		"openai_ocr_prompt":     c.ocrPrompt,
		"openai_ocr_vllm_xargs": xargs,
	}
	if c.ocrRepPenaltySet {
		overrides["openai_ocr_repetition_penalty"] = strconv.FormatFloat(c.ocrRepPenalty, 'f', -1, 64)
	}
	res, err := c.remote.Read(ctx, &types.ReadRequest{
		FileContent:           req.FileContent,
		FileName:              req.FileName,
		FileType:              req.FileType,
		URL:                   req.URL,
		Title:                 req.Title,
		RequestID:             req.RequestID,
		ParserEngine:          "openai_ocr",
		ParserEngineOverrides: overrides,
	})
	if err != nil {
		return &types.ReadResult{Error: fmt.Sprintf("docreader openai_ocr failed: %v", err)}, nil
	}
	if res.Metadata == nil {
		res.Metadata = map[string]string{}
	}
	res.Metadata["conversion_engine"] = "openai_ocr"
	return res, nil
}

// openAIOCRImage sends one image through an OpenAI-compatible
// /chat/completions endpoint and returns the extracted text.
func (c *PaddleOCRVLReader) openAIOCRImage(ctx context.Context, image []byte) (string, error) {
	if len(image) == 0 {
		return "", fmt.Errorf("empty image content")
	}
	prompt := c.ocrPrompt
	if prompt == "" {
		// "OCR:" is the PaddleOCR-VL task prompt SenOCR-Vi was trained with;
		// generic VLM endpoints can override via paddleocr_vl_prompt.
		prompt = "OCR:"
	}
	mime := "image/png"
	if len(image) > 2 && image[0] == 0xFF && image[1] == 0xD8 {
		mime = "image/jpeg"
	}
	reqBody := map[string]any{
		"model": c.ocrModel,
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "image_url", "image_url": map[string]any{
						"url": fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(image)),
					}},
					map[string]any{"type": "text", "text": prompt},
				},
			},
		},
		"temperature": 0.0,
	}
	if c.ocrRepPenalty > 0 && c.ocrRepPenalty != 1.0 {
		reqBody["repetition_penalty"] = c.ocrRepPenalty
	}
	payload, _ := json.Marshal(reqBody)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.ocrAPIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.ocrAPIKey)
	}
	resp, err := (&http.Client{Timeout: 300 * time.Second}).Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OCR endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("invalid OCR response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("OCR response has no choices")
	}
	var text string
	if err := json.Unmarshal(parsed.Choices[0].Message.Content, &text); err != nil {
		var parts []struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(parsed.Choices[0].Message.Content, &parts); err != nil {
			return "", fmt.Errorf("unrecognized OCR response content")
		}
		for _, p := range parts {
			text += p.Text
		}
	}
	return stripOCRMarkup(text), nil
}

// stripOCRMarkup removes detector-style markup some OCR VLMs emit:
// Unlimited-OCR wraps regions as <|det|>LABEL [x1,y1,x2,y2]<|/det|>TEXT and
// may leave stray special tokens (<|im_end|>, <|ref|>…). Mirrors the
// docreader-side _strip_ocr_markup.
var (
	ocrDetBlockPattern     = regexp.MustCompile(`(?s)<\|det\|>.*?<\|/det\|>`)
	ocrSpecialTokenPattern = regexp.MustCompile(`<\|[^|]*?\|>`)
	ocrMultiBlankPattern   = regexp.MustCompile(`\n{3,}`)
)

func stripOCRMarkup(text string) string {
	if strings.Contains(text, "<|") {
		text = ocrDetBlockPattern.ReplaceAllString(text, "")
		text = ocrSpecialTokenPattern.ReplaceAllString(text, "")
	}
	// Degenerate repeats (e.g. SenOCR-Vi echoing its instruction template in a
	// numbered loop until max_tokens) carry no detector markup, so this runs
	// unconditionally — it is a no-op on clean output.
	text = searchutil.CollapseDegenerateTail(text)
	return strings.TrimSpace(ocrMultiBlankPattern.ReplaceAllString(text, "\n\n"))
}

// PingPaddleOCRVL checks whether a self-hosted PaddleOCR-VL service is reachable.
func PingPaddleOCRVL(endpoint string) (bool, string) {
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint == "" {
		return false, "未配置 PaddleOCR-VL 端点"
	}
	if err := utils.ValidateURLForSSRF(endpoint); err != nil {
		return false, fmt.Sprintf("PaddleOCR-VL 端点未通过 SSRF 校验: %v", err)
	}
	client := utils.NewSSRFSafeHTTPClient(utils.SSRFSafeHTTPClientConfig{
		Timeout:      5 * time.Second,
		MaxRedirects: 5,
	})
	path := "/layout-parsing"
	if strings.HasSuffix(endpoint, "/v1") {
		// OpenAI-compatible OCR endpoint (e.g. vLLM): GET /v1/models.
		path = "/models"
	}
	resp, err := client.Get(endpoint + path)
	if err != nil {
		return false, fmt.Sprintf("PaddleOCR-VL 服务不可达: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 500 {
		return false, fmt.Sprintf("PaddleOCR-VL 服务返回状态 %d", resp.StatusCode)
	}
	return true, ""
}
