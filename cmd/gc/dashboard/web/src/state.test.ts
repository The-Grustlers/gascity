import { beforeEach, describe, expect, it, vi } from "vitest";

describe("dashboard state invalidation", () => {
  beforeEach(() => {
    vi.resetModules();
    window.history.pushState({}, "", "/dashboard?city=mc-city");
  });

  it("keeps city bead refresh scoped to status, swarm, and issues", async () => {
    const { consumeInvalidated, invalidateForEventType } = await import("./state");
    consumeInvalidated();

    invalidateForEventType("bead.updated");

    expect([...consumeInvalidated()].sort()).toEqual(["issues", "status", "swarm"]);
  });

  it("does not refresh supervisor resources for city-scoped bead events", async () => {
    window.history.pushState({}, "", "/dashboard");
    const { consumeInvalidated, invalidateForEventType, syncCityScopeFromLocation } = await import("./state");
    syncCityScopeFromLocation();
    consumeInvalidated();

    expect(invalidateForEventType("bead.updated")).toBe(false);

    expect([...consumeInvalidated()]).toEqual([]);
  });

  it("keeps session refresh scoped to status, swarm, crew, and options", async () => {
    const { consumeInvalidated, invalidateForEventType } = await import("./state");
    consumeInvalidated();

    invalidateForEventType("session.updated");

    expect([...consumeInvalidated()].sort()).toEqual(["crew", "options", "status", "swarm"]);
  });
});
