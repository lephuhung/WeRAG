package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/people"
)

// ToolPeopleLookup searches the external MongoDB person-record store.
// Registered only for admin callers when PEOPLE_SEARCH_ENABLED is on — the
// data is sensitive PII (CCCD, BHXH, phone, address).
const ToolPeopleLookup = "people_lookup"

var peopleLookupTool = BaseTool{
	name: ToolPeopleLookup,
	description: `Look up a person's records in the citizen/people database.

## When to Use

Only when the user asks about a specific person's administrative records:
identity numbers, social insurance (BHXH), phone, address history. This is
sensitive personal data — use it solely to answer the user's stated request
and do not volunteer extra fields.

## Lookup Types

- lookup_type="cccd": national ID / CMND — pass the 9- or 12-digit number(s)
  in "query".
- lookup_type="bhxh": social insurance number — digits in "query".
- lookup_type="phone": 10-digit phone number(s) in "query".
- lookup_type="name": full or partial name in "query" (substring match).
- lookup_type="advanced": combine "name", "dob" (date of birth), "address"
  and/or "phone"; all supplied criteria must match (AND).

## What It Returns

found=true with person profiles consolidated across data sources, or
found=false. When the lookup system is unavailable the result says so —
relay that message, do NOT claim the person does not exist.`,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "lookup_type": {
      "type": "string",
      "enum": ["cccd", "bhxh", "phone", "name", "advanced"],
      "description": "Which identifier to search by"
    },
    "query": {
      "type": "string",
      "description": "The identifier value(s) or name for cccd/bhxh/phone/name lookups"
    },
    "name": {
      "type": "string",
      "description": "Person's name (advanced lookup)"
    },
    "dob": {
      "type": "string",
      "description": "Date of birth, e.g. 1985-03-12 or 12/03/1985 (advanced lookup)"
    },
    "address": {
      "type": "string",
      "description": "Address substring (advanced lookup)"
    },
    "phone": {
      "type": "string",
      "description": "Phone number (advanced lookup)"
    },
    "limit": {
      "type": "integer",
      "description": "Max records per data source for name/advanced lookups (default 10)"
    }
  },
  "required": ["lookup_type"]
}`),
}

// PeopleLookupTool wraps the people search service for the agent.
type PeopleLookupTool struct {
	BaseTool
	svc *people.Service
}

// NewPeopleLookupTool creates the tool.
func NewPeopleLookupTool(svc *people.Service) *PeopleLookupTool {
	return &PeopleLookupTool{BaseTool: peopleLookupTool, svc: svc}
}

type peopleLookupInput struct {
	LookupType string `json:"lookup_type"`
	Query      string `json:"query"`
	Name       string `json:"name"`
	DOB        string `json:"dob"`
	Address    string `json:"address"`
	Phone      string `json:"phone"`
	Limit      int64  `json:"limit"`
}

func (t *PeopleLookupTool) fail(msg string) (*types.ToolResult, error) {
	return &types.ToolResult{Success: false, Error: msg}, fmt.Errorf("%s", msg)
}

// Execute dispatches on lookup_type.
func (t *PeopleLookupTool) Execute(
	ctx context.Context, args json.RawMessage,
) (*types.ToolResult, error) {
	var input peopleLookupInput
	if err := json.Unmarshal(args, &input); err != nil {
		return t.fail(fmt.Sprintf("Failed to parse args: %v", err))
	}
	if t.svc == nil || !t.svc.Enabled() {
		return t.fail("people lookup is not enabled on this deployment")
	}

	var res *people.SearchResult
	switch input.LookupType {
	case "cccd":
		res = t.svc.SearchByCCCD(ctx, input.Query)
	case "bhxh":
		res = t.svc.SearchByBHXH(ctx, input.Query)
	case "phone":
		res = t.svc.SearchByPhone(ctx, input.Query)
	case "name":
		res = t.svc.SearchByName(ctx, input.Query, input.Limit)
	case "advanced":
		res = t.svc.SearchAdvanced(ctx, map[string]string{
			"name": input.Name, "dob": input.DOB,
			"address": input.Address, "phone": input.Phone,
		}, input.Limit)
	default:
		return t.fail(fmt.Sprintf(
			"unknown lookup_type %q (expected cccd|bhxh|phone|name|advanced)", input.LookupType))
	}

	var b strings.Builder
	b.WriteString("<people_lookup>\n")
	b.WriteString(res.Display)
	b.WriteString("\n</people_lookup>")
	if res.Unavailable {
		b.WriteString("\nThe lookup system reported it is unavailable. Relay that " +
			"to the user — do NOT report that the person was not found.")
	}
	return &types.ToolResult{
		Success: true,
		Output:  b.String(),
		Data: map[string]interface{}{
			"found":       res.Found,
			"persons":     res.Persons,
			"schemas":     res.Schemas,
			"lookup_type": res.LookupType,
			"unavailable": res.Unavailable,
		},
	}, nil
}
