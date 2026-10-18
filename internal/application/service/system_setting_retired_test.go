package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type retiredSettingRepo struct {
	interfaces.SystemSettingRepository
	rows []*types.SystemSetting
}

func (r *retiredSettingRepo) List(context.Context) ([]*types.SystemSetting, error) {
	return r.rows, nil
}

// A leftover auth.default_tenant_mode row (the removed personal-organization
// switch) must not show up in the settings UI as if it still did something,
// while a genuinely unknown row stays visible for diagnostics.
func TestSystemSettingListHidesRetiredKeys(t *testing.T) {
	repo := &retiredSettingRepo{rows: []*types.SystemSetting{
		{Key: "auth.default_tenant_mode", Value: types.JSON(`"create_personal"`)},
		{Key: "asynq.concurrency", Value: types.JSON(`8`)},
		{Key: "zz.unknown", Value: types.JSON(`1`)},
	}}
	svc := &systemSettingService{repo: repo, cache: map[string]*types.SystemSetting{}}

	rows, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	seenUnknown := false
	for _, row := range rows {
		switch row.Key {
		case "auth.default_tenant_mode", "asynq.concurrency":
			t.Fatalf("retired key %q listed", row.Key)
		case "zz.unknown":
			seenUnknown = true
		}
	}
	if !seenUnknown {
		t.Fatal("unknown row should stay visible")
	}
	if _, ok := registry["auth.default_tenant_mode"]; ok {
		t.Fatal("auth.default_tenant_mode must not be a registered setting")
	}
}
