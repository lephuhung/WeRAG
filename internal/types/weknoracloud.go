package types

// WeKnoraCloudStatusResult 状态检查结果
type WeKnoraCloudStatusResult struct {
	HasModels   bool   `json:"has_models"`       // 是否已Configuration  WeKnoraCloud 凭证
	NeedsReinit bool   `json:"needs_reinit"`     // 是否需要重新Initialization （凭证损坏）
	Reason      string `json:"reason,omitempty"` // 需要重新Initialization 的原因
}
