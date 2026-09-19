package neo4j

import (
	"context"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
)

// Neo4jRepository is a repository for Neo4j
type Neo4jRepository struct {
	driver     neo4j.Driver
	nodePrefix string
}

// NewNeo4jRepository creates a new Neo4j repository
func NewNeo4jRepository(driver neo4j.Driver) interfaces.RetrieveGraphRepository {
	return &Neo4jRepository{driver: driver, nodePrefix: "ENTITY"}
}

// _remove_hyphen removes hyphens from a string
func _remove_hyphen(s string) string {
	return strings.ReplaceAll(s, "-", "_")
}

// Labels returns the labels for a namespace
func (n *Neo4jRepository) Labels(namespace types.NameSpace) []string {
	res := make([]string, 0)
	for _, label := range namespace.Labels() {
		res = append(res, n.nodePrefix+_remove_hyphen(label))
	}
	return res
}

// Label returns the label for a namespace
func (n *Neo4jRepository) Label(namespace types.NameSpace) string {
	labels := n.Labels(namespace)
	return strings.Join(labels, ":")
}

// kbLabel is the shared KB-scoped label used for canonical entity nodes.
// Entities merge across documents inside a knowledge base, so the merge
// key is {name, kb} — not {name, kg} (the legacy document-scoped key).
func (n *Neo4jRepository) kbLabel(namespace types.NameSpace) string {
	return n.nodePrefix + _remove_hyphen(namespace.KnowledgeBase)
}

// nodeLabels returns the label set for a canonical node: the KB label plus
// an ENTITY_<type> label when the entity carries a known legal type. The
// type label keeps same-name entities of different kinds apart (a Person
// "Điều 5" never merges into the Article) — mirroring AIRAG's typed merge.
func (n *Neo4jRepository) nodeLabels(namespace types.NameSpace, entityType string) []string {
	labels := []string{n.kbLabel(namespace)}
	if t := vietnamese_legal.CanonicalEntityType(entityType); t != "" {
		labels = append(labels, n.nodePrefix+"_"+t)
	}
	return labels
}

// entityTypeFromLabels recovers the legal entity type stored as an
// ENTITY_<type> label ("" for untyped / legacy nodes).
func (n *Neo4jRepository) entityTypeFromLabels(labels []string, kbLabel string) string {
	for _, l := range labels {
		if !strings.HasPrefix(l, n.nodePrefix+"_") || l == kbLabel {
			continue
		}
		if t := vietnamese_legal.CanonicalEntityType(strings.TrimPrefix(l, n.nodePrefix+"_")); t != "" {
			return t
		}
	}
	return ""
}

// AddGraph adds a graph to the Neo4j repository
func (n *Neo4jRepository) AddGraph(ctx context.Context, namespace types.NameSpace, graphs []*types.GraphData) error {
	if n.driver == nil {
		logger.Warnf(ctx, "NOT SUPPORT RETRIEVE GRAPH")
		return nil
	}
	for _, graph := range graphs {
		if err := n.addGraph(ctx, namespace, graph); err != nil {
			return err
		}
	}
	return nil
}

