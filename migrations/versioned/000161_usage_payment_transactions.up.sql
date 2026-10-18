-- Migration: 000161_usage_payment_transactions
-- Incoming bank transfers pushed by a payment checker (POST
-- /billing/payments/webhook). Each row is one transaction as the bank
-- reported it; (source, external_id) makes a resend a no-op. A transfer
-- whose content carries a pending order's code and covers its total
-- confirms the order; anything else stays here for SuperAdmin.
DO $$ BEGIN RAISE NOTICE '[Migration 000161] Creating usage_payment_transactions...'; END $$;

CREATE TABLE IF NOT EXISTS usage_payment_transactions (
    id             VARCHAR(36)  PRIMARY KEY,
    source         VARCHAR(32)  NOT NULL,
    external_id    VARCHAR(128) NOT NULL,
    amount_vnd     BIGINT       NOT NULL DEFAULT 0,
    content        TEXT         NOT NULL DEFAULT '',
    account_no     VARCHAR(64)  NOT NULL DEFAULT '',
    transacted_at  TIMESTAMPTZ  NOT NULL,
    status         VARCHAR(24)  NOT NULL,
    reason         TEXT         NOT NULL DEFAULT '',
    order_id       VARCHAR(36),
    order_code     VARCHAR(24)  NOT NULL DEFAULT '',
    raw            JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT ux_usage_payment_transactions_source_external UNIQUE (source, external_id)
);
CREATE INDEX IF NOT EXISTS idx_usage_payment_transactions_status
    ON usage_payment_transactions (status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_usage_payment_transactions_order
    ON usage_payment_transactions (order_id) WHERE order_id IS NOT NULL;
