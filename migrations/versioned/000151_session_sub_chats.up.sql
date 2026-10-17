-- Migration: 000151_session_sub_chats
-- Sub-chats of a document-assistant chat ("chat nhỏ"): a child session names
-- the root session that owns the documents (document_workspaces and
-- temporary_documents stay keyed by the root's id). Each sub-chat keeps its
-- own messages and context, so a long editing chat can carry on in a fresh
-- one without losing the documents. Empty for ordinary sessions and roots.
--
-- Numbered after the highest committed migration (000150); dev databases
-- that applied it as 000146 skip it (the column is already there).
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS chat_root_id VARCHAR(36) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_sessions_chat_root_id
    ON sessions (chat_root_id)
    WHERE chat_root_id <> '';
