package chatpipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

func testDoc() *vietnamese_legal.LegalDocContext {
	return &vietnamese_legal.LegalDocContext{
		IsLegal:        true,
		DocumentNumber: "53/2022/NĐ-CP",
		Title:          "Nghị định 53/2022/NĐ-CP về an ninh mạng",
		IssuingAgency:  "CHÍNH PHỦ",
		PublishedDate:  "15/08/2022",
		RootName:       "53/2022/NĐ-CP",
		CanCu:          []string{"Luật An ninh mạng", "Hiến pháp năm 2013"},
	}
}

func nodeByName(g *types.GraphData, name string) *types.GraphNode {
	for _, n := range g.Node {
		if n.Name == name {
			return n
		}
	}
	return nil
}

func hasRel(g *types.GraphData, n1, rel, n2 string) bool {
	for _, r := range g.Relation {
		if r.Node1 == n1 && r.Type == rel && r.Node2 == n2 {
			return true
		}
	}
	return false
}

func TestPostProcessLegalGraph_CanonicalMerge(t *testing.T) {
	g := &types.GraphData{
		Node: []*types.GraphNode{
			{Name: "sở thông tin và truyền thông", Type: "Organization"},
			{Name: "SỞ THÔNG TIN VÀ TRUYỀN THÔNG", Type: "Organization"},
			{Name: "Nghị định 53/2022/nđ-cp", Type: "Document"},
			{Name: "53/2022/NĐ-CP", Type: "Document"},
		},
	}
	out := PostProcessLegalGraph(context.Background(), g, testDoc(), false)

	// Both spellings of the org fold to one canonical node.
	org := nodeByName(out, "Sở Thông Tin và Truyền Thông")
	if org == nil || org.Type != "Organization" {
		t.Fatalf("canonical org node missing: %+v", out.Node)
	}
	if c := 0; c == 0 {
		count := 0
		for _, n := range out.Node {
			if n.Type == "Organization" && n.Name == "Sở Thông Tin và Truyền Thông" {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("expected 1 canonical org node, got %d", count)
		}
	}
	// Both doc spellings fold to the root số hiệu.
	if nodeByName(out, "53/2022/NĐ-CP") == nil {
		t.Fatal("document root node missing")
	}
	for _, n := range out.Node {
		if n.Name == "53/2022/nđ-cp" || n.Name == "Nghị định 53/2022/nđ-cp" {
			t.Fatalf("non-canonical doc node leaked: %q", n.Name)
		}
	}
}

func TestPostProcessLegalGraph_SelfRefFolding(t *testing.T) {
	g := &types.GraphData{
		Node: []*types.GraphNode{
			{Name: "Nghị định này", Type: "Document"},
			{Name: "Điều 5", Type: "Article"},
		},
		Relation: []*types.GraphRelation{
			{Node1: "Điều 5", Type: "REFERENCES", Node2: "văn bản này"},
		},
	}
	out := PostProcessLegalGraph(context.Background(), g, testDoc(), false)

	for _, n := range out.Node {
		if n.Name == "Nghị định này" || n.Name == "văn bản này" {
			t.Fatalf("self-ref materialized as node: %q", n.Name)
		}
	}
	// Clause nodes are scoped to the document root so "Điều 5" of another
	// document in the same knowledge base never merges with this one.
	if !hasRel(out, "Điều 5 (53/2022/NĐ-CP)", "REFERENCES", "53/2022/NĐ-CP") {
		t.Fatalf("relation endpoint not folded to root: %+v", out.Relation)
	}
	// Article → PART_OF → root is synthesized.
	if !hasRel(out, "Điều 5 (53/2022/NĐ-CP)", "PART_OF", "53/2022/NĐ-CP") {
		t.Fatal("PART_OF edge not synthesized")
	}
	if nodeByName(out, "Điều 5") != nil {
		t.Fatal("unscoped clause node leaked")
	}
}

func TestPostProcessLegalGraph_ClauseScoping(t *testing.T) {
	g := &types.GraphData{
		Node: []*types.GraphNode{
			{Name: "Điều 5", Type: "Article"},
			{Name: "khoản 2 Điều 5 của Nghị định này", Type: "Article"},
			{Name: "Khoản 3 Điều 49 Luật An ninh mạng", Type: "Article"},
		},
		Relation: []*types.GraphRelation{
			{Node1: "Điều 5", Type: "VIEN_DAN", Node2: "Khoản 3 Điều 49 Luật An ninh mạng"},
			{Node1: "53/2022/NĐ-CP", Type: "BAI_BO", Node2: "Điều 7 Nghị định 13/2023/NĐ-CP"},
		},
	}
	out := PostProcessLegalGraph(context.Background(), g, testDoc(), false)

	own := "Điều 5 (53/2022/NĐ-CP)"
	if nodeByName(out, own) == nil || nodeByName(out, "Khoản 2 Điều 5 (53/2022/NĐ-CP)") == nil {
		t.Fatalf("own clauses not scoped to the root: %+v", out.Node)
	}
	if !hasRel(out, own, "PART_OF", "53/2022/NĐ-CP") ||
		!hasRel(out, "Khoản 2 Điều 5 (53/2022/NĐ-CP)", "PART_OF", "53/2022/NĐ-CP") {
		t.Fatalf("own clauses must be PART_OF the root: %+v", out.Relation)
	}

	// A clause of another document keeps its full name, is reachable from
	// the relation, and is NOT attached to this document's structure.
	ext := "Khoản 3 Điều 49 Luật An Ninh Mạng"
	if nodeByName(out, ext) == nil || !hasRel(out, own, "VIEN_DAN", ext) {
		t.Fatalf("external clause reference lost: %+v / %+v", out.Node, out.Relation)
	}
	if hasRel(out, ext, "PART_OF", "53/2022/NĐ-CP") {
		t.Fatal("external clause must not be PART_OF this document")
	}
	// Relation-only endpoints go through the same scoping.
	ext2 := "Điều 7 Nghị Định 13/2023/nđ-cp"
	if !hasRel(out, "53/2022/NĐ-CP", "BAI_BO", ext2) || hasRel(out, ext2, "PART_OF", "53/2022/NĐ-CP") {
		t.Fatalf("relation-only external clause mishandled: %+v", out.Relation)
	}
}

func TestPostProcessLegalGraph_JunkAndGuards(t *testing.T) {
	g := &types.GraphData{
		Node: []*types.GraphNode{
			{Name: "Cơ quan nhà nước", Type: "Organization"},
			{Name: "Mẫu số 02", Type: "Document"},
			{Name: "(tên đơn vị đề nghị)", Type: "Organization"},
			{Name: "Bộ Công an", Type: "Organization"},
		},
		Relation: []*types.GraphRelation{
			{Node1: "Điều 1", Type: "CHU_TRI", Node2: "các cơ quan có liên quan"},
			{Node1: "53/2022/NĐ-CP", Type: "CAN_CU", Node2: "53/2022/NĐ-CP"}, // self-loop
			{Node1: "Điều 1", Type: "GIBBERISH", Node2: "Bộ Công an"},        // unknown type
		},
	}
	out := PostProcessLegalGraph(context.Background(), g, testDoc(), false)

	for _, n := range out.Node {
		switch n.Name {
		case "Cơ Quan Nhà Nước", "Mẫu số 02", "(tên đơn vị đề nghị)", "Các Cơ Quan Có Liên Quan":
			t.Fatalf("junk entity materialized: %q", n.Name)
		}
	}
	for _, r := range out.Relation {
		if r.Type == "GIBBERISH" || r.Type == "CAN_CU" && r.Node1 == r.Node2 {
			t.Fatalf("invalid relation leaked: %+v", r)
		}
		if r.Node2 == "Các Cơ Quan Có Liên Quan" {
			t.Fatalf("junk endpoint relation leaked: %+v", r)
		}
	}
	if nodeByName(out, "Bộ Công An") == nil {
		t.Fatal("legit org dropped")
	}
}

func TestPostProcessLegalGraph_StructuralEdges(t *testing.T) {
	g := &types.GraphData{Node: []*types.GraphNode{{Name: "Điều 1", Type: "Article"}}}

	// includeCanCu=false → no CAN_CU edges even though doc has them.
	out := PostProcessLegalGraph(context.Background(), g, testDoc(), false)
	for _, r := range out.Relation {
		if r.Type == "CAN_CU" {
			t.Fatal("CAN_CU must only be injected by the header chunk task")
		}
	}
	if !hasRel(out, "53/2022/NĐ-CP", "BAN_HANH_BOI", "Chính Phủ") {
		t.Fatalf("BAN_HANH_BOI missing: %+v", out.Relation)
	}

	// includeCanCu=true → CAN_CU edges to preamble refs.
	out = PostProcessLegalGraph(context.Background(), g, testDoc(), true)
	if !hasRel(out, "53/2022/NĐ-CP", "CAN_CU", "Luật An Ninh Mạng") {
		t.Fatalf("CAN_CU edge missing: %+v", out.Relation)
	}
}

func TestPostProcessLegalGraph_PersonCompositeKey(t *testing.T) {
	g := &types.GraphData{
		Node: []*types.GraphNode{
			{Name: "Nguyễn Văn A", Type: "Person"},
			{Name: "Nguyễn Văn B (01/01/1980)", Type: "Person"},
		},
	}
	out := PostProcessLegalGraph(context.Background(), g, testDoc(), false)
	if nodeByName(out, "Nguyễn Văn A (không xác định)") == nil {
		t.Fatalf("person without key not disambiguated: %+v", out.Node)
	}
	if nodeByName(out, "Nguyễn Văn B (01/01/1980)") == nil {
		t.Fatal("person with composite key lost")
	}
}

// The user prompt's JSON schema lists the relations the model may emit; a
// relation the system prompt defines but the schema omits is never produced
// (THAY_THE and BAI_BO were missing, as in AIRAG).
func TestLegalKGUserPromptListsEveryDocumentRelation(t *testing.T) {
	for _, rel := range []string{"CAN_CU", "VIEN_DAN", "SUA_DOI", "THAY_THE", "BAI_BO", "PART_OF", "REFERENCES"} {
		if !strings.Contains(LegalKGSystemPrompt, "- "+rel+":") {
			t.Fatalf("system prompt no longer defines %s", rel)
		}
		if !strings.Contains(LegalKGUserPrompt, rel+"|") && !strings.Contains(LegalKGUserPrompt, "|"+rel) {
			t.Errorf("user prompt schema omits %s", rel)
		}
	}
}

func TestGraphRelationsContext(t *testing.T) {
	if graphRelationsContext(nil, nil) != "" {
		t.Fatal("no graph, no context")
	}
	got := graphRelationsContext(&types.GraphData{Relation: []*types.GraphRelation{
		{Node1: "Điều 2 (13/2023/NĐ-CP)", Type: "PART_OF", Node2: "13/2023/NĐ-CP"},
		{Node1: "Điều 2 (13/2023/NĐ-CP)", Type: "CHU_TRI", Node2: "Bộ Công an"},
		{Node1: "Bộ Công an", Type: "CHU_TRI", Node2: "13/2023/NĐ-CP"},
		{Node1: "13/2023/NĐ-CP", Type: "VIEN_DAN", Node2: "Điều 4 (13/2023/NĐ-CP)"},
		{Node1: "13/2023/NĐ-CP", Type: "THAY_THE", Node2: "52/2013/NĐ-CP"},
		{Node1: "13/2023/NĐ-CP", Type: "THAY_THE", Node2: "52/2013/NĐ-CP"},
		{Node1: "", Type: "CAN_CU", Node2: "x"},
		nil,
	}}, []string{"13/2023/NĐ-CP"})
	want := "13/2023/NĐ-CP —THAY_THE→ 52/2013/NĐ-CP\n" +
		"Bộ Công an —CHU_TRI→ 13/2023/NĐ-CP\n" +
		"Điều 2 (13/2023/NĐ-CP) —CHU_TRI→ Bộ Công an\n" +
		"13/2023/NĐ-CP —VIEN_DAN→ Điều 4 (13/2023/NĐ-CP)\n"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

// Relations must join the entity types the prompt defines: a backwards
// edge is turned round, any other mismatch and a document's references to
// its own clauses are dropped (seen on re-parsed 29/2018/QH14).
func TestPostProcessLegalGraphEnforcesRelationEndpoints(t *testing.T) {
	doc := testDoc()
	raw := &types.GraphData{
		Node: []*types.GraphNode{
			{Name: "Bộ Công an", Type: "Organization"},
			{Name: "Chính phủ", Type: "Organization"},
			{Name: "Bảo đảm an ninh mạng", Type: "Task"},
			{Name: "Điều 5", Type: "Article"},
			{Name: "Luật An ninh mạng", Type: "Document"},
		},
		Relation: []*types.GraphRelation{
			{Node1: "53/2022/NĐ-CP", Type: "CAN_CU", Node2: "Chính phủ"},          // Document → Organization
			{Node1: "Bộ Công an", Type: "CHU_TRI", Node2: "Bảo đảm an ninh mạng"}, // backwards
			{Node1: "53/2022/NĐ-CP", Type: "CHU_TRI", Node2: "Bộ Công an"},        // Document as the task
			{Node1: "53/2022/NĐ-CP", Type: "VIEN_DAN", Node2: "Điều 5"},           // its own clause
			{Node1: "Điều 5", Type: "VIEN_DAN", Node2: "Luật An ninh mạng"},
		},
	}
	out := PostProcessLegalGraph(context.Background(), raw, doc, false)
	task := nodeByName(out, "Bảo Đảm An Ninh Mạng")
	if task == nil {
		task = nodeByName(out, "Bảo đảm an ninh mạng")
	}
	if task == nil {
		t.Fatalf("task node missing: %+v", out.Node)
	}
	org := ""
	for _, n := range out.Node {
		if n.Type == "Organization" && strings.Contains(strings.ToLower(n.Name), "công an") {
			org = n.Name
		}
	}
	art := "Điều 5 (53/2022/NĐ-CP)"
	law := ""
	for _, n := range out.Node {
		if n.Type == "Document" && strings.Contains(strings.ToLower(n.Name), "an ninh mạng") {
			law = n.Name
		}
	}
	if !hasRel(out, task.Name, "CHU_TRI", org) {
		t.Errorf("backwards CHU_TRI should be turned round: %+v", out.Relation)
	}
	for _, r := range out.Relation {
		switch {
		case r.Type == "CAN_CU" && strings.Contains(r.Node2, "Chính"):
			t.Errorf("CAN_CU to an organization kept: %+v", r)
		case r.Type == "CHU_TRI" && r.Node1 == "53/2022/NĐ-CP":
			t.Errorf("CHU_TRI from a document kept: %+v", r)
		case r.Type == "VIEN_DAN" && r.Node1 == "53/2022/NĐ-CP":
			t.Errorf("reference to the document's own clause kept: %+v", r)
		}
	}
	if !hasRel(out, art, "VIEN_DAN", law) {
		t.Errorf("clause citing another law must stay: %+v", out.Relation)
	}
}

func TestPostProcessLegalGraph_QuotedAmendedClause(t *testing.T) {
	doc := testDoc()
	doc.OwnArticles = []string{"1"}
	doc.Amends = []vietnamese_legal.AmendRef{{Document: "Nghị định 13/2023/NĐ-CP", Article: "9"}}
	g := &types.GraphData{
		Node: []*types.GraphNode{
			{Name: "Điều 1", Type: "Article"},
			{Name: "khoản 2 Điều 9", Type: "Article"},
		},
		Relation: []*types.GraphRelation{
			{Node1: "Điều 1", Type: "SUA_DOI", Node2: "khoản 2 Điều 9"},
		},
	}
	out := PostProcessLegalGraph(context.Background(), g, doc, false)
	amended := vietnamese_legal.NormalizeOrgName("Khoản 2 Điều 9 Nghị định 13/2023/NĐ-CP")
	if nodeByName(out, "Điều 1 (53/2022/NĐ-CP)") == nil || nodeByName(out, amended) == nil {
		t.Fatalf("nodes = %+v", out.Node)
	}
	if nodeByName(out, "Khoản 2 Điều 9 (53/2022/NĐ-CP)") != nil || hasRel(out, amended, "PART_OF", "53/2022/NĐ-CP") {
		t.Fatalf("quoted clause of the amended document scoped to this one: %+v / %+v", out.Node, out.Relation)
	}
	if !hasRel(out, "Điều 1 (53/2022/NĐ-CP)", "SUA_DOI", amended) {
		t.Fatalf("relations = %+v", out.Relation)
	}
}
