import { afterEach, beforeEach, describe, expect, it } from "vitest";

import {
  activityStreamCursorFromRecordsForTest,
  isInternalActivity,
  isInternalAlertActivity,
  isNoisyBeadActivity,
  isQuietOrderActivity,
  renderActivity,
  resetActivityFiltersForTest,
  seedActivity,
  type ActivityEntry,
} from "./activity";

describe("activity feed ordering", () => {
  beforeEach(() => {
    resetActivityFiltersForTest();
    document.body.innerHTML = `
      <div id="activity-filters"></div>
      <span id="activity-count"></span>
      <div id="activity-feed"></div>
    `;
  });

  afterEach(async () => {
    resetActivityFiltersForTest();
    await seedActivity([]);
  });

  it("dedupes repeated events and keeps newest entries first", async () => {
    const oldEntry: ActivityEntry = {
      category: "work",
      id: "mc-city:12",
      rig: "city",
      scope: "mc-city",
      seq: 12,
      ts: "2026-04-01T10:00:00Z",
      type: "bead.created",
    };
    const newerEntry: ActivityEntry = {
      category: "work",
      id: "mc-city:13",
      rig: "city",
      scope: "mc-city",
      seq: 13,
      ts: "2026-04-02T10:00:00Z",
      type: "bead.updated",
    };
    const sameTimestampDifferentScope: ActivityEntry = {
      category: "system",
      id: "alpha-city:9",
      rig: "city",
      scope: "alpha-city",
      seq: 9,
      ts: "2026-04-02T10:00:00Z",
      type: "city.updated",
    };

    await seedActivity([oldEntry, newerEntry, { ...oldEntry }, sameTimestampDifferentScope]);
    renderActivity();

    const ids = [...document.querySelectorAll<HTMLElement>(".tl-entry")].map((node) => node.dataset.ts);
    expect(ids).toEqual([
      "2026-04-02T10:00:00Z",
      "2026-04-02T10:00:00Z",
      "2026-04-01T10:00:00Z",
    ]);
    expect(document.querySelectorAll(".tl-entry")).toHaveLength(3);
    expect(document.getElementById("activity-count")?.textContent).toBe("3");
  });

  it("hides internal bead plumbing until the internal toggle is enabled", async () => {
    await seedActivity([
      {
        category: "work",
        id: "mc-city:1",
        rig: "city",
        scope: "mc-city",
        seq: 1,
        ts: "2026-04-02T10:00:00Z",
        type: "bead.updated",
      },
      {
        alert: true,
        category: "work",
        id: "mc-city:2",
        internal: true,
        rig: "city",
        scope: "mc-city",
        seq: 2,
        ts: "2026-04-02T10:01:00Z",
        type: "bead.closed",
      },
    ]);
    renderActivity();

    expect(document.querySelectorAll(".tl-entry")).toHaveLength(1);
    expect(document.getElementById("activity-count")?.textContent).toBe("1");
    expect(document.querySelector(".tl-internal-count")?.textContent).toBe("1");
    expect(document.querySelector(".tl-internal-alert")?.textContent).toBe("1 alert");

    const toggle = document.getElementById("tl-internal-toggle") as HTMLInputElement;
    toggle.checked = true;
    toggle.dispatchEvent(new Event("change", { bubbles: true }));

    expect(document.querySelectorAll(".tl-entry")).toHaveLength(2);
    expect(document.querySelectorAll(".activity-internal")).toHaveLength(1);
    expect(document.querySelectorAll(".activity-alert")).toHaveLength(1);
    expect(document.getElementById("activity-count")?.textContent).toBe("2");
  });

  it("classifies noisy bead payloads as internal activity", () => {
    const sessionClose = {
      actor: "cache-reconcile",
      payload: { bead: { issue_type: "session", labels: ["gc:session"] } },
      seq: 1,
      ts: "2026-04-02T10:00:00Z",
      type: "bead.closed",
    };
    const humanWork = {
      actor: "human",
      payload: { bead: { issue_type: "task", labels: [] } },
      seq: 2,
      ts: "2026-04-02T10:01:00Z",
      type: "bead.updated",
    };

    expect(isNoisyBeadActivity(sessionClose as any)).toBe(true);
    expect(isInternalAlertActivity(sessionClose as any)).toBe(true);
    expect(isNoisyBeadActivity(humanWork as any)).toBe(false);
    expect(isInternalAlertActivity(humanWork as any)).toBe(false);
  });

  it("classifies successful controller order events as internal activity", () => {
    const fired = {
      actor: "controller",
      seq: 1,
      subject: "dolt-health",
      ts: "2026-04-02T10:00:00Z",
      type: "order.fired",
    };
    const completed = {
      actor: "controller",
      seq: 2,
      subject: "dolt-health",
      ts: "2026-04-02T10:01:00Z",
      type: "order.completed",
    };
    const failed = {
      actor: "controller",
      seq: 3,
      subject: "dolt-health",
      ts: "2026-04-02T10:02:00Z",
      type: "order.failed",
    };

    expect(isQuietOrderActivity(fired as any)).toBe(true);
    expect(isInternalActivity(fired as any)).toBe(true);
    expect(isQuietOrderActivity(completed as any)).toBe(true);
    expect(isInternalActivity(completed as any)).toBe(true);
    expect(isQuietOrderActivity(failed as any)).toBe(false);
    expect(isInternalActivity(failed as any)).toBe(false);
  });

  it("hides routine orders behind the orders toggle while keeping failures visible", async () => {
    await seedActivity([
      {
        category: "work",
        id: "mc-city:1",
        internal: true,
        rig: "city",
        scope: "mc-city",
        seq: 1,
        subject: "dolt-health",
        ts: "2026-04-02T10:00:00Z",
        type: "order.fired",
      },
      {
        category: "work",
        id: "mc-city:2",
        internal: true,
        rig: "city",
        scope: "mc-city",
        seq: 2,
        subject: "dolt-health",
        ts: "2026-04-02T10:01:00Z",
        type: "order.completed",
      },
      {
        category: "work",
        id: "mc-city:3",
        internal: true,
        message: "order:dolt-health",
        rig: "city",
        scope: "mc-city",
        seq: 3,
        subject: "gc-order",
        ts: "2026-04-02T10:02:00Z",
        type: "bead.updated",
      },
      {
        category: "work",
        id: "mc-city:4",
        rig: "city",
        scope: "mc-city",
        seq: 4,
        subject: "dolt-health",
        ts: "2026-04-02T10:03:00Z",
        type: "order.failed",
      },
      {
        category: "work",
        id: "mc-city:5",
        rig: "city",
        scope: "mc-city",
        seq: 5,
        subject: "gc-human",
        ts: "2026-04-02T10:04:00Z",
        type: "bead.updated",
      },
    ]);
    renderActivity();

    expect(document.querySelectorAll(".tl-entry")).toHaveLength(2);
    expect(document.querySelectorAll('[data-type="order.failed"]')).toHaveLength(1);
    expect(document.querySelector(".tl-orders-count")?.textContent).toBe("3");
    expect(document.getElementById("tl-internal-toggle")).toBeNull();

    const toggle = document.getElementById("tl-orders-toggle") as HTMLInputElement;
    toggle.checked = true;
    toggle.dispatchEvent(new Event("change", { bubbles: true }));

    expect(document.querySelectorAll(".tl-entry")).toHaveLength(5);
    expect(document.querySelectorAll('[data-order="true"]')).toHaveLength(3);
    expect(document.getElementById("activity-count")?.textContent).toBe("5");
  });

  it("computes a city stream cursor from loaded history", () => {
    const cursor = activityStreamCursorFromRecordsForTest([
      { seq: 12, type: "bead.created", actor: "human", ts: "2026-04-01T10:00:00Z" },
      { seq: 19, type: "bead.updated", actor: "human", ts: "2026-04-01T10:01:00Z" },
      { seq: 15, type: "bead.closed", actor: "human", ts: "2026-04-01T10:02:00Z" },
    ] as any, "mc-city");

    expect(cursor).toEqual({ afterSeq: "19" });
  });

  it("computes a supervisor stream cursor from loaded history", () => {
    const cursor = activityStreamCursorFromRecordsForTest([
      { city: "beta", seq: 3, type: "bead.created", actor: "human", ts: "2026-04-01T10:00:00Z" },
      { city: "alpha", seq: 9, type: "bead.updated", actor: "human", ts: "2026-04-01T10:01:00Z" },
      { city: "beta", seq: 7, type: "bead.closed", actor: "human", ts: "2026-04-01T10:02:00Z" },
    ] as any, "");

    expect(cursor).toEqual({ afterCursor: "alpha:9,beta:7" });
  });
});
