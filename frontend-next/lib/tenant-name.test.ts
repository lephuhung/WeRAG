// Runs with: node --experimental-strip-types --test lib/tenant-name.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  TENANT_NAME_MAX_LENGTH,
  normalizeTenantName,
  tenantNameErrorKey,
  validateTenantName,
} from "./tenant-name.ts";

class FakeApiError extends Error {
  status: number;
  payload?: unknown;
  constructor(status: number, message: string, payload?: unknown) {
    super(message);
    this.status = status;
    this.payload = payload;
  }
}

describe("normalizeTenantName", () => {
  it("trims and collapses inner whitespace", () => {
    assert.equal(
      normalizeTenantName("  UBND \t xã   Phú\nVang "),
      "UBND xã Phú Vang",
    );
    assert.equal(normalizeTenantName("   "), "");
  });
});

describe("validateTenantName", () => {
  it("requires a name", () => {
    assert.equal(validateTenantName(" \n "), "tnp.nameRequired");
  });
  it("caps the length in characters", () => {
    assert.equal(validateTenantName("ạ".repeat(TENANT_NAME_MAX_LENGTH)), null);
    assert.equal(
      validateTenantName("ạ".repeat(TENANT_NAME_MAX_LENGTH + 1)),
      "tnp.nameTooLong",
    );
  });
  it("accepts an official name", () => {
    assert.equal(validateTenantName("Sở Tài chính"), null);
  });
});

describe("tenantNameErrorKey", () => {
  it("prefers the AppError code over the message", () => {
    const body = (code: number) => ({ success: false, error: { code, message: "đổi chữ" } });
    assert.equal(tenantNameErrorKey(new FakeApiError(409, "đổi chữ", body(2001))), "tnp.nameTaken");
    assert.equal(tenantNameErrorKey(new FakeApiError(400, "đổi chữ", body(2003))), "tnp.nameRequired");
    assert.equal(tenantNameErrorKey(new FakeApiError(400, "đổi chữ", body(2006))), "tnp.nameTooLong");
    assert.equal(tenantNameErrorKey(new FakeApiError(409, "đổi chữ", body(1005))), null);
  });
  it("maps the duplicate-name 409", () => {
    assert.equal(
      tenantNameErrorKey(
        new FakeApiError(409, "an organization with this name already exists"),
      ),
      "tnp.nameTaken",
    );
  });
  it("maps the name 400s", () => {
    assert.equal(
      tenantNameErrorKey(
        new FakeApiError(400, "organization name is required"),
      ),
      "tnp.nameRequired",
    );
    assert.equal(
      tenantNameErrorKey(
        new FakeApiError(
          400,
          "organization name must be at most 255 characters",
        ),
      ),
      "tnp.nameTooLong",
    );
  });
  it("leaves unrelated failures alone", () => {
    assert.equal(
      tenantNameErrorKey(new FakeApiError(409, "user already exists")),
      null,
    );
    assert.equal(tenantNameErrorKey(new FakeApiError(500, "boom")), null);
    assert.equal(tenantNameErrorKey(null), null);
  });
});
