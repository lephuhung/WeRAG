package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

// The SMTP password is a Secret shown without any of its characters, and
// the masked value the page shows cannot be saved back over it.
func TestSMTPPasswordSettingIsFullyMasked(t *testing.T) {
	audit := &memAudit{}
	svc := &systemSettingService{
		repo: &memSettingRepo{rows: map[string]*types.SystemSetting{}}, audit: audit,
		cache: map[string]*types.SystemSetting{},
	}
	ctx := context.Background()
	key := types.SMTPPasswordSettingKey
	row, err := svc.Update(ctx, key, "app-password-1234")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := settingString(t, row); got != "••••••••" {
		t.Fatalf("masked = %q, want no tail", got)
	}
	if !row.IsSecret {
		t.Fatal("row must be marked secret")
	}
	if got := svc.GetString(ctx, key, "", ""); got != "app-password-1234" {
		t.Fatalf("server read = %q", got)
	}
	listed, err := svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range listed {
		if strings.Contains(string(r.Value), "1234") {
			t.Fatalf("list leaks the password: %s = %s", r.Key, r.Value)
		}
	}
	for _, e := range audit.entries {
		if strings.Contains(string(e.Details), "app-password") {
			t.Fatalf("audit leaks the password: %s", e.Details)
		}
	}
	if _, err := svc.Update(ctx, key, "••••••••"); err == nil {
		t.Fatal("saving the masked value back must be refused")
	}
	if got := svc.GetString(ctx, key, "", ""); got != "app-password-1234" {
		t.Fatalf("password changed to %q", got)
	}
}

func TestSMTPSettingsValidation(t *testing.T) {
	for _, tc := range []struct {
		key string
		val any
		ok  bool
	}{
		{types.SMTPPortSettingKey, float64(465), true},
		{types.SMTPPortSettingKey, float64(0), false},
		{types.SMTPPortSettingKey, float64(70000), false},
		{types.SMTPHostSettingKey, "smtp.gmail.com", true},
		{types.SMTPHostSettingKey, "smtp://x", false},
		{types.SMTPFromAddressSettingKey, "no-reply@example.com", true},
		{types.SMTPFromAddressSettingKey, "WeRAG <no-reply@example.com>", false},
		{types.SMTPFromNameSettingKey, "WeRAG\r\nBcc: x@y.z", false},
	} {
		err := validateRegistryEntry(tc.key, tc.val)
		if (err == nil) != tc.ok {
			t.Errorf("%s=%v: err=%v, want ok=%v", tc.key, tc.val, err, tc.ok)
		}
	}
}
