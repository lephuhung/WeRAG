// Package runtime cung cấp container dependency injection cho runtime của ứng dụng
// Gói này dùng thư viện dig của uber để quản lý dependency injection
package runtime

import (
	"go.uber.org/dig"
)

// container là container dependency injection toàn cục của ứng dụng
// Mọi service và thành phần đều đăng ký và resolve qua nó
var container *dig.Container

// init khởi tạo container dependency injection
// Tự động gọi khi chương trình khởi động
func init() {
	container = dig.New()
}

// GetContainer trả về tham chiếu tới container dependency injection toàn cục
// Để các gói khác đăng ký hoặc lấy service
func GetContainer() *dig.Container {
	return container
}
