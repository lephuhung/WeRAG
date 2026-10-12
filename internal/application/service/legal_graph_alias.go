package service

import (
	"context"
	"regexp"
	"strings"

	"github.com/Tencent/WeKnora/internal/legalcatalog"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
	"golang.org/x/text/unicode/norm"
)

// legalTitleTypes are the document types a cited title may start with,
// longest first so "Thông tư liên tịch" wins over "Thông tư".
var legalTitleTypes = []string{
	"thông tư liên tịch", "nghị quyết liên tịch", "hiến pháp", "bộ luật", "pháp lệnh",
	"nghị định", "nghị quyết", "quyết định", "thông tư", "chỉ thị", "luật",
}

var (
	legalTitleYearRe   = regexp.MustCompile(`\b(?:năm\s+)?((?:19|20)\d{2})\b`)
	legalTitleNumberRe = regexp.MustCompile(`\bsố\s*:?\s*\S+`)
	legalTitleSpaceRe  = regexp.MustCompile(`\s+`)
)

// splitLegalTitle lower-cases a document title and splits it into its type
// ("luật"), the rest without số hiệu or year, and the year it names.
func splitLegalTitle(title string) (docType, rest, year string) {
	s := strings.ToLower(norm.NFC.String(title))
	if m := legalTitleYearRe.FindStringSubmatch(s); m != nil {
		year = m[1]
	}
	s = legalTitleNumberRe.ReplaceAllString(s, " ")
	s = legalTitleYearRe.ReplaceAllString(s, " ")
	s = strings.TrimSpace(legalTitleSpaceRe.ReplaceAllString(s, " "))
	for _, t := range legalTitleTypes {
		if strings.HasPrefix(s, t+" ") {
			return t, strings.TrimSpace(s[len(t):]), year
		}
	}
	return "", s, year
}

// catalogNumberForTitle returns the số hiệu of the one catalogue document
// whose title is exactly this cited title ("Luật Bảo vệ bí mật nhà nước
// 2018" → "29/2018/QH14"), or "" when none or several match — a title
// without a year often names two versions of a law.
func catalogNumberForTitle(ctx context.Context, catalog interfaces.LegalCatalogRepository, title string) string {
	docType, rest, year := splitLegalTitle(title)
	words := strings.Fields(rest)
	if docType == "" || len(words) < 2 {
		return ""
	}
	candidates, err := catalog.SearchByTitle(ctx, words, 20)
	if err != nil {
		logger.Warnf(ctx, "legal graph alias: catalogue search failed: %v", err)
		return ""
	}
	found := ""
	for _, c := range candidates {
		if strings.ToLower(norm.NFC.String(c.DocType)) != docType {
			continue
		}
		cType, cRest, _ := splitLegalTitle(c.Title)
		if cType != "" && cType != docType || cRest != rest {
			continue
		}
		if year != "" && (c.IssuedOn == nil || c.IssuedOn.Format("2006") != year) {
			continue
		}
		num := vietnamese_legal.NormalizeDocumentNumber(legalcatalog.CleanVBPLNumber(c.DocumentNumber))
		if !strings.Contains(num, "/") {
			continue // "Không số": nothing to key a node by
		}
		if found != "" && found != num {
			return ""
		}
		found = num
	}
	return found
}

// resolveLegalDocumentAliases renames Document nodes known only by title to
// the số hiệu of their catalogue document — the key the cited document's
// own root node has — so one law is one node, not one per way of naming it.
// Merged nodes keep the union of their attributes; edges follow the rename.
func resolveLegalDocumentAliases(
	ctx context.Context, catalog interfaces.LegalCatalogRepository, g *types.GraphData,
) *types.GraphData {
	if catalog == nil || g == nil {
		return g
	}
	rename := map[string]string{}
	for _, n := range g.Node {
		if n == nil || n.Type != vietnamese_legal.EntityTypeDocument || strings.Contains(n.Name, "/") {
			continue
		}
		if num := catalogNumberForTitle(ctx, catalog, n.Name); num != "" {
			rename[n.Name] = num
		}
	}
	if len(rename) == 0 {
		return g
	}
	byKey := map[string]*types.GraphNode{}
	nodes := g.Node[:0]
	for _, n := range g.Node {
		if n == nil {
			continue
		}
		if n.Type == vietnamese_legal.EntityTypeDocument {
			if to, ok := rename[n.Name]; ok {
				n.Name = to
			}
		}
		key := n.Type + "|" + n.Name
		if kept, ok := byKey[key]; ok {
			kept.Attributes = mergeLegalAttributes(kept.Attributes, n.Attributes)
			continue
		}
		byKey[key] = n
		nodes = append(nodes, n)
	}
	g.Node = nodes
	rels := g.Relation[:0]
	for _, r := range g.Relation {
		if r == nil {
			continue
		}
		if to, ok := rename[r.Node1]; ok {
			r.Node1 = to
		}
		if to, ok := rename[r.Node2]; ok {
			r.Node2 = to
		}
		if r.Node1 != r.Node2 {
			rels = append(rels, r)
		}
	}
	g.Relation = rels
	return g
}

func mergeLegalAttributes(a, b []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(a)+len(b))
	for _, s := range append(append([]string{}, a...), b...) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
