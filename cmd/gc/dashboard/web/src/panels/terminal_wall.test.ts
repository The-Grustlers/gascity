import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api } from "../api";
import { connectAgentOutput } from "../sse";
import { syncCityScopeFromLocation } from "../state";
import { installTerminalWallInteractions, syncTerminalWallControl } from "./terminal_wall";

vi.mock("@xterm/addon-fit", () => ({
  FitAddon: class {
    fit = vi.fn();
  },
}));

vi.mock("@xterm/xterm", () => ({
  Terminal: class {
    private mount: HTMLElement | null = null;

    dispose = vi.fn();

    loadAddon = vi.fn();

    open(mount: HTMLElement): void {
      this.mount = mount;
      const node = document.createElement("div");
      node.className = "mock-xterm";
      mount.append(node);
    }

    writeln(text: string): void {
      const line = document.createElement("div");
      line.textContent = text;
      this.mount?.append(line);
    }
  },
}));

vi.mock("../sse", () => ({
  connectAgentOutput: vi.fn(() => ({ close: vi.fn() })),
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
    syncCityScopeFromLocation();
  });

  afterEach(() => {
    vi.restoreAllMocks();
    window.history.pushState({}, "", "/dashboard");
    syncCityScopeFromLocation();
  });

  it("opens xterm panes for running sessions and streams raw frames", async () => {
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
    });

    expect(connectAgentOutput).toHaveBeenCalledWith(
      "mc-city",
      "s-director",
      expect.any(Function),
      { format: "raw" },
    );

    const onEvent = vi.mocked(connectAgentOutput).mock.calls[0]?.[2];
    onEvent?.({
      data: {
        messages: [{
          message: { content: "hello from raw stream", role: "assistant" },
          type: "assistant",
        }],
      },
      type: "message",
    });

    expect(document.getElementById("terminal-wall-grid")?.textContent).toContain("hello from raw stream");
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
