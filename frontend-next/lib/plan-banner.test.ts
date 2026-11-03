import { test } from "node:test";
import assert from "node:assert/strict";
import { lastUsableDay, planBanner, vnDay, type PlanBannerInput } from "./plan-banner.ts";

const NOW = new Date("2026-11-02T03:00:00Z");
const DAY = 24 * 60 * 60 * 1000;

function ent(over: Record<string, unknown> = {}) {
  return {
    source: "user" as const,
    subscription_id: "sub1",
    plan_id: "plan1",
    plan_name: "Go",
    mode: "pool",
    has_seat: true,
    ends_at: new Date(NOW.getTime() + 30 * DAY).toISOString(),
    expired: false,
    limits: [{ metric: "tokens", period: "month", limit: 1000, used: 100, remaining: 900, exhausted: false }],
    ...over,
  };
}

function input(over: Partial<PlanBannerInput> = {}): PlanBannerInput {
  return {
    now: NOW,
    isSystemAdmin: false,
    isTenantAdmin: false,
    enforced: true,
    entitlement: ent(),
    userCreatedAt: "2026-01-01T00:00:00Z",
    dismissed: () => null,
    ...over,
  };
}

test("no banner for a healthy, older account", () => {
  assert.equal(planBanner(input()), null);
});

test("SuperAdmin never sees a banner", () => {
  assert.equal(planBanner(input({ isSystemAdmin: true, entitlement: ent({ expired: true }) })), null);
});

test("expired package asks to renew it", () => {
  const b = planBanner(input({ entitlement: ent({ expired: true }) }));
  assert.equal(b?.kind, "expired");
  assert.equal(b?.tone, "error");
  assert.equal(b?.canBuy, true);
  assert.equal(b?.planId, "plan1");
});

test("expiring within 7 days counts usable days, today included", () => {
  // NOW = 02/11 10:00 VN; ends_at 06/11 00:00 VN → usable 02,03,04,05/11
  const b = planBanner(input({ entitlement: ent({ ends_at: "2026-11-05T17:00:00Z" }) }));
  assert.equal(b?.kind, "expiring");
  assert.equal(b?.daysLeft, 4);
  const today = planBanner(input({ entitlement: ent({ ends_at: "2026-11-02T17:00:00Z" }) }));
  assert.equal(today?.daysLeft, 1);
  assert.equal(planBanner(input({ entitlement: ent({ ends_at: new Date(NOW.getTime() + 8 * DAY).toISOString() }) })), null);
  assert.equal(planBanner(input({ entitlement: ent({ ends_at: null }) })), null);
});

test("exhausted limits: month is an error, day a warning", () => {
  const month = planBanner(input({ entitlement: ent({ limits: [{ metric: "tokens", period: "month", limit: 10, used: 10, remaining: 0, exhausted: true }] }) }));
  assert.equal(month?.kind, "exhausted_month");
  assert.equal(month?.tone, "error");
  const day = planBanner(input({ entitlement: ent({ limits: [{ metric: "questions", period: "day", limit: 10, used: 10, remaining: 0, exhausted: true }] }) }));
  assert.equal(day?.kind, "exhausted_day");
  assert.equal(day?.limit?.limit, 10);
});

test("near cap only for month limits at 90 %", () => {
  const near = planBanner(input({ entitlement: ent({ limits: [{ metric: "tokens", period: "month", limit: 100, used: 93, remaining: 7, exhausted: false }] }) }));
  assert.equal(near?.kind, "near_cap");
  assert.equal(near?.percent, 93);
  assert.equal(planBanner(input({ entitlement: ent({ limits: [{ metric: "questions", period: "day", limit: 10, used: 9, remaining: 1, exhausted: false }] }) })), null);
});

test("Tổ chức package: members are told to ask an admin, admins can renew", () => {
  const e = ent({ source: "tenant", expired: true });
  assert.equal(planBanner(input({ entitlement: e }))?.canBuy, false);
  const admin = planBanner(input({ entitlement: e, isTenantAdmin: true }));
  assert.equal(admin?.canBuy, true);
  assert.equal(admin?.scope, "tenant");
});

test("lapsed Tổ chức package behind the admin's own package still asks the admin to renew", () => {
  const lapsed = { subscription_id: "sub-t", plan_id: "plan-t", plan_name: "Tổ chức", ends_at: "2026-11-01T17:00:00Z" };
  const admin = planBanner(input({ isTenantAdmin: true, entitlement: ent({ lapsed_tenant: lapsed }) }));
  assert.equal(admin?.kind, "expired");
  assert.equal(admin?.scope, "tenant");
  assert.equal(admin?.planId, "plan-t");
  assert.equal(admin?.planName, "Tổ chức");
  assert.equal(admin?.dismissKey, "expired:sub-t");
  // a member just uses their own package: nothing to say
  assert.equal(planBanner(input({ entitlement: ent({ lapsed_tenant: lapsed }) })), null);
});

