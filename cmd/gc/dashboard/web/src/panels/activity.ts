import type {
  CityEventRecord,
  CityEventStreamEnvelope,
  SupervisorEventRecord,
  SupervisorEventStreamEnvelope,
} from "../api";
import { api, cityScope } from "../api";
import { logDebug } from "../logger";
import { byId, clear, el } from "../util/dom";
import {
  connectCityEvents,
  connectEvents,
  semanticEventType,
  type DashboardEventMessage,
  type SSEHandle,
} from "../sse";
import { eventCategory, eventIcon, eventSummary, extractRig, formatAgentAddress } from "../util/legacy";
import { relativeTime } from "../util/time";

export interface ActivityEntry {
  actor?: string;
  alert?: boolean;
  category: string;
  id: string;
  internal?: boolean;
  message?: string;
  rig: string;
  scope: string;
  seq: number;
  subject?: string;
  ts: string;
  type: string;
}

type DashboardEventRecord = CityEventRecord | SupervisorEventRecord | CityEventStreamEnvelope | SupervisorEventStreamEnvelope;

const MAX_ENTRIES = 150;
const HISTORY_LIMIT = 100;
const HISTORY_TAIL_WINDOW = 50;
const entries: ActivityEntry[] = [];
let handle: SSEHandle | null = null;
let categoryFilter = "all";
let rigFilter = "all";
let agentFilter = "all";
let showInternalActivity = false;
let streamAfterSeq = "";
let renderTimer: number | null = null;

export async function seedActivity(entriesFromAPI: ActivityEntry[]): Promise<void> {
  entries.splice(0, entries.length, ...normalizeEntries(entriesFromAPI));
  renderActivity();
}

export async function loadActivityHistory(): Promise<void> {
  const city = cityScope();
  const res = city
    ? await api.GET("/v0/city/{cityName}/events", {
        params: { path: { cityName: city }, query: { since: "1h", limit: HISTORY_LIMIT } },
      })
    : await api.GET("/v0/events", {
        params: { query: { since: "1h" } },
      });
  let normalized = (res.data?.items ?? [])
    .map((item) => toEntryFromRecord(item))
    .filter((item): item is ActivityEntry => item !== null);
  let streamCursor = eventIndexFromResponse(res.response) || latestSeq(normalized) || streamAfterSeq;

  if (city && streamCursor !== "" && normalized.length < HISTORY_TAIL_WINDOW) {
    const tail = await api.GET("/v0/events", {
      params: { query: { limit: HISTORY_TAIL_WINDOW } },
    });
    const tailEntries = (tail.data?.items ?? [])
      .map((item) => toEntryFromRecord(item))
      .filter((item): item is ActivityEntry => item !== null && item.scope === city);
    normalized = normalizeEntries([...normalized, ...tailEntries]);
    streamCursor = maxSeqString(streamCursor, latestSeq(tailEntries));
  }

  streamAfterSeq = streamCursor || streamAfterSeq;
  await seedActivity(normalized);
}

export function startActivityStream(
  onEvent?: (msg: DashboardEventMessage, eventType: string) => void,
  onStatus?: (status: import("../sse").SSEStatus) => void,
): void {
  const city = cityScope();
  handle?.close();
  const opts = onStatus ? { onStatus } : undefined;
  const connect = city
    ? (listener: (msg: DashboardEventMessage) => void) => connectCityEvents(city, listener, opts, streamAfterSeq)
    : (listener: (msg: DashboardEventMessage) => void) => connectEvents(listener, opts);
  handle = connect((msg) => {
    const eventType = eventTypeFromMessage(msg);
    onEvent?.(msg, eventType);
    const entry = toEntryFromMessage(msg);
    if (!entry) return;
    if (entries.some((current) => current.id === entry.id)) {
      logDebug("activity", "Duplicate stream event ignored", { id: entry.id, type: entry.type });
      return;
    }
    entries.splice(0, entries.length, ...normalizeEntries([entry, ...entries]));
    streamAfterSeq = latestSeq([entry]) || streamAfterSeq;
    scheduleRenderActivity();
  });
}

export function stopActivityStream(): void {
  handle?.close();
  handle = null;
}

