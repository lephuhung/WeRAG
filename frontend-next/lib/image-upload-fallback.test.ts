// B-2a(3) RED: the chat image fallback must inline exactly the files whose
// upload failed — slicing the original list by success count inlines the
// wrong image (and duplicates an already-uploaded one) on mixed results.
// Runs with: node --experimental-strip-types --test lib/image-upload-fallback.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { uploadImagesWithFallback } from "./image-upload-fallback.ts";

const img = (name: string) => new File(["img-bytes"], name, { type: "image/png" });
const uploader = (failing: Set<string>) => async (file: File) => {
  if (failing.has(file.name)) throw new Error(`upload failed: ${file.name}`);
  return `doc-${file.name}`;
};
const toDataUri = async (file: File) => `datauri:${file.name}`;

describe("uploadImagesWithFallback", () => {
  it("returns IDs in order with no inline payload when all uploads succeed", async () => {
    const out = await uploadImagesWithFallback([img("a.png"), img("b.png")], uploader(new Set()), toDataUri);
    assert.deepEqual(out.attachmentIds, ["doc-a.png", "doc-b.png"]);
    assert.equal(out.inlineImages, undefined);
  });

  it("first fails + second succeeds: falls back to the FIRST file only", async () => {
    const out = await uploadImagesWithFallback(
      [img("first.png"), img("second.png")],
      uploader(new Set(["first.png"])),
      toDataUri,
    );
    assert.deepEqual(out.attachmentIds, ["doc-second.png"]);
    assert.deepEqual(out.inlineImages, [{ data: "datauri:first.png" }]);
  });

  it("second fails: falls back to the SECOND file only", async () => {
    const out = await uploadImagesWithFallback(
      [img("a.png"), img("b.png")],
      uploader(new Set(["b.png"])),
      toDataUri,
    );
    assert.deepEqual(out.attachmentIds, ["doc-a.png"]);
    assert.deepEqual(out.inlineImages, [{ data: "datauri:b.png" }]);
  });

  it("all fail: inlines every file in original order", async () => {
    const out = await uploadImagesWithFallback(
      [img("a.png"), img("b.png")],
      uploader(new Set(["a.png", "b.png"])),
      toDataUri,
    );
    assert.deepEqual(out.attachmentIds, []);
    assert.deepEqual(out.inlineImages, [{ data: "datauri:a.png" }, { data: "datauri:b.png" }]);
  });

  it("skips files that cannot even be read, without failing the turn", async () => {
    const out = await uploadImagesWithFallback([img("bad.png")], uploader(new Set(["bad.png"])), async () => {
      throw new Error("unreadable");
    });
    assert.deepEqual(out.attachmentIds, []);
    assert.equal(out.inlineImages, undefined);
  });

  it("handles an empty selection", async () => {
    const out = await uploadImagesWithFallback([], uploader(new Set()), toDataUri);
    assert.deepEqual(out.attachmentIds, []);
    assert.equal(out.inlineImages, undefined);
  });
});
