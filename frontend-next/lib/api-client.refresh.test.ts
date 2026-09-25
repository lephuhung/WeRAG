// RED (issues 2+3): refreshAccessToken must preserve tokens + avoid navigation
// on transient failure (500/503/network), and must not let a stale in-flight
// refresh overwrite newer credentials (login/logout/account switch).
// Runs with: node --experimental-strip-types --test lib/api-client.refresh.test.ts
import { describe, it, beforeEach } from "node:test";
import assert from "node:assert/strict";

import {
  apiGet,
  clearTokens,
  getTokens,
  refreshAccessToken,
  setTokens,
  type ApiError,
} from "./api-client.ts";

const store: Record<string, string> = {};
function seedStorage(values: Record<string, string>) {
  for (const k of Object.keys(store)) delete store[k];
  Object.assign(store, values);
}

let win: { location: { href: string } };
let responder: (url: string, init: unknown) => Response | Promise<Response> =
  () => json({ success: true, data: {} });

const json = (payload: unknown, status = 200) =>
  new Response(JSON.stringify(payload), { status });

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
  win = { location: { href: "" } };
  (globalThis as unknown as Record<string, unknown>).window = win;
  (globalThis as unknown as Record<string, unknown>).fetch = async (
    url: unknown,
    init: unknown,
  ) => responder(String(url), init);
}

describe("refreshAccessToken transient policy (issue 2)", () => {
  beforeEach(() => {
    installMocks();
    seedStorage({ weknora_token: "stale-jwt", weknora_refresh_token: "rt-1" });
  });

  it("preserves tokens and avoids navigation when refresh answers 503", async () => {
    responder = (url) =>
      url.includes("/auth/refresh")
        ? json({ success: false, message: "upstream down" }, 503)
        : json({ success: true, data: {} });
    await assert.rejects(refreshAccessToken(), (err: unknown) => {
      assert.equal((err as ApiError).status, 503);
      return true;
    });
    assert.equal(getTokens().token, "stale-jwt");
    assert.equal(getTokens().refreshToken, "rt-1");
    assert.equal(win.location.href, "");
  });

  it("does NOT clear on 503 even when the body looks like an invalid refresh credential", async () => {
    responder = (url) =>
      url.includes("/auth/refresh")
        ? json(
            { success: false, message: "session expired, please sign in again" },
            503,
          )
        : json({ success: true, data: {} });
    await assert.rejects(refreshAccessToken(), (err: unknown) => {
      assert.equal((err as ApiError).status, 503);
      return true;
    });
    assert.equal(getTokens().token, "stale-jwt");
    assert.equal(getTokens().refreshToken, "rt-1");
    assert.equal(win.location.href, "");
  });

  it("retries with the same credentials after a transient 503 (no sign-out)", async () => {
    let calls = 0;
    responder = (url) => {
      if (!url.includes("/auth/refresh")) return json({ success: true, data: {} });
      calls += 1;
      return calls === 1
        ? json(
            { success: false, message: "session expired, please sign in again" },
            503,
          )
        : json({ success: true, access_token: "fresh-jwt", refresh_token: "rt-2" });
    };
    await assert.rejects(refreshAccessToken());
    assert.equal(getTokens().token, "stale-jwt");
    assert.equal(getTokens().refreshToken, "rt-1");
    assert.equal(win.location.href, "");
    assert.equal(await refreshAccessToken(), "fresh-jwt");
    assert.equal(getTokens().token, "fresh-jwt");
    assert.equal(getTokens().refreshToken, "rt-2");
    assert.equal(win.location.href, "");
  });

  it("preserves tokens and avoids navigation when refresh answers 500", async () => {
    responder = (url) =>
      url.includes("/auth/refresh")
        ? json({ success: false, message: "boom" }, 500)
        : json({ success: true, data: {} });
    await assert.rejects(refreshAccessToken());
    assert.equal(getTokens().token, "stale-jwt");
    assert.equal(getTokens().refreshToken, "rt-1");
    assert.equal(win.location.href, "");
  });

  it("preserves tokens and avoids navigation on network failure", async () => {
    responder = () => {
      throw new TypeError("fetch failed");
    };
    await assert.rejects(refreshAccessToken());
    assert.equal(getTokens().token, "stale-jwt");
    assert.equal(getTokens().refreshToken, "rt-1");
    assert.equal(win.location.href, "");
  });

  it("still clears + redirects on confirmed invalid refresh credential (401)", async () => {
    responder = (url) =>
      url.includes("/auth/refresh")
        ? json({ success: false, message: "invalid refresh token" }, 401)
        : json({ success: true, data: {} });
    await assert.rejects(refreshAccessToken());
    assert.equal(getTokens().token, null);
    assert.equal(getTokens().refreshToken, null);
    assert.equal(win.location.href, "/login");
  });

  it("/auth/me 401 followed by /auth/refresh 503 keeps the session (no clear, no nav)", async () => {
    responder = (url) =>
      url.includes("/auth/refresh")
        ? json({ success: false, message: "upstream down" }, 503)
        : new Response("unauthorized", { status: 401 });
    await assert.rejects(
      apiGet("/api/v1/auth/me"),
      (err: unknown) => (err as ApiError).status === 503,
    );
    assert.equal(getTokens().token, "stale-jwt");
    assert.equal(getTokens().refreshToken, "rt-1");
    assert.equal(win.location.href, "");
  });
});

