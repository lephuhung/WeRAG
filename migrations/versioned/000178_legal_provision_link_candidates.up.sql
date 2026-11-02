-- Migration: 000178_legal_provision_link_candidates
-- Provision-level amendment links pushed by a source other than vbpl.vn
-- (POST /system/legal-documents/push, docs/legal-document-feed-spec.md §9).
-- They wait here for a SuperAdmin: an approved candidate is copied into
-- legal_provision_links (source = the feed, e.g. 'tvpl') and only then
-- reaches the current-wording resolver (setting
-- legal.current_wording.feed_source).
--   feed / feed_doc_id / feed_url   who pushed, the document id there
--   target_*        A: catalogue id, the pushed element id, provision id
--                   used in legal_provision_links, level and path
--   relation_code / op   as in legal_provision_links (+ 9 huong_dan, 16 sua_doi)
--   amender_*       B as the feed named it; amender_doc_id = catalogue id
--                   once resolved ('' when not)
--   source_provision_id / source_path   B's provision
--   match_status    matched | doc_only | target_missing | unresolved | duplicate
--   review_status   pending | approved | rejected | auto (duplicate of a
--                   link already held: nothing to review)
DO $$ BEGIN RAISE NOTICE '[Migration 000178] Creating legal_provision_link_candidates'; END $$;

CREATE TABLE IF NOT EXISTS legal_provision_link_candidates (
    id                  BIGSERIAL    PRIMARY KEY,
    feed                VARCHAR(32)  NOT NULL,
    feed_doc_id         VARCHAR(64)  NOT NULL,
    feed_url            TEXT         NOT NULL DEFAULT '',
    target_doc_id       VARCHAR(64)  NOT NULL,
    target_element_id   VARCHAR(160) NOT NULL DEFAULT '',
    target_provision_id VARCHAR(64)  NOT NULL DEFAULT '',
    target_level        VARCHAR(16)  NOT NULL DEFAULT '',
    target_path         VARCHAR(160) NOT NULL DEFAULT '',
    relation_code       SMALLINT     NOT NULL,
    op                  VARCHAR(32)  NOT NULL,
    amender_doc_number  VARCHAR(128) NOT NULL DEFAULT '',
    amender_issue_date  VARCHAR(16)  NOT NULL DEFAULT '',
    amender_issuer      VARCHAR(255) NOT NULL DEFAULT '',
    amender_doc_id      VARCHAR(64)  NOT NULL DEFAULT '',
    source_provision_id VARCHAR(64)  NOT NULL DEFAULT '',
    source_path         VARCHAR(160) NOT NULL DEFAULT '',
    eff_from            DATE         NULL,
    eff_to              DATE         NULL,
    note                TEXT         NOT NULL DEFAULT '',
    match_status        VARCHAR(24)  NOT NULL,
    match_reason        TEXT         NOT NULL DEFAULT '',
    review_status       VARCHAR(16)  NOT NULL DEFAULT 'pending',
    link_id             BIGINT       NULL,
    reviewed_by         VARCHAR(64)  NOT NULL DEFAULT '',
    reviewed_at         TIMESTAMP WITH TIME ZONE NULL,
    detail              JSONB        NULL,
    created_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_legal_link_candidates_feed_doc
    ON legal_provision_link_candidates (feed, feed_doc_id);
CREATE INDEX IF NOT EXISTS idx_legal_link_candidates_target
    ON legal_provision_link_candidates (target_doc_id, review_status);
CREATE INDEX IF NOT EXISTS idx_legal_link_candidates_review
    ON legal_provision_link_candidates (review_status, match_status);
