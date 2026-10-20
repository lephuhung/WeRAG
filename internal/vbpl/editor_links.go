package vbpl

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// vbpl.vn editors tag provision-level relations on the provisions of the
// AMENDED document A: a <p class="prov-article|prov-clause|prov-item"
// id="<A provision>"> carries type="<code>:<source provision id in B>"
// (the first amender) and new-types="<code>:<id>[,<code>:<id>…]" (later
// ones), e.g. type="10:02231655-e8ac-4272-b236-e0c96b62c782". Provision
// ids are global across the portal, so the source id names the document B
// too. "10:PHU-LUC" marks a phụ lục part changed without a source id. The
// converter keeps the tags as metadata.legal.editor_links on A's chunks;
// cmd/legal-provision-links stores them in legal_provision_links, the
// source the amendment resolver reads (plan 2026-10-19 §11).

// Editor relation codes (vbpl.vn).
const (
	EditorCodeBaiBo      = 1  // bãi bỏ
	EditorCodeDinhChi    = 5  // đình chỉ
	EditorCodeDinhChinh  = 6  // đính chính
	EditorCodeSuaDoiBS   = 10 // sửa đổi, bổ sung
	EditorCodeTamNgung   = 11 // tạm ngưng
	EditorCodeThayThe    = 12 // thay thế
	EditorCodeBoSung     = 13 // bổ sung
	EditorCodeGiaiThich  = 14 // giải thích
	EditorSourcePhuLuc   = "PHU-LUC"
	editorAttrType       = "type"
	editorAttrNewTypes   = "new-types"
	editorMaxTagsPerProv = 32
)

// EditorCodeOp names a relation code ("sua_doi_bo_sung"); "" for a code
// the portal does not document.
func EditorCodeOp(code int) string {
	switch code {
	case EditorCodeBaiBo:
		return "bai_bo"
	case EditorCodeDinhChi:
		return "dinh_chi"
	case EditorCodeDinhChinh:
		return "dinh_chinh"
	case EditorCodeSuaDoiBS:
		return "sua_doi_bo_sung"
	case EditorCodeTamNgung:
		return "tam_ngung"
	case EditorCodeThayThe:
		return "thay_the"
	case EditorCodeBoSung:
		return "bo_sung"
	case EditorCodeGiaiThich:
		return "giai_thich"
	}
	return ""
}

// EditorTag is one relation tag on a provision of A.
type EditorTag struct {
	Code     int    `json:"code"`
	SourceID string `json:"source_provision_id"` // B's provision id, or "PHU-LUC"
	Attr     string `json:"attr"`                // "type" or "new-types"
}

// EditorTagStats counts what EditorTags read and skipped.
type EditorTagStats struct {
	Tags      int // tags kept
	Malformed int // "<code>:<id>" not readable, or an unknown code
	PhuLuc    int // "…:PHU-LUC" (kept, no source provision)
	NoID      int // tagged paragraph without an id
}

// editorTagRe is one "<code>:<id>" part; ids are uuids, older "p12_r3"-like
// ids or PHU-LUC.
var editorTagRe = regexp.MustCompile(`^\s*(\d{1,3})\s*:\s*([0-9A-Za-z][0-9A-Za-z_-]*)\s*$`)

// EditorPage is what a vbpl page says about its provisions.
type EditorPage struct {
	// Tags are the relation tags by provision id, in page order (type
	// first, then new-types), on any prov-* level.
	Tags map[string][]EditorTag
	// Levels gives the prov-* level ("article", "clause", "item",
	// "chapter" …) of every provision id on the page, tagged or not: the
	// ids a source provision id may name.
	Levels map[string]string
	Stats  EditorTagStats
}

// ReadEditorPage reads the provision ids and relation tags of a vbpl page.
// A provision is a node whose class list holds a prov-* level ("prov-clause",
// also "MsoNormal prov-clause"); prov-content and prov-table hold none.
func ReadEditorPage(raw []byte) (*EditorPage, error) {
	pg := &EditorPage{Tags: map[string][]EditorTag{}, Levels: map[string]string{}}
	root, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	var visit func(n *html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Head, atom.Style, atom.Script, atom.Noscript, atom.Template:
				return
			}
			if level := editorProvLevel(n); level != "" {
				if id := strings.TrimSpace(attr(n, "id")); id != "" {
					if _, ok := pg.Levels[id]; !ok {
						pg.Levels[id] = level
					}
				}
				readEditorTags(n, pg.Tags, &pg.Stats)
			}
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			visit(ch)
		}
	}
	visit(root)
	return pg, nil
}

// EditorTags reads the relation tags of a vbpl page on its Điều / khoản /
// điểm (prov-article, prov-clause, prov-item), by provision id.
func EditorTags(raw []byte) (map[string][]EditorTag, error) {
	if !bytes.Contains(raw, []byte(`type="`)) && !bytes.Contains(raw, []byte(`types="`)) {
		return nil, nil
	}
	pg, err := ReadEditorPage(raw)
	if err != nil {
		return nil, err
	}
	for id := range pg.Tags {
		switch pg.Levels[id] {
		case "article", "clause", "item":
		default:
			delete(pg.Tags, id)
		}
	}
	return pg.Tags, nil
}

