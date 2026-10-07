-- Migration: 000117_message_document_selection
-- The editor passage a user highlighted when sending a chat message, kept on
-- the user message so chat history shows which passage a question referred to.
ALTER TABLE messages ADD COLUMN IF NOT EXISTS document_selection JSONB;

COMMENT ON COLUMN messages.document_selection IS 'Editor passage ({text, paragraph_hint}) the user highlighted when sending this message';
