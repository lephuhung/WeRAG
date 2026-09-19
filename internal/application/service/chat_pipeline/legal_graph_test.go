package chatpipeline

import (
	"context"
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
	if !hasRel(out, "Điều 5", "REFERENCES", "53/2022/NĐ-CP") {
		t.Fatalf("relation endpoint not folded to root: %+v", out.Relation)
	}
	// Article → PART_OF → root is synthesized.
	if !hasRel(out, "Điều 5", "PART_OF", "53/2022/NĐ-CP") {
		t.Fatal("PART_OF edge not synthesized")
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
