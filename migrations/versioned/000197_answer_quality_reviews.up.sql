-- Migration: 000197_answer_quality_reviews
-- Answer-quality inbox for admins (Tenant Admin: own organization,
-- SuperAdmin: all): disliked answers (messages.feedback = -1) and
-- questions a gate answered without evidence (messages.execution_context
-- -> 'ungrounded'). An admin's cause tag and "reviewed" mark are kept here,
-- one row per (message, list); the user's message is never changed.
-- tenant_id is the organization of the message's session.
DO $$ BEGIN RAISE NOTICE '[Migration 000197] Creating answer_quality_reviews'; END $$;

CREATE TABLE IF NOT EXISTS answer_quality_reviews (
    message_id   VARCHAR(36)  NOT NULL,
    kind         VARCHAR(16)  NOT NULL,
    tenant_id    BIGINT       NOT NULL DEFAULT 0,
    cause        VARCHAR(32)  NOT NULL DEFAULT '',
    note         TEXT         NOT NULL DEFAULT '',
    reviewed     BOOLEAN      NOT NULL DEFAULT FALSE,
    reviewed_by  VARCHAR(36)  NOT NULL DEFAULT '',
    reviewed_at  TIMESTAMP WITH TIME ZONE NULL,
    updated_by   VARCHAR(36)  NOT NULL DEFAULT '',
    updated_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (message_id, kind),
    CONSTRAINT chk_answer_quality_reviews_kind CHECK (kind IN ('disliked', 'ungrounded'))
);

CREATE INDEX IF NOT EXISTS idx_answer_quality_reviews_tenant
    ON answer_quality_reviews (tenant_id, kind);

-- The two lists read only a sliver of messages, newest first.
-- (the disliked list filters and orders by COALESCE(feedback_at, created_at))
CREATE INDEX IF NOT EXISTS idx_messages_feedback_down
    ON messages ((COALESCE(feedback_at, created_at)) DESC)
    WHERE feedback = -1 AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_messages_ungrounded
    ON messages (created_at DESC)
    WHERE (execution_context -> 'ungrounded') IS NOT NULL AND deleted_at IS NULL;
