import "@xterm/xterm/css/xterm.css";

import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";

import type { SessionRecord } from "../api";
import { api, cityScope, mutationHeaders } from "../api";
import { connectAgentOutput, type AgentOutputMessage, type SSEHandle } from "../sse";
import { byId, clear, el } from "../util/dom";
import { reportUIError } from "../ui";

type TerminalPane = {
  fit: FitAddon;
  handle: SSEHandle;
  root: HTMLElement;
  sessionID: string;
  term: Terminal;
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
      pane.fit.fit();
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
  status.textContent = "Loading session streams...";
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
    : `Attached to ${running.length} running session stream(s).`;

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
    pane.fit.fit();
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
  if (status) status.textContent = "No session streams attached.";
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
  const header = el("div", { class: "terminal-pane-header" }, [
    el("div", {}, [
      el("h3", {}, [title]),
      el("p", {}, [meta || session.id]),
    ]),
    el("span", { class: `badge ${session.attached ? "badge-green" : "badge-muted"}` }, [
      session.attached ? "Attached" : "Detached",
    ]),
  ]);
  const mount = el("div", { class: "terminal-pane-screen" });
  const form = el("form", { class: "terminal-pane-form" }, [
    el("input", {
      autocomplete: "off",
      class: "terminal-pane-input",
      placeholder: `Message ${session.template}`,
      type: "text",
    }),
    el("button", { class: "terminal-pane-send", type: "submit" }, ["Send"]),
  ]);

  const term = new Terminal({
    convertEol: true,
    cursorBlink: false,
    disableStdin: true,
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
  root.append(header, mount, form);
  term.open(mount);
  fit.fit();
  term.writeln(`Gas City terminal stream: ${title}`);
  term.writeln(`session ${session.id}`);
  term.writeln("");

  const handle = connectAgentOutput(
    city,
    session.id,
    (msg) => writeStreamMessage(term, msg),
    { format: "raw" },
  );

  form.addEventListener("submit", (event) => {
    event.preventDefault();
    const input = form.querySelector<HTMLInputElement>("input");
    const message = input?.value.trim() ?? "";
    if (!message) return;
    if (input) input.value = "";
    term.writeln(`\x1b[36mhuman>\x1b[0m ${message}`);
    void submitTerminalMessage(city, session.id, message, term);
  });

  return { fit, handle, root, sessionID: session.id, term };
}

async function submitTerminalMessage(
  city: string,
  sessionID: string,
  message: string,
  term: Terminal,
): Promise<void> {
  const res = await api.POST("/v0/city/{cityName}/session/{id}/submit", {
    params: { path: { cityName: city, id: sessionID }, header: mutationHeaders },
    body: { message, intent: "default" },
  });
  if (res.error) {
    term.writeln(`\x1b[31mmessage failed:\x1b[0m ${res.error.detail ?? "Could not message session"}`);
    return;
  }
  term.writeln("\x1b[32mmessage accepted\x1b[0m");
}

function writeStreamMessage(term: Terminal, msg: AgentOutputMessage): void {
  if (msg.type === "heartbeat") return;
  if (msg.type === "activity") {
    const activity = field(msg.data, "activity");
    if (typeof activity === "string") term.writeln(`\x1b[90mactivity: ${activity}\x1b[0m`);
    return;
  }
  if (msg.type === "pending") {
    term.writeln("\x1b[33mpending interaction\x1b[0m");
    return;
  }
  const text = streamText(msg.data);
  if (!text) return;
  for (const line of text.split("\n")) {
    term.writeln(line);
  }
}

function streamText(value: unknown): string {
  if (!isRecord(value)) return textFromValue(value);
  const turns = value.turns;
  if (Array.isArray(turns)) {
    return turns.map(turnText).filter(Boolean).join("\n");
  }
  const messages = value.messages;
  if (Array.isArray(messages)) {
    return messages.map(rawFrameText).filter(Boolean).join("\n");
  }
  return textFromValue(value);
}

function turnText(value: unknown): string {
  if (!isRecord(value)) return "";
  const role = typeof value.role === "string" ? value.role : "agent";
  const text = typeof value.text === "string" ? value.text : "";
  return text ? `\x1b[35m${role}>\x1b[0m ${text}` : "";
}

function rawFrameText(frame: unknown): string {
  if (typeof frame === "string") return frame;
  if (!isRecord(frame)) return textFromValue(frame);

  const payload = isRecord(frame.payload) ? frame.payload : undefined;
  if (payload) {
    const payloadText = messageContentText(payload.message) || contentText(payload.content);
    if (payloadText) return withRole(payload.role ?? payload.type, payloadText);
  }

  const messageText = messageContentText(frame.message);
  if (messageText) return withRole(frame.type ?? field(frame.message, "role"), messageText);

  const content = contentText(frame.content);
  if (content) return withRole(frame.role ?? frame.type, content);

  return textFromValue(frame);
}

function messageContentText(value: unknown): string {
  if (typeof value === "string") {
    try {
      return messageContentText(JSON.parse(value) as unknown);
    } catch {
      return value;
    }
  }
  if (!isRecord(value)) return "";
  return contentText(value.content);
}

function contentText(value: unknown): string {
  if (typeof value === "string") return value;
  if (Array.isArray(value)) {
    return value.map(contentBlockText).filter(Boolean).join("\n");
  }
  return "";
}

function contentBlockText(value: unknown): string {
  if (!isRecord(value)) return textFromValue(value);
  if (typeof value.text === "string") return value.text;
  if (typeof value.content === "string") return value.content;
  if (typeof value.name === "string") return `[${value.name}]`;
  return "";
}

function textFromValue(value: unknown): string {
  if (typeof value === "string") return value;
  if (value == null) return "";
  try {
    return JSON.stringify(value);
  } catch (error) {
    reportUIError("Terminal stream stringify failed", error);
    return "";
  }
}

function withRole(roleValue: unknown, text: string): string {
  const role = typeof roleValue === "string" && roleValue ? roleValue : "agent";
  return `\x1b[35m${role}>\x1b[0m ${text}`;
}

function field(value: unknown, key: string): unknown {
  return isRecord(value) ? value[key] : undefined;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function closeMissingPanes(nextIDs: Set<string>): void {
  for (const [id, pane] of panes) {
    if (nextIDs.has(id)) continue;
    pane.handle.close();
    pane.term.dispose();
    panes.delete(id);
  }
}
