// Builds the .docx the web UI writes for a ready prepare_document_template
// result, with the same builder (frontend-next/lib/document-templates.ts).
// Usage: node --experimental-strip-types build_template_docx.mjs result.json out.docx
import { readFileSync, writeFileSync } from 'node:fs';
import { buildDocumentTemplate } from '../../frontend-next/lib/document-templates.ts';

const [, , input, output] = process.argv;
const r = JSON.parse(readFileSync(input, 'utf8'));
writeFileSync(output, await buildDocumentTemplate(r.template_id, r.fields, r.font_sizes));