export function renderActivity(): void {
  if (renderTimer !== null) {
    window.clearTimeout(renderTimer);
    renderTimer = null;
  }
  renderFilters();
  const feed = byId("activity-feed");
  if (!feed) return;
  clear(feed);

  const filtered = entries.filter((entry) => {
    if (!showInternalActivity && entry.internal) return false;
    if (categoryFilter !== "all" && entry.category !== categoryFilter) return false;
    if (rigFilter !== "all" && entry.rig !== rigFilter) return false;
    if (agentFilter !== "all" && entry.actor !== agentFilter) return false;
    return true;
  });
  byId("activity-count")!.textContent = String(filtered.length);

  if (filtered.length === 0) {
    feed.append(el("div", { class: "empty-state" }, [el("p", {}, ["No recent activity"])]));
    return;
  }

  const timeline = el("div", { class: "tl-timeline", id: "activity-timeline" });
  filtered.forEach((entry) => {
    timeline.append(el("div", {
      class: `tl-entry ${activityTypeClass(entry.category)}${entry.internal ? " activity-internal" : ""}${entry.alert ? " activity-alert" : ""}`,
      "data-category": entry.category,
      "data-internal": entry.internal ? "true" : "false",
      "data-rig": entry.rig,
      "data-agent": entry.actor ?? "",
      "data-type": entry.type,
      "data-ts": entry.ts,
    }, [
      el("div", { class: "tl-rail" }, [
        el("span", { class: "tl-time" }, [relativeTime(entry.ts)]),
        el("span", { class: "tl-node" }),
      ]),
      el("div", { class: "tl-content" }, [
        el("div", { class: "tl-header" }, [
          el("span", { class: "tl-icon" }, [eventIcon(entry.type)]),
          el("span", { class: "tl-summary" }, [eventSummary(entry.type, entry.actor, entry.subject, entry.message)]),
        ]),
        el("div", { class: "tl-meta" }, [
          entry.actor ? el("span", { class: "tl-badge tl-badge-agent" }, [formatAgentAddress(entry.actor)]) : null,
          entry.rig ? el("span", { class: "tl-badge tl-badge-rig" }, [entry.rig]) : null,
          entry.internal ? el("span", { class: `tl-badge ${entry.alert ? "tl-badge-alert" : "tl-badge-internal"}` }, ["internal"]) : null,
          el("span", { class: "tl-badge tl-badge-type" }, [entry.type]),
        ]),
      ]),
    ]));
  });
  feed.append(timeline);
}

function scheduleRenderActivity(): void {
  if (renderTimer !== null) return;
  renderTimer = window.setTimeout(() => {
    renderTimer = null;
    renderActivity();
  }, 250);
}

export function installActivityInteractions(): void {
  document.addEventListener("click", (event) => {
    const target = (event.target as HTMLElement | null)?.closest(".tl-filter-btn") as HTMLElement | null;
    if (!target) return;
    categoryFilter = target.dataset.value ?? "all";
    document.querySelectorAll(".tl-filter-btn").forEach((button) => button.classList.remove("active"));
    target.classList.add("active");
    renderActivity();
  });

  byId<HTMLSelectElement>("tl-rig-filter")?.addEventListener("change", (event) => {
    rigFilter = (event.currentTarget as HTMLSelectElement).value;
    renderActivity();
  });
  byId<HTMLSelectElement>("tl-agent-filter")?.addEventListener("change", (event) => {
    agentFilter = (event.currentTarget as HTMLSelectElement).value;
    renderActivity();
  });
}

