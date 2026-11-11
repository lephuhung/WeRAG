package service

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/abbreviation"
)

// legalPickHistory bounds how many recent messages of the session are read
// for an earlier pick from a candidates card.
const legalPickHistory = 60

// rememberedLegalPick returns the document the user picked for the same
// bare reference (same type and number) earlier in this conversation: after
// a candidates card for it, the newest user message that re-sends one of
// its choices, answers with a choice's ordinal right after the card, or
// names exactly one of its candidates' số hiệu. A card shown again for the
// reference drops the earlier pick until the user picks again. The pick is
// used only while it is still among the current card's candidates: "Nghị
// định 53 về thuế"
// after picking the cyber-security decree shows the narrowed card. nil when
// there is none or the history cannot be read (the card is shown as
// before).
func (s *sessionService) rememberedLegalPick(ctx context.Context, req *types.QARequest, card *tools.LegalCandidatesCard) *types.LegalReferencePin {
	if s.messageRepo == nil || req == nil || req.Session == nil || card == nil {
		return nil
	}
	messages, err := s.messageRepo.GetRecentMessagesBySession(ctx, req.Session.ID, legalPickHistory)
	if err != nil {
		logger.Warnf(ctx, "legal candidates: history unavailable: %v", err)
		return nil
	}
	var (
		known    []legalCardCandidate // every choice offered for the reference so far
		lastCard []legalCardCandidate // the choices of the card just before, for an ordinal reply
		pick     *legalCardCandidate
	)
	for _, m := range messages {
		if m == nil || (req.UserMessageID != "" && m.ID == req.UserMessageID) {
			continue
		}
		switch m.Role {
		case "assistant":
			cands := legalCardCandidates(m, card.Reference)
			known = append(known, cands...)
			lastCard = cands
			if len(cands) > 0 {
				// the card was shown again ("Không phải?", or the pick no
				// longer fitted): the earlier pick stands only if re-made
				pick = nil
			}
		case "user":
			if len(known) > 0 {
				if c := pickedCandidate(known, lastCard, m.Content); c != nil {
					pick = c
				}
			}
			lastCard = nil
		}
	}
	if pick == nil {
		return nil
	}
	want := foldNumber(pick.number)
	for _, d := range card.Candidates {
		if d != nil && foldNumber(d.DocumentNumber) == want {
			title := pick.title
			if t := strings.TrimSpace(d.Title); t != "" {
				title = t
			}
			return &types.LegalReferencePin{Reference: card.Reference.Text, DocumentNumber: strings.TrimSpace(d.DocumentNumber),
				DocType: card.Reference.DocType, Title: title}
		}
	}
	return nil
}

// legalCardCandidate is one choice of a stored candidates card.
type legalCardCandidate struct {
	number, title, resend string
}

// legalCardCandidates returns the choices of the candidates card an
// assistant message carries for ref (same doc type and bare number), nil
// when it carries none. The card comes from the pre-agent gate or from
// check_legal_status; both use display_type legal_candidates.
func legalCardCandidates(m *types.Message, ref tools.LegalReference) []legalCardCandidate {
	var out []legalCardCandidate
	for _, step := range m.AgentSteps {
		for _, call := range step.ToolCalls {
			if call.Result == nil || call.Result.Data == nil {
				continue
			}
			data := call.Result.Data
			if fmt.Sprint(data["display_type"]) != tools.LegalCandidatesType ||
				!strings.EqualFold(strings.TrimSpace(fmt.Sprint(data["doc_type"])), ref.DocType) ||
				strings.TrimSpace(fmt.Sprint(data["number"])) != ref.Number {
				continue
			}
			for _, raw := range anySlice(data["candidates"]) {
				c, ok := raw.(map[string]interface{})
				if !ok {
					continue
				}
				number := strings.TrimSpace(stringOf(c["document_number"]))
				if number == "" {
					continue
				}
				out = append(out, legalCardCandidate{number: number, title: strings.TrimSpace(stringOf(c["title"])),
					resend: strings.TrimSpace(stringOf(c["resend"]))})
			}
		}
	}
	return out
}

// ordinalReplyRe is a reply that is only a choice number ("2", "số 2").
var ordinalReplyRe = regexp.MustCompile(`^\s*(?:(?i:số|chọn|văn bản)\s*)?(\d{1,2})\s*[.)]?\s*$`)

