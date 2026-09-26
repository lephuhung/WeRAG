// History payloads (`LoadMessages`) serialize `Message.Artifacts` with the
// storage reference named `url`, while live complete events send it as
// `handle`. Both shapes must resolve to the artifact index, otherwise a
// reload drops the image the live turn rendered.
// Runs with: node --experimental-strip-types --test lib/artifact-images.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { resolveArtifactImage } from "./artifact-images.ts";
import type { ArtifactMeta } from "./api/chat.ts";

const HANDLE = "resource://9W3ngUICMq1RBAQruhbrsQ";
const DEST = "![generated image](resource://9W3ngUICMq1RBAQruhbrsQ)";

function historyArtifact(): ArtifactMeta {
  return {
    index: 0,
    url: HANDLE,
    file_name: "generated-image-1.png",
    file_type: ".png",
    file_size: 2043066,
    source_path: "tool-result:generated-image-1.png",
    mod_time: "",
    created_at: "",
  };
}

describe("resolveArtifactImage (history vs live payloads)", () => {
  it("resolves a history payload carrying the reference as url", () => {
    const found = resolveArtifactImage(HANDLE, [historyArtifact()]);
    assert.equal(found?.index, 0);
    assert.equal(found?.file_name, "generated-image-1.png");
  });

  it("resolves a live payload carrying the reference as handle", () => {
    const live: ArtifactMeta = { ...historyArtifact(), handle: HANDLE };
    delete live.url;
    assert.equal(resolveArtifactImage(HANDLE, [live])?.index, 0);
  });

  it("prefers handle when both are present and url is stale", () => {
    const both: ArtifactMeta = {
      ...historyArtifact(),
      handle: HANDLE,
      url: "resource://AAAAAAAAAAAAAAAAAAAAAA",
    };
    assert.equal(resolveArtifactImage(HANDLE, [both])?.index, 0);
    assert.equal(resolveArtifactImage("resource://AAAAAAAAAAAAAAAAAAAAAA", [both]), null);
  });

  it("still resolves a bare file name without any reference", () => {
    assert.equal(resolveArtifactImage("generated-image-1.png", [historyArtifact()])?.index, 0);
  });

  it("returns null for an unknown handle so ordinary images render as-is", () => {
    assert.equal(resolveArtifactImage("resource://BBBBBBBBBBBBBBBBBBBBBB", [historyArtifact()]), null);
    assert.equal(resolveArtifactImage(HANDLE, []), null);
  });

  it("destination from the persisted answer body matches", () => {
    const dest = DEST.slice(DEST.indexOf("(") + 1, DEST.lastIndexOf(")"));
    assert.equal(resolveArtifactImage(dest, [historyArtifact()])?.index, 0);
  });
});
