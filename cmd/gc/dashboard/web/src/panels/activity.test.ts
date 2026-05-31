import { afterEach, beforeEach, describe, expect, it } from "vitest";

import {
  activityStreamCursorFromRecordsForTest,
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

  it("hides routine order tracking chatter unless enabled", async () => {
    await seedActivity([
      {
        category: "work",
        id: "mc-city:20",
        rig: "city",
        scope: "mc-city",
        seq: 20,
        subject: "gr-wisp-abcde",
        ts: "2026-04-02T10:00:00Z",
        type: "bead.updated",
      },
      {
        category: "system",
        id: "mc-city:21",
        message: "beads-health",
        rig: "city",
        scope: "mc-city",
        seq: 21,
        subject: "beads-health",
        ts: "2026-04-02T10:01:00Z",
        type: "order.completed",
      },
      {
        category: "system",
        id: "mc-city:22",
        message: "dolt-remotes-patrol failed",
        rig: "city",
        scope: "mc-city",
        seq: 22,
        subject: "dolt-remotes-patrol",
        ts: "2026-04-02T10:02:00Z",
        type: "order.failed",
      },
      {
        category: "work",
        id: "mc-city:23",
        rig: "city",
        scope: "mc-city",
        seq: 23,
        subject: "gr-real",
        ts: "2026-04-02T10:03:00Z",
        type: "bead.updated",
      },
      {
        category: "work",
        id: "mc-city:24",
        message: "manual deploy completed",
        rig: "city",
        scope: "mc-city",
        seq: 24,
        subject: "manual-deploy",
        ts: "2026-04-02T10:04:00Z",
        type: "order.completed",
      },
    ]);

    expect([...document.querySelectorAll<HTMLElement>(".tl-entry")].map((node) => node.dataset.type)).toEqual([
      "order.completed",
      "bead.updated",
      "order.failed",
    ]);
    expect(document.getElementById("activity-count")?.textContent).toBe("3");

    const toggle = document.getElementById("tl-routine-filter") as HTMLInputElement;
    toggle.checked = true;
    toggle.dispatchEvent(new Event("change", { bubbles: true }));

    expect(document.querySelectorAll(".tl-entry")).toHaveLength(5);
    expect(document.getElementById("activity-count")?.textContent).toBe("5");

    const resetToggle = document.getElementById("tl-routine-filter") as HTMLInputElement;
    resetToggle.checked = false;
    resetToggle.dispatchEvent(new Event("change", { bubbles: true }));
  });

  it("computes a city stream cursor from loaded history", () => {
    const cursor = activityStreamCursorFromRecordsForTest([
      { seq: 12, type: "bead.created", actor: "human", ts: "2026-04-01T10:00:00Z" },
      { seq: 19, type: "bead.updated", actor: "human", ts: "2026-04-01T10:01:00Z" },
      { seq: 15, type: "bead.closed", actor: "human", ts: "2026-04-01T10:02:00Z" },
    ] as any, "mc-city");

    expect(cursor).toEqual({ afterSeq: "19" });
  });

  it("resumes the supervisor stream from the history response cursor", () => {
    const cursor = activityStreamCursorFromRecordsForTest([
      { city: "beta", seq: 3, type: "bead.created", actor: "human", ts: "2026-04-01T10:00:00Z" },
      { city: "alpha", seq: 9, type: "bead.updated", actor: "human", ts: "2026-04-01T10:01:00Z" },
      { city: "beta", seq: 7, type: "bead.closed", actor: "human", ts: "2026-04-01T10:02:00Z" },
    ] as any, "", "alpha:12,beta:8");

    expect(cursor).toEqual({ afterCursor: "alpha:12,beta:8" });
  });
});
