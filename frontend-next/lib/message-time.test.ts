// Message timestamps and day dividers.
// Runs with (any TZ — the helpers ignore it): node --experimental-strip-types --test lib/message-time.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { dayLabel, dayStarts, messageDateTime, messageTime } from "./message-time.ts";

// 2026-10-17 10:00 in Vietnam.
const NOW = new Date("2026-10-17T03:00:00Z");

describe("messageTime", () => {
  it("shows hours and minutes in local time", () => {
    assert.equal(messageTime("2026-10-17T07:05:00Z", "vi"), "14h05");
    assert.equal(messageTime("2026-10-17T07:05:00Z", "en"), "14:05");
  });
  it("is empty without a timestamp", () => {
    assert.equal(messageTime(undefined, "vi"), "");
    assert.equal(messageTime("0001-01-01T00:00:00Z", "vi"), "");
    assert.equal(messageTime("nope", "vi"), "");
  });
  it("gives the full date on hover", () => {
    assert.equal(messageDateTime("2026-10-16T07:05:00Z", "vi"), "14h05 - 16/10/2026");
  });
});

describe("dayLabel", () => {
  it("names today and yesterday by local day", () => {
    // 2026-10-16 17:30 UTC is already the 17th in Vietnam.
    assert.deepEqual(dayLabel("2026-10-16T17:30:00Z", NOW), { kind: "today" });
    assert.deepEqual(dayLabel("2026-10-16T16:30:00Z", NOW), { kind: "yesterday" });
  });
  it("gives older days the plain date", () => {
    assert.deepEqual(dayLabel("2026-10-14T03:00:00Z", NOW), { kind: "date", text: "14/10/2026" });
  });
});

describe("dayStarts", () => {
  it("marks the first message of each day", () => {
    const starts = dayStarts([
      "2026-10-15T02:00:00Z",
      "2026-10-15T02:01:00Z",
      "2026-10-16T02:00:00Z",
      undefined,
      "2026-10-16T05:00:00Z",
      "2026-10-17T01:00:00Z",
    ]);
    assert.deepEqual([...starts], [0, 2, 5]);
  });
});
