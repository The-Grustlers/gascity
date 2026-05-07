import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api } from "../api";
import { syncCityScopeFromLocation } from "../state";
import { installTerminalWallInteractions, syncTerminalWallControl } from "./terminal_wall";

type MockTerminalInstance = {
  cols: number;
  emitData(data: string): void;
  rows: number;
};

type MockMessageEvent = {
  data: unknown;
};

const mockState = vi.hoisted(() => ({
  sockets: [] as MockWebSocket[],
  terminals: [] as MockTerminalInstance[],
}));

class MockWebSocket {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSED = 3;

  binaryType = "";
  readyState = MockWebSocket.CONNECTING;
  sent: string[] = [];

  private listeners = new Map<string, Array<(event: Event | MockMessageEvent) => void>>();

  constructor(readonly url: string) {
    mockState.sockets.push(this);
  }

  addEventListener(type: string, listener: (event: Event | MockMessageEvent) => void): void {
    const listeners = this.listeners.get(type) ?? [];
    listeners.push(listener);
    this.listeners.set(type, listeners);
  }

  close(): void {
    this.readyState = MockWebSocket.CLOSED;
    this.emit("close", new Event("close"));
  }

  emitMessage(data: unknown): void {
    this.emit("message", { data });
  }

  open(): void {
    this.readyState = MockWebSocket.OPEN;
    this.emit("open", new Event("open"));
  }

  send(data: string): void {
    this.sent.push(data);
  }

  private emit(type: string, event: Event | MockMessageEvent): void {
    for (const listener of this.listeners.get(type) ?? []) {
      listener(event);
    }
  }
}

vi.mock("@xterm/addon-fit", () => ({
  FitAddon: class {
    fit = vi.fn();
  },
}));

vi.mock("@xterm/xterm", () => ({
  Terminal: class {
    cols = 80;
    rows = 24;
    private dataListener: ((data: string) => void) | null = null;
    private mount: HTMLElement | null = null;

    constructor() {
      mockState.terminals.push(this);
    }

    dispose = vi.fn();

    loadAddon = vi.fn();

    onData(listener: (data: string) => void): { dispose(): void } {
      this.dataListener = listener;
      return { dispose: vi.fn() };
    }

    emitData(data: string): void {
      this.dataListener?.(data);
    }

    open(mount: HTMLElement): void {
      this.mount = mount;
      const node = document.createElement("div");
      node.className = "mock-xterm";
      mount.append(node);
    }

    write(text: string): void {
      const node = document.createElement("span");
      node.textContent = text;
      this.mount?.append(node);
    }

    writeln(text: string): void {
      this.write(`${text}\n`);
    }
  },
}));

describe("terminal wall", () => {
  beforeEach(() => {
    document.body.innerHTML = `
      <button id="toggle-terminal-wall-btn" disabled>Terminal View</button>
      <div id="terminal-wall-panel" style="display:none">
        <span id="terminal-wall-count"></span>
        <button id="terminal-wall-refresh-btn">Refresh</button>
        <button id="terminal-wall-close-btn">Close</button>
        <span id="terminal-wall-status"></span>
        <div id="terminal-wall-grid"></div>
      </div>
    `;
    window.history.pushState({}, "", "/dashboard?city=mc-city");
    Object.defineProperty(window, "WebSocket", {
      configurable: true,
      value: MockWebSocket,
    });
    Object.defineProperty(globalThis, "WebSocket", {
      configurable: true,
      value: MockWebSocket,
    });
    syncCityScopeFromLocation();
    mockState.sockets.length = 0;
    mockState.terminals.length = 0;
  });

  afterEach(() => {
    vi.restoreAllMocks();
    window.history.pushState({}, "", "/dashboard");
    syncCityScopeFromLocation();
  });

  it("opens xterm panes for running sessions and connects websocket terminals", async () => {
    vi.spyOn(api, "GET").mockImplementation(async (path: string) => {
      if (path === "/v0/city/{cityName}/sessions") {
        return {
          data: {
            items: [
              {
                attached: false,
                display_name: "Director",
                id: "s-director",
                last_active: "2026-05-07T16:00:00Z",
                last_output: "",
                provider: "claude",
                rig: "",
                running: true,
                state: "active",
                template: "director",
                title: "Director",
              },
              {
                attached: false,
                id: "s-asleep",
                provider: "claude",
                running: false,
                state: "asleep",
                template: "mayor",
                title: "Mayor",
              },
            ],
          },
        } as never;
      }
      throw new Error(`unexpected GET ${path}`);
    });

    installTerminalWallInteractions();
    syncTerminalWallControl(true);
    document.getElementById("toggle-terminal-wall-btn")?.click();

    await waitFor(() => {
      expect(document.getElementById("terminal-wall-panel")?.style.display).toBe("block");
      expect(document.getElementById("terminal-wall-count")?.textContent).toBe("1");
      expect(document.getElementById("terminal-wall-grid")?.textContent).toContain("Director");
      expect(mockState.sockets).toHaveLength(1);
    });

    const socket = mockState.sockets[0];
    expect(socket.url).toBe("ws://localhost:3000/v0/city/mc-city/session/s-director/terminal");
    socket.open();
    expect(JSON.parse(socket.sent[0] ?? "{}")).toEqual({ type: "resize", cols: 80, rows: 24 });

    mockState.terminals[0]?.emitData("hello\n");
    expect(socket.sent.map((frame) => JSON.parse(frame) as unknown)).toContainEqual({
      type: "input",
      data: "hello\n",
    });

    socket.emitMessage(new TextEncoder().encode("hello from terminal").buffer);
    expect(document.getElementById("terminal-wall-grid")?.textContent).toContain("hello from terminal");
  });
});

async function waitFor(assertion: () => void): Promise<void> {
  const started = Date.now();
  let lastError: unknown;
  while (Date.now() - started < 1000) {
    try {
      assertion();
      return;
    } catch (error) {
      lastError = error;
      await new Promise((resolve) => setTimeout(resolve, 10));
    }
  }
  throw lastError;
}
