package retriever

import (
	"context"
	"math"

	"github.com/Tencent/WeKnora/internal/types"
)

// ScoreNormalizer maps raw retriever scores to a common [0, 1] scale so that
// vector scores produced by different engines can be compared in a single
// ranked list. Implementations MUST be safe for concurrent use and MUST be
// IO-free (Normalize is called inside a hot loop and may not log or block).
//
// Only vector scores are normalized. Keyword (BM25) scores have an unbounded
// positive range; rescaling them would collapse the long tail. Downstream
// RRF fusion is rank-based and immune to scale, so keyword scores pass
// through unchanged.
type ScoreNormalizer interface {
	Normalize(
		ctx context.Context,
		score float64,
		retrieverType types.RetrieverType,
		engineType types.RetrieverEngineType,
	) float64
}

// EngineAwareNormalizer applies the per-engine cosine-score formula. The
// caller (HybridSearch) enforces a same-embedding-model precondition via
// ResolveEmbeddingModelKeys, so post-normalization values are comparable.
//
// Postgres pgvector computes `1 - cosine distance` (`(1 - distance) as
// score`, `<=>` cosine distance in [0, 2]).
//
// The theoretical range is therefore [-1, 1], but modern RAG embeddings are
// L2-normalized positive-component unit vectors (BGE, E5, OpenAI
// text-embedding-3, ...) that keep cosine in [0, 1] in practice, so the
// score is clamped to [0, 1]. A negative cosine (rare for IR embeddings)
// clamps to 0.
//
// Unknown engines clamp to [0, 1]; the fan-out caller emits a single WARN
// per request via warnIfUnknownEngine so Normalize itself stays lock-free
// and panic-free even on nil ctx.
type EngineAwareNormalizer struct{}

// Compile-time interface satisfaction assertion.
var _ ScoreNormalizer = EngineAwareNormalizer{}

// Normalize implements ScoreNormalizer.
func (EngineAwareNormalizer) Normalize(
	_ context.Context,
	score float64,
	retrieverType types.RetrieverType,
	engineType types.RetrieverEngineType,
) float64 {
	if retrieverType != types.VectorRetrieverType {
		// BM25 and other non-vector retrievers: passthrough. RRF rank-based
		// fusion handles scale-mixed input correctly.
		return score
	}

	switch engineType {
	case types.PostgresRetrieverEngineType:
		// See the struct godoc: theoretical [-1, 1], observed [0, 1].
		return clamp01(score)
	default:
		// Unknown engine. Clamp defensively; the caller emits WARN with ctx.
		return clamp01(score)
	}
}

// clamp01 maps any float64 into [0, 1] safely, including NaN/Inf inputs that
// could otherwise break slices.SortFunc's strict-weak-ordering invariant
// downstream (NaN compares neither greater nor less than anything).
func clamp01(s float64) float64 {
	if math.IsNaN(s) {
		return 0
	}
	if s <= 0 || math.IsInf(s, -1) {
		return 0
	}
	if s >= 1 || math.IsInf(s, 1) {
		return 1
	}
	return s
}
