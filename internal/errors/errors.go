package errors

import (
	"fmt"
	"net/http"
)

// ErrorCode defines the error code type
type ErrorCode int

// System error codes
const (
	// Common error codes (1000-1999)
	ErrBadRequest         ErrorCode = 1000
	ErrUnauthorized       ErrorCode = 1001
	ErrForbidden          ErrorCode = 1002
	ErrNotFound           ErrorCode = 1003
	ErrMethodNotAllowed   ErrorCode = 1004
	ErrConflict           ErrorCode = 1005
	ErrTooManyRequests    ErrorCode = 1006
	ErrInternalServer     ErrorCode = 1007
	ErrServiceUnavailable ErrorCode = 1008
	ErrTimeout            ErrorCode = 1009
	ErrValidation         ErrorCode = 1010

	// Tenant related error codes (2000-2099)
	ErrTenantNotFound         ErrorCode = 2000
	ErrTenantAlreadyExists    ErrorCode = 2001
	ErrTenantInactive         ErrorCode = 2002
	ErrTenantNameRequired     ErrorCode = 2003
	ErrTenantInvalidStatus    ErrorCode = 2004
	ErrTenantCreationDisabled ErrorCode = 2005

	// Agent related error codes (2100-2199)
	ErrAgentMissingThinkingModel ErrorCode = 2100
	ErrAgentMissingAllowedTools  ErrorCode = 2101
	ErrAgentInvalidMaxIterations ErrorCode = 2102
	ErrAgentInvalidTemperature   ErrorCode = 2103

	// VectorStore binding related error codes (2200-2299).
	// Both map to HTTP 400 with a generic message; the typed code lets
	// clients distinguish "wrong UUID / cross-tenant" from "store exists
	// but is currently unavailable" without parsing the message text.
	ErrVectorStoreBindingInvalid ErrorCode = 2200
	ErrVectorStoreUnavailable    ErrorCode = 2201

	// Model lifecycle errors (2300-2399).
	// The dedicated code lets clients render structured usage details without
	// parsing the backwards-compatible English error message.
	ErrModelInUse ErrorCode = 2300

	// Add more error codes here
)

// AppError defines the application error structure
type AppError struct {
	Code     ErrorCode `json:"code"`
	Message  string    `json:"message"`
	Details  any       `json:"details,omitempty"`
	HTTPCode int       `json:"-"`
}

// Error implements the error interface
func (e *AppError) Error() string {
	return fmt.Sprintf("error code: %d, error message: %s", e.Code, e.Message)
}

// WithDetails adds error details
func (e *AppError) WithDetails(details any) *AppError {
	e.Details = details
	return e
}

// NewBadRequestError creates a bad request error
func NewBadRequestError(message string) *AppError {
	return &AppError{
		Code:     ErrBadRequest,
		Message:  message,
		HTTPCode: http.StatusBadRequest,
	}
}

// NewModelInUseError creates the typed HTTP 400 returned when active tenant
// resources still reference a model.
func NewModelInUseError(message string, details any) *AppError {
	return &AppError{
		Code:     ErrModelInUse,
		Message:  message,
		Details:  details,
		HTTPCode: http.StatusBadRequest,
	}
}

// NewUnauthorizedError creates an unauthorized error
func NewUnauthorizedError(message string) *AppError {
	return &AppError{
		Code:     ErrUnauthorized,
		Message:  message,
		HTTPCode: http.StatusUnauthorized,
	}
}

// NewForbiddenError creates a forbidden error
func NewForbiddenError(message string) *AppError {
	return &AppError{
		Code:     ErrForbidden,
		Message:  message,
		HTTPCode: http.StatusForbidden,
	}
}

// NewNotFoundError creates a not found error
func NewNotFoundError(message string) *AppError {
	return &AppError{
		Code:     ErrNotFound,
		Message:  message,
		HTTPCode: http.StatusNotFound,
	}
}

// NewConflictError creates a conflict error
func NewConflictError(message string) *AppError {
	return &AppError{
		Code:     ErrConflict,
		Message:  message,
		HTTPCode: http.StatusConflict,
	}
}

// NewTooManyRequestsError creates a 429 error, used by quota-style
// guards (e.g. per-user self-service tenant creation cap).
func NewTooManyRequestsError(message string) *AppError {
	if message == "" {
		message = "quá nhiều yêu cầu, vui lòng thử lại sau"
	}
	return &AppError{
		Code:     ErrTooManyRequests,
		Message:  message,
		HTTPCode: http.StatusTooManyRequests,
	}
}

