-- Mirrors versioned migration 000117_message_document_selection: the editor
-- passage the user highlighted when sending a chat message.
ALTER TABLE messages ADD COLUMN document_selection TEXT;
