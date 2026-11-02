import assert from "node:assert/strict";
import test from "node:test";
import { renderWordManifest, resolveWordAddinOrigin } from "./manifest.ts";

test("renders the Word add-in manifest for the configured HTTPS origin", () => {
  const xml = renderWordManifest("https://legal.example.vn/");
  assert.ok(xml.includes('DefaultValue="https://legal.example.vn/word/"'));
  assert.ok(xml.includes("586702cc-0e78-484c-9d54-14271f53dd09"));
  assert.ok(!xml.includes("{{WERAG_ORIGIN}}"));
  assert.ok(!xml.includes("localhost"));
});
test("requires an HTTPS origin without credentials, paths or XML injection", () => {
  for (const origin of ["http://legal.example.vn", "https://user:secret@example.vn", "https://example.vn/path", "https://example.vn/?x=1", 'https://example.vn/"<x>']) {
    assert.throws(() => renderWordManifest(origin));
  }
});
test("uses an explicit deployment origin and a separate local add-in origin", () => {
  assert.equal(resolveWordAddinOrigin("https://legal.example.vn", "http://localhost:3002", false), "https://legal.example.vn");
  assert.equal(resolveWordAddinOrigin(undefined, "https://legal.example.vn", false), "https://legal.example.vn");
  assert.equal(resolveWordAddinOrigin(undefined, "http://localhost:3002", true), "https://localhost:3000");
  assert.throws(() => resolveWordAddinOrigin(undefined, "http://internal:3000", false));
});
