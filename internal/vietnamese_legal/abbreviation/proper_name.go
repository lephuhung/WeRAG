package abbreviation

import "github.com/Tencent/WeKnora/internal/types"

// DropProperNames removes from a detection the words the dictionary holds
// as active proper names (unit codes such as PV01, kept as typed): they are
// neither asked about nor expanded. A word that also has an active meaning
// is left to it. A heuristic detection becomes an
// explicit one over the remaining candidates, as DropSettled does.
func DropProperNames(text string, det types.AbbreviationDetection, active []*types.Abbreviation) types.AbbreviationDetection {
	names, meanings := map[string]bool{}, map[string]bool{}
	for _, a := range active {
		if a == nil || !a.IsActive {
			continue
		}
		if k := termKey(a.ShortForm); k != "" {
			if a.IsProperName {
				names[k] = true
			} else {
				meanings[k] = true
			}
		}
	}
	// an active meaning of the word wins: it was given a meaning after
	// someone kept it as typed
	for k := range meanings {
		delete(names, k)
	}
	if len(names) == 0 {
		return det
	}
	keys := det.Keys
	if det.Detector == types.AbbreviationDetectorHeuristic {
		keys = nil
		for _, tok := range FindCandidates(text) {
			keys = append(keys, termKey(tok))
		}
	}
	var kept []string
	dropped := false
	for _, k := range keys {
		if names[k] {
			dropped = true
			continue
		}
		kept = append(kept, k)
	}
	if !dropped {
		return det
	}
	out := det
	if det.Detector == types.AbbreviationDetectorHeuristic {
		out.Detector = types.AbbreviationDetectorHeuristicSpellcheck
	}
	out.Keys = kept
	out.Typos = keepTyposExcept(det.Typos, names)
	out.Corrections = keepTyposExcept(det.Corrections, names)
	return out
}

func keepTyposExcept(typos []types.AbbreviationTypo, names map[string]bool) []types.AbbreviationTypo {
	var out []types.AbbreviationTypo
	for _, t := range typos {
		if !names[termKey(t.Wrong)] {
			out = append(out, t)
		}
	}
	return out
}
