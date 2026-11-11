package abbreviation

import "github.com/Tencent/WeKnora/internal/types"

// ApplyRememberedSelections resolves the ambiguous terms (several active
// meanings, none chosen) whose meaning the user picked earlier in the same
// conversation: remembered maps a term key to the chosen meaning ID. A
// remembered ID that is no longer one of the term's active meanings is
// ignored, as are keys in askAgain (the user asked to choose again).
// Terms left ambiguous keep the resolution at selection_required, so the
// user is asked about those only. Each applied term is listed in
// RememberedSelections. The input is never mutated.
func ApplyRememberedSelections(r types.AbbreviationResolution, remembered map[string]string, askAgain map[string]bool) types.AbbreviationResolution {
	if len(remembered) == 0 || r.Status != types.AbbreviationStatusBlockedError ||
		r.ErrorCode != types.AbbreviationErrorSelectionRequired {
		return r
	}
	out := cloneResolution(r)
	applied := false
	for i := range out.Terms {
		t := &out.Terms[i]
		if t.Source != "" || len(t.Meanings) < 2 || askAgain[t.Key] {
			continue
		}
		id, ok := remembered[t.Key]
		if !ok {
			continue
		}
		for _, m := range t.Meanings {
			if m.ID != id {
				continue
			}
			t.SelectedID = m.ID
			t.FullForm = m.FullForm
			t.Source = types.AbbreviationSourceDictionaryActive
			out.RememberedSelections = append(out.RememberedSelections,
				types.AbbreviationTypo{Wrong: t.ShortForm, Correct: m.FullForm})
			applied = true
			break
		}
	}
	if !applied {
		return r
	}
	finalizeResolution(&out)
	return out
}

// SelectionsOf lists the meanings chosen among several in a resolution:
// term key → meaning ID, for terms resolved from the dictionary that had
// more than one active meaning. Today only the user makes such a choice
// (a pick from the choices, "X = <meaning>" typed, or a pick remembered
// from it); if the model meaning selector is ever fed conversation history
// (RelevantHistory), its choices would count here too — mark user picks
// explicitly before enabling it.
func SelectionsOf(r types.AbbreviationResolution) map[string]string {
	var out map[string]string
	for _, t := range r.Terms {
		if t.Source != types.AbbreviationSourceDictionaryActive || t.SelectedID == "" || len(t.Meanings) < 2 {
			continue
		}
		if out == nil {
			out = map[string]string{}
		}
		out[t.Key] = t.SelectedID
	}
	return out
}
