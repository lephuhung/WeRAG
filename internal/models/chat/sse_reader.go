package chat

import (
	"bufio"
	"io"
	"strings"
)

// SSEEvent biểu diễn một sự kiện Server-Sent Events
type SSEEvent struct {
	Data []byte
	Done bool
}

// SSEReader dùng để đọc luồng SSE
type SSEReader struct {
	scanner *bufio.Scanner
}

// NewSSEReader tạo bộ đọc SSE
func NewSSEReader(reader io.Reader) *SSEReader {
	scanner := bufio.NewScanner(reader)
	// Đặt buffer lớn hơn để xử lý dòng dài (nội dung chuỗi suy luận có thể rất dài)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 1024*1024)
	return &SSEReader{scanner: scanner}
}

// ReadEvent đọc sự kiện SSE tiếp theo
func (r *SSEReader) ReadEvent() (*SSEEvent, error) {
	for r.scanner.Scan() {
		line := r.scanner.Text()

		// Dòng trống, bỏ qua
		if line == "" {
			continue
		}

		// Kiểm tra có phải dấu kết thúc không
		if line == "data: [DONE]" {
			return &SSEEvent{Done: true}, nil
		}

		// Phân tích dòng data
		if strings.HasPrefix(line, "data: ") {
			jsonStr := line[6:]
			return &SSEEvent{Data: []byte(jsonStr)}, nil
		}

		// Tăng tương thích (sau data: không có dấu cách)
		if strings.HasPrefix(line, "data:") {
			jsonStr := line[5:]
			return &SSEEvent{Data: []byte(jsonStr)}, nil
		}

		// Các dòng khác (event:, id:, ...) bỏ qua
	}

	if err := r.scanner.Err(); err != nil {
		return nil, err
	}

	return nil, io.EOF
}
