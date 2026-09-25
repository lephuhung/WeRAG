// Regression test: apiUpload must honor opts.headers (custom Authorization /
// tenant / desktop-token overrides) instead of silently dropping them.
// Runs with: node --experimental-strip-types --test lib/api-client.upload.test.ts
import { describe, it, beforeEach } from "node:test";
import assert from "node:assert/strict";

import { apiUpload } from "./api-client.ts";

type HeaderMap = Record<string, string>;

class FakeUpload {
  onprogress: ((e: { lengthComputable: boolean; loaded: number; total: number }) => void) | null = null;
}

class FakeXHR {
  static instances: FakeXHR[] = [];
  static onSend: ((xhr: FakeXHR) => void) | null = null;
  method = "";
  url = "";
  headers: HeaderMap = {};
  upload = new FakeUpload();
  onload: (() => void) | null = null;
  onabort: (() => void) | null = null;
  onerror: (() => void) | null = null;
  ontimeout: (() => void) | null = null;
  status = 0;
  responseText = "";
  timeout = 0;
  sent: unknown = null;

  constructor() {
    FakeXHR.instances.push(this);
  }
  open(method: string, url: string) {
    this.method = method;
    this.url = url;
  }
  setRequestHeader(k: string, v: string) {
    this.headers[k.toLowerCase()] = v;
  }
  send(body: unknown) {
    this.sent = body;
    FakeXHR.onSend?.(this);
  }
  succeed(payload: unknown, status = 200) {
    this.status = status;
    this.responseText = JSON.stringify(payload);
    this.onload?.();
  }
}

const store: Record<string, string> = {};
function seedStorage(values: Record<string, string>) {
  for (const k of Object.keys(store)) delete store[k];
  Object.assign(store, values);
}

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
  (globalThis as unknown as Record<string, unknown>).XMLHttpRequest = FakeXHR;
}

function uploadOnce(path: string, opts?: { headers?: HeaderMap }) {
  const form = new FormData();
  form.append("file", new Blob(["x"]), "x.bin");
  const promise = apiUpload<{ success: boolean }>(path, form, undefined, opts);
  const xhr = FakeXHR.instances[FakeXHR.instances.length - 1];
  xhr.succeed({ success: true });
  return promise.then(() => xhr);
}

describe("apiUpload custom headers", () => {
  beforeEach(() => {
    FakeXHR.instances = [];
    FakeXHR.onSend = null;
    installMocks();
    seedStorage({ weknora_token: "jwt-123", weknora_selected_tenant_id: "42" });
  });

  it("sends stored Bearer + tenant by default and never sets Content-Type", async () => {
    const xhr = await uploadOnce("/api/v1/sessions/s1/attachments");
    assert.equal(xhr.headers["authorization"], "Bearer jwt-123");
    assert.equal(xhr.headers["x-tenant-id"], "42");
    assert.ok(xhr.headers["x-request-id"]);
    assert.ok(xhr.headers["accept-language"]);
    assert.ok(!("content-type" in xhr.headers));
  });

  it("an explicit Embed Authorization wins and suppresses stored auth + tenant", async () => {
    const xhr = await uploadOnce("/api/v1/models/m1/debug", {
      headers: { Authorization: "Embed pub-token" },
    });
    assert.equal(xhr.headers["authorization"], "Embed pub-token");
    assert.ok(!("x-tenant-id" in xhr.headers));
  });

  it("embed paths suppress stored auth + tenant", async () => {
    const xhr = await uploadOnce("/api/v1/embed/abc/upload");
    assert.ok(!("authorization" in xhr.headers));
    assert.ok(!("x-tenant-id" in xhr.headers));
  });

  it("forwards the desktop token and other custom headers", async () => {
    const xhr = await uploadOnce("/api/v1/auth/auto-setup", {
      headers: { "X-WeKnora-Desktop-Token": "desk-1", "X-Custom": "yes" },
    });
    assert.equal(xhr.headers["x-weknora-desktop-token"], "desk-1");
    assert.equal(xhr.headers["x-custom"], "yes");
  });

  it("custom tenant / request-id / language override the defaults", async () => {
    const xhr = await uploadOnce("/api/v1/sessions/s1/attachments", {
      headers: {
        "X-Tenant-ID": "99",
        "X-Request-ID": "req-1",
        "Accept-Language": "en-US",
      },
    });
    assert.equal(xhr.headers["authorization"], "Bearer jwt-123");
    assert.equal(xhr.headers["x-tenant-id"], "99");
    assert.equal(xhr.headers["x-request-id"], "req-1");
    assert.equal(xhr.headers["accept-language"], "en-US");
  });

  it("ignores a custom Content-Type so the multipart boundary survives", async () => {
    const xhr = await uploadOnce("/api/v1/sessions/s1/attachments", {
      headers: { "Content-Type": "multipart/form-data" },
    });
    assert.ok(!("content-type" in xhr.headers));
  });
});