// addGraph adds a graph to the Neo4j repository
func (n *Neo4jRepository) addGraph(ctx context.Context, namespace types.NameSpace, graph *types.GraphData) error {
	session := n.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		// Node import query — merge on {name, kb} under the KB label (plus
		// the entity-type label when known). knowledge_ids / chunks /
		// attributes accumulate via set-union so repeated writes from other
		// documents or chunks only add ownership, never overwrite.
		node_import_query := `
			UNWIND $data AS row
			CALL apoc.merge.node(row.labels, {name: row.name, kb: row.kb}, {}, {}) YIELD node
			SET node.kb = row.kb,
			    node.chunks = apoc.coll.union(coalesce(node.chunks, []), row.chunks),
			    node.attributes = apoc.coll.union(coalesce(node.attributes, []), row.attributes),
			    node.knowledge_ids = apoc.coll.union(coalesce(node.knowledge_ids, []), [row.knowledge_id])
			RETURN distinct 'done' AS result
		`
		nodeData := []map[string]interface{}{}
		for _, node := range graph.Node {
			if node == nil || strings.TrimSpace(node.Name) == "" {
				continue
			}
			nodeData = append(nodeData, map[string]interface{}{
				"name":         node.Name,
				"kb":           namespace.KnowledgeBase,
				"knowledge_id": namespace.Knowledge,
				"attributes":   node.Attributes,
				"chunks":       node.Chunks,
				"labels":       n.nodeLabels(namespace, node.Type),
			})
		}
		if len(nodeData) > 0 {
			if _, err := tx.Run(ctx, node_import_query, map[string]interface{}{"data": nodeData}); err != nil {
				return nil, fmt.Errorf("failed to create nodes: %v", err)
			}
		}

		// Relationship import query — endpoints resolve by name under the KB
		// label only (their type label was already applied in the node pass).
		// The rel keeps its own knowledge_ids ownership list so deleting one
		// document only removes rels no other document still owns.
		rel_import_query := `
			UNWIND $data AS row
			CALL apoc.merge.node(row.endpoint_labels, {name: row.source, kb: row.kb}, {}, {}) YIELD node as source
			CALL apoc.merge.node(row.endpoint_labels, {name: row.target, kb: row.kb}, {}, {}) YIELD node as target
			CALL apoc.merge.relationship(source, row.type, {}, {}, target) YIELD rel
			SET rel.knowledge_ids = apoc.coll.union(coalesce(rel.knowledge_ids, []), [row.knowledge_id]),
			    source.knowledge_ids = apoc.coll.union(coalesce(source.knowledge_ids, []), [row.knowledge_id]),
			    target.knowledge_ids = apoc.coll.union(coalesce(target.knowledge_ids, []), [row.knowledge_id])
			RETURN distinct 'done'
		`
		relData := []map[string]interface{}{}
		for _, rel := range graph.Relation {
			if rel == nil || rel.Node1 == "" || rel.Node2 == "" || rel.Type == "" {
				continue
			}
			relData = append(relData, map[string]interface{}{
				"source":          rel.Node1,
				"target":          rel.Node2,
				"kb":              namespace.KnowledgeBase,
				"knowledge_id":    namespace.Knowledge,
				"type":            rel.Type,
				"endpoint_labels": []string{n.kbLabel(namespace)},
			})
		}
		if len(relData) > 0 {
			if _, err := tx.Run(ctx, rel_import_query, map[string]interface{}{"data": relData}); err != nil {
				return nil, fmt.Errorf("failed to create relationships: %v", err)
			}
		}
		return nil, nil
	})
	if err != nil {
		logger.Errorf(ctx, "failed to add graph: %v", err)
		return err
	}
	return nil
}

// DelGraph deletes a graph from the Neo4j repository
func (n *Neo4jRepository) DelGraph(ctx context.Context, namespaces []types.NameSpace) error {
	if n.driver == nil {
		logger.Warnf(ctx, "NOT SUPPORT RETRIEVE GRAPH")
		return nil
	}
	session := n.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	result, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		for _, namespace := range namespaces {
			kbLabel := n.kbLabel(namespace)
			legacyLabelExpr := n.Label(namespace)
			params := map[string]interface{}{"knowledge_id": namespace.Knowledge}

			// 1. KB-scoped shared rels: strip this document's ownership and
			//    delete the rel only when no owner remains. Serial (not
			//    parallel) — concurrent SET on the same rel can deadlock.
			unownRelsQuery := `
				CALL apoc.periodic.iterate(
					"MATCH (a:` + kbLabel + `)-[r]-(b:` + kbLabel + `)
					 WHERE $knowledge_id IN coalesce(r.knowledge_ids, []) RETURN r",
					"SET r.knowledge_ids = [x IN r.knowledge_ids WHERE x <> $knowledge_id]
					 WITH r WHERE size(r.knowledge_ids) = 0 DELETE r",
					{batchSize: 1000, parallel: false, params: {knowledge_id: $knowledge_id}}
				) YIELD batches, total
				RETURN total
        	`
			if _, err := tx.Run(ctx, unownRelsQuery, params); err != nil {
				return nil, fmt.Errorf("failed to unown relationships: %v", err)
			}

			// 2. KB-scoped shared nodes: same ownership removal, DETACH
			//    DELETE only when the node has no remaining owners. Nodes
			//    that never carried knowledge_ids (size = null) are
			//    untouched — null never equals 0 in Cypher.
			unownNodesQuery := `
				CALL apoc.periodic.iterate(
					"MATCH (n:` + kbLabel + `)
					 WHERE $knowledge_id IN coalesce(n.knowledge_ids, []) RETURN n",
					"SET n.knowledge_ids = [x IN n.knowledge_ids WHERE x <> $knowledge_id]
					 WITH n WHERE size(n.knowledge_ids) = 0 DETACH DELETE n",
					{batchSize: 1000, parallel: false, params: {knowledge_id: $knowledge_id}}
				) YIELD batches, total
				RETURN total
        	`
			if _, err := tx.Run(ctx, unownNodesQuery, params); err != nil {
				return nil, fmt.Errorf("failed to unown nodes: %v", err)
			}

			// 3. Legacy document-scoped rows (kg property + the two-label
			//    namespace). Kept verbatim so graphs written before the
			//    KB-scoped schema still delete cleanly.
			deleteRelsQuery := `
				CALL apoc.periodic.iterate(
					"MATCH (n:` + legacyLabelExpr + ` {kg: $knowledge_id})-[r]-(m:` + legacyLabelExpr + ` {kg: $knowledge_id}) RETURN r",
					"DELETE r",
					{batchSize: 1000, parallel: true, params: {knowledge_id: $knowledge_id}}
				) YIELD batches, total
				RETURN total
        	`
			if _, err := tx.Run(ctx, deleteRelsQuery, params); err != nil {
				return nil, fmt.Errorf("failed to delete relationships: %v", err)
			}

			deleteNodesQuery := `
				CALL apoc.periodic.iterate(
					"MATCH (n:` + legacyLabelExpr + ` {kg: $knowledge_id}) RETURN n",
					"DELETE n",
					{batchSize: 1000, parallel: true, params: {knowledge_id: $knowledge_id}}
				) YIELD batches, total
				RETURN total
        	`
			if _, err := tx.Run(ctx, deleteNodesQuery, params); err != nil {
				return nil, fmt.Errorf("failed to delete nodes: %v", err)
			}
		}
		return nil, nil
	})
	if err != nil {
		return err
	}
	logger.Infof(ctx, "delete graph result: %v", result)
	return nil
}