// pickedCandidate is the choice a user message makes among known: a choice
// re-sent as is, the ordinal of a choice of the card just before (Word
// add-in answers by typing it), or the only candidate whose số hiệu the
// message names. nil when it makes none.
func pickedCandidate(known, lastCard []legalCardCandidate, reply string) *legalCardCandidate {
	reply = strings.TrimSpace(reply)
	for i := range known {
		if known[i].resend != "" && known[i].resend == reply {
			return &known[i]
		}
	}
	if m := ordinalReplyRe.FindStringSubmatch(reply); m != nil && len(lastCard) > 0 {
		if n, err := strconv.Atoi(m[1]); err == nil && n >= 1 && n <= len(lastCard) {
			return &lastCard[n-1]
		}
		return nil
	}
	text := foldNumber(reply)
	var found *legalCardCandidate
	for i := range known {
		n := foldNumber(known[i].number)
		if n == "" || !containsNumber(text, n) {
			continue
		}
		if found != nil && foldNumber(found.number) != n {
			return nil // names two of them: not a pick
		}
		found = &known[i]
	}
	return found
}

// foldNumber puts a số hiệu (or a text holding one) in one comparable form:
// NFC, upper case, single spaces, the look-alike Ð (U+00D0) as Đ.
func foldNumber(s string) string {
	s = strings.ReplaceAll(norm.NFC.String(s), "\u00d0", "Đ")
	s = strings.ReplaceAll(s, "\u00f0", "đ")
	return vietnamese_legal.NormalizeDocumentNumber(s)
}

// containsNumber reports whether text holds number as a whole số hiệu:
// "153/2022/NĐ-CP" does not hold "53/2022/NĐ-CP".
func containsNumber(text, number string) bool {
	for from := 0; ; {
		i := strings.Index(text[from:], number)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(number)
		before, _ := utf8.DecodeLastRuneInString(text[:start])
		after, _ := utf8.DecodeRuneInString(text[end:])
		if (start == 0 || !isNumberRune(before)) && (end == len(text) || !isNumberRune(after)) {
			return true
		}
		from = start + 1
	}
}

func isNumberRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '/' || r == '-'
}

func anySlice(v interface{}) []interface{} {
	switch x := v.(type) {
	case []interface{}:
		return x
	case []map[string]interface{}:
		out := make([]interface{}, len(x))
		for i := range x {
			out[i] = x[i]
		}
		return out
	}
	return nil
}

func stringOf(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// noteRememberedChoice shows under the user's question that a choice made
// earlier in the conversation was reused ("Nghị định 53" → its picked số
// hiệu): it is added to the turn's abbreviation state, which already
// carries the reused abbreviation meanings to the chat (live event and
// history) with a "Không phải?" button that re-sends the question with the
// reference to ask about again. Best effort: without a gate turn (IM
// channels) or on a store error the turn just runs on.
func (s *sessionService) noteRememberedChoice(ctx context.Context, req *types.QARequest, bus *event.EventBus, choice types.AbbreviationTypo) {
	if s.abbreviationStore == nil || req == nil || req.Session == nil {
		return
	}
	r, ok := abbreviation.ResolutionFromContext(ctx)
	if !ok || r.RequestID == "" {
		return
	}
	owner, err := s.abbreviationOwner(ctx, req)
	if err != nil {
		return
	}
	row, err := s.abbreviationStore.Get(ctx, owner, r.RequestID)
	if err != nil || row.State != types.AbbreviationTurnRunning || row.ExecutingMessageID != req.AssistantMessageID {
		return
	}
	for _, c := range row.Payload.Resolution.RememberedSelections {
		if c.Wrong == choice.Wrong {
			return
		}
	}
	row.Payload.Resolution.RememberedSelections = append(row.Payload.Resolution.RememberedSelections, choice)
	swapped, err := s.abbreviationStore.CompareAndSwap(ctx, owner, row.Version, row)
	if err != nil || !swapped {
		logger.Warnf(ctx, "legal candidates: reused pick not noted: %v", err)
		return
	}
	row.Version++
	if bus == nil {
		return
	}
	public := row.Payload.Resolution.PublicState()
	public.RequestID, public.RootUserMessageID, public.Version, public.ExpiresAt =
		row.ID, row.RootUserMessageID, row.Version, row.ExpiresAt
	public.Status = row.State
	_ = bus.Emit(ctx, event.Event{Type: event.EventAbbreviationResolution, SessionID: req.Session.ID,
		Data: event.AbbreviationResolutionData{AbbreviationPublicState: public, Origin: "runtime"}})
}

// askedAgain reports whether the user asked to choose again for reference
// ("Không phải?" under a reused pick re-sends the question with it).
func askedAgain(req *types.QARequest, reference string) bool {
	want := strings.ToLower(norm.NFC.String(strings.Join(strings.Fields(reference), " ")))
	for _, w := range req.AbbreviationAskWords {
		if strings.ToLower(norm.NFC.String(strings.Join(strings.Fields(w), " "))) == want {
			return true
		}
	}
	return false
}
