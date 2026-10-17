package types

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// DocumentTemplateFontSizesSettingKey is the system setting holding the
// platform-wide font sizes of the NĐ30 templates drafted from chat; a
// workspace may override them (TenantTemplateStyle).
const DocumentTemplateFontSizesSettingKey = "document_template.font_sizes"

// TemplateFontSizeSpec is one setting of a template that can be chosen, with
// the range NĐ 30/2020/NĐ-CP Phụ lục I allows: a component's font size (pt)
// or a page margin (mm). Both live in the same font_sizes document.
type TemplateFontSizeSpec struct {
	Key     string `json:"key"`
	Unit    string `json:"unit"`
	Min     int    `json:"min"`
	Max     int    `json:"max"`
	Default int    `json:"default"`
}

// TemplateFontSizeSpecs lists the configurable components in page order,
// then the page margins. Số ký hiệu (13) and Nơi nhận (12/11) have a single
// NĐ30 size and stay fixed in the builders.
var TemplateFontSizeSpecs = []TemplateFontSizeSpec{
	{Key: "co_quan_chu_quan", Unit: "pt", Min: 12, Max: 13, Default: 13},
	{Key: "co_quan_ban_hanh", Unit: "pt", Min: 12, Max: 13, Default: 13},
	{Key: "quoc_hieu", Unit: "pt", Min: 12, Max: 13, Default: 13},
	{Key: "tieu_ngu", Unit: "pt", Min: 13, Max: 14, Default: 14},
	{Key: "trich_yeu_cong_van", Unit: "pt", Min: 12, Max: 13, Default: 12},
	{Key: "dia_danh_ngay_thang", Unit: "pt", Min: 13, Max: 14, Default: 13},
	{Key: "ten_loai", Unit: "pt", Min: 13, Max: 14, Default: 14},
	{Key: "trich_yeu", Unit: "pt", Min: 13, Max: 14, Default: 14},
	{Key: "tham_quyen_ban_hanh", Unit: "pt", Min: 13, Max: 14, Default: 13},
	{Key: "noi_dung", Unit: "pt", Min: 13, Max: 14, Default: 14},
	{Key: "chuc_vu_nguoi_ky", Unit: "pt", Min: 13, Max: 14, Default: 13},
	{Key: "ho_ten_nguoi_ky", Unit: "pt", Min: 13, Max: 14, Default: 14},
	{Key: "so_trang", Unit: "pt", Min: 13, Max: 14, Default: 13},
	{Key: "le_tren", Unit: "mm", Min: 20, Max: 25, Default: 20},
	{Key: "le_duoi", Unit: "mm", Min: 20, Max: 25, Default: 20},
	{Key: "le_trai", Unit: "mm", Min: 30, Max: 35, Default: 30},
	{Key: "le_phai", Unit: "mm", Min: 15, Max: 20, Default: 20},
}

// TemplateFontSizes maps a component key to its size in points. A key left
// out takes the next level's size (workspace → platform → NĐ30 default).
type TemplateFontSizes map[string]int

// ErrTemplateFontSizesInvalid wraps every validation failure.
var ErrTemplateFontSizesInvalid = errors.New("invalid template font sizes")

func templateFontSizeSpec(key string) (TemplateFontSizeSpec, bool) {
	for _, s := range TemplateFontSizeSpecs {
		if s.Key == key {
			return s, true
		}
	}
	return TemplateFontSizeSpec{}, false
}

// Validate rejects unknown components and sizes outside the NĐ30 range.
func (f TemplateFontSizes) Validate() error {
	for k, v := range f {
		s, ok := templateFontSizeSpec(k)
		if !ok {
			return fmt.Errorf("%w: unknown component %q", ErrTemplateFontSizesInvalid, k)
		}
		if v < s.Min || v > s.Max {
			return fmt.Errorf("%w: %s must be %d–%d %s", ErrTemplateFontSizesInvalid, k, s.Min, s.Max, s.Unit)
		}
	}
	return nil
}

// ParseTemplateFontSizes strictly decodes and validates a stored document;
// empty input is an empty set.
func ParseTemplateFontSizes(raw []byte) (TemplateFontSizes, error) {
	f := TemplateFontSizes{}
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return f, nil
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTemplateFontSizesInvalid, err)
	}
	return f, f.Validate()
}

// PlatformTemplateFontSizes returns the SuperAdmin's saved sizes, empty when
// none are saved or the stored document is unreadable (writes are validated).
func PlatformTemplateFontSizes(ctx context.Context, settings interface {
	GetJSON(ctx context.Context, key string) (JSON, bool)
},
) TemplateFontSizes {
	if settings == nil {
		return TemplateFontSizes{}
	}
	raw, found := settings.GetJSON(ctx, DocumentTemplateFontSizesSettingKey)
	if !found {
		return TemplateFontSizes{}
	}
	f, err := ParseTemplateFontSizes(raw)
	if err != nil {
		return TemplateFontSizes{}
	}
	return f
}

// ResolveTemplateFontSizes gives every component its size: the NĐ30
// default, overridden by the platform's, overridden by the workspace's.
func ResolveTemplateFontSizes(platform, tenant TemplateFontSizes) TemplateFontSizes {
	out := make(TemplateFontSizes, len(TemplateFontSizeSpecs))
	for _, s := range TemplateFontSizeSpecs {
		out[s.Key] = s.Default
	}
	for _, level := range []TemplateFontSizes{platform, tenant} {
		for k, v := range level {
			if _, ok := out[k]; ok {
				out[k] = v
			}
		}
	}
	return out
}

// TenantTemplateStyle is a workspace's override of the template font sizes.
type TenantTemplateStyle struct {
	TenantID  uint64    `json:"tenant_id" gorm:"primaryKey"`
	FontSizes JSON      `json:"font_sizes" gorm:"type:jsonb"`
	UpdatedBy string    `json:"updated_by"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName names the table of TenantTemplateStyle.
func (TenantTemplateStyle) TableName() string { return "tenant_document_template_styles" }
