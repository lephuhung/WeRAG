package handler

import (
	"strings"
	"testing"
)

// This deployment offers Telegram only: channel creation must accept it and
// refuse every other platform, including ones whose adapter is still compiled in.
func TestValidIMPlatforms_TelegramOnly(t *testing.T) {
	if !validIMPlatforms["telegram"] {
		t.Error("telegram is not accepted")
	}
	for _, platform := range []string{
		"wecom", "feishu", "lark", "slack", "dingtalk", "mattermost", "wechat", "qqbot", "yunzhijia", "nonsense",
	} {
		if validIMPlatforms[platform] {
			t.Errorf("platform %q must not be accepted", platform)
		}
	}
}

// The 400 message is derived from validIMPlatforms; it must not drift as
// platforms are added.
func TestInvalidIMPlatformError_ListsEveryPlatform(t *testing.T) {
	for platform := range validIMPlatforms {
		if !strings.Contains(invalidIMPlatformError, "'"+platform+"'") {
			t.Errorf("error message omits %q: %s", platform, invalidIMPlatformError)
		}
	}
}
