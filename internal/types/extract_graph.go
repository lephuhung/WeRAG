package types

import (
	"sort"
	"strings"
)

// ChunkContext represents chunk content with surrounding context
type ChunkContext struct {
	ChunkID     string `json:"chunk_id"`
	Content     string `json:"content"`
	PrevContent string `json:"prev_content,omitempty"` // Previous chunk content for context
	NextContent string `json:"next_content,omitempty"` // Next chunk content for context
}

// PromptTemplateStructured represents the prompt template structured
type PromptTemplateStructured struct {
	Description string      `json:"description"`
	Tags        []string    `json:"tags"`
	Examples    []GraphData `json:"examples"`
}

type GraphNode struct {
	Name       string   `json:"name,omitempty"`
	Type       string   `json:"type,omitempty"`
	Chunks     []string `json:"chunks,omitempty"`
	Attributes []string `json:"attributes,omitempty"`
}

// GraphRelation represents the relation of the graph
type GraphRelation struct {
	Node1 string `json:"node1,omitempty"`
	Node2 string `json:"node2,omitempty"`
	Type  string `json:"type,omitempty"`
}

type GraphData struct {
	Text     string           `json:"text,omitempty"`
	Node     []*GraphNode     `json:"node,omitempty"`
	Relation []*GraphRelation `json:"relation,omitempty"`
}

// NameSpace represents the name space of the knowledge base and knowledge
type NameSpace struct {
	KnowledgeBase string `json:"knowledge_base"`
	Knowledge     string `json:"knowledge"`
}

// Labels returns the labels of the name space
func (n NameSpace) Labels() []string {
	res := make([]string, 0)
	if n.KnowledgeBase != "" {
		res = append(res, n.KnowledgeBase)
	}
	if n.Knowledge != "" {
		res = append(res, n.Knowledge)
	}
	return res
}

// GraphView is a graph prepared for display: nodes with a stable ID and a
// type to colour them by, and the edges among them.
type GraphView struct {
	Nodes []*GraphViewNode `json:"nodes"`
	Edges []*GraphViewEdge `json:"edges"`
	// Truncated reports that more nodes matched than were returned.
	Truncated bool `json:"truncated"`
}

// GraphViewNode is one node of a GraphView. Meta carries type-specific
// details (số hiệu, status, knowledge_id…).
type GraphViewNode struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Type   string            `json:"type"`
	Degree int               `json:"degree"`
	Meta   map[string]string `json:"meta,omitempty"`
}

// GraphViewEdge is a directed edge of a GraphView.
type GraphViewEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
}

// GraphViewFilter narrows an entity graph: name words, entity types, one
// document, and the node budget.
type GraphViewFilter struct {
	Query       string
	Types       []string
	KnowledgeID string
	Limit       int
}

// graphDocumentRelations are the legal-graph edges between documents — what
// a question about a document is usually after.
var graphDocumentRelations = map[string]bool{
	"CAN_CU": true, "VIEN_DAN": true, "SUA_DOI": true, "THAY_THE": true, "BAI_BO": true, "REFERENCES": true,
}

// graphNodeScope is the document a node belongs to: "Điều 5 (53/2022/NĐ-CP)"
// and "53/2022/NĐ-CP" both give "53/2022/nđ-cp" (clause nodes are scoped
// by vietnamese_legal.ScopeClauseName); any other node is its own scope.
func graphNodeScope(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if i := strings.LastIndex(name, " ("); i >= 0 && strings.HasSuffix(name, ")") {
		return name[i+2 : len(name)-1]
	}
	return name
}

// RankGraphRelations picks at most max relations, best first: between two
// documents, then touching a node named exactly like one of entities, then
// the rest, and last a document's references to its own clauses. PART_OF
// (Điều → văn bản) is dropped: a name match on a số hiệu hits every clause
// node of the document, whose structural edges would fill the budget.
// Duplicates and incomplete edges are dropped.
func RankGraphRelations(rels []*GraphRelation, entities []string, max int) []*GraphRelation {
	exact := map[string]bool{}
	for _, e := range entities {
		exact[strings.ToLower(strings.TrimSpace(e))] = true
	}
	rank := func(r *GraphRelation) int {
		switch {
		case graphNodeScope(r.Node1) == graphNodeScope(r.Node2):
			return 3
		case graphDocumentRelations[r.Type]:
			return 0
		case exact[strings.ToLower(r.Node1)] || exact[strings.ToLower(r.Node2)]:
			return 1
		default:
			return 2
		}
	}
	var picked []*GraphRelation
	seen := map[string]bool{}
	for _, r := range rels {
		if r == nil || r.Node1 == "" || r.Node2 == "" || r.Type == "PART_OF" {
			continue
		}
		key := r.Node1 + "\x00" + r.Type + "\x00" + r.Node2
		if seen[key] {
			continue
		}
		seen[key] = true
		picked = append(picked, r)
	}
	sort.SliceStable(picked, func(i, j int) bool { return rank(picked[i]) < rank(picked[j]) })
	if max > 0 && len(picked) > max {
		picked = picked[:max]
	}
	return picked
}
