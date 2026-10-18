import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { authHeaders, pathTenantFor, setPathTenant } from "../api-client.ts";

describe("path tenant override", () => {
  it("matches the prefix, sub-paths, queries and absolute URLs only", () => {
    setPathTenant("/api/v1/sessions/abc", 10000);
    assert.equal(pathTenantFor("/api/v1/sessions/abc"), "10000");
    assert.equal(pathTenantFor("/api/v1/sessions/abc/documents/d1/forcesave"), "10000");
    assert.equal(pathTenantFor("/api/v1/sessions/abc?x=1"), "10000");
    assert.equal(pathTenantFor("http://host:3000/api/v1/sessions/abc/documents/d1"), "10000");
    assert.equal(pathTenantFor("/api/v1/sessions/abcd"), null);
    assert.equal(pathTenantFor("/api/v1/sessions"), null);
  });
  it("authHeaders(path) carries it", () => {
    assert.equal(authHeaders("/api/v1/sessions/abc/documents/d1/forcesave")["X-Tenant-ID"], "10000");
    assert.equal(authHeaders("/api/v1/other")["X-Tenant-ID"], undefined);
  });
  it("null removes it", () => {
    setPathTenant("/api/v1/sessions/abc", null);
    assert.equal(pathTenantFor("/api/v1/sessions/abc/documents/d1"), null);
  });
});
