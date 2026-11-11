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

// legalBasisIntent reads the turn's request; only the documents the user
// named with @ count as pointing at one (not attachments).
func legalBasisIntent(ctx context.Context) agenttools.LegalBasisIntent {
	return agenttools.ClassifyLegalBasisRequest(types.UserQueryFromContext(ctx), agenttools.ExplicitMentionsFromContext(ctx))
}

// legalBasisFirstTool is check_document_legal_basis when the turn's request
// is a legal basis check and the tool is offered, "" otherwise.
func legalBasisFirstTool(ctx context.Context, tools []chat.Tool) string {
	if !hasChatTool(tools, agenttools.ToolCheckDocumentLegalBasis) || !legalBasisIntent(ctx).Check {
		return ""
	}
	return agenttools.ToolCheckDocumentLegalBasis
}

// legalBasisSuccess is the first successful check_document_legal_basis
// result of the steps, nil when none.
func legalBasisSuccess(steps []types.AgentStep) *types.ToolResult {
	for _, s := range steps {
		for _, tc := range s.ToolCalls {
			if tc.ExecutionName() == agenttools.ToolCheckDocumentLegalBasis && tc.Result != nil && tc.Result.Success {
				return tc.Result
			}
		}
	}
	return nil
}

// legalBasisChecked reports whether the steps ran check_document_legal_basis
// successfully.
func legalBasisChecked(steps []types.AgentStep) bool { return legalBasisSuccess(steps) != nil }

// legalBasisFailurePrefix opens the fixed answer of a failed check.
const legalBasisFailurePrefix = "Không rà soát được căn cứ pháp lý"

// legalBasisFailureMessage is the fixed answer of a pure legal basis check
// whose check failed in the last round (a runner failure: the catalogue,
// a timeout, a closed document, an internal error) while none succeeded
// this turn: "Không rà soát được căn cứ pháp lý: <lý do>". "" otherwise —
// a refused call, a wrong argument or document reference is the model's
// to retry, and when one document was checked the model answers.
func legalBasisFailureMessage(ctx context.Context, steps []types.AgentStep) string {
	if len(steps) == 0 || legalBasisChecked(steps) {
		return ""
	}
	reason := ""
	for _, tc := range steps[len(steps)-1].ToolCalls {
		r := tc.Result
		if tc.ExecutionName() != agenttools.ToolCheckDocumentLegalBasis || r == nil || r.Refused {
			continue
		}
		if failed, _ := r.Data[agenttools.LegalBasisRunnerFailedKey].(bool); failed && reason == "" {
			reason = strings.TrimSpace(r.Error)
		}
	}
	if reason == "" || !legalBasisIntent(ctx).Pure {
		return ""
	}
	if i := strings.Index(reason, "\n\n["); i > 0 {
		reason = strings.TrimSpace(reason[:i])
	}
	if strings.HasPrefix(reason, legalBasisFailurePrefix) {
		return reason
	}
	return legalBasisFailurePrefix + ": " + reason
}

// legalBasisRestNote follows the check's summary when the rest of the
// request found no evidence.
const legalBasisRestNote = "Phần còn lại của câu hỏi chưa tìm được căn cứ trong kho."

type legalBasisSummaryKey struct{}

// withLegalBasisSummary keeps the summary line of the turn's successful
// check for ungroundedAnswerFor: a turn that checked the document but
// found nothing for the rest answers with what it has.
func withLegalBasisSummary(ctx context.Context, steps []types.AgentStep) context.Context {
	r := legalBasisSuccess(steps)
	if r == nil {
		return ctx
	}
	line, _ := r.Data["summary_line"].(string)
	if strings.TrimSpace(line) == "" {
		return ctx
	}
	return context.WithValue(ctx, legalBasisSummaryKey{}, strings.TrimSpace(line))
}

func legalBasisSummaryFromContext(ctx context.Context) string {
	s, _ := ctx.Value(legalBasisSummaryKey{}).(string)
	return s
}