function renderFilters(): void {
  const container = byId("activity-filters");
  if (!container) return;
  clear(container);
  if (entries.length === 0) return;
  const hiddenInternalCount = entries.filter((entry) => entry.internal).length;
  const alertCount = entries.filter((entry) => entry.alert).length;
  const rigs = [...new Set(entries.map((entry) => entry.rig).filter(Boolean))].sort();
  const agents = [...new Set(entries.map((entry) => entry.actor).filter(Boolean))].sort() as string[];

  const rigSelect = el("select", { class: "tl-filter-select", id: "tl-rig-filter" }) as HTMLSelectElement;
  rigSelect.append(el("option", { value: "all" }, ["All rigs"]));
  rigs.forEach((rig) => rigSelect.append(el("option", { value: rig, selected: rig === rigFilter }, [rig])));
  rigSelect.addEventListener("change", () => {
    rigFilter = rigSelect.value;
    renderActivity();
  });

  const agentSelect = el("select", { class: "tl-filter-select", id: "tl-agent-filter" }) as HTMLSelectElement;
  agentSelect.append(el("option", { value: "all" }, ["All agents"]));
  agents.forEach((agent) => agentSelect.append(el("option", { value: agent, selected: agent === agentFilter }, [formatAgentAddress(agent)])));
  agentSelect.addEventListener("change", () => {
    agentFilter = agentSelect.value;
    renderActivity();
  });

  const internalControl = hiddenInternalCount > 0 ? el("label", { class: "tl-internal-control" }, [
    el("input", {
      checked: showInternalActivity,
      id: "tl-internal-toggle",
      type: "checkbox",
    }),
    el("span", {}, ["Internal"]),
    el("span", { class: "tl-internal-count" }, [String(hiddenInternalCount)]),
    alertCount > 0 ? el("span", { class: "tl-internal-alert" }, [`${alertCount} alert`]) : null,
  ]) : null;
  internalControl?.querySelector<HTMLInputElement>("#tl-internal-toggle")?.addEventListener("change", (event) => {
    showInternalActivity = (event.currentTarget as HTMLInputElement).checked;
    renderActivity();
  });

  container.append(el("div", { class: "tl-filters" }, [
    el("div", { class: "tl-filter-group" }, [
      el("label", {}, ["Category:"]),
      filterButton("all", "All"),
      filterButton("agent", "Agent"),
      filterButton("work", "Work"),
      filterButton("comms", "Comms"),
      filterButton("system", "System"),
    ]),
    el("div", { class: "tl-filter-group" }, [el("label", {}, ["Rig:"]), rigSelect]),
    el("div", { class: "tl-filter-group" }, [el("label", {}, ["Agent:"]), agentSelect]),
    internalControl,
  ]));
}

function filterButton(value: string, label: string): HTMLElement {
  const btn = el("button", {
    class: `tl-filter-btn${categoryFilter === value ? " active" : ""}`,
    "data-filter": "category",
    "data-value": value,
    type: "button",
  }, [label]);
  btn.addEventListener("click", () => {
    categoryFilter = value;
    renderActivity();
  });
  return btn;
}

function toEntryFromMessage(msg: DashboardEventMessage): ActivityEntry | null {
  if (msg.event === "heartbeat") return null;
  return toActivityEntry(msg.data, msg.id);
}

function toEntryFromRecord(record: CityEventRecord | SupervisorEventRecord): ActivityEntry | null {
  return toActivityEntry(record);
}

function toActivityEntry(record: DashboardEventRecord, eventID?: string): ActivityEntry | null {
  if (!record.type) return null;
  const internal = isNoisyBeadActivity(record);
  const scope = recordCity(record) ?? cityScope();
  const seq = typeof record.seq === "number" ? record.seq : 0;
  return {
    alert: isInternalAlertActivity(record),
    id: stableEventID(record, eventID),
    type: record.type,
    category: eventCategory(record.type),
    internal,
    actor: record.actor || undefined,
    subject: record.subject || undefined,
    message: record.message || undefined,
    ts: record.ts,
    scope,
    seq,
    rig: extractRig(record.actor) || ("city" in record ? (record.city || "") : ""),
  };
}

export function isNoisyBeadActivity(record: DashboardEventRecord): boolean {
  if (!record.type.startsWith("bead.")) return false;
  if (isOrderTrackingBeadEvent(record)) return true;
  if (record.actor === "cache-reconcile") return true;

  const payload = beadPayload(eventPayload(record));
  const issueType = payloadString(payload, "issue_type") || payloadString(payload, "type");
  if (issueType === "session" || issueType === "message") return true;
  return payloadStringArray(payload, "labels").includes("gc:session");
}

export function isInternalAlertActivity(record: DashboardEventRecord): boolean {
  if (record.type !== "bead.closed" || record.actor !== "cache-reconcile") return false;
  const payload = beadPayload(eventPayload(record));
  const issueType = payloadString(payload, "issue_type") || payloadString(payload, "type");
  return issueType === "session" || payloadStringArray(payload, "labels").includes("gc:session");
}

