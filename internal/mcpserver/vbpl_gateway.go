package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// vbplGateway reads one document from the public vbpl.vn gateway (the same
// endpoint scripts/legal_catalog/crawl_vbpl.py uses): metadata plus the
// full text as HTML with the prov-* provision markup internal/vbpl chunks.
// The portal's WAF answers 403 to non-browser user agents and 429 when
// called too fast, so callers fetch one document per tool call.
const (
	vbplDetailURL = "https://vbpl-bientap-gateway.moj.gov.vn/api/qtdc/public/doc/%s"
	vbplDocURL    = "https://vbpl.vn/van-ban/chi-tiet/%s"
	vbplUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"
	vbplTimeout   = 60 * time.Second
	// vbplMinHTMLBytes: smaller pages are the portal's empty placeholders.
	vbplMinHTMLBytes = 200
)

// vbplDetail is the subset of the gateway's detail payload the feed needs.
type vbplDetail struct {
	ID         string         `json:"id"`
	DocNum     string         `json:"docNum"`
	Title      string         `json:"title"`
	DocType    vbplNamed      `json:"docType"`
	EffStatus  vbplNamed      `json:"effStatus"`
	IssueDate  string         `json:"issueDate"`
	EffFrom    string         `json:"effFrom"`
	Content    string         `json:"-"`
	Related    []vbplRelated  `json:"documentRelatedList"`
	RawContent map[string]any `json:"documentContent"`
}

type vbplNamed struct {
	Name string `json:"name"`
}

type vbplRelated struct {
	RelatedType string `json:"relatedType"`
	FileName    string `json:"fileName"`
	FileTitle   string `json:"fileTitle"`
}

// vbplFetchDetail downloads and decodes one document.
func vbplFetchDetail(ctx context.Context, client *http.Client, id string) (*vbplDetail, error) {
	id = strings.TrimSpace(id)
	if id == "" || !vbplIDPattern.MatchString(id) {
		return nil, fmt.Errorf("vbpl_id %q is not a vbpl.vn document id", id)
	}
	if client == nil {
		client = &http.Client{Timeout: vbplTimeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(vbplDetailURL, id), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", vbplUserAgent)
	req.Header.Set("Referer", "https://vbpl.vn/")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vbpl.vn gateway: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
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
		return nil, fmt.Errorf("vbpl.vn gateway has no document %s", id)
	}
	var d vbplDetail
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

// vbplIDPattern: old documents have integer ids, new ones UUIDs.
var vbplIDPattern = regexp.MustCompile(`^[0-9A-Za-z-]{1,64}$`)

// vbplFileName mirrors file_name() of scripts/legal_catalog/ingest_vbpl.py so
// a document uploaded by either path gets the same title.
func vbplFileName(d *vbplDetail) string {
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
	name = vbplBadFileChars.ReplaceAllString(name, " ")
	name = strings.Join(strings.Fields(name), " ")
	if r := []rune(name); len(r) > 180 {
		name = string(r[:180])
	}
	return name + ".html"
}

var vbplBadFileChars = regexp.MustCompile(`[\\:*?"<>|]+`)

// vbplMetadata mirrors metadata() of ingest_vbpl.py: the keys legal review,
// retrieval and the appendix links read back from knowledges.metadata.
func vbplMetadata(d *vbplDetail) map[string]string {
	meta := map[string]string{
		"external_id": d.ID,
		"source":      "vbpl",
		"doc_number":  strings.TrimSpace(d.DocNum),
		"doc_type":    strings.TrimSpace(d.DocType.Name),
		"issue_date":  vbplDate(d.IssueDate),
		"eff_from":    vbplDate(d.EffFrom),
		"eff_status":  strings.TrimSpace(d.EffStatus.Name),
		"vbpl_url":    fmt.Sprintf(vbplDocURL, d.ID),
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

func vbplDate(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}
