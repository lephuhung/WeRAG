package chatpipeline

import (
	"context"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestFilterSeenChunkRanksByLinksAndCaps(t *testing.T) {
	var many []string
	for i := 0; i < maxEntityChunks+20; i++ {
		many = append(many, fmt.Sprintf("c%d", i))
	}
	graph := &types.GraphData{Node: []*types.GraphNode{
		{Name: "29/2018/QH14", Chunks: many},
		{Name: "Phạm vi bí mật nhà nước", Chunks: []string{"c45", "seen"}},
		{Name: "Bí mật nhà nước", Chunks: []string{"c45", "c40"}},
	}}
	got := filterSeenChunk(context.Background(), graph, []*types.SearchResult{{ID: "seen"}})
	if len(got) != maxEntityChunks {
		t.Fatalf("got %d chunks, want the cap %d", len(got), maxEntityChunks)
	}
	if got[0] != "c45" || got[1] != "c40" {
		t.Fatalf("chunks linked to most matched nodes come first, got %v", got[:3])
	}
	for _, id := range got {
		if id == "seen" {
			t.Fatal("a chunk search already found must not come back")
		}
	}
}

func TestGraphBaseScoreUsesOtherCandidatesScale(t *testing.T) {
	cands := []*types.SearchResult{
		{Score: 0.0163}, {Score: 0.0151}, {Score: 0.0130},
		{Score: 1, MatchType: types.MatchTypeGraph}, {Score: 1, MatchType: types.MatchTypeGraph},
	}
	if got := graphBaseScore(cands); got != 0.0151 {
		t.Fatalf("median of the hybrid scores expected, got %v", got)
	}
	if got := graphBaseScore(cands[3:]); got != 0 {
		t.Fatalf("graph only: want 0, got %v", got)
	}
	// With the median base a stronger reranker score wins again.
	article := compositeScore(&types.SearchResult{}, 0.976, 0.0151)
	graphChunk := compositeScore(&types.SearchResult{}, 0.960, graphBaseScore(cands))
	if article <= graphChunk {
		t.Fatalf("hybrid hit %.4f must outrank a weaker graph chunk %.4f", article, graphChunk)
	}
}
