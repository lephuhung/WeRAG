package types

// The catalogue map: one document and the documents around it to a given
// depth, across the whole legal catalogue (not one knowledge base). Each
// side is walked in one direction only: the "in" side follows the
// documents acting on what was reached (the decrees guiding a law, then
// the circulars guiding those decrees), the "out" side follows what the
// reached documents act on (what the focus replaced, what that one
// replaced before it).

// LegalGraphExploreNode is one document of the map.
type LegalGraphExploreNode struct {
	Doc *LegalGraphDoc `json:"doc"`
	// Depth: 0 the focus; +k reached in k steps on the "in" side (acts on
	// the focus or on what acts on it); -k on the "out" side.
	Depth int `json:"depth"`
	// Copies the caller may read, across the knowledge bases they can see.
	Copies []LegalKnowledgeRef `json:"copies,omitempty"`
	// Dropped counts, per relation, the documents of the next step that
	// the per-node cap left out ("+ 240 văn bản căn cứ nữa").
	Dropped map[string]int `json:"dropped,omitempty"`
}

// LegalGraphExploreEdge is one catalogue relation between two map nodes:
// From acts on To.
type LegalGraphExploreEdge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Relation string `json:"relation"`
}

// LegalGraphExplore is the map answer.
type LegalGraphExplore struct {
	Focus *LegalGraphDoc           `json:"focus"`
	Nodes []*LegalGraphExploreNode `json:"nodes"`
	// Edges are every catalogue relation between two nodes of the map, not
	// only the ones the walk followed.
	Edges []*LegalGraphExploreEdge `json:"edges"`
	Depth int                      `json:"depth"`
	// NodeCapHit: the walk stopped adding documents at the overall cap.
	NodeCapHit bool `json:"node_cap_hit"`
}