test("a package bought ahead says when it starts, with nothing to buy", () => {
  const b = planBanner(input({ entitlement: ent({ expired: true, not_started: true, starts_at: "2026-11-09T17:00:00Z" }) }));
  assert.equal(b?.kind, "not_started");
  assert.equal(b?.canBuy, false);
  assert.equal(b?.startsAt, "2026-11-09T17:00:00Z");
  assert.equal(b?.dismissValue, "forever");
  // a Tổ chức package that has not started yet asks the admin for nothing
  const lapsed = { subscription_id: "sub-t", plan_id: "plan-t", plan_name: "Tổ chức", not_started: true };
  assert.equal(planBanner(input({ isTenantAdmin: true, entitlement: ent({ lapsed_tenant: lapsed }) })), null);
});

test("seat package without a seat", () => {
  assert.equal(planBanner(input({ entitlement: ent({ source: "tenant", mode: "seat", has_seat: false }) }))?.kind, "no_seat");
});

test("no package while enforced offers a personal one", () => {
  const b = planBanner(input({ entitlement: null }));
  assert.equal(b?.kind, "no_plan");
  assert.equal(b?.canBuy, true);
  assert.equal(b?.scope, "user");
  assert.equal(planBanner(input({ entitlement: null, enforced: false })), null);
});

test("welcome for a new account, once, never for a covered member", () => {
  const fresh = { userCreatedAt: new Date(NOW.getTime() - DAY).toISOString() };
  const b = planBanner(input(fresh));
  assert.equal(b?.kind, "welcome");
  assert.equal(b?.dismissKey, "welcome");
  assert.equal(b?.dismissValue, "forever");
  assert.equal(planBanner(input({ ...fresh, dismissed: (k) => (k === "welcome" ? "forever" : null) })), null);
  assert.equal(planBanner(input({ ...fresh, entitlement: ent({ source: "tenant" }) })), null);
  assert.equal(planBanner(input({ userCreatedAt: new Date(NOW.getTime() - 15 * DAY).toISOString() })), null);
});

test("urgent banners win over welcome and are dismissed per day", () => {
  const fresh = new Date(NOW.getTime() - DAY).toISOString();
  const b = planBanner(input({ userCreatedAt: fresh, entitlement: ent({ expired: true }) }));
  assert.equal(b?.kind, "expired");
  assert.equal(b?.dismissKey, "expired:sub1");
  assert.equal(b?.dismissValue, vnDay(NOW));
  // dismissing the urgent one today does not bring the welcome back in its place
  const today = (k: string) => (k === "expired:sub1" ? vnDay(NOW) : null);
  assert.equal(planBanner(input({ userCreatedAt: fresh, entitlement: ent({ expired: true }), dismissed: today })), null);
  // ...and it is back the next day
  const yesterday = (k: string) => (k === "expired:sub1" ? "2026-11-01" : null);
  assert.equal(planBanner(input({ entitlement: ent({ expired: true }), dismissed: yesterday }))?.kind, "expired");
});

test("lastUsableDay is the day before the stored VN midnight", () => {
  assert.equal(vnDay(lastUsableDay("2026-12-02T17:00:00Z")), "2026-12-02");
});

test("vnDay follows the Vietnam calendar", () => {
  assert.equal(vnDay(new Date("2026-11-01T17:30:00Z")), "2026-11-02");
  assert.equal(vnDay(new Date("2026-11-01T16:30:00Z")), "2026-11-01");
});

test("a Tổ chức admin without a Tổ chức package is sent to buy for the Tổ chức", () => {
  const none = planBanner(input({ isTenantAdmin: true, hasTenant: true, entitlement: null }));
  assert.equal(none?.kind, "no_plan");
  assert.equal(none?.scope, "tenant");
  // their own (gift) package standing in: renewing it would not cover the members
  const gift = planBanner(input({ isTenantAdmin: true, hasTenant: true, entitlement: ent({ expired: true }) }));
  assert.equal(gift?.kind, "expired");
  assert.equal(gift?.scope, "tenant");
  assert.equal(gift?.planId, "");
  // a tenantless person or a member keeps the personal scope
  assert.equal(planBanner(input({ entitlement: null }))?.scope, "user");
  assert.equal(planBanner(input({ isTenantAdmin: true, hasTenant: false, entitlement: null }))?.scope, "user");
});
