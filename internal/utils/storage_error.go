package utils

import "strings"

// SanitizeStorageConnectivityError converts a raw storage connectivity error
// into a safe, user-facing message. It deliberately avoids echoing the raw
// driver/network error so responses never leak internal hostnames, IPs, ports
// or TLS/certificate details. Callers that need the full error must log it
// server-side instead of returning it to the client.
func SanitizeStorageConnectivityError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "Endpoint url cannot have fully qualified paths"):
		return "Địa chỉ endpoint sai định dạng: bỏ tiền tố http:// hoặc https://, chỉ điền tên miền hoặc IP và cổng (ví dụ: minio.example.com:9000)"
	case strings.Contains(msg, "no such host"):
		return "Phân giải DNS thất bại, hãy kiểm tra lại địa chỉ"
	case strings.Contains(msg, "connection refused"):
		return "Kết nối bị từ chối, hãy kiểm tra dịch vụ đã chạy và cổng đúng"
	case strings.Contains(msg, "no route to host"):
		return "Không định tuyến được tới địa chỉ đích, hãy kiểm tra cấu hình mạng"
	case strings.Contains(msg, "i/o timeout") || strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "context deadline"):
		return "Kết nối quá hạn, hãy kiểm tra mạng hoặc trạng thái dịch vụ"
	case strings.Contains(msg, "403") || strings.Contains(msg, "AccessDenied") || strings.Contains(msg, "access denied"):
		return "Xác thực thất bại, hãy kiểm tra lại thông tin truy cập"
	case strings.Contains(msg, "certificate") || strings.Contains(msg, "tls") || strings.Contains(msg, "x509"):
		return "Lỗi chứng chỉ TLS/SSL, hãy kiểm tra cấu hình SSL"
	case strings.Contains(msg, "404") || strings.Contains(msg, "NoSuchBucket"):
		return "Bucket không tồn tại, hãy kiểm tra tên và Region"
	default:
		return "Kết nối thất bại, hãy kiểm tra lại tham số cấu hình"
	}
}
