-- Usage packages, phase 2: plans, subscriptions and seats.
--
-- Plans are data, not code: SuperAdmin creates and edits them at runtime,
-- so this migration seeds nothing. A plan's limits are a JSON list of
-- {"metric": "questions"|"tokens", "period": "day"|"month", "limit": N};
-- a subscription may override that list. One active subscription per scope
-- (a workspace or a person). Seats belong to seat-mode subscriptions.

CREATE TABLE IF NOT EXISTS usage_plans (
    id           VARCHAR(36)  PRIMARY KEY,
    code         VARCHAR(64)  NOT NULL UNIQUE,
    name         VARCHAR(128) NOT NULL,
    description  TEXT         NOT NULL DEFAULT '',
    scope_kind   VARCHAR(16)  NOT NULL DEFAULT 'tenant',
    mode         VARCHAR(16)  NOT NULL DEFAULT 'pool',
    limits       JSONB        NOT NULL DEFAULT '[]'::jsonb,
    features     JSONB        NOT NULL DEFAULT '{}'::jsonb,
    price_label  VARCHAR(128) NOT NULL DEFAULT '',
    sort_order   INTEGER      NOT NULL DEFAULT 0,
    is_active    BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS usage_subscriptions (
    id              VARCHAR(36) PRIMARY KEY,
    scope_type      VARCHAR(16) NOT NULL,
    scope_id        VARCHAR(64) NOT NULL,
    plan_id         VARCHAR(36) NOT NULL REFERENCES usage_plans(id),
    mode            VARCHAR(16) NOT NULL DEFAULT 'pool',
    seats           INTEGER     NOT NULL DEFAULT 0,
    limits_override JSONB,
    starts_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    ends_at         TIMESTAMPTZ,
    status          VARCHAR(16) NOT NULL DEFAULT 'active',
    note            TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_subscriptions_active_scope
    ON usage_subscriptions (scope_type, scope_id) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_usage_subscriptions_plan ON usage_subscriptions (plan_id);

CREATE TABLE IF NOT EXISTS usage_seats (
    subscription_id VARCHAR(36) NOT NULL REFERENCES usage_subscriptions(id) ON DELETE CASCADE,
    user_id         VARCHAR(36) NOT NULL,
    assigned_by     VARCHAR(36) NOT NULL DEFAULT '',
    assigned_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (subscription_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_usage_seats_user ON usage_seats (user_id);