// SearchNode searches for nodes in the Neo4j repository
func (n *Neo4jRepository) SearchNode(
	ctx context.Context,
	namespace types.NameSpace,
	nodes []string,
) (*types.GraphData, error) {
	if n.driver == nil {
		logger.Warnf(ctx, "NOT SUPPORT RETRIEVE GRAPH")
		return nil, nil
	}
	session := n.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		// KB-scoped canonical nodes live under the ENTITY_<kb> label with
		// per-document ownership in knowledge_ids. A document-scoped
		// namespace filters on ownership (new rows) or kg (legacy rows);
		// a KB-wide namespace searches the whole merged graph.
		kbLabel := n.kbLabel(namespace)
		query := `
			MATCH (n:` + kbLabel + `)-[r]-(m:` + kbLabel + `)
			WHERE ANY(nodeText IN $nodes WHERE n.name CONTAINS nodeText)
			RETURN n, r, m
		`
		params := map[string]interface{}{"nodes": nodes}
		if namespace.Knowledge != "" {
			query = `
				MATCH (n:` + kbLabel + `)-[r]-(m:` + kbLabel + `)
				WHERE ANY(nodeText IN $nodes WHERE n.name CONTAINS nodeText)
				  AND ($knowledge_id IN coalesce(n.knowledge_ids, []) OR n.kg = $knowledge_id)
				  AND ($knowledge_id IN coalesce(m.knowledge_ids, []) OR m.kg = $knowledge_id)
				RETURN n, r, m
			`
			params["knowledge_id"] = namespace.Knowledge
		}
		result, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, fmt.Errorf("failed to run query: %v", err)
		}

		graphData := &types.GraphData{}
		nodeSeen := make(map[string]bool)
		for result.Next(ctx) {
			record := result.Record()
			node, _ := record.Get("n")
			rel, _ := record.Get("r")
			targetNode, _ := record.Get("m")

			nodeData := node.(neo4j.Node)
			targetNodeData := targetNode.(neo4j.Node)

			// Convert node to types.Node
			for _, nd := range []neo4j.Node{nodeData, targetNodeData} {
				nameStr, _ := nd.Props["name"].(string)
				if nameStr == "" {
					continue
				}
				if _, ok := nodeSeen[nameStr]; !ok {
					nodeSeen[nameStr] = true
					chunks, _ := nd.Props["chunks"].([]interface{})
					attributes, _ := nd.Props["attributes"].([]interface{})
					graphData.Node = append(graphData.Node, &types.GraphNode{
						Name:       nameStr,
						Type:       n.entityTypeFromLabels(nd.Labels, kbLabel),
						Chunks:     listI2listS(chunks),
						Attributes: listI2listS(attributes),
					})
				}
			}

			// Convert relationship to types.Relation
			relData := rel.(neo4j.Relationship)
			graphData.Relation = append(graphData.Relation, &types.GraphRelation{
				Node1: nodeData.Props["name"].(string),
				Node2: targetNodeData.Props["name"].(string),
				Type:  relData.Type,
			})
		}
		return graphData, nil
	})
	if err != nil {
		logger.Errorf(ctx, "search node failed: %v", err)
		return nil, err
	}
	return result.(*types.GraphData), nil
}

func listI2listS(list []any) []string {
	result := make([]string, len(list))
	for i, v := range list {
		result[i] = fmt.Sprintf("%v", v)
	}
	return result
}
