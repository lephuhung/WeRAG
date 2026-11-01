package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type stubChunkForEmbed struct {
	interfaces.ChunkService
	chunks map[string]*types.Chunk
}

func (s *stubChunkForEmbed) GetChunkByIDOnly(_ context.Context, id string) (*types.Chunk, error) {
	chunk, ok := s.chunks[id]
	if !ok {
		return nil, ErrChunkNotFound
	}
	return chunk, nil
}

func newEmbedChunkService(agent *types.CustomAgent, chunks map[string]*types.Chunk) *embedChannelService {
	return &embedChannelService{
		agentService: &stubAgentForEmbed{agent: agent},
		chunkService: &stubChunkForEmbed{chunks: chunks},
	}
}

func TestChunkAllowedForEmbedAllowedKB(t *testing.T) {
	svc := newEmbedChunkService(
		&types.CustomAgent{
			Config: types.CustomAgentConfig{
				KBSelectionMode: "selected",
				KnowledgeBases:  []string{"kb-allowed"},
			},
		},
		map[string]*types.Chunk{
			"chunk-1": {ID: "chunk-1", TenantID: 5, KnowledgeBaseID: "kb-allowed"},
		},
	)
	ch := &types.EmbedChannel{ID: "ch-1", TenantID: 5, AgentID: "agent-1"}

	if !svc.chunkAllowedForEmbed(context.Background(), ch, &types.Chunk{TenantID: 5, KnowledgeBaseID: "kb-allowed"}) {
		t.Fatal("chunk in allowed KB should be permitted")
	}

	chunk, err := svc.EmbedChunk(context.Background(), ch, "chunk-1")
	if err != nil {
		t.Fatalf("EmbedChunk() = %v", err)
	}
	if chunk == nil || chunk.ID != "chunk-1" {
		t.Fatalf("unexpected chunk: %#v", chunk)
	}
}

func TestChunkAllowedForEmbedCrossTenantDenied(t *testing.T) {
	svc := newEmbedChunkService(
		&types.CustomAgent{
			Config: types.CustomAgentConfig{
				KBSelectionMode: "selected",
				KnowledgeBases:  []string{"kb-allowed"},
			},
		},
		map[string]*types.Chunk{
			"chunk-x": {ID: "chunk-x", TenantID: 999, KnowledgeBaseID: "kb-allowed"},
		},
	)
	ch := &types.EmbedChannel{ID: "ch-1", TenantID: 5, AgentID: "agent-1"}
	chunk := &types.Chunk{ID: "chunk-x", TenantID: 999, KnowledgeBaseID: "kb-allowed"}

	if svc.chunkAllowedForEmbed(context.Background(), ch, chunk) {
		t.Fatal("cross-tenant chunk must be denied before KB checks")
	}

	_, err := svc.EmbedChunk(context.Background(), ch, "chunk-x")
	if !errors.Is(err, ErrEmbedChunkForbidden) {
		t.Fatalf("EmbedChunk() = %v, want ErrEmbedChunkForbidden", err)
	}
}

func TestChunkAllowedForEmbedWrongKBDenied(t *testing.T) {
	svc := newEmbedChunkService(
		&types.CustomAgent{
			Config: types.CustomAgentConfig{
				KBSelectionMode: "selected",
				KnowledgeBases:  []string{"kb-allowed"},
			},
		},
		map[string]*types.Chunk{
			"chunk-other": {ID: "chunk-other", TenantID: 5, KnowledgeBaseID: "kb-other"},
		},
	)
	ch := &types.EmbedChannel{ID: "ch-1", TenantID: 5, AgentID: "agent-1"}
	chunk := &types.Chunk{ID: "chunk-other", TenantID: 5, KnowledgeBaseID: "kb-other"}

	if svc.chunkAllowedForEmbed(context.Background(), ch, chunk) {
		t.Fatal("chunk outside allowed KB list must be denied")
	}

	_, err := svc.EmbedChunk(context.Background(), ch, "chunk-other")
	if !errors.Is(err, ErrEmbedChunkForbidden) {
		t.Fatalf("EmbedChunk() = %v, want ErrEmbedChunkForbidden", err)
	}
}

