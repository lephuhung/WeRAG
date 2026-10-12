package types

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
