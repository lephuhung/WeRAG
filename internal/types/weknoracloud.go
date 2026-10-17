package types

// WeKnoraCloudStatusResult là kết quả kiểm tra trạng thái
type WeKnoraCloudStatusResult struct {
	HasModels   bool   `json:"has_models"`       // Đã cấu hình thông tin xác thực WeKnoraCloud hay chưa
	NeedsReinit bool   `json:"needs_reinit"`     // Có cần khởi tạo lại không (thông tin xác thực bị hỏng)
	Reason      string `json:"reason,omitempty"` // Lý do cần khởi tạo lại
}
