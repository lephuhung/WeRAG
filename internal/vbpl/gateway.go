package vbpl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// The gateway reads one document from the public vbpl.vn gateway (the same
// endpoint scripts/legal_catalog/crawl_vbpl.py uses): metadata plus the
// full text as HTML with the prov-* provision markup internal/vbpl chunks.
// The portal's WAF answers 403 to non-browser user agents and 429 when
// called too fast, so callers fetch one document per tool call.
const (
	DetailURL = "https://vbpl-bientap-gateway.moj.gov.vn/api/qtdc/public/doc/%s"
	// AttachmentURL downloads a file of a document (the original scan
	// "VanBanGoc_….pdf", appendices) from the same gateway: document id,
	// then the path-escaped file name.
	AttachmentURL = "https://vbpl-bientap-gateway.moj.gov.vn/api/qtdc/public/doc/minio/buckets/vbpl/%s/%s/download"
	// MaxAttachmentBytes caps a downloaded attachment.
	MaxAttachmentBytes = 50 << 20
	DocURL             = "https://vbpl.vn/van-ban/chi-tiet/%s"
	userAgent          = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"
	Timeout            = 60 * time.Second
	// MinHTMLBytes: smaller pages are the portal's empty placeholders.
	MinHTMLBytes = 200
)

// Detail is the subset of the gateway's detail payload the feed needs.
type Detail struct {
	ID        string `json:"id"`
	DocNum    string `json:"docNum"`
	Title     string `json:"title"`
	DocType   Named  `json:"docType"`
	EffStatus Named  `json:"effStatus"`
	IssueDate string `json:"issueDate"`
	EffFrom   string `json:"effFrom"`
	Content   string `json:"-"`
	// ContentFileName is the original file of the document on the portal
	// ("VanBanGoc_74.TT.pdf", usually a scan), "" when there is none.
	ContentFileName string         `json:"documentContentFileName"`
	Related         []Related      `json:"documentRelatedList"`
	RawContent      map[string]any `json:"documentContent"`
}

type Named struct {
	Name string `json:"name"`
}

type Related struct {
	RelatedType string `json:"relatedType"`
	FileName    string `json:"fileName"`
	FileTitle   string `json:"fileTitle"`
}

// HasPDF reports whether the portal names an original PDF for d.
func (d *Detail) HasPDF() bool {
	return d != nil && strings.HasSuffix(strings.ToLower(strings.TrimSpace(d.ContentFileName)), ".pdf")
}

// HasHTML reports whether the portal publishes d's full text as HTML.
func (d *Detail) HasHTML() bool {
	return d != nil && len(d.Content) >= MinHTMLBytes
}

// FetchDetail downloads and decodes one document.
func FetchDetail(ctx context.Context, client *http.Client, id string) (*Detail, error) {
	id = strings.TrimSpace(id)
	if id == "" || !idPattern.MatchString(id) {
		return nil, fmt.Errorf("vbpl_id %q is not a vbpl.vn document id", id)
	}
	if client == nil {
		client = &http.Client{Timeout: Timeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(DetailURL, id), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", "https://vbpl.vn/")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vbpl.vn gateway: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// the portal answers an unknown id with 400 (or 404) and
		// {"success":"false","messageCode":"invalid.document.entity.not.found"}:
		// a stale catalogue id, not a gateway failure
		if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusNotFound {
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
			if gatewayNotFound(raw) {
				return nil, &NoDocumentError{ID: id}
			}
		}
		return nil, fmt.Errorf("vbpl.vn gateway answered HTTP %d for document %s", resp.StatusCode, id)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("vbpl.vn gateway: bad JSON: %w", err)
	}
	if !envelope.Success || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil, &NoDocumentError{ID: id}
	}
	var d Detail
	if err := json.Unmarshal(envelope.Data, &d); err != nil {
		return nil, fmt.Errorf("vbpl.vn gateway: bad document payload: %w", err)
	}
	if d.ID == "" {
		d.ID = id
	}
	if c, ok := d.RawContent["content"].(string); ok {
		d.Content = c
	}
	d.RawContent = nil
	return &d, nil
}

