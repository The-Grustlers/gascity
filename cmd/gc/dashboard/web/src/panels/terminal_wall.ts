import "@xterm/xterm/css/xterm.css";

import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";

import type { SessionRecord } from "../api";
import { api, cityScope, supervisorBaseURL } from "../api";
import { byId, clear, el } from "../util/dom";
import { reportUIError } from "../ui";

type TerminalPane = {
  decoder: TextDecoder;
  fit: FitAddon;
  inputDisposable: { dispose(): void };
  root: HTMLElement;
  sessionID: string;
  socket: WebSocket;
  status: HTMLElement;
  terminalError: boolean;
  term: Terminal;
};

type TerminalControlFrame = {
  data?: unknown;
  type: string;
};

const panes = new Map<string, TerminalPane>();
let open = false;

export function installTerminalWallInteractions(): void {
  byId<HTMLButtonElement>("toggle-terminal-wall-btn")?.addEventListener("click", () => {
    void toggleTerminalWall();
  });
  byId<HTMLButtonElement>("terminal-wall-refresh-btn")?.addEventListener("click", () => {
    void renderTerminalWall();
  });
  byId<HTMLButtonElement>("terminal-wall-close-btn")?.addEventListener("click", () => {
    closeTerminalWallExternal();
  });
  window.addEventListener("resize", () => {
    for (const pane of panes.values()) {
      fitAndResize(pane);
    }
  });
}

export function syncTerminalWallControl(hasCity: boolean): void {
  const button = byId<HTMLButtonElement>("toggle-terminal-wall-btn");
  if (!button) return;
  button.disabled = !hasCity;
  button.title = hasCity ? "Open browser terminal view" : "Select a city to open terminal view";
}

export async function renderTerminalWall(): Promise<void> {
  if (!open) return;
  const panel = byId("terminal-wall-panel");
  const grid = byId("terminal-wall-grid");
  const count = byId("terminal-wall-count");
  const status = byId("terminal-wall-status");
  const city = cityScope();
  if (!panel || !grid || !count || !status || !city) return;

  panel.style.display = "block";
  status.textContent = "Loading terminal sessions...";
  const { data, error } = await api.GET("/v0/city/{cityName}/sessions", {
    params: { path: { cityName: city }, query: { peek: true } },
  });
  if (error || !data?.items) {
    status.textContent = "Failed to load sessions.";
    closeMissingPanes(new Set());
    return;
  }

  const running = data.items.filter((session) => session.running);
  count.textContent = String(running.length);
  status.textContent = running.length === 0
    ? "No running sessions to attach."
    : `Showing ${running.length} active terminal pane(s).`;

  const nextIDs = new Set(running.map((session) => session.id));
  closeMissingPanes(nextIDs);
  if (running.length === 0) {
    clear(grid);
    grid.append(el("div", { class: "empty-state terminal-wall-empty" }, [
      el("p", {}, ["No running sessions."]),
    ]));
    return;
  }

  clear(grid);
  for (const session of running) {
    const pane = panes.get(session.id) ?? createPane(city, session);
    panes.set(session.id, pane);
    grid.append(pane.root);
    fitAndResize(pane);
  }
}

export function closeTerminalWallExternal(): void {
  open = false;
  closeMissingPanes(new Set());
  const panel = byId("terminal-wall-panel");
  if (panel) panel.style.display = "none";
  const count = byId("terminal-wall-count");
  if (count) count.textContent = "0";
  const status = byId("terminal-wall-status");
  if (status) status.textContent = "No terminal sessions attached.";
}

async function toggleTerminalWall(): Promise<void> {
  if (open) {
    closeTerminalWallExternal();
    return;
  }
  open = true;
  await renderTerminalWall();
}

