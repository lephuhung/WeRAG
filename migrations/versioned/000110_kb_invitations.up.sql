-- Migration: 000110_kb_invitations
-- Recipient-bound, read-only KB invitations (tenant/KB permission plan).
--
-- A KB invitation grants READ-ONLY access to ONE knowledge base to ONE
-- specific user (recipient_user_id) in another tenant, only after that
-- recipient authenticates and accepts. It never grants access to other
-- members of the recipient's tenant, never grants write, and cannot be
-- re-shared. The bearer token is never stored: only token_hash (SHA-256
-- hex) is persisted; the plaintext is returned once at creation.
--
-- This table coexists with kb_access_grants (tenant-wide legacy grants).
-- No existing grant rows are modified by this migration; narrowing of
-- tenant-wide grants requires an explicit reviewed decision (see plan
-- Migration and Rollout Gate) and is NOT performed here.

DO $$ BEGIN RAISE NOTICE '[Migration 000110] Creating kb_invitations'; END $$;

CREATE TABLE IF NOT EXISTS kb_invitations (
    id                  VARCHAR(36) PRIMARY KEY,
    kb_id               VARCHAR(36) NOT NULL,
    owner_tenant_id     BIGINT      NOT NULL,
    recipient_user_id   VARCHAR(36) NOT NULL,
    recipient_tenant_id BIGINT      NOT NULL,
    inviter_user_id     VARCHAR(36) NOT NULL,
    token_hash          VARCHAR(64) NOT NULL,
    status              VARCHAR(16) NOT NULL DEFAULT 'pending',
    message             VARCHAR(500) NOT NULL DEFAULT '',
    expires_at          TIMESTAMP WITH TIME ZONE,
    accepted_at         TIMESTAMP WITH TIME ZONE,
    created_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at          TIMESTAMP WITH TIME ZONE,
    CONSTRAINT chk_kb_invites_no_self_grant CHECK (owner_tenant_id <> recipient_tenant_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_kb_invites_token_hash
    ON kb_invitations(token_hash) WHERE deleted_at IS NULL;

-- At most one pending invite per (kb, recipient user); accepted rows
-- accumulate for the audit trail (replay-safe: accept is single-use and
-- checks status=pending atomically).
CREATE UNIQUE INDEX IF NOT EXISTS uniq_kb_invite_pending
    ON kb_invitations(kb_id, recipient_user_id)
    WHERE status = 'pending' AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_kb_invites_kb
    ON kb_invitations(kb_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_kb_invites_recipient
    ON kb_invitations(recipient_user_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_kb_invites_owner
    ON kb_invitations(owner_tenant_id) WHERE deleted_at IS NULL;
