// RED: JSON + SSE 401s must share one refresh flight and persist both
// rotated tokens; embed streams must never refresh.
// Runs with: node --experimental-strip-types --test lib/api/stream-refresh.test.ts
import { describe, it, beforeEach } from "node:test";
import assert from "node:assert/strict";

import { apiGet } from "../api-client.ts";
import { streamChat, streamEmbedChat } from "./stream.ts";

const store: Record<string, string> = {};
function seedStorage(values: Record<string, string>) {
  for (const k of Object.keys(store)) delete store[k];
  Object.assign(store, values);
}

let refreshCalls = 0;
let chatCalls = 0;
let chatFirst401 = true;

const sse = (payload: unknown) =>
  new Response(`data: ${JSON.stringify(payload)}\n\n`, {
    status: 200,
    headers: { "Content-Type": "text/event-stream" },
  });

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
  refreshCalls = 0;
  chatCalls = 0;
  chatFirst401 = true;
  (globalThis as unknown as Record<string, unknown>).fetch = async (
    url: unknown,
    init: unknown,
  ) => {
    const u = String(url);
    const headers = ((init as { headers?: Record<string, string> })?.headers ?? {}) as Record<
      string,
      string
    >;
    const auth = headers["Authorization"] ?? "";
    if (u.includes("/api/v1/auth/refresh")) {
      refreshCalls++;
      await new Promise((r) => setTimeout(r, 10));
      return new Response(
        JSON.stringify({ success: true, access_token: "fresh-jwt", refresh_token: "rt-2" }),
        { status: 200 },
      );
    }
    if (u.includes("/knowledge-chat/") || u.includes("/agent-chat/")) {
      chatCalls++;
      if (auth.startsWith("Embed ")) {
        return new Response("unauthorized", { status: 401 });
      }
      if (auth === "Bearer fresh-jwt") return sse({ content: "hi" });
      if (chatFirst401) {
        chatFirst401 = false;
        return new Response("unauthorized", { status: 401 });
      }
      return sse({ content: "hi" });
    }
    if (auth === "Bearer fresh-jwt") {
      return new Response(JSON.stringify({ success: true, data: {} }), { status: 200 });
    }
    return new Response("unauthorized", { status: 401 });
  };
}

describe("stream refresh single-flight (issue 3)", () => {
  beforeEach(() => {
    installMocks();
    seedStorage({
      weknora_token: "stale-jwt",
      weknora_refresh_token: "rt-1",
      weknora_selected_tenant_id: "42",
    });
  });

  it("consecutive streams recover via the shared refresh and persist both rotated tokens", async () => {
    const seen: string[] = [];
    await streamChat({
      sessionId: "s1",
      query: "hello",
      onChunk: (c) => seen.push(c.content ?? ""),
    });
    assert.deepEqual(seen, ["hi"]);
    assert.equal(store["weknora_token"], "fresh-jwt");
    assert.equal(store["weknora_refresh_token"], "rt-2");

    chatFirst401 = true;
    seedStorage({
      weknora_token: "expired-again",
      weknora_refresh_token: "rt-2",
      weknora_selected_tenant_id: "42",
    });
    const seen2: string[] = [];
    await streamChat({
      sessionId: "s1",
      query: "again",
      onChunk: (c) => seen2.push(c.content ?? ""),
    });
    assert.deepEqual(seen2, ["hi"]);
    assert.equal(refreshCalls, 2);
  });

  it("a concurrent stream + JSON 401 shares one refresh flight", async () => {
    const seen: string[] = [];
    const [streamRes, jsonRes] = await Promise.all([
      streamChat({ sessionId: "s1", query: "hello", onChunk: (c) => seen.push(c.content ?? "") }),
      apiGet<{ success: boolean }>("/api/v1/tenants/all"),
    ]);
    assert.equal(streamRes, undefined);
    assert.equal(jsonRes.success, true);
    assert.deepEqual(seen, ["hi"]);
    assert.equal(refreshCalls, 1);
    assert.equal(store["weknora_token"], "fresh-jwt");
    assert.equal(store["weknora_refresh_token"], "rt-2");
  });

  it("embed streams never hit the refresh endpoint", async () => {
    await assert.rejects(
      streamEmbedChat({
        sessionId: "s1",
        query: "hello",
        channelId: "ch-1",
        embedToken: "bad-token",
        onChunk: () => {},
      }),
      (err: unknown) => (err as { status?: number }).status === 401,
    );
    assert.equal(refreshCalls, 0);
  });
});
