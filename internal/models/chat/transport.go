package chat

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	secutils "github.com/Tencent/WeKnora/internal/utils"
)

// Cấu hình timeout gọi LLM. Chỉ là dự phòng khi "tầng trên chưa đặt deadline", tránh yêu cầu treo
// chặn worker mãi. Nếu ctx tầng trên đã đặt deadline (dù ngắn hơn hay dài hơn mặc định)
// thì tôn trọng nguyên vẹn, không chồng thêm timeout mặc định. Có thể ghi đè qua biến môi trường:
//   - WEKNORA_LLM_CHAT_TIMEOUT_SECONDS    timeout dự phòng cho gọi không stream (mặc định 600s)
//   - WEKNORA_LLM_STREAM_TIMEOUT_SECONDS  timeout dự phòng cho gọi stream (mặc định 1800s)
var (
	defaultChatTimeout   = envDurationSeconds("WEKNORA_LLM_CHAT_TIMEOUT_SECONDS", 300*time.Second)
	defaultStreamTimeout = envDurationSeconds("WEKNORA_LLM_STREAM_TIMEOUT_SECONDS", 600*time.Second)
)

// envDurationSeconds đọc biến môi trường tính bằng giây; phân tích lỗi hoặc không dương thì dùng fallback.
func envDurationSeconds(key string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return time.Duration(n) * time.Second
}

// withLLMTimeout chỉ thêm timeout dự phòng khi ctx tầng trên chưa có deadline;
// nếu tầng trên đã đặt deadline rõ ràng (dù ngắn hơn hay dài hơn) thì trả về nguyên vẹn,
// để bên gọi có quyền quyết định cuối cùng về chính sách timeout của mình.
func withLLMTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}

// rawHTTPClient is a shared HTTP client for raw HTTP LLM calls with connection-level timeouts.
// Per-request timeout is enforced via context deadline (see defaultChatTimeout / defaultStreamTimeout)
// rather than http.Client.Timeout, so streaming calls are not prematurely terminated.
// Uses SSRFSafeDialContext to prevent DNS rebinding attacks at the connection layer.
var rawHTTPTransport = &http.Transport{
	Proxy:               http.ProxyFromEnvironment,
	DialContext:         secutils.SSRFSafeDialContext,
	TLSHandshakeTimeout: 10 * time.Second,
	IdleConnTimeout:     90 * time.Second,
	MaxIdleConnsPerHost: 5,
}

var rawHTTPClient = secutils.NewSSRFSafeHTTPClientWithTransport(
	secutils.SSRFSafeHTTPClientConfig{Timeout: 0, MaxRedirects: 10},
	rawHTTPTransport,
)
