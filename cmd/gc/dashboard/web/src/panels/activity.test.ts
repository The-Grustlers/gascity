import { afterEach, beforeEach, describe, expect, it } from "vitest";

import {
  activityStreamCursorFromRecordsForTest,
  isInternalActivity,
  isInternalAlertActivity,
  isNoisyBeadActivity,
  renderActivity,
  seedActivity,
  type ActivityEntry,
} from "./activity";

describe("activity feed ordering", () => {
  beforeEach(() => {
    document.body.innerHTML = `
      <div id="activity-filters"></div>
      <span id="activity-count"></span>
      <div id="activity-feed"></div>
    `;
  });

  afterEach(async () => {
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
    expect(isInternalActivity({
      actor: "controller",
      seq: 1,
      subject: "dolt-health",
      ts: "2026-04-02T10:00:00Z",
      type: "order.fired",
    } as any)).toBe(true);
    expect(isInternalActivity({
      actor: "controller",
      seq: 2,
      subject: "dolt-health",
      ts: "2026-04-02T10:01:00Z",
      type: "order.completed",
    } as any)).toBe(true);
    expect(isInternalActivity({
      actor: "controller",
      seq: 3,
      subject: "dolt-health",
      ts: "2026-04-02T10:02:00Z",
      type: "order.failed",
    } as any)).toBe(false);
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
