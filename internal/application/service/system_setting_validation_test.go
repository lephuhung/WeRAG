package service

import "testing"

func TestValidateWorkerConcurrencyMinimums(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   any
		wantErr bool
	}{
		{name: "core zero", key: "asynq.core_concurrency", value: 0, wantErr: true},
		{name: "core minimum", key: "asynq.core_concurrency", value: 1},
		{name: "postprocess minimum", key: "asynq.postprocess_concurrency", value: 1},
		{name: "enrichment minimum", key: "asynq.enrichment_concurrency", value: 1},
		{name: "maintenance minimum", key: "asynq.maintenance_concurrency", value: 1},
		{name: "shared minimum", key: "asynq.shared_concurrency", value: 1},
		{name: "wiki zero", key: "asynq.wiki_concurrency", value: 0, wantErr: true},
		{name: "wiki minimum", key: "asynq.wiki_concurrency", value: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRegistryEntry(tt.key, tt.value)
			if tt.wantErr && err == nil {
				t.Fatal("expected validation error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestValidateParseDefaults(t *testing.T) {
	tests := []struct {
		name    string
		value   any
		wantErr bool
	}{
		{name: "enabled with overrides", value: map[string]any{
			"enabled":         true,
			"summary_enabled": true,
			"chunking_config": map[string]any{"chunk_size": 1024},
			"vlm_config":      map[string]any{"enabled": true, "model_id": "vlm-1"},
		}},
		{name: "disabled empty", value: map[string]any{"enabled": false}},
		{name: "unknown field rejected", value: map[string]any{
			"enabled":      true,
			"chunking_siz": 512, // typo must surface, not be silently dropped
		}, wantErr: true},
		{name: "enabled wrong type", value: map[string]any{"enabled": "yes"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRegistryEntry(SystemParseDefaultsKey, tt.value)
			if tt.wantErr && err == nil {
				t.Fatal("expected validation error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestValidateUITheme(t *testing.T) {
	spec, ok := registry[SystemUIThemeKey]
	if !ok {
		t.Fatal("ui.theme must be registered")
	}
	if spec.Type != "string" {
		t.Fatalf("ui.theme type = %q, want string", spec.Type)
	}
	if spec.Default != "elevenlabs" {
		t.Fatalf("ui.theme default = %v, want elevenlabs", spec.Default)
	}
	want := []string{"elevenlabs", "anthropic"}
	if len(spec.Enum) != len(want) || spec.Enum[0] != want[0] || spec.Enum[1] != want[1] {
		t.Fatalf("ui.theme enum = %v, want %v", spec.Enum, want)
	}
	// Update() enforces spec.Type via encodeForType before the enum check;
	// assert both legal values encode and a non-string is rejected.
	for _, v := range want {
		if _, err := encodeForType(spec.Type, v); err != nil {
			t.Fatalf("encodeForType(%q) = %v, want nil", v, err)
		}
	}
	if _, err := encodeForType(spec.Type, 42); err == nil {
		t.Fatal("encodeForType must reject a non-string value")
	}
}
