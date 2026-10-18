// Vietnam-time formatting helpers.
// Runs with: node --experimental-strip-types --test lib/datetime.test.ts
// The zone cases re-run the helpers in child processes with TZ set, so the
// result does not depend on the zone of the machine running the test.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

import { formatDate, formatDateTime, formatTime, vnDayKey, vnDaysAgo, vnIntl, vnParts, TIME_ZONE } from "./datetime.ts";

const HERE = fileURLToPath(new URL("./datetime.ts", import.meta.url));

/** Runs the helpers under another process time zone. */
function underZone(tz: string): Record<string, unknown> {
  const script = `
    const m = await import(${JSON.stringify(HERE)});
    const at = "2026-10-16T07:05:00Z";
    console.log(JSON.stringify({
      date: m.formatDate(at),
      time: m.formatTime(at, "vi"),
      dateTime: m.formatDateTime(at, "vi"),
      before5pm: m.vnDayKey("2026-10-16T16:59:59Z"),
      at5pm: m.vnDayKey("2026-10-16T17:00:00Z"),
      daysAgo: m.vnDaysAgo("2026-10-16T16:59:00Z", new Date("2026-10-16T17:01:00Z")),
      localHour: new Date(at).getHours(),
    }));
  `;
  const out = execFileSync(process.execPath, ["--experimental-strip-types", "--input-type=module", "-e", script], {
    env: { ...process.env, TZ: tz },
    encoding: "utf8",
  });
  return JSON.parse(out.trim().split("\n").pop()!);
}

describe("formatters", () => {
  it("formats an instant as Vietnam time", () => {
    const at = "2026-10-16T07:05:00Z";
    assert.equal(formatDate(at), "16/10/2026");
    assert.equal(formatTime(at, "vi"), "14h05");
    assert.equal(formatTime(at, "en"), "14:05");
    assert.equal(formatDateTime(at), "14h05 - 16/10/2026");
    assert.equal(formatDateTime(at, "en"), "14:05 - 16/10/2026");
  });
  it("is empty for missing or invalid input", () => {
    for (const v of [null, undefined, "", "nope"]) {
      assert.equal(formatDate(v), "");
      assert.equal(formatDateTime(v), "");
      assert.equal(vnParts(v), null);
    }
  });
  it("shows midnight as 00, not 24", () => {
    assert.equal(formatTime("2026-10-16T17:00:00Z", "vi"), "00h00");
  });
  it("vnIntl pins the zone", () => {
    assert.equal(vnIntl({ month: "long" }).timeZone, TIME_ZONE);
  });
});

describe("whatever the process zone", () => {
  for (const tz of ["UTC", "America/New_York"]) {
    it(`gives Vietnam output under TZ=${tz}`, () => {
      const r = underZone(tz);
      assert.equal(r.date, "16/10/2026");
      assert.equal(r.time, "14h05");
      assert.equal(r.dateTime, "14h05 - 16/10/2026");
      assert.equal(r.before5pm, "2026-10-16");
      assert.equal(r.at5pm, "2026-10-17");
      assert.equal(r.daysAgo, 1);
    });
  }
  it("really ran in a different zone", () => {
    assert.equal(underZone("UTC").localHour, 7);
    assert.equal(underZone("America/New_York").localHour, 3);
  });
});

describe("vnDayKey / vnDaysAgo", () => {
  it("rolls the day over at 17:00 UTC", () => {
    assert.equal(vnDayKey("2026-10-16T16:59:59Z"), "2026-10-16");
    assert.equal(vnDayKey("2026-10-16T17:00:00Z"), "2026-10-17");
  });
  it("counts Vietnam calendar days", () => {
    const now = new Date("2026-10-17T03:00:00Z"); // 10:00 on 17/10 in Vietnam
    assert.equal(vnDaysAgo("2026-10-16T17:30:00Z", now), 0); // 00:30 same day
    assert.equal(vnDaysAgo("2026-10-16T16:30:00Z", now), 1); // 23:30 the day before
    assert.equal(vnDaysAgo("2026-10-10T03:00:00Z", now), 7);
    assert.equal(vnDaysAgo("2026-09-30T20:00:00Z", now), 16); // across a month end
  });
});