describe("refreshAccessToken credential race (issue 3)", () => {
  beforeEach(() => {
    installMocks();
    seedStorage({ weknora_token: "old-jwt", weknora_refresh_token: "old-rt" });
  });

  it("does not overwrite newer credentials when the refresh resolves late", async () => {
    let release!: (res: Response) => void;
    responder = (url) => {
      if (url.includes("/auth/refresh")) {
        return new Promise<Response>((resolve) => {
          release = resolve;
        });
      }
      return json({ success: true, data: {} });
    };
    const pending = refreshAccessToken();
    // Account switch lands mid-flight: newer credentials are stored first.
    await new Promise((r) => setTimeout(r, 10));
    setTokens("new-jwt", "new-rt");
    release(json({ success: true, access_token: "stale-fresh", refresh_token: "stale-rt" }));
    await assert.rejects(pending, (err: unknown) => {
      // The OLD request is rejected — never replayed under the new account.
      assert.notEqual((err as ApiError).status, undefined);
      return true;
    });
    assert.equal(getTokens().token, "new-jwt");
    assert.equal(getTokens().refreshToken, "new-rt");
  });

  it("does not resurrect a session after logout clears mid-flight", async () => {
    let release!: (res: Response) => void;
    responder = (url) => {
      if (url.includes("/auth/refresh")) {
        return new Promise<Response>((resolve) => {
          release = resolve;
        });
      }
      return json({ success: true, data: {} });
    };
    const pending = refreshAccessToken();
    await new Promise((r) => setTimeout(r, 10));
    clearTokens();
    release(json({ success: true, access_token: "stale-fresh", refresh_token: "stale-rt" }));
    await assert.rejects(pending);
    assert.equal(getTokens().token, null);
    assert.equal(getTokens().refreshToken, null);
    assert.equal(win.location.href, "");
  });

  it("an invalid-refresh verdict arriving after an account switch must not wipe the new session", async () => {
    let release!: (res: Response) => void;
    responder = (url) => {
      if (url.includes("/auth/refresh")) {
        return new Promise<Response>((resolve) => {
          release = resolve;
        });
      }
      return json({ success: true, data: {} });
    };
    const pending = refreshAccessToken();
    await new Promise((r) => setTimeout(r, 10));
    setTokens("new-jwt", "new-rt");
    release(json({ success: false, message: "invalid refresh token" }, 401));
    await assert.rejects(pending);
    assert.equal(getTokens().token, "new-jwt");
    assert.equal(getTokens().refreshToken, "new-rt");
    assert.equal(win.location.href, "");
  });
});
