package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type memSettingRepo struct {
	interfaces.SystemSettingRepository
	rows map[string]*types.SystemSetting
}

func (r *memSettingRepo) Get(_ context.Context, key string) (*types.SystemSetting, error) {
	if row, ok := r.rows[key]; ok {
		cp := *row
		return &cp, nil
	}
	return nil, nil
}
func (r *memSettingRepo) List(context.Context) ([]*types.SystemSetting, error) {
	out := []*types.SystemSetting{}
	for _, row := range r.rows {
		cp := *row
		out = append(out, &cp)
	}
	return out, nil
}
func (r *memSettingRepo) Upsert(_ context.Context, s *types.SystemSetting) error {
	cp := *s
	r.rows[s.Key] = &cp
	return nil
}

type memAudit struct {
	interfaces.AuditLogService
	entries []*types.AuditLog
}

func (a *memAudit) Log(_ context.Context, e *types.AuditLog) error {
	a.entries = append(a.entries, e)
	return nil
}

func settingString(t *testing.T, row *types.SystemSetting) string {
	t.Helper()
	var v string
	if err := json.Unmarshal(row.Value, &v); err != nil {
		t.Fatalf("value %s: %v", row.Value, err)
	}
	return v
}

const testWebhookSecret = "0123456789abcdef0123456789abcdef9f3c"

// The webhook secret is readable by the server (typed Get*) but every
// API-facing read returns it masked, and the audit log says only that it
// changed.
func TestWebhookSecretSettingIsMaskedOutsideTheServer(t *testing.T) {
	audit := &memAudit{}
	svc := &systemSettingService{
		repo: &memSettingRepo{rows: map[string]*types.SystemSetting{}}, audit: audit,
		cache: map[string]*types.SystemSetting{},
	}
	ctx := context.Background()
	key := types.BillingWebhookSecretSettingKey

	updated, err := svc.Update(ctx, key, testWebhookSecret)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got := settingString(t, updated); got != "••••••••9f3c" || !updated.IsSecret {
		t.Fatalf("Update returned %q (secret=%v), want the masked value", got, updated.IsSecret)
	}
	if got := svc.GetString(ctx, key, types.BillingWebhookSecretEnv, ""); got != testWebhookSecret {
		t.Fatalf("server read %q, want the real secret", got)
	}
	row, err := svc.Get(ctx, key)
	if err != nil || settingString(t, row) != "••••••••9f3c" {
		t.Fatalf("Get returned %v, %v", row, err)
	}
	rows, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, r := range rows {
		if strings.Contains(string(r.Value), testWebhookSecret) {
			t.Fatalf("List leaks the secret in %q", r.Key)
		}
	}
	if len(audit.entries) != 1 || strings.Contains(string(audit.entries[0].Details), testWebhookSecret) {
		t.Fatalf("audit must not carry the secret: %v", audit.entries)
	}
	if !strings.Contains(string(audit.entries[0].Details), `"new_value":"[set]"`) {
		t.Fatalf("audit should record that the secret was set: %s", audit.entries[0].Details)
	}
}

func TestWebhookSecretFallsBackToEnvButStaysMasked(t *testing.T) {
	t.Setenv(types.BillingWebhookSecretEnv, testWebhookSecret)
	svc := &systemSettingService{repo: &memSettingRepo{rows: map[string]*types.SystemSetting{}}, cache: map[string]*types.SystemSetting{}}
	ctx := context.Background()
	key := types.BillingWebhookSecretSettingKey
	if got := svc.GetString(ctx, key, types.BillingWebhookSecretEnv, ""); got != testWebhookSecret {
		t.Fatalf("ENV fallback read %q", got)
	}
	row, err := svc.Get(ctx, key)
	if err != nil || settingString(t, row) != "••••••••9f3c" {
		t.Fatalf("Get of the ENV fallback returned %v, %v", row, err)
	}
}

func TestWebhookSecretValidation(t *testing.T) {
	svc := &systemSettingService{repo: &memSettingRepo{rows: map[string]*types.SystemSetting{}}, cache: map[string]*types.SystemSetting{}}
	ctx := context.Background()
	key := types.BillingWebhookSecretSettingKey
	for _, bad := range []string{"short", "••••••••9f3c", testWebhookSecret + " x"} {
		if _, err := svc.Update(ctx, key, bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	if _, err := svc.Update(ctx, key, ""); err != nil {
		t.Fatalf("empty (webhook off) refused: %v", err)
	}
}

// "" saved without a user in context (a script) still turns the webhook
// off rather than reading as a bootstrap placeholder that falls back to ENV.
func TestWebhookSecretSavedEmptyDoesNotFallBackToEnv(t *testing.T) {
	t.Setenv(types.BillingWebhookSecretEnv, testWebhookSecret)
	svc := &systemSettingService{repo: &memSettingRepo{rows: map[string]*types.SystemSetting{}}, cache: map[string]*types.SystemSetting{}}
	ctx := context.Background()
	key := types.BillingWebhookSecretSettingKey
	if _, err := svc.Update(ctx, key, ""); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got := svc.GetString(ctx, key, types.BillingWebhookSecretEnv, ""); got != "" {
		t.Fatalf("saved \"\" read as %q, want the webhook off", got)
	}
	if !IsSecretSetting(key) || IsSecretSetting("billing.bank_name") {
		t.Fatal("IsSecretSetting must flag only secret keys")
	}
}