// editorProvLevel is the prov-* level of a tagged node ("" when none):
// "prov-content" and "prov-table" carry no provision.
func editorProvLevel(n *html.Node) string {
	for _, c := range strings.Fields(attr(n, "class")) {
		if !strings.HasPrefix(c, "prov-") {
			continue
		}
		switch lv := strings.TrimPrefix(c, "prov-"); lv {
		case "content", "table", "img":
		default:
			return lv
		}
	}
	return ""
}

func readEditorTags(n *html.Node, out map[string][]EditorTag, st *EditorTagStats) {
	var found []EditorTag
	for _, name := range []string{editorAttrType, editorAttrNewTypes} {
		v := attr(n, name)
		if v == "" || !strings.Contains(v, ":") {
			continue // type="1" on lists, type="a" …: not a relation tag
		}
		for _, part := range strings.Split(v, ",") {
			if strings.TrimSpace(part) == "" {
				continue
			}
			m := editorTagRe.FindStringSubmatch(part)
			if m == nil {
				st.Malformed++
				continue
			}
			code, _ := strconv.Atoi(m[1])
			if EditorCodeOp(code) == "" {
				st.Malformed++
				continue
			}
			found = append(found, EditorTag{Code: code, SourceID: m[2], Attr: name})
		}
	}
	if len(found) == 0 {
		return
	}
	id := strings.TrimSpace(attr(n, "id"))
	if id == "" {
		st.NoID += len(found)
		return
	}
	for _, t := range found {
		dup := false
		for _, have := range out[id] {
			if have.Code == t.Code && have.SourceID == t.SourceID {
				dup = true
				break
			}
		}
		if dup || len(out[id]) >= editorMaxTagsPerProv {
			continue
		}
		if t.SourceID == EditorSourcePhuLuc {
			st.PhuLuc++
		}
		out[id] = append(out[id], t)
		st.Tags++
	}
}

// ProvisionPlace is where a provision id sits in its document.
type ProvisionPlace struct {
	Level string // "article", "clause", "item"
	// Path is "dieu:5", "dieu:5/khoan:2", "dieu:5/khoan:2/diem:a" or, for
	// an điểm directly under a Điều, "dieu:5/diem:a"; "" when a label is
	// missing. Provisions of an appendix (phụ lục, quy chế kèm theo) are
	// prefixed "phuluc/" — their numbers are not the main text's.
	Path string
	// Block is the index in Document.Blocks.
	Block int
}

// ProvisionPlaces maps the provision ids of the document's own Điều /
// khoản / điểm (quoted text excluded) to their paths.
func ProvisionPlaces(d *Document) map[string]ProvisionPlace {
	out := map[string]ProvisionPlace{}
	if d == nil {
		return out
	}
	for i, b := range d.Blocks {
		if b.ID == "" {
			continue
		}
		var level string
		switch b.Kind {
		case KindArticle:
			level = "article"
		case KindClause:
			level = "clause"
		case KindItem:
			level = "item"
		default:
			continue
		}
		if _, dup := out[b.ID]; dup {
			continue
		}
		out[b.ID] = ProvisionPlace{Level: level, Path: blockPath(d.Blocks, i), Block: i}
	}
	return out
}

// blockPath walks the parents of block i: "dieu:N[/khoan:K][/diem:D]".
func blockPath(blocks []Block, i int) string {
	var dieu, khoan, diem string
	appendix := false
	for j, guard := i, 0; j >= 0 && j < len(blocks) && guard < 64; j, guard = blocks[j].Parent, guard+1 {
		b := blocks[j]
		switch b.Kind {
		case KindArticle:
			if dieu == "" {
				dieu = "dieu:" + strings.ToLower(b.Number)
				if b.Number == "" {
					return ""
				}
			}
		case KindClause:
			if khoan == "" && dieu == "" {
				if b.Number == "" {
					return ""
				}
				khoan = "khoan:" + strings.ToLower(b.Number)
			}
		case KindItem:
			if diem == "" && khoan == "" && dieu == "" {
				if b.Number == "" {
					return ""
				}
				diem = "diem:" + strings.ToLower(b.Number)
			}
		case KindAppendix:
			appendix = true
		}
	}
	if dieu == "" {
		return ""
	}
	path := dieu
	if khoan != "" {
		path += "/" + khoan
	}
	if diem != "" {
		path += "/" + diem
	}
	if appendix {
		path = "phuluc/" + path
	}
	return path
}

// EditorLinks are the relation tags of the provisions a chunk holds (the
// Điều / khoản / điểm blocks starting inside it), for
// metadata.legal.editor_links. places is ProvisionPlaces(d).
func EditorLinks(d *Document, c Chunk, tags map[string][]EditorTag, places map[string]ProvisionPlace) []vietnamese_legal.EditorLink {
	if d == nil || len(tags) == 0 {
		return nil
	}
	var out []vietnamese_legal.EditorLink
	for i, b := range d.Blocks {
		if b.ID == "" || b.Start < c.Start || b.Start >= c.End {
			continue
		}
		list := tags[b.ID]
		if len(list) == 0 {
			continue
		}
		p, ok := places[b.ID]
		if !ok || p.Block != i {
			continue
		}
		for _, t := range list {
			out = append(out, vietnamese_legal.EditorLink{ProvisionID: b.ID, Path: p.Path, Code: t.Code, SourceProvisionID: t.SourceID})
		}
	}
	return out
}
