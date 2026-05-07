import { afterEach, beforeEach, describe, expect, it } from "vitest";

import type { CityEventRecord } from "../api";
import {
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

  it("hides internal bead plumbing from the human activity feed", () => {
    expect(isNoisyBeadActivity(eventRecord({
      actor: "cache-reconcile",
      payload: { bead: { issue_type: "task", labels: [] } },
    }))).toBe(true);
    expect(isNoisyBeadActivity(eventRecord({
      actor: "human",
      payload: { bead: { issue_type: "session", labels: ["gc:session"] } },
    }))).toBe(true);
    expect(isNoisyBeadActivity(eventRecord({
      actor: "human",
      payload: { bead: { issue_type: "message", labels: [] } },
    }))).toBe(true);
    expect(isNoisyBeadActivity(eventRecord({
      actor: "director",
      payload: { bead: { issue_type: "task", labels: ["customer-visible"] } },
    }))).toBe(false);
    expect(isInternalAlertActivity(eventRecord({
      actor: "cache-reconcile",
      payload: { bead: { issue_type: "session", labels: ["gc:session"] } },
      type: "bead.closed",
    }))).toBe(true);
  });

  it("keeps internal entries available behind the internal toggle", async () => {
    const visibleEntry: ActivityEntry = {
      category: "work",
      id: "mc-city:20",
      rig: "city",
      scope: "mc-city",
      seq: 20,
      ts: "2026-04-03T10:00:00Z",
      type: "bead.updated",
    };
    const internalEntry: ActivityEntry = {
      alert: true,
      category: "work",
      id: "mc-city:21",
      internal: true,
      rig: "city",
      scope: "mc-city",
      seq: 21,
      ts: "2026-04-03T10:01:00Z",
      type: "bead.closed",
    };

    await seedActivity([visibleEntry, internalEntry]);
    renderActivity();

    expect(document.querySelectorAll(".tl-entry")).toHaveLength(1);
    expect(document.getElementById("activity-count")?.textContent).toBe("1");
    expect(document.querySelector(".tl-internal-count")?.textContent).toBe("1");
    expect(document.querySelector(".tl-internal-alert")?.textContent).toBe("1 alert");

    document.querySelector<HTMLInputElement>("#tl-internal-toggle")?.click();

    expect(document.querySelectorAll(".tl-entry")).toHaveLength(2);
    expect(document.querySelectorAll(".activity-internal")).toHaveLength(1);
    expect(document.querySelectorAll(".activity-alert")).toHaveLength(1);
    expect(document.getElementById("activity-count")?.textContent).toBe("2");
  });
});

function eventRecord(overrides: Record<string, unknown>): CityEventRecord {
  return ({
    actor: "human",
    payload: { bead: { issue_type: "task", labels: [] } },
    seq: 1,
    subject: "gc-test",
    ts: "2026-05-07T12:00:00Z",
    type: "bead.updated",
    ...overrides,
  } as unknown) as CityEventRecord;
}
