package config

import (
	"testing"
	"time"
)

func TestApplyOnlyOfficeEnvOverrides(t *testing.T) {
	for _, k := range []string{"ONLYOFFICE_JWT_SECRET", "ONLYOFFICE_PUBLIC_URL", "ONLYOFFICE_INTERNAL_URL",
		"ONLYOFFICE_BACKEND_URL", "ONLYOFFICE_SAVE_WAIT_SECONDS", "APP_EXTERNAL_URL"} {
		t.Setenv(k, "")
	}
	cfg := &Config{}
	applyOnlyOfficeEnvOverrides(cfg)
	if cfg.OnlyOffice == nil || cfg.OnlyOffice.Enabled() {
		t.Fatal("section must exist and be disabled without env")
	}

	t.Setenv("ONLYOFFICE_JWT_SECRET", "s3cret")
	t.Setenv("ONLYOFFICE_PUBLIC_URL", "https://docs.example/")
	t.Setenv("APP_EXTERNAL_URL", "https://app.example/")
	cfg = &Config{}
	applyOnlyOfficeEnvOverrides(cfg)
	oo := cfg.OnlyOffice
	if !oo.Enabled() {
		t.Fatal("public URL + secret should enable the editor")
	}
	if oo.PublicURL != "https://docs.example" || oo.InternalURL != "https://docs.example" {
		t.Fatalf("internal URL should default to the public URL: %+v", oo)
	}
	if oo.BackendURL != "https://app.example" {
		t.Fatalf("backend URL should default to APP_EXTERNAL_URL: %q", oo.BackendURL)
	}
	if oo.SaveWait() != 20*time.Second {
		t.Fatalf("save wait default = %s", oo.SaveWait())
	}

	t.Setenv("ONLYOFFICE_INTERNAL_URL", "http://documentserver")
	t.Setenv("ONLYOFFICE_BACKEND_URL", "http://app:8080")
	t.Setenv("ONLYOFFICE_SAVE_WAIT_SECONDS", "7")
	cfg = &Config{}
	applyOnlyOfficeEnvOverrides(cfg)
	if cfg.OnlyOffice.InternalURL != "http://documentserver" || cfg.OnlyOffice.BackendURL != "http://app:8080" ||
		cfg.OnlyOffice.SaveWait() != 7*time.Second {
		t.Fatalf("explicit env not applied: %+v", cfg.OnlyOffice)
	}
}
