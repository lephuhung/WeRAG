-- The Word file a Word add-in target belongs to: an ID the add-in keeps
-- inside the .docx. A file with another ID may not bind to the document
-- (it would upload itself over that document). '' = not bound yet.
ALTER TABLE document_workspaces ADD COLUMN IF NOT EXISTS word_file_id VARCHAR(64) NOT NULL DEFAULT '';
