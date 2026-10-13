-- Migration: 000138_usage_billing
-- Self-service purchase of usage packages: a plan carries a price in VND
-- per month (per seat per month in seat mode); promo codes are SuperAdmin
-- data (percent or fixed amount off, optional plan restriction, validity
-- window, use cap); an order is a quote frozen at creation time that the
-- buyer pays by bank transfer (VietQR, transfer content = order code) and
-- that SuperAdmin confirms, which creates or extends the subscription.
-- Nothing is seeded.

ALTER TABLE usage_plans ADD COLUMN IF NOT EXISTS price_vnd BIGINT NOT NULL DEFAULT 0;
-- Plans created before this release typed the price as digits in price_label.
UPDATE usage_plans SET price_vnd = price_label::bigint
WHERE price_vnd = 0 AND price_label ~ '^[0-9]{1,15}$';

CREATE TABLE IF NOT EXISTS usage_promo_codes (
    id             VARCHAR(36)  PRIMARY KEY,
    code           VARCHAR(32)  NOT NULL UNIQUE,
    name           VARCHAR(128) NOT NULL DEFAULT '',
    discount_type  VARCHAR(16)  NOT NULL DEFAULT 'percent',
    discount_value BIGINT       NOT NULL DEFAULT 0,
    plan_ids       JSONB        NOT NULL DEFAULT '[]'::jsonb,
    max_uses       INTEGER      NOT NULL DEFAULT 0,
    used_count     INTEGER      NOT NULL DEFAULT 0,
    starts_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    ends_at        TIMESTAMPTZ,
    is_active      BOOLEAN      NOT NULL DEFAULT TRUE,
    note           TEXT         NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS usage_orders (
    id              VARCHAR(36)  PRIMARY KEY,
    order_code      VARCHAR(24)  NOT NULL UNIQUE,
    scope_type      VARCHAR(16)  NOT NULL,
    scope_id        VARCHAR(64)  NOT NULL,
    plan_id         VARCHAR(36)  NOT NULL REFERENCES usage_plans(id),
    plan_name       VARCHAR(128) NOT NULL DEFAULT '',
    mode            VARCHAR(16)  NOT NULL DEFAULT 'pool',
    seats           INTEGER      NOT NULL DEFAULT 0,
    months          INTEGER      NOT NULL DEFAULT 1,
    unit_price_vnd  BIGINT       NOT NULL DEFAULT 0,
    subtotal_vnd    BIGINT       NOT NULL DEFAULT 0,
    promo_code_id   VARCHAR(36),
    promo_code      VARCHAR(32)  NOT NULL DEFAULT '',
    discount_vnd    BIGINT       NOT NULL DEFAULT 0,
    total_vnd       BIGINT       NOT NULL DEFAULT 0,
    status          VARCHAR(16)  NOT NULL DEFAULT 'pending',
    payment_method  VARCHAR(32)  NOT NULL DEFAULT 'bank_transfer',
    created_by      VARCHAR(36)  NOT NULL DEFAULT '',
    confirmed_by    VARCHAR(36)  NOT NULL DEFAULT '',
    paid_at         TIMESTAMPTZ,
    expires_at      TIMESTAMPTZ,
    subscription_id VARCHAR(36),
    note            TEXT         NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_usage_orders_scope ON usage_orders (scope_type, scope_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_usage_orders_status ON usage_orders (status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_usage_orders_created_by ON usage_orders (created_by, created_at DESC);
