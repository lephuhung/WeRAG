package types

import "errors"

// A turn fails when the answering model cannot produce its answer: the call
// was refused (transport error, non-2xx), broke off mid-stream, or came back
// with no text and no tool call. The turn then ends with a code-written
// notice instead of an empty answer, and the assistant message is marked
// (MessageExecutionContext.TurnFailure) so a reloaded conversation shows the
// same notice and the next turn's history leaves the failed turn out.
//
// A user stop is not a failure, nor is any fixed answer (grounding gate,
// quota messages, clarification cards).

// Values of MessageExecutionContext.TurnFailure, also sent as
// event.ErrorData.ErrorCode ("error_code" in the SSE error event).
const (
	// TurnFailureModelUnavailable: no answer at all.
	TurnFailureModelUnavailable = "llm_unavailable"
	// TurnFailureModelUnavailableAfterSearch: tools returned results, the
	// answer could not be composed from them.
	TurnFailureModelUnavailableAfterSearch = "llm_unavailable_after_search"
	// TurnFailureModelInterrupted: part of the answer was shown, then the
	// model broke off; the partial text is kept with ModelInterruptedNote.
	TurnFailureModelInterrupted = "llm_interrupted"
)

// User-facing texts of a failed turn. The raw cause (HTTP status, provider
// message) goes to the server log only.
const (
	ModelUnavailableMessage = "Mô hình ngôn ngữ đang gặp sự cố nên chưa trả lời được câu hỏi này. " +
		"Vui lòng thử lại sau ít phút."
	ModelUnavailableAfterSearchMessage = "Đã tìm được kết quả tra cứu nhưng mô hình ngôn ngữ đang gặp sự cố " +
		"nên chưa soạn được câu trả lời. Vui lòng thử lại sau ít phút."
	// ModelInterruptedNote is appended, on its own paragraph, to the partial
	// answer of an interrupted turn.
	ModelInterruptedNote = "\n\n_Câu trả lời bị gián đoạn do mô hình ngôn ngữ gặp sự cố. Vui lòng thử lại._"
)

// IsTurnFailure reports whether code is one of the TurnFailure* values.
func IsTurnFailure(code string) bool {
	switch code {
	case TurnFailureModelUnavailable, TurnFailureModelUnavailableAfterSearch, TurnFailureModelInterrupted:
		return true
	}
	return false
}

// TurnFailureMessage is the user-facing text sent with a failure code.
func TurnFailureMessage(code string) string {
	switch code {
	case TurnFailureModelUnavailableAfterSearch:
		return ModelUnavailableAfterSearchMessage
	case TurnFailureModelInterrupted:
		return ModelInterruptedNote[2:]
	}
	return ModelUnavailableMessage
}

// ModelCallError marks an error of the answering model call itself (as
// opposed to a gate, a tool or a cancelled context). Error() is the cause's
// text unchanged, so callers that classify by message keep working.
type ModelCallError struct {
	Err error
}

func (e *ModelCallError) Error() string { return e.Err.Error() }
func (e *ModelCallError) Unwrap() error { return e.Err }

// NewModelCallError wraps err; nil stays nil and an already wrapped error is
// returned as is.
func NewModelCallError(err error) error {
	if err == nil || IsModelCallError(err) {
		return err
	}
	return &ModelCallError{Err: err}
}

// IsModelCallError reports whether err is (or wraps) a ModelCallError.
func IsModelCallError(err error) bool {
	var mce *ModelCallError
	return errors.As(err, &mce)
}