// FetchAttachment downloads one file of document id from the gateway: the
// original scan named by Detail.ContentFileName. The portal serves it as
// application/octet-stream, so the bytes must start with "%PDF". A file
// over MaxAttachmentBytes is refused; 400/404 answers are NoDocumentError
// like FetchDetail (FileName then names the missing file).
func FetchAttachment(ctx context.Context, client *http.Client, id, fileName string) ([]byte, error) {
	id, fileName = strings.TrimSpace(id), strings.TrimSpace(fileName)
	if id == "" || !idPattern.MatchString(id) {
		return nil, fmt.Errorf("vbpl_id %q is not a vbpl.vn document id", id)
	}
	if fileName == "" || strings.ContainsAny(fileName, "/\\") {
		return nil, fmt.Errorf("attachment name %q is not a vbpl.vn file name", fileName)
	}
	if client == nil {
		client = &http.Client{Timeout: Timeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf(AttachmentURL, id, url.PathEscape(fileName)), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", "https://vbpl.vn/")
	req.Header.Set("Accept", "application/pdf,application/octet-stream,*/*")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vbpl.vn gateway: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusNotFound {
			return nil, &NoDocumentError{ID: id, FileName: fileName}
		}
		return nil, fmt.Errorf("vbpl.vn gateway answered HTTP %d for file %s of document %s", resp.StatusCode, fileName, id)
	}
	if resp.ContentLength > MaxAttachmentBytes {
		return nil, fmt.Errorf("the attachment %s of document %s is %d bytes, over the %d MB limit",
			fileName, id, resp.ContentLength, MaxAttachmentBytes>>20)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxAttachmentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("vbpl.vn gateway: reading %s: %w", fileName, err)
	}
	if len(body) > MaxAttachmentBytes {
		return nil, fmt.Errorf("the attachment %s of document %s is over the %d MB limit", fileName, id, MaxAttachmentBytes>>20)
	}
	if !bytes.HasPrefix(body, []byte("%PDF")) {
		return nil, fmt.Errorf("the attachment is not a PDF (%s of document %s)", fileName, id)
	}
	return body, nil
}

// NoDocumentError reports that vbpl.vn holds no document with the id (the
// catalogue row is stale): the caller's input, not a gateway failure.
type NoDocumentError struct {
	ID string
	// FileName is set when an attachment of the document is missing.
	FileName string
}

func (e *NoDocumentError) Error() string {
	if e.FileName != "" {
		return fmt.Sprintf("vbpl.vn gateway has no file %s for document %s: vbpl.vn không có tệp %s của văn bản %s",
			e.FileName, e.ID, e.FileName, e.ID)
	}
	return fmt.Sprintf("vbpl.vn gateway has no document %s: vbpl.vn không có văn bản với id %s (danh mục có thể đã cũ)", e.ID, e.ID)
}

// gatewayNotFound reports whether an error body of the gateway says the
// document does not exist: messageCode invalid.document.entity.not.found,
// or success false (a string or a boolean, the portal sends both).
func gatewayNotFound(body []byte) bool {
	var env struct {
		Success     any    `json:"success"`
		MessageCode string `json:"messageCode"`
	}
	if json.Unmarshal(body, &env) != nil {
		return false
	}
	if strings.Contains(env.MessageCode, "not.found") {
		return true
	}
	switch v := env.Success.(type) {
	case bool:
		return !v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "false")
	}
	return false
}

// idPattern: old documents have integer ids, new ones UUIDs.
var idPattern = regexp.MustCompile(`^[0-9A-Za-z-]{1,64}$`)

// FileName mirrors file_name() of scripts/legal_catalog/ingest_vbpl.py so
// a document uploaded by either path gets the same title.
func FileName(d *Detail) string {
	num := strings.TrimSpace(d.DocNum)
	title := strings.Join(strings.Fields(d.Title), " ")
	name := title
	if num != "" && !strings.Contains(title, num) {
		name = num + " - " + title
	}
	if name == "" {
		name = d.ID
	}
	name = strings.ReplaceAll(name, "/", "-")
	name = badFileChars.ReplaceAllString(name, " ")
	name = strings.Join(strings.Fields(name), " ")
	if r := []rune(name); len(r) > 180 {
		name = string(r[:180])
	}
	return name + ".html"
}

// PDFFileName is FileName with the .pdf extension: the title of a
// document ingested from the portal's original scan.
func PDFFileName(d *Detail) string {
	return strings.TrimSuffix(FileName(d), ".html") + ".pdf"
}

var badFileChars = regexp.MustCompile(`[\\:*?"<>|]+`)

// Metadata mirrors metadata() of ingest_vbpl.py: the keys legal review,
// retrieval and the appendix links read back from knowledges.metadata.
func Metadata(d *Detail) map[string]string {
	meta := map[string]string{
		"external_id": d.ID,
		"source":      "vbpl",
		"doc_number":  strings.TrimSpace(d.DocNum),
		"doc_type":    strings.TrimSpace(d.DocType.Name),
		"issue_date":  dateOnly(d.IssueDate),
		"eff_from":    dateOnly(d.EffFrom),
		"eff_status":  strings.TrimSpace(d.EffStatus.Name),
		"vbpl_url":    fmt.Sprintf(DocURL, d.ID),
	}
	for k, v := range meta {
		if v == "" {
			delete(meta, k)
		}
	}
	type appendix struct {
		Title    string `json:"title"`
		FileName string `json:"file_name"`
	}
	var files []appendix
	for _, r := range d.Related {
		if r.RelatedType == "APPENDIX" && r.FileName != "" {
			files = append(files, appendix{Title: strings.Join(strings.Fields(r.FileTitle), " "), FileName: r.FileName})
		}
	}
	if len(files) > 0 {
		if raw, err := json.Marshal(files); err == nil {
			meta["vbpl_appendix_files"] = string(raw)
		}
	}
	return meta
}

func dateOnly(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}
