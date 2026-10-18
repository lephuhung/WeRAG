// Artifact library date groups and timestamps (Vietnam days).
// Runs with: node --experimental-strip-types --test lib/artifact-library.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { artifactDateGroup, formatArtifactDateTime } from "./artifact-library.ts";

describe("artifact library dates", () => {
  const now = new Date("2026-10-17T03:00:00Z");
  it("groups by Vietnam day", () => {
    assert.equal(artifactDateGroup("2026-10-16T17:30:00Z", now), "today");
    assert.equal(artifactDateGroup("2026-10-16T16:30:00Z", now), "yesterday");
    assert.equal(artifactDateGroup("2026-10-12T03:00:00Z", now), "last7Days");
    assert.equal(artifactDateGroup("nope", now), "earlier");
  });
  it("formats as hours then date", () => {
    assert.equal(formatArtifactDateTime("2026-10-16T07:05:00Z"), "14h05 - 16/10/2026");
    assert.equal(formatArtifactDateTime(null), "—");
  });
});
