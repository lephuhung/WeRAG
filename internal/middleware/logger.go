package middleware

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	maxBodySize = 1024 * 10 // ghi log tối đa 10KB nội dung body
)

// loggerResponseBodyWriter là ResponseWriter tuỳ chỉnh để bắt nội dung phản hồi (dùng cho middleware logger)
type loggerResponseBodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

// Write ghi đè phương thức Write, ghi đồng thời vào buffer và writer gốc
// Giới hạn kích thước buffer để phản hồi dạng stream như SSE không làm bộ nhớ tăng vô hạn
func (r loggerResponseBodyWriter) Write(b []byte) (int, error) {
	if r.body.Len() < maxBodySize {
		remaining := maxBodySize - r.body.Len()
		if len(b) <= remaining {
			r.body.Write(b)
		} else {
			r.body.Write(b[:remaining])
		}
	}
	return r.ResponseWriter.Write(b)
}

// sensitiveFieldRegex khớp các trường nhạy cảm trong JSON (không phân biệt hoa thường, hỗ trợ snake_case / camelCase / PascalCase).
// $1 bắt tên trường gốc (gồm cả dấu nháy hai bên), giữ nguyên tên trường trong log, chỉ thay giá trị bằng "***".
var sensitiveFieldRegex = regexp.MustCompile(
	`(?i)("(?:new[_-]?password|old[_-]?password|password|passwd|ticket|token|access[_-]?token|` +
		`refresh[_-]?token|next[_-]?token|device[_-]?token|pending[_-]?token|pairing[_-]?link|` +
		`id[_-]?token|authorization|auth[_-]?token|api[_-]?key|` +
		`api[_-]?secret|secret[_-]?key|client[_-]?secret|private[_-]?key|secret|` +
		`authorization[_-]?url|authorization[_-]?attempt)")\s*:\s*"[^"]*"`,
)

// sanitizeBody che thông tin nhạy cảm
func sanitizeBody(body string) string {
	return sensitiveFieldRegex.ReplaceAllString(body, `$1:"***"`)
}

var sensitiveQueryFields = map[string]struct{}{
	"access_token":          {},
	"authorization_attempt": {},
	"code":                  {},
	"id_token":              {},
	"refresh_token":         {},
	"state":                 {},
	// ticket is the sandbox terminal's WebSocket handshake credential. A
	// browser cannot set Authorization on an upgrade, so it travels in the
	// query string; anyone holding it for its 2-minute TTL can open a shell
	// in the session's sandbox, which is why it must never reach a log line.
	"ticket": {},
	"token":  {},
}

// sanitizeQuery prevents OAuth authorization codes, CSRF/attempt state, and
// handshake credentials from being copied into access logs. Parsing the query
// also covers repeated and percent-encoded parameters without relying on
// fragile string replacement.
func sanitizeQuery(raw string) string {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "[invalid query omitted]"
	}
	for key := range values {
		if _, sensitive := sensitiveQueryFields[strings.ToLower(key)]; sensitive {
			values[key] = []string{"***"}
		}
	}
	return values.Encode()
}

// readRequestBody đọc request body (giới hạn kích thước cho log, nhưng đọc đầy đủ để đặt lại)
func readRequestBody(c *gin.Context) string {
	if c.Request.Body == nil {
		return ""
	}

	// Kiểm tra Content-Type, chỉ ghi log loại JSON
	contentType := c.GetHeader("Content-Type")
	if !strings.Contains(contentType, "application/json") &&
		!strings.Contains(contentType, "application/x-www-form-urlencoded") &&
		!strings.Contains(contentType, "text/") {
		return "[non-text type, skipped]"
	}

	// Đọc đầy đủ body (không giới hạn) vì cần đặt lại nguyên vẹn cho handler phía sau
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return "[failed to read request body]"
	}

	// Đặt lại request body bằng nội dung đầy đủ để handler phía sau đọc được toàn bộ dữ liệu
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// Body dùng cho log (giới hạn kích thước)
	var logBodyBytes []byte
	if len(bodyBytes) > maxBodySize {
		logBodyBytes = bodyBytes[:maxBodySize]
	} else {
		logBodyBytes = bodyBytes
	}

	bodyStr := string(logBodyBytes)
	if len(bodyBytes) > maxBodySize {
		bodyStr += "... [content too long, truncated]"
	}

	return sanitizeBody(bodyStr)
}

