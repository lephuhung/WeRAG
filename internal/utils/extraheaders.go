package utils

import (
	"net/http"
	"strings"
)

// reservedHeaderKeys liệt kê các header quan trọng không cho phép header tùy chỉnh của người dùng ghi đè.
// Các header này do luồng ký, xác thực hoặc SSE của từng provider kiểm soát; ghi đè có thể làm lời gọi thất bại ngay.
var reservedHeaderKeys = map[string]struct{}{
	"authorization":     {},
	"api-key":           {},
	"x-api-key":         {},
	"x-goog-api-key":    {},
	"content-type":      {},
	"content-length":    {},
	"accept-encoding":   {},
	"host":              {},
	"connection":        {},
	"transfer-encoding": {},
}

// IsReservedHeader cho biết một header key có phải header dành riêng hay không; header dành riêng không được ghi đè.
func IsReservedHeader(key string) bool {
	_, ok := reservedHeaderKeys[strings.ToLower(strings.TrimSpace(key))]
	return ok
}

// ApplyCustomHeaders ghi các header tùy chỉnh của người dùng vào http.Request.
// Header dành riêng (Authorization, api-key, Content-Type...) bị bỏ qua để không phá xác thực/chữ ký.
// Các header khác ghi đè mục cùng tên, cho phép người dùng thay giá trị mặc định (ví dụ Accept).
func ApplyCustomHeaders(req *http.Request, headers map[string]string) {
	if req == nil || len(headers) == 0 {
		return
	}
	for k, v := range headers {
		name := strings.TrimSpace(k)
		if name == "" {
			continue
		}
		if IsReservedHeader(name) {
			continue
		}
		req.Header.Set(name, v)
	}
}

// CustomHeadersRoundTripper là một lớp bọc http.RoundTripper,
// chèn header tùy chỉnh của người dùng trước khi gửi mỗi yêu cầu HTTP.
// Dùng khi không lấy được *http.Request bên dưới (ví dụ go-openai SDK).
type CustomHeadersRoundTripper struct {
	Headers map[string]string
	Base    http.RoundTripper
}

// RoundTrip hiện thực giao diện http.RoundTripper.
func (t *CustomHeadersRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	if len(t.Headers) > 0 {
		// Sao chép yêu cầu để không sửa đối tượng bên gọi truyền vào.
		cloned := req.Clone(req.Context())
		ApplyCustomHeaders(cloned, t.Headers)
		return base.RoundTrip(cloned)
	}
	return base.RoundTrip(req)
}

// WrapHTTPClientWithHeaders trả về *http.Client mới, chèn header tùy chỉnh dựa trên client gốc.
// Nếu headers rỗng thì trả lại client gốc để tránh chi phí thừa.
func WrapHTTPClientWithHeaders(client *http.Client, headers map[string]string) *http.Client {
	if len(headers) == 0 {
		return client
	}
	if client == nil {
		client = NewSSRFSafeHTTPClient(DefaultSSRFSafeHTTPClientConfig())
	}
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	wrapped := *client
	wrapped.Transport = &CustomHeadersRoundTripper{Headers: headers, Base: base}
	return &wrapped
}
