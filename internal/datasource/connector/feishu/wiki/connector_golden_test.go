package wiki

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
)

// ──────────────────────────────────────────────────────────────────────
// Golden end-to-end test: a single rich docx node exercising EVERY new
// capability at once, driven through the real Connector.FetchAll → real
// core.Client → Feishu-shaped fake endpoints (blocks / sheets-v2 / bitable-v1 /
// medias). This is the glue test the per-function unit tests can't be:
// it proves the whole pipeline produces correct, ordered Markdown plus the
// correctly filtered attachment sub-items.
// ──────────────────────────────────────────────────────────────────────

// blk constructors kept local to this file to keep the fixture readable.
func sheetBlk(id, token string) core.DocxBlock {
	return core.DocxBlock{BlockID: id, BlockType: core.BlockTypeSheet, Sheet: &core.BlockTokenRef{Token: token}}
}

func bitableBlk(id, token string) core.DocxBlock {
	return core.DocxBlock{BlockID: id, BlockType: core.BlockTypeBitable, Bitable: &core.BlockTokenRef{Token: token}}
}

func imageBlk(id, token string) core.DocxBlock {
	return core.DocxBlock{BlockID: id, BlockType: core.BlockTypeImage, Image: &core.BlockTokenRef{Token: token}}
}

func fileBlk(id, token, name string) core.DocxBlock {
	return core.DocxBlock{BlockID: id, BlockType: core.BlockTypeFile, File: &core.BlockFileRef{Token: token, Name: name}}
}

// cellBlk builds a table_cell container. A real Feishu table_cell holds no
// inline text of its own — its text lives in a child block, so the cell only
// references the child id (see cellTextBlk).
func cellBlk(id string) core.DocxBlock {
	return core.DocxBlock{BlockID: id, BlockType: core.BlockTypeTableCell, Children: []string{id + "_txt"}}
}

// cellTextBlk builds the child text block a table cell points at.
func cellTextBlk(id, text string) core.DocxBlock {
	return core.DocxBlock{BlockID: id + "_txt", BlockType: core.BlockTypeText, Text: txt(text)}
}

func tableBlk(id string, cols int, cellIDs ...string) core.DocxBlock {
	b := core.DocxBlock{BlockID: id, BlockType: core.BlockTypeTable}
	b.Table = &core.BlockTable{Cells: cellIDs}
	b.Table.Property = &core.BlockTableProperty{ColumnSize: cols}
	return b
}

// fakeFeishuGolden serves the full API surface a rich docx node needs.
func fakeFeishuGolden(nodes []core.WikiNode, docToken string, blocks []core.DocxBlock,
	mediaByToken map[string][]byte,
) (*httptest.Server, *core.Config) {
	mux := http.NewServeMux()

	mux.HandleFunc("/open-apis/auth/v3/tenant_access_token/internal", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, core.TokenResponse{ApiResponse: core.ApiResponse{Code: 0}, TenantAccessToken: "fake-token", Expire: 7200})
	})
	mux.HandleFunc("/open-apis/wiki/v2/spaces", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, core.WikiSpaceListResponse{ApiResponse: core.ApiResponse{Code: 0}, Data: core.WikiSpaceListData{Items: []core.WikiSpace{{SpaceID: "space1", Name: "Test Space"}}}})
	})
	mux.HandleFunc("/open-apis/wiki/v2/spaces/space1/nodes", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, core.WikiNodeListResponse{ApiResponse: core.ApiResponse{Code: 0}, Data: core.WikiNodeListData{Items: nodes}})
	})

	// docx blocks — paginated across two pages to exercise the paging glue.
	mux.HandleFunc("/open-apis/docx/v1/documents/"+docToken+"/blocks", func(w http.ResponseWriter, r *http.Request) {
		half := len(blocks) / 2
		page := blocks[:half]
		hasMore := true
		if r.URL.Query().Get("page_token") == "p2" {
			page = blocks[half:]
			hasMore = false
		}
		nextTok := "p2"
		if !hasMore {
			nextTok = ""
		}
		writeJSON(w, core.DocxBlocksResponse{ApiResponse: core.ApiResponse{Code: 0}, Data: core.DocxBlocksData{Items: page, HasMore: hasMore, PageToken: nextTok}})
	})

	// sheets-v2 values: sht_spread_0 → spreadsheet "sht_spread", sheet "0".
	// Raw JSON keeps the fake honest to the real wire shape without wrestling
	// nested anonymous-struct literals.
	mux.HandleFunc("/open-apis/sheets/v2/spreadsheets/sht_spread/values/0", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":0,"msg":"","data":{"valueRange":{"values":[["Tên","Số lượng"],["Táo",3]]}}}`))
	})

	// bitable-v1 fields + records: bascApp_tblMain → app "bascApp", table "tblMain".
	mux.HandleFunc("/open-apis/bitable/v1/apps/bascApp/tables/tblMain/fields", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":0,"msg":"","data":{"items":[{"field_name":"Công việc"},{"field_name":"Trạng thái"}]}}`))
	})
	mux.HandleFunc("/open-apis/bitable/v1/apps/bascApp/tables/tblMain/records/search", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":0,"msg":"","data":{"has_more":false,"page_token":"","items":[{"fields":{"Công việc":"Viết mã","Trạng thái":"Hoàn thành"}}]}}`))
	})

	// medias download, routed by token embedded in the path.
	mux.HandleFunc("/open-apis/drive/v1/medias/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/download") {
			http.NotFound(w, r)
			return
		}
		// path: /open-apis/drive/v1/medias/<token>/download
		p := strings.TrimPrefix(r.URL.Path, "/open-apis/drive/v1/medias/")
		tok := strings.TrimSuffix(p, "/download")
		data, ok := mediaByToken[tok]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(data)
	})

	ts := httptest.NewServer(mux)
	return ts, &core.Config{AppID: "test-app-id", AppSecret: "test-app-secret", BaseURL: ts.URL}
}

