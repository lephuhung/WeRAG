-- Answers to the chat gate's question about an unclear word ("cascc":
-- abbreviation / you mistyped "các" / keep / the rewritten question /
-- other). Each row records what was offered and what the user chose, as
-- labelled training data for the spell-check model.
CREATE TABLE IF NOT EXISTS abbreviation_typo_feedback (
    id               BIGSERIAL PRIMARY KEY,
    tenant_id        BIGINT       NOT NULL,
    session_id       VARCHAR(64)  NOT NULL DEFAULT '',
    request_id       VARCHAR(64)  NOT NULL DEFAULT '',
    user_message_id  VARCHAR(64)  NOT NULL DEFAULT '',
    original_query   TEXT         NOT NULL,
    word             VARCHAR(100) NOT NULL DEFAULT '',
    spellcheck_guess VARCHAR(255) NOT NULL DEFAULT '',
    rewrite          TEXT         NOT NULL DEFAULT '',
    choice           VARCHAR(20)  NOT NULL,
    answer           TEXT         NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_abbreviation_typo_feedback_created
    ON abbreviation_typo_feedback (created_at);
