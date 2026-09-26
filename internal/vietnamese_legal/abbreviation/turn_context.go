package abbreviation

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// sealedAbbreviationTurn is the only payload ever stored under
// types.AbbreviationResolutionContextKey. The type is private so it cannot be
// fabricated from a JSON body or a generic map: any map or DTO under the same
// key fails the type assertion in ResolutionFromContext.
type sealedAbbreviationTurn struct {
	binding    types.AbbreviationBinding
	resolution types.AbbreviationResolution
}

// BindTurn seals a ready resolution to its turn binding and stores a private
// deep copy on the context under types.AbbreviationResolutionContextKey.
//
//   - Only internally-complete ready resolutions are accepted: status ready,
//     no unknowns, every term's source contents verified, exact
//     candidate/span coverage of the original query, and an effective query
//     that recomputes exactly. Ready-by-status-only is rejected.
//   - The binding's RawQuery is the CURRENT user reply and is never equated
//     with the resolution's OriginalQuery, which may be the ROOT question on
//     resume. A populated resolution CurrentUserMessageID must agree with the
//     binding's user message ID.
//   - The binding must carry a positive tenant, both actual message IDs, the
//     session, the principal and the current raw input. The owner scope keeps
//     its caller-specific semantics and is compared exactly; no human owner
//     identity is invented.
//   - Nothing is read from an HTTP body; the caller passes structs.
func BindTurn(ctx context.Context, b types.AbbreviationBinding, r types.AbbreviationResolution) (context.Context, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// Readiness is reported first so an unready resolution is never masked by
	// a sparse binding; a ready resolution with an incomplete binding
	// conflicts instead.
	if err := validateReadyResolution(r); err != nil {
		return nil, err
	}
	if b.Owner.TenantID == 0 || b.UserMessageID == "" || b.AssistantMessageID == "" ||
		b.Owner.SessionID == "" || b.Owner.PrincipalID == "" || b.RawQuery == "" {
		return nil, types.ErrAbbreviationConflict
	}
	if r.CurrentUserMessageID != "" && r.CurrentUserMessageID != b.UserMessageID {
		return nil, types.ErrAbbreviationConflict
	}
	sealed := sealedAbbreviationTurn{
		binding:    b,
		resolution: cloneResolution(r),
	}
	return context.WithValue(ctx, types.AbbreviationResolutionContextKey, sealed), nil
}

// ResolutionFromContext returns a deep copy of the sealed resolution. The
// copy isolates all nested slices and pointers, so caller mutation cannot
// corrupt the sealed payload. Foreign payloads (maps, DTOs) under the same
// key are rejected.
func ResolutionFromContext(ctx context.Context) (types.AbbreviationResolution, bool) {
	if ctx == nil {
		return types.AbbreviationResolution{}, false
	}
	sealed, ok := ctx.Value(types.AbbreviationResolutionContextKey).(sealedAbbreviationTurn)
	if !ok {
		return types.AbbreviationResolution{}, false
	}
	return cloneResolution(sealed.resolution), true
}

// RequireTurn verifies that the presented binding equals the sealed binding
// in full — tenant, principal, session, owner scope, message IDs and raw
// query, not just status — and that the sealed resolution is still
// internally complete with a matching current message.
func RequireTurn(ctx context.Context, b types.AbbreviationBinding) error {
	if ctx == nil {
		return types.ErrAbbreviationNotFound
	}
	sealed, ok := ctx.Value(types.AbbreviationResolutionContextKey).(sealedAbbreviationTurn)
	if !ok {
		return types.ErrAbbreviationNotFound
	}
	if sealed.binding != b {
		return types.ErrAbbreviationConflict
	}
	if sealed.resolution.CurrentUserMessageID != "" && sealed.resolution.CurrentUserMessageID != b.UserMessageID {
		return types.ErrAbbreviationConflict
	}
	return validateReadyResolution(sealed.resolution)
}

// validateReadyResolution enforces internal readiness beyond the status
// string. Anything incomplete reports ErrAbbreviationNotReady.
func validateReadyResolution(r types.AbbreviationResolution) error {
	if r.Status != types.AbbreviationStatusReady {
		return types.ErrAbbreviationNotReady
	}
	if overBudget(r) {
		return types.ErrAbbreviationNotReady
	}
	if len(r.UnknownTerms) > 0 {
		return types.ErrAbbreviationNotReady
	}
	for i := range r.Terms {
		if err := validateTermStructure(&r.Terms[i]); err != nil {
			return types.ErrAbbreviationNotReady
		}
	}
	if err := validateCoverage(r.OriginalQuery, r.Terms); err != nil {
		return types.ErrAbbreviationNotReady
	}
	rendered, err := RenderResolvedQuery(r)
	if err != nil || rendered != r.EffectiveQuery {
		return types.ErrAbbreviationNotReady
	}
	return nil
}
