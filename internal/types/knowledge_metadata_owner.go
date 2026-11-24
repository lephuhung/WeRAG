package types

import "context"

// knowledgeMetadataReadOnlyKey marks a context whose knowledge saves must
// not write metadata / custom_metadata.
type knowledgeMetadataReadOnlyKey struct{}

// WithKnowledgeMetadataReadOnly marks ctx as a background task that loads a
// knowledge row, works for minutes and saves the whole row back (parsing,
// enrichment). Such a task owns none of the metadata keys others write
// meanwhile (adoption's external_id, the catalogue import's eff_status, the
// retention clock eff_expired_at, a user's custom metadata): its saves
// through KnowledgeRepository.UpdateKnowledge leave both metadata columns
// alone instead of writing back the copy it loaded. A
// key the task does own is written on its own (UpdateKnowledgeColumn after
// re-reading the row).
func WithKnowledgeMetadataReadOnly(ctx context.Context) context.Context {
	return context.WithValue(ctx, knowledgeMetadataReadOnlyKey{}, true)
}

// KnowledgeMetadataReadOnly reports whether ctx carries that mark.
func KnowledgeMetadataReadOnly(ctx context.Context) bool {
	v, _ := ctx.Value(knowledgeMetadataReadOnlyKey{}).(bool)
	return v
}
