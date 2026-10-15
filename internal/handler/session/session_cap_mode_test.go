package session

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

// The cap mode comes from what the entry already learned about the session's
// documents: no second listing per turn.
func TestSessionCapModeUsesTheEntrysDocumentListing(t *testing.T) {
	h := &Handler{} // no document service: a listing here would be impossible
	ctx := context.Background()
	cases := []struct {
		name string
		rc   *qaRequestContext
		want types.SessionCapMode
	}{
		{"word session", &qaRequestContext{documents: sessionDocuments{known: true, word: true}}, types.SessionCapSoft},
		{"plain session", &qaRequestContext{documents: sessionDocuments{known: true}}, types.SessionCapHard},
		{"document assistant", &qaRequestContext{customAgent: &types.CustomAgent{ID: types.BuiltinDocumentAssistantID}},
			types.SessionCapSoft},
		{"unknown, no document service", &qaRequestContext{session: &types.Session{ID: "s"}}, types.SessionCapHard},
	}
	for _, c := range cases {
		if got := h.sessionCapMode(ctx, c.rc); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
