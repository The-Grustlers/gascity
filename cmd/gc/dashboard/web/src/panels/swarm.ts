import type { BeadRecord, DashboardSchema, SessionRecord } from "../api";
import { api, cityScope } from "../api";
import { byId, clear, el } from "../util/dom";
import { beadPriority, priorityBadgeClass, truncate } from "../util/legacy";
import { openSessionCockpit } from "./session_cockpit";

type BeadGraphResponse = DashboardSchema["BeadGraphResponse"];
type WorkflowDepResponse = DashboardSchema["WorkflowDepResponse"];

interface SwarmRow {
  active: boolean;
  activeSessions: SessionRecord[];
  blockerCount: number;
  childCount: number;
  childStatusCounts: Record<string, number>;
  error?: string;
  partial: boolean;
  ready: boolean;
  root: BeadRecord;
}

const infraTypes = new Set(["agent", "gate", "message", "role", "rig", "session", "step", "wisp"]);

export async function renderSwarm(): Promise<void> {
  const city = cityScope();
  const container = byId("swarm-list");
  if (!container) return;
  if (!city) {
    resetSwarmNoCity();
    return;
  }

  const [openR, progressR, sessionsR] = await Promise.all([
    api.GET("/v0/city/{cityName}/beads", {
      params: { path: { cityName: city }, query: { status: "open", limit: 500 } },
    }),
    api.GET("/v0/city/{cityName}/beads", {
      params: { path: { cityName: city }, query: { status: "in_progress", limit: 500 } },
    }),
    api.GET("/v0/city/{cityName}/sessions", {
      params: { path: { cityName: city }, query: { state: "all" } },
    }),
  ]);

  if ((openR.error && progressR.error) || (!openR.data?.items && !progressR.data?.items)) {
    clear(container);
    byId("swarm-count")!.textContent = "0";
    container.append(el("div", { class: "panel-error" }, ["Could not load work graph."]));
    return;
  }

  const sessions = (sessionsR.data?.items ?? []) as SessionRecord[];
  const roots = selectRootWork([...(openR.data?.items ?? []), ...(progressR.data?.items ?? [])]);
  const rows = await Promise.all(roots.map((root) => buildSwarmRow(city, root, sessions)));
  rows.sort(compareSwarmRows);
  byId("swarm-count")!.textContent = String(rows.length);
  renderSwarmRows(container, rows);
}

export function resetSwarmNoCity(): void {
  const container = byId("swarm-list");
  if (!container) return;
  byId("swarm-count")!.textContent = "0";
  clear(container);
  container.append(el("div", { class: "empty-state" }, [el("p", {}, ["Select a city to view swarm work"])]));
}

function selectRootWork(beads: BeadRecord[]): BeadRecord[] {
  const roots = new Map<string, BeadRecord>();
  for (const bead of beads) {
    if (!isRootWork(bead)) continue;
    const id = bead.id ?? "";
    if (!id || roots.has(id)) continue;
    roots.set(id, bead);
  }
  return [...roots.values()];
}

function isRootWork(bead: BeadRecord): boolean {
  const status = (bead.status ?? "").toLowerCase();
  if (status !== "open" && status !== "in_progress") return false;
  const type = (bead.issue_type ?? "task").toLowerCase();
  if (infraTypes.has(type)) return false;
  const metadata = bead.metadata ?? {};
  if ((metadata["gc.root_bead_id"] ?? "").trim() !== "") return false;
  if ((bead.parent ?? "").trim() !== "") return false;
  return true;
}

async function buildSwarmRow(city: string, root: BeadRecord, sessions: SessionRecord[]): Promise<SwarmRow> {
  const rootID = root.id ?? "";
  const graphR = rootID
    ? await api.GET("/v0/city/{cityName}/beads/graph/{rootID}", {
      params: { path: { cityName: city, rootID } },
    })
    : { data: undefined, error: { detail: "missing root id" } };

  if (graphR.error || !graphR.data) {
    return rowFromGraph(root, null, sessions, graphR.error?.detail ?? "graph unavailable");
  }
  return rowFromGraph(root, graphR.data as BeadGraphResponse, sessions);
}

function rowFromGraph(root: BeadRecord, graph: BeadGraphResponse | null, sessions: SessionRecord[], error = ""): SwarmRow {
  const graphBeads = (graph?.beads ?? [root]).filter(Boolean) as BeadRecord[];
  const beadIDs = new Set(graphBeads.map((bead) => bead.id ?? "").filter(Boolean));
  const assignees = new Set(graphBeads.map((bead) => bead.assignee ?? "").filter(Boolean));
  const childStatusCounts: Record<string, number> = {};
  const unfinishedIDs = new Set<string>();

  for (const bead of graphBeads) {
    const id = bead.id ?? "";
    const status = (bead.status ?? "unknown").toLowerCase();
    if (id && status !== "closed") unfinishedIDs.add(id);
    if (id === root.id) continue;
    childStatusCounts[status] = (childStatusCounts[status] ?? 0) + 1;
  }

  const blockerCount = countBlockers(graph?.deps ?? [], unfinishedIDs);
  const activeSessions = sessions.filter((session) => sessionAttachedToWork(session, beadIDs, assignees));
  const rootStatus = (root.status ?? "").toLowerCase();
  const active = rootStatus === "in_progress" || Boolean(root.assignee) || activeSessions.length > 0;
  return {
    active,
    activeSessions,
    blockerCount,
    childCount: Math.max(beadIDs.size - (root.id && beadIDs.has(root.id) ? 1 : 0), 0),
    childStatusCounts,
    error,
    partial: error !== "",
    ready: rootStatus === "open" && !root.assignee && blockerCount === 0,
    root,
  };
}

