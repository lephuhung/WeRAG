package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// WeKnoraCloudService quản lý thông tin xác thực WeKnoraCloud
type WeKnoraCloudService interface {
	// SaveCredentials chỉ lưu APPID/APPSECRET vào cấu hình đơn vị, không tự tạo mô hình
	SaveCredentials(ctx context.Context, appID, appSecret string) error
	// CheckStatus kiểm tra thông tin xác thực WeKnoraCloud của đơn vị hiện tại có giải mã được không
	// needsReinit=true nghĩa là trạng thái mã hóa đã hỏng (đổi salt...), người dùng cần nhập lại thông tin xác thực
	CheckStatus(ctx context.Context) (*types.WeKnoraCloudStatusResult, error)
}
