-- SQLite counterpart of versioned 000118 (knowledges.created_by) and the
-- kb_subscriptions table of 000119.
ALTER TABLE knowledges ADD COLUMN created_by VARCHAR(36) NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_knowledges_created_by ON knowledges (created_by);

CREATE TABLE IF NOT EXISTS kb_subscriptions (
    id          VARCHAR(36) PRIMARY KEY,
    kb_id       VARCHAR(36) NOT NULL,
    tenant_id   INTEGER     NOT NULL,
    user_id     VARCHAR(36) NOT NULL DEFAULT '',
    created_by  VARCHAR(36) NOT NULL DEFAULT '',
    created_at  DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS uniq_kb_subscriptions_subject
    ON kb_subscriptions(kb_id, tenant_id, user_id);
CREATE INDEX IF NOT EXISTS idx_kb_subscriptions_reader
    ON kb_subscriptions(tenant_id, user_id);
