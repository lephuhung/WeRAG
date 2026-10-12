package neo4j

import (
	"context"
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
)

const (
	graphViewDefaultLimit = 150
	graphViewMaxLimit     = 500
	// graphViewMaxEdges bounds the edges returned among the chosen nodes.
	graphViewMaxEdges = 3000
)

// ViewGraph picks the best-connected nodes of the KB graph that match the
// filter. A filtered view (words or one document) adds the neighbours of
// those nodes so their relations show; then the edges among all chosen
// nodes are read.
func (n *Neo4jRepository) ViewGraph(
	ctx context.Context, namespace types.NameSpace, filter types.GraphViewFilter,
) (*types.GraphView, error) {
	view := &types.GraphView{Nodes: []*types.GraphViewNode{}, Edges: []*types.GraphViewEdge{}}
	if n.driver == nil {
		return view, nil
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = graphViewDefaultLimit
	}
	limit = min(limit, graphViewMaxLimit)
	kbLabel := n.kbLabel(namespace)
	var typeLabels []string
	for _, t := range filter.Types {
		if c := vietnamese_legal.CanonicalEntityType(t); c != "" {
			typeLabels = append(typeLabels, n.nodePrefix+"_"+c)
		}
	}
	filtered := filter.Query != "" || filter.KnowledgeID != ""

	session := n.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)
	_, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		params := map[string]interface{}{
			"q": filter.Query, "kid": filter.KnowledgeID, "types": typeLabels, "limit": limit + 1,
		}
		seedQuery := `
			MATCH (s:` + kbLabel + `)
			WHERE ($q = '' OR toLower(s.name) CONTAINS toLower($q))
			  AND ($kid = '' OR $kid IN coalesce(s.knowledge_ids, []) OR s.kg = $kid)
			  AND (size($types) = 0 OR any(l IN labels(s) WHERE l IN $types))
			WITH s, COUNT { (s)--() } AS degree
			RETURN elementId(s) AS id, s.name AS name, labels(s) AS labels, degree
			ORDER BY degree DESC LIMIT $limit`
		seen := map[string]bool{}
		var ids []string
		add := func(rec *neo4j.Record) {
			id, _ := rec.Get("id")
			idStr, _ := id.(string)
			if idStr == "" || seen[idStr] {
				return
			}
			if len(view.Nodes) == limit {
				view.Truncated = true
				return
			}
			seen[idStr] = true
			ids = append(ids, idStr)
			name, _ := rec.Get("name")
			labels, _ := rec.Get("labels")
			degree, _ := rec.Get("degree")
			nameStr, _ := name.(string)
			deg, _ := degree.(int64)
			view.Nodes = append(view.Nodes, &types.GraphViewNode{
				ID: idStr, Name: nameStr, Degree: int(deg),
				Type: n.entityTypeFromLabels(listI2listS(asList(labels)), kbLabel),
			})
		}
		res, err := tx.Run(ctx, seedQuery, params)
		if err != nil {
			return nil, fmt.Errorf("graph view seeds: %w", err)
		}
		for res.Next(ctx) {
			add(res.Record())
		}
		if err := res.Err(); err != nil {
			return nil, err
		}

		if filtered && len(ids) > 0 && len(view.Nodes) < limit {
			res, err = tx.Run(ctx, `
				MATCH (s:`+kbLabel+`)--(m:`+kbLabel+`)
				WHERE elementId(s) IN $ids
				WITH DISTINCT m, COUNT { (m)--() } AS degree
				RETURN elementId(m) AS id, m.name AS name, labels(m) AS labels, degree
				ORDER BY degree DESC LIMIT $limit`,
				map[string]interface{}{"ids": ids, "limit": limit - len(view.Nodes) + 1})
			if err != nil {
				return nil, fmt.Errorf("graph view neighbours: %w", err)
			}
			for res.Next(ctx) {
				add(res.Record())
			}
			if err := res.Err(); err != nil {
				return nil, err
			}
		}

		if len(ids) == 0 {
			return nil, nil
		}
		res, err = tx.Run(ctx, `
			MATCH (a:`+kbLabel+`)-[r]->(b:`+kbLabel+`)
			WHERE elementId(a) IN $ids AND elementId(b) IN $ids
			RETURN elementId(a) AS source, elementId(b) AS target, type(r) AS type
			LIMIT $limit`,
			map[string]interface{}{"ids": ids, "limit": graphViewMaxEdges})
		if err != nil {
			return nil, fmt.Errorf("graph view edges: %w", err)
		}
		for res.Next(ctx) {
			rec := res.Record()
			src, _ := rec.Get("source")
			dst, _ := rec.Get("target")
			typ, _ := rec.Get("type")
			s, _ := src.(string)
			d, _ := dst.(string)
			t, _ := typ.(string)
			view.Edges = append(view.Edges, &types.GraphViewEdge{Source: s, Target: d, Type: t})
		}
		return nil, res.Err()
	})
	if err != nil {
		return nil, err
	}
	return view, nil
}

func asList(v interface{}) []interface{} {
	l, _ := v.([]interface{})
	return l
}
