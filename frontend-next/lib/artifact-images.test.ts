// History payloads (`LoadMessages`) serialize `Message.Artifacts` with the
// storage reference named `url`, while live complete events send it as
// `handle`. History rows also omit `index` — the download endpoint addresses
// artifacts by array position, so withArtifactIndexes must assign it before
// the renderer/hydration consumes the list.
// Runs with: node --experimental-strip-types --test lib/artifact-images.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { resolveArtifactImage, withArtifactIndexes } from "./artifact-images.ts";
import type { ArtifactMeta } from "./api/chat.ts";

const HANDLE = "resource://9W3ngUICMq1RBAQruhbrsQ";
const DEST = "![generated image](resource://9W3ngUICMq1RBAQruhbrsQ)";

function historyArtifact(): ArtifactMeta {
  // Real LoadMessages shape: no `index`.
  return {
    url: HANDLE,
    file_name: "generated-image-1.png",
    file_type: ".png",
    file_size: 2043066,
    source_path: "tool-result:generated-image-1.png",
    mod_time: "",
    created_at: "",
  };
}

describe("withArtifactIndexes", () => {
  it("assigns the array position as index for history payloads", () => {
    const list = withArtifactIndexes([historyArtifact(), { ...historyArtifact(), url: "resource://BBBBBBBBBBBBBBBBBBBBBB" }]);
    assert.equal(list[0].index, 0);
    assert.equal(list[1].index, 1);
  });

  it("keeps a payload-provided index over the position", () => {
    const list = withArtifactIndexes([{ ...historyArtifact(), index: 7 }]);
    assert.equal(list[0].index, 7);
  });
});

describe("resolveArtifactImage (history vs live payloads)", () => {
  it("resolves a history payload carrying the reference as url", () => {
    const artifacts = withArtifactIndexes([historyArtifact()]);
    const found = resolveArtifactImage(HANDLE, artifacts);
    assert.equal(found?.index, 0);
    assert.equal(found?.file_name, "generated-image-1.png");
  });

  it("resolves a live payload carrying the reference as handle", () => {
    const live: ArtifactMeta = { ...historyArtifact(), handle: HANDLE };
    delete live.url;
    assert.equal(resolveArtifactImage(HANDLE, [live])?.file_name, "generated-image-1.png");
  });

  it("prefers handle when both are present and url is stale", () => {
    const both: ArtifactMeta = {
      ...historyArtifact(),
      handle: HANDLE,
      url: "resource://AAAAAAAAAAAAAAAAAAAAAA",
    };
    assert.equal(resolveArtifactImage(HANDLE, [both])?.file_name, "generated-image-1.png");
    assert.equal(resolveArtifactImage("resource://AAAAAAAAAAAAAAAAAAAAAA", [both]), null);
  });

  it("still resolves a bare file name without any reference", () => {
    assert.equal(resolveArtifactImage("generated-image-1.png", [historyArtifact()])?.file_name, "generated-image-1.png");
  });

  it("returns null for an unknown handle so ordinary images render as-is", () => {
    assert.equal(resolveArtifactImage("resource://BBBBBBBBBBBBBBBBBBBBBB", [historyArtifact()]), null);
    assert.equal(resolveArtifactImage(HANDLE, []), null);
  });

  it("destination from the persisted answer body matches", () => {
    const dest = DEST.slice(DEST.indexOf("(") + 1, DEST.lastIndexOf(")"));
    assert.equal(resolveArtifactImage(dest, [historyArtifact()])?.file_name, "generated-image-1.png");
  });
});
