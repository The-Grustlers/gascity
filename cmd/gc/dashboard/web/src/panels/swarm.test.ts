import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api, type BeadRecord, type SessionRecord } from "../api";
import { renderSwarm } from "./swarm";
import { openSessionCockpit } from "./session_cockpit";

vi.mock("../api", () => ({
  api: {
    GET: vi.fn(),
  },
  cityScope: vi.fn(() => "mc-city"),
}));

vi.mock("./session_cockpit", () => ({
  openSessionCockpit: vi.fn(),
}));

const getMock = api.GET as unknown as ReturnType<typeof vi.fn>;
const openCockpitMock = openSessionCockpit as unknown as ReturnType<typeof vi.fn>;

function installDOM(): void {
  document.body.innerHTML = `
    <span id="swarm-count"></span>
    <div id="swarm-list"></div>
  `;
}

function bead(overrides: Partial<BeadRecord>): BeadRecord {
  return {
    created_at: "2026-06-05T12:00:00Z",
    id: "gc-root",
    issue_type: "task",
    priority: 1,
    status: "open",
    title: "Root",
    ...overrides,
  };
}

function session(overrides: Partial<SessionRecord>): SessionRecord {
  return {
    id: "s-worker",
    running: true,
    state: "active",
    title: "project-worker",
    ...overrides,
  } as SessionRecord;
}

describe("swarm panel", () => {
  beforeEach(() => {
    getMock.mockReset();
    openCockpitMock.mockReset();
    installDOM();
  });

  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("renders root work first with blockers and attached sessions", async () => {
    const mission = bead({ id: "gc-mission", title: "Build Rabble item shop", priority: 3 });
    const partial = bead({ id: "gc-partial", title: "Needs graph" });
    const workflow = bead({
      id: "gc-workflow",
      issue_type: "molecule",
      status: "in_progress",
      title: "Ship live proof",
      metadata: { "gc.kind": "workflow" },
      assignee: "s-proof",
    });
    const ignoredMessage = bead({ id: "gc-mail", issue_type: "message", title: "Mail" });
    const ignoredChild = bead({
      id: "gc-child",
      title: "Child workflow bead",
      metadata: { "gc.root_bead_id": "gc-mission" },
    });
    const sessions = [
      session({ id: "s-worker", title: "project-worker", active_bead: "gc-child-1" }),
      session({ id: "s-proof", title: "infra-worker" }),
    ];

    getMock.mockImplementation(async (path: string, init?: { params?: { path?: { rootID?: string }; query?: { status?: string } } }) => {
      if (path === "/v0/city/{cityName}/beads") {
        const status = init?.params?.query?.status;
        return {
          data: { items: status === "open" ? [mission, partial, ignoredMessage, ignoredChild] : [workflow] },
          error: undefined,
          request: undefined,
          response: undefined,
        };
      }
      if (path === "/v0/city/{cityName}/sessions") {
        return { data: { items: sessions }, error: undefined, request: undefined, response: undefined };
      }
      if (path === "/v0/city/{cityName}/beads/graph/{rootID}") {
        const rootID = init?.params?.path?.rootID;
        if (rootID === "gc-mission") {
          return {
            data: {
              root: mission,
              beads: [
                mission,
                bead({ id: "gc-blocker", title: "Design source", status: "open" }),
                bead({ id: "gc-child-1", title: "Implement UI", status: "open", assignee: "s-worker" }),
                bead({ id: "gc-done", title: "Copy", status: "closed" }),
              ],
              deps: [{ from: "gc-blocker", to: "gc-child-1", kind: "blocks" }],
            },
            error: undefined,
            request: undefined,
            response: undefined,
          };
        }
        if (rootID === "gc-workflow") {
          return {
            data: { root: workflow, beads: [workflow], deps: [] },
            error: undefined,
            request: undefined,
            response: undefined,
          };
        }
        return {
          data: undefined,
          error: { detail: "graph unavailable" },
          request: undefined,
          response: undefined,
        };
      }
      throw new Error(`unexpected GET ${path}`);
    });

    await renderSwarm();

    expect(document.getElementById("swarm-count")?.textContent).toBe("3");
    expect(document.getElementById("swarm-list")?.textContent).toContain("Build Rabble item shop");
    expect(document.getElementById("swarm-list")?.textContent).toContain("1 blocked");
    expect(document.getElementById("swarm-list")?.textContent).toContain("project-worker");
    expect(document.getElementById("swarm-list")?.textContent).toContain("graph unavailable");
    expect(document.getElementById("swarm-list")?.textContent).not.toContain("Mail");
    expect(document.getElementById("swarm-list")?.textContent).not.toContain("Child workflow bead");

    document.querySelector<HTMLButtonElement>(".swarm-workers button")?.click();
    expect(openCockpitMock).toHaveBeenCalledWith("s-worker", "project-worker");
  });
});
