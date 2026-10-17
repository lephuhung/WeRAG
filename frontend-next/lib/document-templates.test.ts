// Runs with: node --experimental-strip-types --test lib/document-templates.test.ts
import assert from "node:assert/strict";
import { test } from "node:test";
import JSZip from "jszip";
import { DOCUMENT_TEMPLATES, buildBlankDocument, buildDocumentTemplate } from "./document-templates.ts";
import type { TemplateFields } from "./template-protocol.ts";

const fields: TemplateFields = {
  agency: "ỦY BAN NHÂN DÂN TỈNH", locality: "Huế", date: "ngày 16 tháng 10 năm 2026", number_symbol: "DỰ THẢO",
  subject: "Đào tạo kỹ năng <AI> & dữ liệu", addressee: "Sở Nội vụ", grounds: ["Căn cứ Luật Tổ chức chính quyền địa phương;"],
  request_basis: "Theo đề nghị của Giám đốc Sở Nội vụ.", decision_authority: "CHỦ TỊCH ỦY BAN NHÂN DÂN TỈNH",
  paragraphs: [{ kind: "heading", text: "I. MỤC ĐÍCH" }, { kind: "body", text: "Tổ chức đào tạo." }],
  recipients: ["- Như trên;", "- Lưu: VT."], signature_lines: ["CHỦ TỊCH"], signer_name: "Nguyễn Văn A",
};

async function documentXml(bytes: Uint8Array): Promise<string> {
  const zip = await JSZip.loadAsync(bytes);
  return (await zip.file("word/document.xml")?.async("string")) ?? "";
}

test("every template carries the server's fields on an A4 page with NĐ30 margins", async () => {
  for (const t of DOCUMENT_TEMPLATES) {
    const xml = await documentXml(await buildDocumentTemplate(t.id, fields));
    assert.match(xml, /<w:pgSz w:w="11906" w:h="16838"/, t.id);
    assert.match(xml, /w:left="1701"/, t.id); // 30 mm
    assert.ok(xml.includes("Huế, ngày 16 tháng 10 năm 2026"), t.id);
    assert.ok(xml.includes("Đào tạo kỹ năng &lt;AI&gt; &amp; dữ liệu"), `${t.id}: text is escaped`);
    assert.ok(!xml.includes("[CƠ QUAN"), `${t.id}: no placeholder left`);
  }
});

test("the blank document is empty, A4", async () => {
  const xml = await documentXml(await buildBlankDocument());
  assert.match(xml, /<w:pgSz w:w="11906" w:h="16838"/);
  assert.ok(!/<w:t[ >]/.test(xml));
});