// RequestID middleware adds a unique request ID to the context
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get request ID from header or generate a new one
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}
		safeRequestID := secutils.SanitizeForLog(requestID)
		// Set request ID in header
		c.Header("X-Request-ID", requestID)

		// Set request ID in context
		c.Set(types.RequestIDContextKey.String(), requestID)

		// Set logger in context
		requestLogger := logger.GetLogger(c)
		requestLogger = requestLogger.WithField("request_id", safeRequestID)
		c.Set(types.LoggerContextKey.String(), requestLogger)

		// Set request ID in the global context for logging
		c.Request = c.Request.WithContext(
			context.WithValue(
				context.WithValue(c.Request.Context(), types.RequestIDContextKey, requestID),
				types.LoggerContextKey, requestLogger,
			),
		)

		c.Next()
	}
}

// Logger middleware logs request details with request ID, input and output
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery
		// The ONLYOFFICE callback path carries a capability ticket: never log
		// it, nor the DS-signed body.
		onlyOfficeCallback := strings.HasPrefix(path, "/onlyoffice/callback/")
		if onlyOfficeCallback {
			path = "/onlyoffice/callback/[REDACTED]"
		}

		isWikiStats := strings.HasPrefix(path, "/api/v1/knowledgebase/") && strings.HasSuffix(path, "/wiki/stats")
		if strings.HasPrefix(path, "/assets/") || isWikiStats {
			c.Next()
			return
		}

		// Browser traffic contains credentials, page content and screenshots.
		// Keep access metadata, but never read or buffer these request/response bodies.
		browserTraffic := strings.HasPrefix(path, "/api/v1/local-browser/") ||
			path == "/api/v1/me/browser" || strings.HasSuffix(path, "/local-browser") ||
			onlyOfficeCallback
		// Đọc request body (trước Next, vì Next sẽ tiêu thụ body)
		var requestBody string
		if !browserTraffic && (c.Request.Method == "POST" || c.Request.Method == "PUT" || c.Request.Method == "PATCH") {
			requestBody = readRequestBody(c)
		}

		// Tạo bộ bắt response body
		responseBody := &bytes.Buffer{}
		responseWriter := &loggerResponseBodyWriter{
			ResponseWriter: c.Writer,
			body:           responseBody,
		}
		if !browserTraffic {
			c.Writer = responseWriter
		}

		// Process request
		c.Next()

		// Get request ID from context
		requestID, exists := c.Get(types.RequestIDContextKey.String())
		requestIDStr := "unknown"
		if exists {
			if idStr, ok := requestID.(string); ok && idStr != "" {
				requestIDStr = idStr
			}
		}
		safeRequestID := secutils.SanitizeForLog(requestIDStr)

		// Calculate latency
		latency := time.Since(start)

		// Get client IP and status code
		clientIP := c.ClientIP()
		statusCode := c.Writer.Status()
		method := c.Request.Method

		if raw != "" {
			path = path + "?" + sanitizeQuery(raw)
		}

		// Đọc response body
		responseBodyStr := ""
		if responseBody.Len() > 0 {
			contentType := c.Writer.Header().Get("Content-Type")
			if strings.Contains(contentType, "text/event-stream") {
				responseBodyStr = "[SSE stream response, skipped]"
			} else if strings.Contains(contentType, "application/json") ||
				strings.Contains(contentType, "text/") {
				bodyBytes := responseBody.Bytes()
				if len(bodyBytes) >= maxBodySize {
					responseBodyStr = string(bodyBytes[:maxBodySize]) + "... [content too long, truncated]"
				} else {
					responseBodyStr = string(bodyBytes)
				}
				responseBodyStr = sanitizeBody(responseBodyStr)
			} else {
				responseBodyStr = "[non-text type, skipped]"
			}
		}

		// Dựng thông điệp log
		logMsg := logger.GetLogger(c)
		logMsg = logMsg.WithFields(map[string]interface{}{
			"request_id":  safeRequestID,
			"method":      method,
			"path":        secutils.SanitizeForLog(path),
			"status_code": statusCode,
			"size":        c.Writer.Size(),
			"latency":     latency.String(),
			"client_ip":   secutils.SanitizeForLog(clientIP),
		})

		// Thêm request body (nếu có), trừ khi handler báo body chứa bí mật
		if c.GetBool(types.LogOmitRequestBodyContextKey.String()) {
			requestBody = ""
		}
		if requestBody != "" {
			logMsg = logMsg.WithField("request_body", secutils.SanitizeForLog(requestBody))
		}

		// Thêm response body (nếu có)
		if responseBodyStr != "" {
			logMsg = logMsg.WithField("response_body", secutils.SanitizeForLog(responseBodyStr))
		}
		if last := c.Errors.Last(); last != nil && last.Err != nil {
			logMsg = logMsg.WithField("error", secutils.SanitizeForLog(last.Err.Error()))
		}
		switch {
		case statusCode >= 500:
			logMsg.Error()
		case statusCode >= 400:
			logMsg.Warn()
		default:
			logMsg.Info()
		}
	}
}
