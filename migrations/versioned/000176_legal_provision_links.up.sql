-- Migration: 000176_legal_provision_links
-- Provision-level amendment edges tagged by vbpl.vn editors (plan
-- 2026-10-19-legal-amendment-kg §11): on the page of the amended document
-- A, a provision carries type / new-types="<code>:<provision id in B>".
-- One row per (A provision, relation code, B provision). Written by
-- cmd/legal-provision-links from the crawled HTML; read by the
-- current-wording resolver (setting legal.current_wording.editor_source).
--   target_*  A: catalogue id (= vbpl id), vbpl provision id, level
--             (article | clause | item) and path ("dieu:5/khoan:2/diem:a",
--             "phuluc/…" inside an appendix, '' when a label is missing)
--   relation_code / op  10 sua_doi_bo_sung, 1 bai_bo, 13 bo_sung,
--             12 thay_the, 14 giai_thich, 6 dinh_chinh, 11 tam_ngung,
--             5 dinh_chi; tag_attr 'type' or 'new-types'
--   source_*  B: catalogue id ('' when not found), how it was found
--             (source_doc_from 'html' = B's crawled page holds the id,
--             'provision_target' = vbpl's provision-target answer), B's
--             provision id and path
--   eff_from / eff_to  when the change applies; eff_from_source
--             'provision_target' (vbpl's per-provision date), 'catalogue'
--             (B's effective_on) or '' (unknown)
DO $$ BEGIN RAISE NOTICE '[Migration 000176] Creating legal_provision_links'; END $$;

CREATE TABLE IF NOT EXISTS legal_provision_links (
    id                  BIGSERIAL    PRIMARY KEY,
    target_doc_id       VARCHAR(64)  NOT NULL,
    target_provision_id VARCHAR(64)  NOT NULL,
    target_level        VARCHAR(16)  NOT NULL DEFAULT '',
    target_path         VARCHAR(160) NOT NULL DEFAULT '',
    relation_code       SMALLINT     NOT NULL,
    op                  VARCHAR(32)  NOT NULL,
    tag_attr            VARCHAR(16)  NOT NULL DEFAULT 'type',
    source_doc_id       VARCHAR(64)  NOT NULL DEFAULT '',
    source_doc_from     VARCHAR(32)  NOT NULL DEFAULT '',
    source_provision_id VARCHAR(64)  NOT NULL,
    source_path         VARCHAR(160) NOT NULL DEFAULT '',
    eff_from            DATE         NULL,
    eff_to              DATE         NULL,
    eff_from_source     VARCHAR(32)  NOT NULL DEFAULT '',
    source              VARCHAR(32)  NOT NULL DEFAULT 'vbpl_editor',
    created_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_legal_provision_links_edge
    ON legal_provision_links (target_provision_id, relation_code, source_provision_id);
CREATE INDEX IF NOT EXISTS idx_legal_provision_links_target
    ON legal_provision_links (target_doc_id, target_path);
CREATE INDEX IF NOT EXISTS idx_legal_provision_links_source_doc
    ON legal_provision_links (source_doc_id);
CREATE INDEX IF NOT EXISTS idx_legal_provision_links_source_prov
    ON legal_provision_links (source_provision_id);
