// Runs with: node --experimental-strip-types --test lib/billing-invoice.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  invoiceEditable,
  invoiceNumberLabel,
  invoiceProblems,
  invoiceState,
  normalizeInvoice,
  validInvoiceEmail,
  validTaxCode,
} from "./billing-invoice.ts";

const good = { company_name: " Công ty  TNHH A ", tax_code: "0101 234 567", address: "12 Lê Lợi,\n Huế", email: " kt@a.vn " };

describe("validTaxCode", () => {
  it("accepts 10 digits and 10 digits + branch", () => {
    assert.equal(validTaxCode("0101234567"), true);
    assert.equal(validTaxCode("0101234567-001"), true);
    assert.equal(validTaxCode(" 0101 234 567 - 001 "), true);
  });
  it("refuses other shapes", () => {
    for (const bad of ["", "010123456", "01012345678", "0101234567-01", "0101234567-0011", "0101234567001", "010123456A", "-0101234567", "0101234567-"]) {
      assert.equal(validTaxCode(bad), false, bad);
    }
  });
});

describe("validInvoiceEmail", () => {
  // the same answers as the server (types.validInvoiceEmail, checked with go test)
  const server: [string, boolean][] = [
    ["kt@a.vn", true], ["a+b@x.vn", true], ["Nguyễn@x.vn", true], ["kt@hưng.vn", true], ["a.b@x.vn", true],
    [".a@x.vn", false], ["a.@x.vn", false], ["a..b@x.vn", false], ['"a"@x.vn', false], ['"a..b"@x.vn', false],
    ['"a b"@x.vn', false], ["a@[1.2.3.4]", true], ["a@x..vn", false], ["a@.x.vn", false], ["a@x.vn.", false],
    ["kt@localhost", false], ["a!#$%&'*/=?^_`{|}~-@x.vn", true], ["a@x_y.vn", true], ["a@x-y.vn", true],
    ["a@b@c.vn", false], ["a\t@x.vn", false], ["a(c)@x.vn", false], ["a@x.vn(c)", false], ["a\\b@x.vn", false],
    ["a@xn--abc.vn", true], ["a@1.2", true], ["A@B.VN", true], ["a@-x.vn", true],
    ["", false], ["kt", false], ["kt@", false], ["a@b.vn,c@d.vn", false], ["a b@c.vn", false], ["Kế toán <kt@a.vn>", false],
  ];
  for (const [email, want] of server) {
    it(`${want ? "accepts" : "refuses"} ${JSON.stringify(email)}`, () => {
      assert.equal(validInvoiceEmail(email), want);
    });
  }
});

describe("invoiceProblems", () => {
  it("is empty for good details", () => {
    assert.deepEqual(invoiceProblems(good), {});
  });
  it("names each bad field", () => {
    assert.deepEqual(invoiceProblems({ company_name: " ", tax_code: "123", address: "", email: "x" }), {
      company_name: "invoice_company_required",
      tax_code: "invoice_tax_code_invalid",
      address: "invoice_address_required",
      email: "invoice_email_invalid",
    });
  });
  it("normalizes as the server stores", () => {
    assert.deepEqual(normalizeInvoice(good), { company_name: "Công ty TNHH A", tax_code: "0101234567", address: "12 Lê Lợi, Huế", email: "kt@a.vn" });
  });
});

describe("invoice state", () => {
  it("follows the request and the issue", () => {
    assert.equal(invoiceState({}), "none");
    assert.equal(invoiceState({ invoice_requested: true }), "requested");
    assert.equal(invoiceState({ invoice_requested: true, invoice_issued_at: "2026-11-03T00:00:00Z" }), "issued");
  });
  it("is editable while pending or paid and not issued", () => {
    assert.equal(invoiceEditable({ status: "pending" }), true);
    assert.equal(invoiceEditable({ status: "paid" }), true);
    assert.equal(invoiceEditable({ status: "paid", invoice_issued_at: "2026-11-03T00:00:00Z" }), false);
    assert.equal(invoiceEditable({ status: "cancelled" }), false);
    assert.equal(invoiceEditable({ status: "expired" }), false);
  });
  it("labels the number", () => {
    assert.equal(invoiceNumberLabel({ invoice_series: "1C26TAA", invoice_no: "0000123" }), "1C26TAA 0000123");
    assert.equal(invoiceNumberLabel({ invoice_series: "", invoice_no: "" }), "");
  });
});