// NewInternalServerError creates an internal server error
func NewInternalServerError(message string) *AppError {
	if message == "" {
		message = "lỗi hệ thống"
	}
	return &AppError{
		Code:     ErrInternalServer,
		Message:  message,
		HTTPCode: http.StatusInternalServerError,
	}
}

// NewServiceUnavailableError creates a service unavailable (503)
// error. Used for transient failures where the caller can retry.
func NewServiceUnavailableError(message string) *AppError {
	if message == "" {
		message = "dịch vụ tạm thời không khả dụng"
	}
	return &AppError{
		Code:     ErrServiceUnavailable,
		Message:  message,
		HTTPCode: http.StatusServiceUnavailable,
	}
}

// NewValidationError creates a validation error
func NewValidationError(message string) *AppError {
	return &AppError{
		Code:     ErrValidation,
		Message:  message,
		HTTPCode: http.StatusBadRequest,
	}
}

// Tenant related errors
func NewTenantNotFoundError() *AppError {
	return &AppError{
		Code:     ErrTenantNotFound,
		Message:  "không tìm thấy tổ chức",
		HTTPCode: http.StatusNotFound,
	}
}

// NewTenantAlreadyExistsError creates a tenant already exists error
func NewTenantAlreadyExistsError() *AppError {
	return &AppError{
		Code:     ErrTenantAlreadyExists,
		Message:  "tổ chức đã tồn tại",
		HTTPCode: http.StatusConflict,
	}
}

// NewTenantInactiveError creates a tenant inactive error
func NewTenantInactiveError() *AppError {
	return &AppError{
		Code:     ErrTenantInactive,
		Message:  "tổ chức đã bị vô hiệu hóa",
		HTTPCode: http.StatusForbidden,
	}
}

// NewTenantCreationDisabledError reports a deployment-policy denial for
// ordinary self-service tenant creation.
func NewTenantCreationDisabledError() *AppError {
	return &AppError{
		Code:     ErrTenantCreationDisabled,
		Message:  "đã tắt tự tạo tổ chức; hãy tham gia tổ chức qua lời mời",
		HTTPCode: http.StatusForbidden,
	}
}

// Agent related errors
func NewAgentMissingThinkingModelError() *AppError {
	return &AppError{
		Code:     ErrAgentMissingThinkingModel,
		Message:  "cần chọn mô hình suy luận trước khi bật chế độ trợ lý",
		HTTPCode: http.StatusBadRequest,
	}
}

func NewAgentMissingAllowedToolsError() *AppError {
	return &AppError{
		Code:     ErrAgentMissingAllowedTools,
		Message:  "cần chọn ít nhất một công cụ được phép",
		HTTPCode: http.StatusBadRequest,
	}
}

func NewAgentInvalidMaxIterationsError() *AppError {
	return &AppError{
		Code:     ErrAgentInvalidMaxIterations,
		Message:  "số vòng lặp tối đa phải là số nguyên dương, hoặc -1 nếu không giới hạn",
		HTTPCode: http.StatusBadRequest,
	}
}

func NewAgentInvalidTemperatureError() *AppError {
	return &AppError{
		Code:     ErrAgentInvalidTemperature,
		Message:  "tham số temperature phải nằm trong khoảng 0-2",
		HTTPCode: http.StatusBadRequest,
	}
}

// NewVectorStoreBindingInvalidError signals that a knowledge base create
// request referenced a vector store that does not exist under the caller's
// tenant (or carried a malformed UUID). The user-facing message is
// intentionally generic to avoid enumeration oracles — see the structured
// log at the call site for the tenant/store pair.
func NewVectorStoreBindingInvalidError(message string) *AppError {
	if message == "" {
		message = "không tìm thấy kho vector"
	}
	return &AppError{
		Code:     ErrVectorStoreBindingInvalid,
		Message:  message,
		HTTPCode: http.StatusBadRequest,
	}
}

// NewVectorStoreUnavailableError signals that the requested vector store
// row exists in the database but is not currently wired into the in-memory
// engine registry (factory failure on CreateStore, dynamic config error,
// or stale state after a connection-config rotation).
func NewVectorStoreUnavailableError(message string) *AppError {
	if message == "" {
		message = "kho vector tạm thời không khả dụng"
	}
	return &AppError{
		Code:     ErrVectorStoreUnavailable,
		Message:  message,
		HTTPCode: http.StatusBadRequest,
	}
}

// IsAppError checks if the error is an AppError type
func IsAppError(err error) (*AppError, bool) {
	appErr, ok := err.(*AppError)
	return appErr, ok
}
