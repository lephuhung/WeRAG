package im

import (
	"testing"

	"gorm.io/gorm"
)

func TestValidateSessionMode(t *testing.T) {
	tests := []struct {
		name        string
		sessionMode string
		wantErr     bool
	}{
		{"user mode", "user", false},
		{"thread mode", "thread", false},
		{"empty defaults to user in BeforeCreate", "", false},
		{"invalid mode", "invalid", true},
		{"random string", "foo", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := &IMChannel{SessionMode: tt.sessionMode}
			err := ch.validateSessionMode()

			if tt.sessionMode == "" {
				// empty is handled by BeforeCreate, not validateSessionMode
				// validateSessionMode treats empty as invalid
				if err == nil {
					t.Error("expected error for empty session_mode in validateSessionMode")
				}
				return
			}

			if (err != nil) != tt.wantErr {
				t.Errorf("validateSessionMode(%q) error = %v, wantErr %v", tt.sessionMode, err, tt.wantErr)
			}
		})
	}
}

func TestIMChannelBeforeCreate_SessionModeDefault(t *testing.T) {
	tests := []struct {
		name         string
		inputMode    string
		expectedMode string
	}{
		{"empty defaults to user", "", "user"},
		{"user preserved", "user", "user"},
		{"thread preserved", "thread", "thread"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := &IMChannel{
				TenantID:    1,
				AgentID:     "agent-1",
				Platform:    "telegram",
				SessionMode: tt.inputMode,
				Credentials: []byte("{}"),
			}
			err := ch.BeforeCreate(&gorm.DB{})
			if err != nil {
				t.Fatalf("BeforeCreate error: %v", err)
			}
			if ch.SessionMode != tt.expectedMode {
				t.Errorf("SessionMode = %q, want %q", ch.SessionMode, tt.expectedMode)
			}
		})
	}
}

func TestIMChannelBeforeCreate_InvalidSessionMode(t *testing.T) {
	ch := &IMChannel{
		TenantID:    1,
		AgentID:     "agent-1",
		Platform:    "telegram",
		SessionMode: "invalid",
		Credentials: []byte("{}"),
	}
	err := ch.BeforeCreate(&gorm.DB{})
	if err == nil {
		t.Error("expected error for invalid session_mode")
	}
}

func TestIMChannelBeforeSave_SessionModeValidation(t *testing.T) {
	tests := []struct {
		name        string
		sessionMode string
		wantMode    string
		wantErr     bool
	}{
		{"empty defaults to user", "", "user", false},
		{"user preserved", "user", "user", false},
		{"thread preserved", "thread", "thread", false},
		{"invalid rejected", "invalid", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := &IMChannel{
				SessionMode: tt.sessionMode,
				Credentials: []byte("{}"),
			}
			err := ch.BeforeSave(&gorm.DB{})
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ch.SessionMode != tt.wantMode {
				t.Errorf("SessionMode = %q, want %q", ch.SessionMode, tt.wantMode)
			}
		})
	}
}

func TestSessionModeConstants(t *testing.T) {
	if SessionModeUser != "user" {
		t.Errorf("SessionModeUser = %q, want %q", SessionModeUser, "user")
	}
	if SessionModeThread != "thread" {
		t.Errorf("SessionModeThread = %q, want %q", SessionModeThread, "thread")
	}
}

func TestComputeBotIdentity_Telegram(t *testing.T) {
	ch := &IMChannel{Platform: "telegram", Credentials: []byte(`{"bot_token":"123456:ABC-secret"}`)}
	if got := ch.computeBotIdentity(); got != "telegram:123456" {
		t.Errorf("identity = %q, want %q", got, "telegram:123456")
	}
	ch = &IMChannel{Platform: "telegram", Credentials: []byte(`{}`)}
	if got := ch.computeBotIdentity(); got != "" {
		t.Errorf("identity = %q, want empty when bot_token is missing", got)
	}
}

// A row left over from a removed platform derives no identity.
func TestComputeBotIdentity_UnsupportedPlatform(t *testing.T) {
	ch := &IMChannel{Platform: "feishu", Credentials: []byte(`{"app_id":"cli_a1b2c3"}`)}
	if got := ch.computeBotIdentity(); got != "" {
		t.Errorf("identity = %q, want empty", got)
	}
}

func TestChannelSessionThreadIDField(t *testing.T) {
	cs := ChannelSession{
		Platform: "telegram",
		UserID:   "U123",
		ChatID:   "C456",
		ThreadID: "1234567890.123456",
	}
	if cs.ThreadID != "1234567890.123456" {
		t.Errorf("ThreadID = %q, want %q", cs.ThreadID, "1234567890.123456")
	}

	// empty ThreadID for user-mode sessions
	csUser := ChannelSession{
		Platform: "telegram",
		UserID:   "U123",
		ChatID:   "C456",
	}
	if csUser.ThreadID != "" {
		t.Errorf("ThreadID = %q, want empty", csUser.ThreadID)
	}
}