func TestChunkAllowedForEmbedAllModePermitsInTenant(t *testing.T) {
	svc := newEmbedChunkService(
		&types.CustomAgent{
			Config: types.CustomAgentConfig{KBSelectionMode: "all"},
		},
		map[string]*types.Chunk{
			"chunk-all": {ID: "chunk-all", TenantID: 5, KnowledgeBaseID: "kb-any"},
		},
	)
	ch := &types.EmbedChannel{ID: "ch-1", TenantID: 5, AgentID: "agent-1"}

	if !svc.chunkAllowedForEmbed(context.Background(), ch, &types.Chunk{TenantID: 5, KnowledgeBaseID: "kb-any"}) {
		t.Fatal("all-mode agent should permit in-tenant chunks")
	}
}

type stubKBRepoForEmbed struct {
	interfaces.KnowledgeBaseRepository
	kbs map[string]*types.KnowledgeBase
}

func (s *stubKBRepoForEmbed) GetKnowledgeBaseByID(_ context.Context, id string) (*types.KnowledgeBase, error) {
	kb, ok := s.kbs[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return kb, nil
}

// An agent on the "public" scope reads platform public bases only. They live
// in the platform tenant (10000), not the channel's: such a chunk is
// permitted although its tenant differs, while a chunk of the channel
// tenant's own base, a chunk whose tenant is not its base's, and an unknown
// base are refused.
func TestChunkAllowedForEmbedPublicScope(t *testing.T) {
	svc := newEmbedChunkService(
		&types.CustomAgent{Config: types.CustomAgentConfig{KBSelectionMode: types.KBSelectionModePublic}}, nil)
	svc.kbRepo = &stubKBRepoForEmbed{kbs: map[string]*types.KnowledgeBase{
		"kb-public": {ID: "kb-public", TenantID: 10000, OwnerTenantID: 0, Visibility: types.KBVisibilityPublic},
		"kb-own":    {ID: "kb-own", TenantID: 5, OwnerTenantID: 5, Visibility: types.KBVisibilityTenant},
		"kb-shown":  {ID: "kb-shown", TenantID: 5, OwnerTenantID: 5, Visibility: types.KBVisibilityPublic},
	}}
	ch := &types.EmbedChannel{ID: "ch-1", TenantID: 5, AgentID: "agent-1"}
	ctx := context.Background()

	if !svc.chunkAllowedForEmbed(ctx, ch, &types.Chunk{TenantID: 10000, KnowledgeBaseID: "kb-public"}) {
		t.Fatal("chunk of a platform public base (platform tenant) should be permitted")
	}
	for name, chunk := range map[string]*types.Chunk{
		"own base":              {TenantID: 5, KnowledgeBaseID: "kb-own"},
		"tenant-owned public":   {TenantID: 5, KnowledgeBaseID: "kb-shown"},
		"tenant not its base's": {TenantID: 5, KnowledgeBaseID: "kb-public"},
		"unknown base":          {TenantID: 10000, KnowledgeBaseID: "kb-missing"},
	} {
		if svc.chunkAllowedForEmbed(ctx, ch, chunk) {
			t.Fatalf("%s: must be denied in the public scope", name)
		}
	}
	svc.kbRepo = nil
	if svc.chunkAllowedForEmbed(ctx, ch, &types.Chunk{TenantID: 10000, KnowledgeBaseID: "kb-public"}) {
		t.Fatal("no base lookup must fail closed")
	}

	// every other scope keeps the channel-tenant check
	all := newEmbedChunkService(&types.CustomAgent{Config: types.CustomAgentConfig{KBSelectionMode: "all"}}, nil)
	if all.chunkAllowedForEmbed(ctx, ch, &types.Chunk{TenantID: 10000, KnowledgeBaseID: "kb-public"}) {
		t.Fatal("an \"all\" agent must not read another tenant's chunk")
	}
	if !all.chunkAllowedForEmbed(ctx, ch, &types.Chunk{TenantID: 5, KnowledgeBaseID: "kb-own"}) {
		t.Fatal("an \"all\" agent reads the channel tenant's chunks")
	}
}
