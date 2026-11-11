package agent

import (
	"context"
	"strings"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

// A request to check the legal basis of the open document ("rà soát căn
// cứ pháp lý", "các văn bản viện dẫn còn hiệu lực không") reads to the
// knowledge-lookup classifier as a legal question: the turn would start
// with search_knowledge under the grounding gate and the model answer
// from search hits, citation by citation from memory. When the request is
// one (agenttools.ClassifyLegalBasisRequest) and the tool is offered, the
// turn starts with check_document_legal_basis instead.
//
// The grounding gate then treats a pure check like a format check: its
// report, read from the legal catalogue, answers it (markUngrounded). A
// request that also asks something else keeps the gate until a search
// returns evidence. A pure check whose tool fails ends with a fixed
// answer saying why (legalBasisFailureMessage), never with "no evidence"
// nor with the model's memory.

// legalBasisIntent reads the turn's request.
func legalBasisIntent(ctx context.Context) agenttools.LegalBasisIntent {
	return agenttools.ClassifyLegalBasisRequest(types.UserQueryFromContext(ctx), len(types.MentionedDocumentsFromContext(ctx)))
}

// legalBasisFirstTool is check_document_legal_basis when the turn's request
// is a legal basis check and the tool is offered, "" otherwise.
func legalBasisFirstTool(ctx context.Context, tools []chat.Tool) string {
	if !hasChatTool(tools, agenttools.ToolCheckDocumentLegalBasis) || !legalBasisIntent(ctx).Check {
		return ""
	}
	return agenttools.ToolCheckDocumentLegalBasis
}

// legalBasisChecked reports whether the steps ran check_document_legal_basis
// successfully.
func legalBasisChecked(steps []types.AgentStep) bool {
	for _, s := range steps {
		for _, tc := range s.ToolCalls {
			if tc.ExecutionName() == agenttools.ToolCheckDocumentLegalBasis && tc.Result != nil && tc.Result.Success {
				return true
			}
		}
	}
	return false
}

// legalBasisFailurePrefix opens the fixed answer of a failed check.
const legalBasisFailurePrefix = "Không rà soát được căn cứ pháp lý"

// legalBasisFailureMessage is the fixed answer of a pure legal basis check
// whose call in this round failed and none succeeded: "Không rà soát được
// căn cứ pháp lý: <lý do>". "" otherwise.
func legalBasisFailureMessage(ctx context.Context, step types.AgentStep) string {
	reason := ""
	for _, tc := range step.ToolCalls {
		if tc.ExecutionName() != agenttools.ToolCheckDocumentLegalBasis || tc.Result == nil {
			continue
		}
		if tc.Result.Success {
			return ""
		}
		if reason == "" {
			reason = strings.TrimSpace(tc.Result.Error)
		}
	}
	if reason == "" || !legalBasisIntent(ctx).Pure {
		return ""
	}
	// a guard's refusal carries its stop note for the model: not for the user
	if i := strings.Index(reason, "\n\n["); i > 0 {
		reason = strings.TrimSpace(reason[:i])
	}
	if strings.HasPrefix(reason, legalBasisFailurePrefix) {
		return reason
	}
	return legalBasisFailurePrefix + ": " + reason
}