describe("apiUpload 401 refresh", () => {
  type FetchCall = { url: string; body: string };

  beforeEach(() => {
    FakeXHR.instances = [];
    FakeXHR.onSend = null;
    installMocks();
    seedStorage({
      weknora_token: "stale-jwt",
      weknora_refresh_token: "rt-1",
      weknora_selected_tenant_id: "42",
    });
  });

  function mockRefreshFetch(calls: FetchCall[]) {
    (globalThis as unknown as Record<string, unknown>).fetch = async (
      url: unknown,
      init: unknown,
    ) => {
      calls.push({
        url: String(url),
        body: String((init as { body?: unknown })?.body ?? ""),
      });
      return {
        ok: true,
        status: 200,
        json: async () => ({
          success: true,
          access_token: "fresh-jwt",
          refresh_token: "rt-2",
        }),
      };
    };
  }

  function uploadForm(): FormData {
    const form = new FormData();
    form.append("file", new Blob(["x"]), "x.bin");
    return form;
  }

  it("replays a stale custom Bearer with the freshly refreshed token", async () => {
    const fetchCalls: FetchCall[] = [];
    mockRefreshFetch(fetchCalls);
    let sends = 0;
    FakeXHR.onSend = (xhr) => {
      sends++;
      if (sends === 1) {
        queueMicrotask(() =>
          xhr.succeed({ success: false, error: "unauthorized" }, 401),
        );
      } else {
        queueMicrotask(() => xhr.succeed({ success: true }, 200));
      }
    };
    await apiUpload<{ success: boolean }>(
      "/api/v1/sessions/s1/attachments",
      uploadForm(),
      undefined,
      { headers: { Authorization: "Bearer stale-jwt" } },
    );
    assert.equal(sends, 2);
    assert.equal(fetchCalls.length, 1);
    assert.ok(fetchCalls[0].url.includes("/api/v1/auth/refresh"));
    assert.equal(
      FakeXHR.instances[0].headers["authorization"],
      "Bearer stale-jwt",
    );
    assert.equal(
      FakeXHR.instances[1].headers["authorization"],
      "Bearer fresh-jwt",
    );
    assert.equal(store["weknora_token"], "fresh-jwt");
  });

  it("refreshes the stored token when no custom Authorization is set", async () => {
    const fetchCalls: FetchCall[] = [];
    mockRefreshFetch(fetchCalls);
    let sends = 0;
    FakeXHR.onSend = (xhr) => {
      sends++;
      if (sends === 1) {
        queueMicrotask(() =>
          xhr.succeed({ success: false, error: "unauthorized" }, 401),
        );
      } else {
        queueMicrotask(() => xhr.succeed({ success: true }, 200));
      }
    };
    await apiUpload<{ success: boolean }>(
      "/api/v1/sessions/s1/attachments",
      uploadForm(),
    );
    assert.equal(sends, 2);
    assert.equal(fetchCalls.length, 1);
    assert.equal(
      FakeXHR.instances[1].headers["authorization"],
      "Bearer fresh-jwt",
    );
  });

  it("does not refresh Embed auth on 401", async () => {
    const fetchCalls: FetchCall[] = [];
    mockRefreshFetch(fetchCalls);
    FakeXHR.onSend = (xhr) => {
      queueMicrotask(() =>
        xhr.succeed({ success: false, error: "unauthorized" }, 401),
      );
    };
    await assert.rejects(
      apiUpload<{ success: boolean }>("/api/v1/models/m1/debug", uploadForm(), undefined, {
        headers: { Authorization: "Embed pub-token" },
      }),
      (err: unknown) => (err as { status?: number }).status === 401,
    );
    assert.equal(fetchCalls.length, 0);
  });

  it("does not refresh other caller-managed Authorization schemes on 401", async () => {
    const fetchCalls: FetchCall[] = [];
    mockRefreshFetch(fetchCalls);
    FakeXHR.onSend = (xhr) => {
      queueMicrotask(() =>
        xhr.succeed({ success: false, error: "unauthorized" }, 401),
      );
    };
    await assert.rejects(
      apiUpload<{ success: boolean }>(
        "/api/v1/sessions/s1/attachments",
        uploadForm(),
        undefined,
        { headers: { Authorization: "Token desk-1" } },
      ),
      (err: unknown) => (err as { status?: number }).status === 401,
    );
    assert.equal(fetchCalls.length, 0);
    assert.equal(
      FakeXHR.instances[0].headers["authorization"],
      "Token desk-1",
    );
  });
});
