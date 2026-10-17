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

function sizeOf(xml: string, text: string): number {
  const run = xml.split("</w:r>").find((r) => r.includes(`>${text}</w:t>`)) ?? "";
  return Number(/<w:sz w:val="(\d+)"/.exec(run)?.[1]) / 2;
}

test("the header uses NĐ30 13/14 sizes, or the server's sizes when sent", async () => {
  let xml = await documentXml(await buildDocumentTemplate("thong-bao", fields));
  assert.equal(sizeOf(xml, "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM"), 13);
  assert.equal(sizeOf(xml, "Độc lập - Tự do - Hạnh phúc"), 14);
  assert.equal(sizeOf(xml, "ỦY BAN NHÂN DÂN TỈNH"), 13);
  xml = await documentXml(await buildDocumentTemplate("thong-bao", fields, { quoc_hieu: 12, tieu_ngu: 13, co_quan_ban_hanh: 12, noi_dung: 13 }));
  assert.equal(sizeOf(xml, "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM"), 12);
  assert.equal(sizeOf(xml, "Độc lập - Tự do - Hạnh phúc"), 13);
  assert.equal(sizeOf(xml, "ỦY BAN NHÂN DÂN TỈNH"), 12);
  assert.equal(sizeOf(xml, "Tổ chức đào tạo."), 13);
});

test("page margins follow the server's values and the header table fits the text width", async () => {
  let xml = await documentXml(await buildDocumentTemplate("cong-van", fields));
  assert.match(xml, /w:top="1134"[^>]*w:right="1134"[^>]*w:bottom="1134"[^>]*w:left="1701"/);
  assert.match(xml, /<w:tblW w:type="dxa" w:w="9071"\/>/);
  xml = await documentXml(await buildDocumentTemplate("cong-van", fields, { le_tren: 25, le_trai: 35, le_phai: 15 }));
  assert.match(xml, /w:top="1417"[^>]*w:right="850"[^>]*w:bottom="1134"[^>]*w:left="1984"/);
  assert.match(xml, /<w:tblW w:type="dxa" w:w="9071"\/>/); // 210 - 35 - 15 = 160 mm
  xml = await documentXml(await buildDocumentTemplate("cong-van", fields, { le_trai: 35 }));
  assert.match(xml, /<w:tblW w:type="dxa" w:w="8787"\/>/); // 210 - 35 - 20 = 155 mm
});
