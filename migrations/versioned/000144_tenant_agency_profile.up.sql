-- The issuing agency of a workspace (một đơn vị: UBND tỉnh, Sở, Xã), as
-- its documents' header and signature need it: the documents the Word
-- add-in drafts from chat (prepare_document_template) take the agency,
-- its parent agency, the place name and the usual recipients from here,
-- never from the model. Edited by the workspace's admin or a SuperAdmin.
CREATE TABLE IF NOT EXISTS tenant_agency_profiles (
    tenant_id          BIGINT       PRIMARY KEY,
    -- cơ quan chủ quản, e.g. "UBND TỈNH THỪA THIÊN HUẾ"; '' = none
    parent_agency      VARCHAR(255) NOT NULL DEFAULT '',
    -- cơ quan ban hành, e.g. "SỞ NỘI VỤ"
    agency_name        VARCHAR(255) NOT NULL DEFAULT '',
    -- viết tắt in số ký hiệu, e.g. "SNV"
    agency_abbr        VARCHAR(32)  NOT NULL DEFAULT '',
    -- địa danh in "<địa danh>, ngày … tháng … năm …", e.g. "Huế"
    locality           VARCHAR(128) NOT NULL DEFAULT '',
    -- chức vụ người đứng đầu (after KT./TL./TUQ./Q.), e.g. "GIÁM ĐỐC"
    head_title         VARCHAR(255) NOT NULL DEFAULT '',
    -- tập thể lãnh đạo (after TM.) when the agency works collectively,
    -- e.g. "ỦY BAN NHÂN DÂN"; '' = not collective
    collective_name    VARCHAR(255) NOT NULL DEFAULT '',
    -- usual "Nơi nhận" lines, e.g. ["- UBND tỉnh (để b/c);"]
    default_recipients JSONB        NOT NULL DEFAULT '[]',
    version            INTEGER      NOT NULL DEFAULT 1,
    updated_by         VARCHAR(36)  NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
