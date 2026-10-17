package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type fontSizesSettings struct {
	interfaces.SystemSettingService
	raw string
}

func (s *fontSizesSettings) GetJSON(_ context.Context, key string) (types.JSON, bool) {
	if key != types.DocumentTemplateFontSizesSettingKey || s.raw == "" {
		return nil, false
	}
	return types.JSON(s.raw), true
}

type fakeTemplateStyles struct {
	rows map[uint64]*types.TenantTemplateStyle
	err  error
}

func (f *fakeTemplateStyles) Get(_ context.Context, tenantID uint64) (*types.TenantTemplateStyle, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rows[tenantID], nil
}

func (f *fakeTemplateStyles) Save(_ context.Context, s *types.TenantTemplateStyle) error {
	f.rows[s.TenantID] = s
	return nil
}

// A ready template is built with the NĐ30 default, the platform's sizes and
// the workspace's on top; a broken workspace row never fails the template.
func TestTemplateFontSizesLevels(t *testing.T) {
	ctx := context.Background()
	styles := &fakeTemplateStyles{rows: map[uint64]*types.TenantTemplateStyle{
		7: {TenantID: 7, FontSizes: types.JSON(`{"quoc_hieu":13,"le_trai":35}`)},
		8: {TenantID: 8, FontSizes: types.JSON(`{"quoc_hieu":99}`)},
	}}
	s := &sessionService{systemSettings: &fontSizesSettings{raw: `{"quoc_hieu":12,"tieu_ngu":13}`}}

	// no repository wired: platform over default
	got := s.templateFontSizes(ctx, 7)
	if got["quoc_hieu"] != 12 || got["tieu_ngu"] != 13 || got["noi_dung"] != 14 {
		t.Fatalf("without workspace sizes: %v", got)
	}

	s.SetTemplateStyles(styles)
	got = s.templateFontSizes(ctx, 7)
	if got["quoc_hieu"] != 13 || got["le_trai"] != 35 || got["tieu_ngu"] != 13 {
		t.Fatalf("workspace override: %v", got)
	}
	// a size outside the range (range narrowed later) follows the platform
	if got := s.templateFontSizes(ctx, 8); got["quoc_hieu"] != 12 {
		t.Fatalf("invalid workspace row: %v", got)
	}
	// no row
	if got := s.templateFontSizes(ctx, 9); got["quoc_hieu"] != 12 {
		t.Fatalf("no workspace row: %v", got)
	}
	styles.err = errors.New("db down")
	if got := s.templateFontSizes(ctx, 7); got["quoc_hieu"] != 12 || len(got) != len(types.TemplateFontSizeSpecs) {
		t.Fatalf("repository error: %v", got)
	}
	// unreadable platform document: NĐ30 defaults
	s.systemSettings = &fontSizesSettings{raw: `{"quoc_hieu":"x"}`}
	styles.err = nil
	if got := s.templateFontSizes(ctx, 9); got["quoc_hieu"] != 13 || got["tieu_ngu"] != 14 {
		t.Fatalf("unreadable platform sizes: %v", got)
	}
}

func TestValidateTemplateFontSizesSetting(t *testing.T) {
	key := types.DocumentTemplateFontSizesSettingKey
	for _, ok := range []any{
		map[string]any{},
		map[string]any{"quoc_hieu": 12, "le_phai": 15},
	} {
		if err := validateRegistryEntry(key, ok); err != nil {
			t.Errorf("%v: unexpected error %v", ok, err)
		}
	}
	for _, bad := range []any{
		map[string]any{"quoc_hieu": 14},
		map[string]any{"so_ky_hieu": 13},
		map[string]any{"le_tren": "20"},
		"13",
	} {
		if err := validateRegistryEntry(key, bad); err == nil {
			t.Errorf("%v: want a validation error", bad)
		}
	}
}
