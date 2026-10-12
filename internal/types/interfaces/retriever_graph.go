package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// RetrieveGraphRepository is a repository for retrieving graphs
type RetrieveGraphRepository interface {
	// AddGraph adds a graph to the repository
	AddGraph(ctx context.Context, namespace types.NameSpace, graphs []*types.GraphData) error
	// DelGraph deletes a graph from the repository
	DelGraph(ctx context.Context, namespace []types.NameSpace) error
	// SearchNode searches for nodes in the repository
	SearchNode(ctx context.Context, namespace types.NameSpace, nodes []string) (*types.GraphData, error)
	// ViewGraph returns a display-sized slice of a KB's graph: the
	// best-connected nodes matching filter (plus, when filtered, their
	// neighbours) and the edges among them.
	ViewGraph(ctx context.Context, namespace types.NameSpace, filter types.GraphViewFilter) (*types.GraphView, error)
}
