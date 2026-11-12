package errors

import "errors"

var (
	// ErrSessionNotFound session not found error
	ErrSessionNotFound = errors.New("session not found")
	// ErrSessionExpired session expired error
	ErrSessionExpired = errors.New("session expired")
	// ErrSessionLimitExceeded session limit exceeded error
	ErrSessionLimitExceeded = errors.New("session limit exceeded")
	// ErrInvalidSessionID invalid session ID error
	ErrInvalidSessionID = errors.New("invalid session id")
	// ErrInvalidTenantID invalid tenant ID error
	ErrInvalidTenantID = errors.New("invalid organization id")
	// ErrFeedbackNotAllowed feedback was sent for a user message or an unfinished answer
	ErrFeedbackNotAllowed = errors.New("feedback is only accepted on a completed assistant message")
	// ErrAnswerExportNotAnswer an export was asked of a message that is not an assistant answer
	ErrAnswerExportNotAnswer = errors.New("chỉ tải được câu trả lời của trợ lý ra .docx")
	// ErrAnswerExportIncomplete the answer is still being written
	ErrAnswerExportIncomplete = errors.New("câu trả lời chưa xong, hãy tải lại sau khi trả lời xong")
	// ErrAnswerExportFailedTurn the turn failed and has no answer to export
	ErrAnswerExportFailedTurn = errors.New("lượt này không có câu trả lời hoàn chỉnh (lỗi mô hình), không tải được .docx")
)
