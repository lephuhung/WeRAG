package types

import (
	"strings"
	"testing"
	"time"
)

func TestKBInviteTokenHashNeverStoresRaw(t *testing.T) {
	raw := "kb-invite-plaintext-token-abc123"
	hash := HashKBInviteToken(raw)
	if hash == raw {
		t.Fatal("hash must differ from plaintext")
	}
	if len(hash) != 64 {
		t.Fatalf("expected 64-char hex sha256, got %d", len(hash))
	}
	if !VerifyKBInviteToken(raw, hash) {
		t.Error("correct token must verify")
	}
	if VerifyKBInviteToken("wrong-token", hash) {
		t.Error("wrong token must NOT verify")
	}
	if VerifyKBInviteToken("", hash) {
		t.Error("empty candidate must NOT verify")
	}
	if VerifyKBInviteToken(raw, "") {
		t.Error("empty stored hash must NOT verify")
	}
	// Case-insensitive stored hash still verifies (hex normalization).
	if !VerifyKBInviteToken(raw, strings.ToUpper(hash)) {
		t.Error("uppercase hex hash should also verify")
	}
}

func TestKBInviteIsLive(t *testing.T) {
	now := time.Now()
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)
	accepted := &KBInvitation{Status: KBInvitationStatusAccepted}
	if !accepted.IsLive(now) {
		t.Error("accepted invite without expiry must be live")
	}
	accepted.ExpiresAt = &future
	if !accepted.IsLive(now) {
		t.Error("accepted invite with future expiry must be live")
	}
	accepted.ExpiresAt = &past
	if accepted.IsLive(now) {
		t.Error("accepted invite past expiry must NOT be live")
	}
	pending := &KBInvitation{Status: KBInvitationStatusPending}
	if pending.IsLive(now) {
		t.Error("pending invite must NOT be live")
	}
	revoked := &KBInvitation{Status: KBInvitationStatusRevoked}
	if revoked.IsLive(now) {
		t.Error("revoked invite must NOT be live")
	}
}

func TestKBInvitationStatusTerminal(t *testing.T) {
	if KBInvitationStatusPending.IsTerminal() {
		t.Error("pending must not be terminal")
	}
	for _, s := range []KBInvitationStatus{
		KBInvitationStatusAccepted, KBInvitationStatusRevoked, KBInvitationStatusExpired,
	} {
		if !s.IsTerminal() {
			t.Errorf("%q must be terminal", s)
		}
	}
}
