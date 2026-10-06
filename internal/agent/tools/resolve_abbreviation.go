package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/abbreviation"
)

// ToolResolveAbbreviation expands / looks up / suggests Vietnamese
// abbreviations against the global dictionary.
const ToolResolveAbbreviation = "resolve_abbreviation"

var resolveAbbreviationTool = BaseTool{
	name: ToolResolveAbbreviation,
	description: `Work with the Vietnamese abbreviation dictionary.

## When to Use

In a Q&A turn, abbreviations in the user's message were already resolved and
validated by the runtime before you were invoked — use the resolved mapping
shown in the request or in this tool's output, and never re-expand or guess a
short form. This tool's remaining jobs are narrower:

## Actions

- action="expand" (default): pass text in "text". Inside a resolved Q&A turn
  the runtime's validated mapping is applied; outside one, the active
  dictionary is consulted. Returns the rewritten text plus which
  abbreviations were applied, which matched several meanings (ambiguous —
  ask the user which they meant), and which look like abbreviations but are
  unknown to the dictionary.
- action="lookup": pass one short form in "short_form" to list its known
  meanings (active and pending — pending entries are diagnostic only and are
  never used for expansion).
- action="suggest": propose a new abbreviation with "short_form",
  "full_form" and optional "description". Inside a resolved Q&A turn it is
  accepted only when it reproduces the exact definition the user supplied
  for this request. Never infer or invent a full form. An accepted
  suggestion applies to the current request and stays pending for the shared
  dictionary until a workspace Owner or SuperAdmin activates it — say so
  when reporting back to the user.`,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "action": {
      "type": "string",
      "enum": ["expand", "lookup", "suggest"],
      "description": "expand rewrites text (default); lookup lists meanings of one short form; suggest proposes a new dictionary entry"
    },
    "text": {
      "type": "string",
      "description": "Full text to expand (action=expand)"
    },
    "short_form": {
      "type": "string",
      "description": "The abbreviation, e.g. \"UBND\" (action=lookup or suggest)"
    },
    "full_form": {
      "type": "string",
      "description": "The expanded meaning, e.g. \"Ủy ban nhân dân\" (action=suggest)"
    },
    "description": {
      "type": "string",
      "description": "Optional note for the suggestion (action=suggest)"
    }
  },
  "required": ["action"]
}`),
}

// ResolveAbbreviationTool wraps the abbreviation service for the agent.
type ResolveAbbreviationTool struct {
	BaseTool
	svc interfaces.AbbreviationService
}

// NewResolveAbbreviationTool creates the tool.
func NewResolveAbbreviationTool(svc interfaces.AbbreviationService) *ResolveAbbreviationTool {
	return &ResolveAbbreviationTool{BaseTool: resolveAbbreviationTool, svc: svc}
}

type resolveAbbreviationInput struct {
	Action      string `json:"action"`
	Text        string `json:"text"`
	ShortForm   string `json:"short_form"`
	FullForm    string `json:"full_form"`
	Description string `json:"description"`
}

func (t *ResolveAbbreviationTool) fail(msg string) (*types.ToolResult, error) {
	return &types.ToolResult{Success: false, Error: msg}, fmt.Errorf("%s", msg)
}

// Execute dispatches on the action parameter.
func (t *ResolveAbbreviationTool) Execute(
	ctx context.Context, args json.RawMessage,
) (*types.ToolResult, error) {
	var input resolveAbbreviationInput
	if err := json.Unmarshal(args, &input); err != nil {
		return t.fail(fmt.Sprintf("Failed to parse args: %v", err))
	}
	if t.svc == nil {
		return t.fail("abbreviation dictionary is not available")
	}
	switch input.Action {
	case "", "expand":
		return t.expand(ctx, input)
	case "lookup":
		return t.lookup(ctx, input)
	case "suggest":
		return t.suggest(ctx, input)
	default:
		return t.fail(fmt.Sprintf("unknown action %q (expected expand|lookup|suggest)", input.Action))
	}
}

func (t *ResolveAbbreviationTool) expand(
	ctx context.Context, input resolveAbbreviationInput,
) (*types.ToolResult, error) {
	if strings.TrimSpace(input.Text) == "" {
		return t.fail("text is required for action=expand")
	}
	var res *abbreviation.ExpandResult
	if sealed, ok := abbreviation.ResolutionFromContext(ctx); ok {
		// Bound QA turn: reuse the validated mapping — the dictionary is not
		// consulted again and model arguments cannot widen it.
		res = abbreviation.ExpandDetected(input.Text, sealedAbbreviationActives(&sealed), sealed.Detection())
	} else {
		actives, err := t.svc.ListActive(ctx)
		if err != nil {
			return t.fail(fmt.Sprintf("failed to load abbreviation dictionary: %v", err))
		}
		res = abbreviation.ExpandDetected(input.Text, actives, t.svc.DetectCandidates(ctx, input.Text))
	}

	var b strings.Builder
	b.WriteString("<abbreviation_expand>\n")
	fmt.Fprintf(&b, "expanded_text: %s\n", res.Expanded)
	for _, a := range res.Applied {
		fmt.Fprintf(&b, "applied: %s → %s\n", a.ShortForm, a.FullForm)
	}
	for short, meanings := range res.Ambiguous {
		fmt.Fprintf(&b, "ambiguous: %s could mean", short)
		for _, m := range meanings {
			fmt.Fprintf(&b, " \"%s\"", m.FullForm)
			if m.Description != "" {
				fmt.Fprintf(&b, " (%s)", m.Description)
			}
			b.WriteString(";")
		}
		b.WriteString(" — ask the user which meaning they intended before expanding\n")
	}
	for _, p := range res.Potential {
		fmt.Fprintf(&b, "unknown_candidate: %s (looks like an abbreviation but is not in the dictionary)\n", p)
	}
	b.WriteString("</abbreviation_expand>")

	return &types.ToolResult{
		Success: true,
		Output:  b.String(),
		Data:    map[string]interface{}{"result": res},
	}, nil
}

func (t *ResolveAbbreviationTool) lookup(
	ctx context.Context, input resolveAbbreviationInput,
) (*types.ToolResult, error) {
	short := strings.TrimSpace(input.ShortForm)
	if short == "" {
		return t.fail("short_form is required for action=lookup")
	}
	rows, err := t.svc.ListByShortForm(ctx, short)
	if err != nil {
		return t.fail(fmt.Sprintf("lookup failed: %v", err))
	}
	if len(rows) == 0 {
		return &types.ToolResult{
			Success: true,
			Output:  fmt.Sprintf("No dictionary entry for %q. You may suggest one with action=suggest.", short),
			Data:    map[string]interface{}{"short_form": short, "matches": 0},
		}, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Entries for %q:\n", short)
	for _, r := range rows {
		state := "pending"
		if r.IsActive {
			state = "active"
		}
		fmt.Fprintf(&b, "- %s → %s [%s]", r.ShortForm, r.FullForm, state)
		if r.Description != "" {
			fmt.Fprintf(&b, " — %s", r.Description)
		}
		b.WriteString("\n")
	}
	return &types.ToolResult{
		Success: true,
		Output:  b.String(),
		Data:    map[string]interface{}{"short_form": short, "matches": len(rows), "entries": rows},
	}, nil
}

func (t *ResolveAbbreviationTool) suggest(
	ctx context.Context, input resolveAbbreviationInput,
) (*types.ToolResult, error) {
	short := strings.TrimSpace(input.ShortForm)
	full := strings.TrimSpace(input.FullForm)
	if short == "" || full == "" {
		return t.fail("short_form and full_form are required for action=suggest")
	}
	if sealed, ok := abbreviation.ResolutionFromContext(ctx); ok {
		// Inside a bound QA turn a suggestion is accepted only when it
		// reproduces a definition the user supplied for this request (the
		// sealed user_current_request term, normalized short and full form
		// with its source message). Model tool arguments alone are never
		// authoritative user intent.
		if !sealedUserDefinitionMatches(&sealed, short, full) {
			return t.fail("action=suggest requires the exact definition the user supplied " +
				"for this turn (matching short and full form); the model cannot propose " +
				"abbreviation meanings on its own")
		}
	}
	row, err := t.svc.Suggest(ctx, &types.AbbreviationCreateRequest{
		ShortForm:   short,
		FullForm:    full,
		Description: input.Description,
	})
	if err != nil {
		return t.fail(fmt.Sprintf("suggest failed: %v", err))
	}
	status := "pending_review"
	output := fmt.Sprintf(
		"Recorded suggestion %q → %q (or it was already pending). The meaning applies to the "+
			"current request; for the shared dictionary it stays pending until a workspace "+
			"Owner or SuperAdmin approves it — tell the user so.",
		row.ShortForm, row.FullForm)
	if row.IsActive {
		status = "active"
		output = fmt.Sprintf("%q → %q is already an active dictionary entry.", row.ShortForm, row.FullForm)
	}
	return &types.ToolResult{
		Success: true,
		Output:  output,
		Data: map[string]interface{}{
			"id":         row.ID,
			"short_form": row.ShortForm,
			"full_form":  row.FullForm,
			"is_active":  row.IsActive,
			"status":     status,
		},
	}, nil
}
