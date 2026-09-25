// RED: request()/downloadRequest() ignore opts.headers — embed chunks /
// suggestions and desktop autoSetup lose their credentials.
// Runs with: node --experimental-strip-types --test lib/api-client.headers.test.ts
import { describe, it, beforeEach } from "node:test";
import assert from "node:assert/strict";

import { apiGet, apiPost, apiDownload } from "./api-client.ts";

type FetchCall = { url: string; headers: Record<string, string>; body: string };

const store: Record<string, string> = {};
function seedStorage(values: Record<string, string>) {
  for (const k of Object.keys(store)) delete store[k];
  Object.assign(store, values);
}

let calls: FetchCall[] = [];
let responder: (call: FetchCall, n: number) => Response = () =>
  new Response(JSON.stringify({ success: true, data: {} }), { status: 200 });

function installMocks() {
  (globalThis as unknown as Record<string, unknown>).localStorage = {
    getItem: (k: string) => store[k] ?? null,
    setItem: (k: string, v: string) => {
      store[k] = v;
    },
    removeItem: (k: string) => {
      delete store[k];
    },
  };
  (globalThis as unknown as Record<string, unknown>).window = { location: { href: "" } };
  calls = [];
  (globalThis as unknown as Record<string, unknown>).fetch = async (
    url: unknown,
    init: unknown,
  ) => {
    const rawHeaders = (init as { headers?: Record<string, string> })?.headers ?? {};
    const headers: Record<string, string> = {};
    for (const [k, v] of Object.entries(rawHeaders)) headers[k.toLowerCase()] = String(v);
    const call: FetchCall = {
      url: String(url),
      headers,
      body: String((init as { body?: unknown })?.body ?? ""),
    };
    calls.push(call);
    return responder(call, calls.length);
  };
}

const json = (payload: unknown, status = 200) =>
  new Response(JSON.stringify(payload), { status });

describe("api-client opts.headers (issue 1)", () => {
  beforeEach(() => {
    installMocks();
    responder = () => json({ success: true, data: {} });
    seedStorage({ weknora_token: "jwt-123", weknora_selected_tenant_id: "42" });
  });

  it("apiGet sends stored Bearer + tenant by default", async () => {
    await apiGet("/api/v1/tenants/all");
    assert.equal(calls[0].headers["authorization"], "Bearer jwt-123");
    assert.equal(calls[0].headers["x-tenant-id"], "42");
  });

  it("explicit Embed Authorization wins (case-insensitive) and suppresses stored auth + tenant", async () => {
    await apiGet("/api/v1/embed/abc/chunks/c1", {
      headers: { authorization: "Embed pub-token" },
    });
    assert.equal(calls[0].headers["authorization"], "Embed pub-token");
    assert.ok(!("x-tenant-id" in calls[0].headers));
  });

  it("embed suggestion headers ride along (session sig + visitor)", async () => {
    await apiGet("/api/v1/embed/abc/sessions/s1/messages/m1/suggestions", {
      headers: {
        Authorization: "Embed pub-token",
        "X-Embed-Session": "sig-1",
        "X-Embed-Visitor": "v-1",
      },
    });
    assert.equal(calls[0].headers["authorization"], "Embed pub-token");
    assert.equal(calls[0].headers["x-embed-session"], "sig-1");
    assert.equal(calls[0].headers["x-embed-visitor"], "v-1");
    assert.ok(!("x-tenant-id" in calls[0].headers));
  });

  it("forwards the desktop token on auto-setup", async () => {
    await apiPost("/api/v1/auth/auto-setup", {}, {
      headers: { "X-WeKnora-Desktop-Token": "desk-1" },
    });
    assert.equal(calls[0].headers["x-weknora-desktop-token"], "desk-1");
  });

  it("custom tenant / request-id / language override the defaults", async () => {
    await apiGet("/api/v1/tenants/all", {
      headers: {
        "X-Tenant-ID": "99",
        "X-Request-ID": "req-1",
        "Accept-Language": "en-US",
      },
    });
    assert.equal(calls[0].headers["authorization"], "Bearer jwt-123");
    assert.equal(calls[0].headers["x-tenant-id"], "99");
    assert.equal(calls[0].headers["x-request-id"], "req-1");
    assert.equal(calls[0].headers["accept-language"], "en-US");
  });

  it("embed calls never trigger a refresh on 401", async () => {
    responder = (call) =>
      call.url.includes("/auth/refresh")
        ? json({ success: true, access_token: "fresh", refresh_token: "rt-2" })
        : new Response("unauthorized", { status: 401 });
    await assert.rejects(
      apiGet("/api/v1/embed/abc/chunks/c1", {
        headers: { Authorization: "Embed pub-token" },
      }),
      (err: unknown) => (err as { status?: number }).status === 401,
    );
    assert.ok(!calls.some((c) => c.url.includes("/auth/refresh")));
  });

  it("caller-managed Authorization schemes never trigger a refresh on 401", async () => {
    responder = (call) =>
      call.url.includes("/auth/refresh")
        ? json({ success: true, access_token: "fresh", refresh_token: "rt-2" })
        : new Response("unauthorized", { status: 401 });
    await assert.rejects(
      apiGet("/api/v1/tenants/all", { headers: { Authorization: "Token desk-1" } }),
      (err: unknown) => (err as { status?: number }).status === 401,
    );
    assert.ok(!calls.some((c) => c.url.includes("/auth/refresh")));
    assert.equal(calls[0].headers["authorization"], "Token desk-1");
  });

  it("a stale custom Bearer still self-heals via refresh and replays fresh", async () => {
    seedStorage({
      weknora_token: "stale-jwt",
      weknora_refresh_token: "rt-1",
      weknora_selected_tenant_id: "42",
    });
    responder = (call) => {
      if (call.url.includes("/auth/refresh")) {
        return json({ success: true, access_token: "fresh-jwt", refresh_token: "rt-2" });
      }
      if (call.headers["authorization"] === "Bearer fresh-jwt") {
        return json({ success: true, data: {} });
      }
      return new Response("unauthorized", { status: 401 });
    };
    await apiGet("/api/v1/tenants/all", { headers: { Authorization: "Bearer stale-jwt" } });
    assert.ok(calls.some((c) => c.url.includes("/auth/refresh")));
    assert.equal(calls[calls.length - 1].headers["authorization"], "Bearer fresh-jwt");
    assert.equal(store["weknora_token"], "fresh-jwt");
  });

  it("apiDownload honors custom auth and skips refresh for caller-managed credentials", async () => {
    responder = (call) =>
      call.url.includes("/auth/refresh")
        ? json({ success: true, access_token: "fresh", refresh_token: "rt-2" })
        : new Response("ok-blob");
    const blob = await apiDownload("/api/v1/knowledge/k1/download", {
      headers: { Authorization: "Token desk-1" },
    });
    assert.ok(blob instanceof Blob);
    assert.equal(calls[0].headers["authorization"], "Token desk-1");
    assert.ok(!calls.some((c) => c.url.includes("/auth/refresh")));
  });
});