function countBlockers(deps: WorkflowDepResponse[], unfinishedIDs: Set<string>): number {
  return deps.filter((dep) => (dep.kind ?? "").toLowerCase() === "blocks" && unfinishedIDs.has(dep.to)).length;
}

function sessionAttachedToWork(session: SessionRecord, beadIDs: Set<string>, assignees: Set<string>): boolean {
  const activeBead = session.active_bead ?? "";
  if (activeBead && beadIDs.has(activeBead)) return true;
  for (const id of sessionIdentifiers(session)) {
    if (assignees.has(id)) return true;
  }
  return false;
}

function sessionIdentifiers(session: SessionRecord): string[] {
  return [
    session.id,
    session.alias,
    session.title,
    session.template,
    session.session_name,
    session.display_name,
  ].filter((value): value is string => Boolean(value));
}

function renderSwarmRows(container: HTMLElement, rows: SwarmRow[]): void {
  clear(container);
  if (rows.length === 0) {
    container.append(el("div", { class: "empty-state" }, [el("p", {}, ["No root work beads"])]));
    return;
  }

  const tbody = el("tbody");
  rows.forEach((row) => {
    tbody.append(el("tr", { class: `swarm-row${row.partial ? " partial" : ""}` }, [
      el("td", {}, [el("span", { class: `badge ${priorityBadgeClass(row.root.priority)}` }, [`P${beadPriority(row.root.priority)}`])]),
      el("td", { class: "swarm-root" }, [
        el("span", { class: "issue-id" }, [row.root.id ?? ""]),
        el("div", { class: "swarm-title" }, [truncate(row.root.title ?? row.root.id ?? "Mission", 96)]),
        row.partial ? el("div", { class: "swarm-warning" }, [row.error ?? "partial graph"]) : null,
      ]),
      el("td", {}, [swarmStateBadge(row)]),
      el("td", {}, [swarmProgress(row)]),
      el("td", { class: "swarm-workers" }, workerButtons(row.activeSessions)),
    ]));
  });

  container.append(el("table", { class: "swarm-table" }, [
    el("thead", {}, [el("tr", {}, [
      el("th", {}, ["Priority"]),
      el("th", {}, ["Mission"]),
      el("th", {}, ["State"]),
      el("th", {}, ["Graph"]),
      el("th", {}, ["Workers"]),
    ])]),
    tbody,
  ]));
}

function swarmStateBadge(row: SwarmRow): HTMLElement {
  if (row.blockerCount > 0) return el("span", { class: "badge badge-orange" }, [`${row.blockerCount} blocked`]);
  if (row.active) return el("span", { class: "badge badge-blue" }, ["active"]);
  if (row.ready) return el("span", { class: "badge badge-green" }, ["ready"]);
  return el("span", { class: "badge" }, [row.root.status ?? "open"]);
}

function swarmProgress(row: SwarmRow): HTMLElement {
  const counts = row.childStatusCounts;
  const parts = [
    `${row.childCount} child${row.childCount === 1 ? "" : "ren"}`,
    counts.open ? `${counts.open} open` : "",
    counts.in_progress ? `${counts.in_progress} active` : "",
    counts.closed ? `${counts.closed} closed` : "",
  ].filter(Boolean);
  return el("span", {}, [parts.join(" / ") || "root only"]);
}

function workerButtons(sessions: SessionRecord[]): (HTMLElement | string)[] {
  if (sessions.length === 0) return ["-"];
  const buttons = sessions.slice(0, 4).flatMap((session, index) => {
    const sessionID = session.id ?? "";
    if (!sessionID) return [];
    const label = sessionLabel(session);
    const button = el("button", { class: "agent-log-link", type: "button", "data-session-id": sessionID, title: label }, [label]);
    button.addEventListener("click", () => {
      void openSessionCockpit(sessionID, label);
    });
    return index === 0 ? [button] : [" ", button];
  });
  return buttons.length > 0 ? buttons : ["-"];
}

function sessionLabel(session: SessionRecord): string {
  return session.alias ?? session.title ?? session.template ?? session.id;
}

function compareSwarmRows(left: SwarmRow, right: SwarmRow): number {
  return (
    Number(right.active) - Number(left.active) ||
    Number(right.blockerCount > 0) - Number(left.blockerCount > 0) ||
    beadPriority(right.root.priority) - beadPriority(left.root.priority) ||
    (left.root.title ?? left.root.id ?? "").localeCompare(right.root.title ?? right.root.id ?? "")
  );
}