function isOrderTrackingBeadEvent(record: DashboardEventRecord): boolean {
  if (!record.type.startsWith("bead.")) return false;
  return typeof record.message === "string" && record.message.startsWith("order:");
}

function eventPayload(record: DashboardEventRecord): unknown {
  return "payload" in record ? record.payload : undefined;
}

function beadPayload(payload: unknown): unknown {
  if (!isUnknownRecord(payload)) return payload;
  const bead = payload.bead;
  return isUnknownRecord(bead) ? bead : payload;
}

function isUnknownRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function payloadString(payload: unknown, key: string): string {
  if (!isUnknownRecord(payload)) return "";
  const value = payload[key];
  return typeof value === "string" ? value : "";
}

function payloadStringArray(payload: unknown, key: string): string[] {
  if (!isUnknownRecord(payload)) return [];
  const value = payload[key];
  if (!Array.isArray(value)) return [];
  return value.filter((item): item is string => typeof item === "string");
}

function normalizeEntries(nextEntries: ActivityEntry[]): ActivityEntry[] {
  const deduped = new Map<string, ActivityEntry>();
  nextEntries.forEach((entry) => {
    if (!deduped.has(entry.id)) {
      deduped.set(entry.id, entry);
    }
  });
  return [...deduped.values()]
    .sort(compareEntries)
    .slice(0, MAX_ENTRIES);
}

function eventIndexFromResponse(response: Response | undefined): string {
  const raw = response?.headers.get("X-Gc-Index") ?? "";
  return /^\d+$/.test(raw) && raw !== "0" ? raw : "";
}

function latestSeq(nextEntries: ActivityEntry[]): string {
  const latest = nextEntries.reduce((max, entry) => Math.max(max, entry.seq), 0);
  return latest > 0 ? String(latest) : "";
}

function maxSeqString(...seqs: string[]): string {
  const max = seqs.reduce((current, seq) => {
    const value = Number.parseInt(seq, 10);
    return Number.isFinite(value) ? Math.max(current, value) : current;
  }, 0);
  return max > 0 ? String(max) : "";
}

function compareEntries(a: ActivityEntry, b: ActivityEntry): number {
  const byTimestamp = compareTimestampDesc(a.ts, b.ts);
  if (byTimestamp !== 0) return byTimestamp;
  const byScope = a.scope.localeCompare(b.scope);
  if (byScope !== 0) return byScope;
  const bySeq = b.seq - a.seq;
  if (bySeq !== 0) return bySeq;
  const byType = a.type.localeCompare(b.type);
  if (byType !== 0) return byType;
  const byActor = (a.actor ?? "").localeCompare(b.actor ?? "");
  if (byActor !== 0) return byActor;
  return (a.subject ?? "").localeCompare(b.subject ?? "");
}

function compareTimestampDesc(a: string, b: string): number {
  const aTime = Number.isNaN(Date.parse(a)) ? 0 : Date.parse(a);
  const bTime = Number.isNaN(Date.parse(b)) ? 0 : Date.parse(b);
  return bTime - aTime;
}

function recordCity(record: DashboardEventRecord): string | undefined {
  if ("city" in record && typeof record.city === "string" && record.city !== "") {
    return record.city;
  }
  return undefined;
}

function stableEventID(record: DashboardEventRecord, eventID?: string): string {
  const scope = recordCity(record) ?? cityScope();
  if (typeof record.seq === "number" && record.seq > 0) {
    return `${scope}:${record.seq}`;
  }
  const fallback = [
    record.type,
    record.ts,
    record.actor ?? "",
    record.subject ?? "",
    record.message ?? "",
    eventID ?? "",
  ].join(":");
  return `${scope}:${fallback}`;
}

export function eventTypeFromMessage(msg: DashboardEventMessage): string {
  if (msg.event !== "heartbeat" && isOrderTrackingBeadEvent(msg.data)) return "";
  return semanticEventType(msg);
}

function activityTypeClass(category: string): string {
  switch (category) {
    case "agent":
      return "activity-agent";
    case "work":
      return "activity-work";
    case "comms":
      return "activity-comms";
    default:
      return "activity-system";
  }
}