func TestGolden_RichDocxAllCapabilities(t *testing.T) {
	t.Setenv("FEISHU_DOCX_PARSE_MODE", "blocks")
	const docToken = "obj-golden"
	bigPDF := bytes.Repeat([]byte("A"), core.MinAttachmentBytes+512) // kept
	tinyPDF := bytes.Repeat([]byte("B"), 100)                        // whitelisted but too small → dropped

	blocks := []core.DocxBlock{
		{BlockID: "root", BlockType: core.BlockTypePage},
		{BlockID: "h1", BlockType: core.BlockTypeHeading1, Heading1: txt("Báo cáo quý")},
		{BlockID: "p1", BlockType: core.BlockTypeText, Text: txt("Tổng quan quý này.")},
		{BlockID: "h2", BlockType: core.BlockTypeHeading1 + 1, Heading2: txt("Chỉ tiêu chính")},
		{BlockID: "b1", BlockType: core.BlockTypeBullet, Bullet: txt("Tăng trưởng thu ngân sách")},
		{BlockID: "o1", BlockType: core.BlockTypeOrdered, Ordered: txt("Bước một lập dự án")},
		{BlockID: "code1", BlockType: core.BlockTypeCode, Code: txt("SELECT 1")},
		{BlockID: "q1", BlockType: core.BlockTypeQuote, Quote: txt("Lưu ý quan trọng")},
		{BlockID: "todo1", BlockType: core.BlockTypeTodo, Todo: txt("Hoàn thành tổng kết")},
		{BlockID: "call1", BlockType: core.BlockTypeCallout, Callout: txt("Chú ý rủi ro")},
		{BlockID: "div1", BlockType: core.BlockTypeDivider},
		tableBlk("tbl1", 2, "c1", "c2", "c3", "c4"),
		sheetBlk("sh1", "sht_spread_0"),
		bitableBlk("bt1", "bascApp_tblMain"),
		imageBlk("im1", "img-tok-SECRET"),
		fileBlk("fbig", "tok-big", "Sổ_tay.pdf"),
		fileBlk("flogo", "tok-logo", "logo.png"),    // non-whitelisted ext → no sub-item
		fileBlk("fsmall", "tok-small", "small.pdf"), // whitelisted but too small → no sub-item
		// table cells (containers) + their child text blocks — both are core.Skipped by
		// the main loop and read by the table renderer via the cells' Children.
		cellBlk("c1"), cellBlk("c2"), cellBlk("c3"), cellBlk("c4"),
		cellTextBlk("c1", "Cột A"), cellTextBlk("c2", "Cột B"),
		cellTextBlk("c3", "1"), cellTextBlk("c4", "2"),
	}

	nodes := []core.WikiNode{{
		NodeToken: "nt-golden", ObjToken: docToken, ObjType: "docx",
		Title: "Tài liệu báo cáo quý", NodeEditTime: "1711468800",
	}}
	media := map[string][]byte{"tok-big": bigPDF, "tok-small": tinyPDF}

	ts, cfg := fakeFeishuGolden(nodes, docToken, blocks, media)
	defer ts.Close()

	c := NewConnector(core.RegionFeishu)
	items, err := c.FetchAll(context.Background(), makeConfig(cfg, []string{"space1"}), []string{"space1"})
	if err != nil {
		t.Fatalf("FetchAll error: %v", err)
	}

	// ── main markdown item + exactly one attachment sub-item ──
	if len(items) != 2 {
		t.Fatalf("want 2 items (main + 1 kept attachment), got %d:\n%+v", len(items), items)
	}
	var main, att *types.FetchedItem
	for i := range items {
		if items[i].ExternalID == "nt-golden" {
			main = &items[i]
		} else {
			att = &items[i]
		}
	}
	if main == nil {
		t.Fatal("main markdown item missing")
	}
	if main.ContentType != "text/markdown" {
		t.Errorf("main ContentType = %q, want text/markdown", main.ContentType)
	}
	if !main.ReplacesSubtree {
		t.Error("main item must set ReplacesSubtree for orphan sweeping")
	}
	if !strings.HasSuffix(main.FileName, ".md") {
		t.Errorf("main FileName = %q, want .md", main.FileName)
	}
	md := string(main.Content)

	// ── every text construct rendered correctly ──
	fragments := []string{
		"# Báo cáo quý",
		"Tổng quan quý này.",
		"## Chỉ tiêu chính",
		"- Tăng trưởng thu ngân sách",
		"1. Bước một lập dự án",
		"```\nSELECT 1\n```",
		"> Lưu ý quan trọng",
		"- [ ] Hoàn thành tổng kết",
		"> Chú ý rủi ro",
		"---",
		"| Cột A | Cột B |",          // native docx table header
		"| 1 | 2 |",                  // native docx table row
		"| Tên | Số lượng |",         // embedded sheet header
		"| Táo | 3 |",                // embedded sheet row
		"| Công việc | Trạng thái |", // embedded bitable header
		"| Viết mã | Hoàn thành |",   // embedded bitable row
		"![hình ảnh]()",              // token-free image placeholder
		"📎 Tệp đính kèm: Sổ_tay.pdf",
		"📎 Tệp đính kèm: logo.png",
		"📎 Tệp đính kèm: small.pdf",
	}
	for _, f := range fragments {
		if !strings.Contains(md, f) {
			t.Errorf("markdown missing fragment %q\n--- full markdown ---\n%s", f, md)
		}
	}

	// ── document order preserved (heading before table before attachments) ──
	order := []string{"# Báo cáo quý", "## Chỉ tiêu chính", "| Cột A | Cột B |", "| Tên | Số lượng |", "| Công việc | Trạng thái |", "📎 Tệp đính kèm: Sổ_tay.pdf"}
	last := -1
	for _, f := range order {
		idx := strings.Index(md, f)
		if idx <= last {
			t.Errorf("fragment %q out of order (idx=%d, prev=%d)\n%s", f, idx, last, md)
		}
		last = idx
	}

	// ── the internal image media token must NEVER leak into embeddings ──
	if strings.Contains(md, "img-tok-SECRET") {
		t.Errorf("internal image token leaked into markdown:\n%s", md)
	}

	// ── attachment filtering: only the big whitelisted PDF becomes a sub-item ──
	if att == nil {
		t.Fatal("kept attachment sub-item missing")
	}
	if att.ExternalID != "nt-golden#file#tok-big" {
		t.Errorf("attachment ExternalID = %q, want nt-golden#file#tok-big", att.ExternalID)
	}
	if att.Title != "Sổ_tay.pdf" {
		t.Errorf("attachment Title = %q", att.Title)
	}
	if !bytes.Equal(att.Content, bigPDF) {
		t.Errorf("attachment content mismatch: got %d bytes want %d", len(att.Content), len(bigPDF))
	}
	if att.Metadata["attachment"] != "true" || att.Metadata["parent_node_token"] != "nt-golden" {
		t.Errorf("attachment metadata wrong: %+v", att.Metadata)
	}
}