function createPane(city: string, session: SessionRecord): TerminalPane {
  const root = el("article", { class: "terminal-pane", "data-session-id": session.id });
  const title = session.display_name || session.title || session.template || session.id;
  const meta = [session.rig || "city", session.pool, session.model].filter(Boolean).join(" / ");
  const status = el("span", { class: "badge badge-muted" }, ["Connecting"]);
  const header = el("div", { class: "terminal-pane-header" }, [
    el("div", {}, [
      el("h3", {}, [title]),
      el("p", {}, [meta || session.id]),
    ]),
    status,
  ]);
  const mount = el("div", { class: "terminal-pane-screen" });

  const term = new Terminal({
    convertEol: true,
    cursorBlink: true,
    disableStdin: false,
    fontFamily: "'SF Mono', Menlo, Monaco, Consolas, monospace",
    fontSize: 12,
    scrollback: 2000,
    theme: {
      background: "#06090d",
      black: "#0f1419",
      blue: "#59c2ff",
      cyan: "#95e6cb",
      foreground: "#e6e1cf",
      green: "#c2d94c",
      magenta: "#d2a6ff",
      red: "#f07178",
      white: "#e6e1cf",
      yellow: "#ffb454",
    },
  });
  const fit = new FitAddon();
  term.loadAddon(fit);
  root.append(header, mount);
  term.open(mount);
  term.writeln(`Gas City terminal: ${title}`);
  term.writeln(`session ${session.id}`);
  term.writeln("");

  const socket = new WebSocket(terminalWebSocketURL(city, session.id));
  socket.binaryType = "arraybuffer";
  const pane: TerminalPane = {
    decoder: new TextDecoder(),
    fit,
    inputDisposable: { dispose() {} },
    root,
    sessionID: session.id,
    socket,
    status,
    terminalError: false,
    term,
  };
  pane.inputDisposable = term.onData((data) => {
    sendTerminalInput(pane, data);
  });

  socket.addEventListener("open", () => {
    setPaneStatus(pane, "Attached", "badge-green");
    sendTerminalResize(pane);
  });
  socket.addEventListener("message", (event) => {
    void handleTerminalMessage(pane, event.data as unknown);
  });
  socket.addEventListener("close", () => {
    if (!panes.has(pane.sessionID)) return;
    if (pane.terminalError) return;
    setPaneStatus(pane, "Closed", "badge-muted");
  });
  socket.addEventListener("error", (event) => {
    pane.terminalError = true;
    setPaneStatus(pane, "Error", "badge-red");
    reportUIError("Terminal websocket error", event);
  });

  return pane;
}

function terminalWebSocketURL(city: string, sessionID: string): string {
  const base = supervisorBaseURL() || window.location.origin;
  const url = new URL(
    `/v0/city/${encodeURIComponent(city)}/session/${encodeURIComponent(sessionID)}/terminal`,
    base,
  );
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  return url.toString();
}

function fitAndResize(pane: TerminalPane): void {
  pane.fit.fit();
  sendTerminalResize(pane);
}

function sendTerminalInput(pane: TerminalPane, data: string): void {
  sendTerminalFrame(pane, { type: "input", data });
}

function sendTerminalResize(pane: TerminalPane): void {
  const cols = Number.isFinite(pane.term.cols) ? pane.term.cols : 80;
  const rows = Number.isFinite(pane.term.rows) ? pane.term.rows : 24;
  sendTerminalFrame(pane, { type: "resize", cols, rows });
}

function sendTerminalFrame(pane: TerminalPane, frame: Record<string, unknown>): void {
  if (pane.socket.readyState !== WebSocket.OPEN) return;
  pane.socket.send(JSON.stringify(frame));
}

async function handleTerminalMessage(pane: TerminalPane, data: unknown): Promise<void> {
  if (typeof data === "string") {
    const control = parseTerminalControl(data);
    if (control) {
      handleTerminalControl(pane, control);
      return;
    }
    pane.term.write(data);
    return;
  }
  if (isArrayBufferLike(data)) {
    writeTerminalBytes(pane, data);
    return;
  }
  if (data instanceof Blob) {
    try {
      writeTerminalBytes(pane, await data.arrayBuffer());
    } catch (error) {
      reportUIError("Terminal blob decode failed", error);
    }
  }
}

function handleTerminalControl(pane: TerminalPane, frame: TerminalControlFrame): void {
  if (frame.type === "ready") {
    setPaneStatus(pane, "Attached", "badge-green");
    return;
  }
  if (frame.type === "error") {
    pane.terminalError = true;
    setPaneStatus(pane, "Error", "badge-red");
    const detail = typeof frame.data === "string" ? frame.data : "terminal error";
    pane.term.writeln(`\x1b[31m${detail}\x1b[0m`);
  }
}

function parseTerminalControl(data: string): TerminalControlFrame | null {
  try {
    const parsed = JSON.parse(data) as unknown;
    if (!isRecord(parsed) || typeof parsed.type !== "string") return null;
    if (parsed.type !== "ready" && parsed.type !== "error") return null;
    return { type: parsed.type, data: parsed.data };
  } catch {
    return null;
  }
}

function writeTerminalBytes(pane: TerminalPane, data: ArrayBuffer): void {
  const text = pane.decoder.decode(new Uint8Array(data), { stream: true });
  if (text) pane.term.write(text);
}

function isArrayBufferLike(value: unknown): value is ArrayBuffer {
  return value instanceof ArrayBuffer || Object.prototype.toString.call(value) === "[object ArrayBuffer]";
}

function setPaneStatus(pane: TerminalPane, label: string, className: string): void {
  pane.status.className = `badge ${className}`;
  pane.status.textContent = label;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function closeMissingPanes(nextIDs: Set<string>): void {
  for (const [id, pane] of panes) {
    if (nextIDs.has(id)) continue;
    pane.inputDisposable.dispose();
    if (pane.socket.readyState === WebSocket.CONNECTING || pane.socket.readyState === WebSocket.OPEN) {
      pane.socket.close();
    }
    pane.term.dispose();
    panes.delete(id);
  }
}
