import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api } from "../api";
import { connectAgentOutput } from "../sse";
import { syncCityScopeFromLocation } from "../state";
import { installCrewInteractions, renderCrew } from "./crew";

vi.mock("../sse", () => ({
  connectAgentOutput: vi.fn(() => ({ close: vi.fn() })),
}));

describe("session cockpit", () => {
  beforeEach(() => {
    document.body.innerHTML = `
      <div id="crew-loading">Loading crew...</div>
      <table id="crew-table" style="display:none"><tbody id="crew-tbody"></tbody></table>
      <div id="crew-empty" style="display:none"><p>No crew configured</p></div>
      <div id="rigged-body"></div>
      <div id="pooled-body"></div>
      <span id="sessions-count"></span>
      <div id="sessions-list"></div>
      <div id="sessions-detail"><div id="sessions-detail-summary"></div></div>
      <span id="crew-count"></span>
      <span id="rigged-count"></span>
      <span id="pooled-count"></span>
      <div id="agent-log-drawer" style="display:none"></div>
    `;
    window.history.pushState({}, "", "/dashboard?city=mc-city");
    syncCityScopeFromLocation();
  });

  afterEach(() => {
    vi.restoreAllMocks();
    window.history.pushState({}, "", "/dashboard");
    syncCityScopeFromLocation();
  });

  it("loads older transcript pages without losing the drawer loading sentinel", async () => {
    document.body.innerHTML = `
      <div id="crew-loading">Loading crew...</div>
      <table id="crew-table" style="display:none"><tbody id="crew-tbody"></tbody></table>
      <div id="crew-empty" style="display:none"><p>No crew configured</p></div>
      <div id="rigged-body"></div>
      <div id="pooled-body"></div>
      <span id="crew-count"></span>
      <span id="rigged-count"></span>
      <span id="pooled-count"></span>
      <div id="agent-log-drawer" style="display:none">
        <span id="log-drawer-agent-name"></span>
        <span id="log-drawer-count"></span>
        <button id="log-drawer-older-btn" style="display:none">Load older</button>
        <button id="log-drawer-close-btn">Close</button>
        <div id="log-drawer-body">
          <div id="log-drawer-messages">
            <div id="log-drawer-loading">Loading logs...</div>
          </div>
        </div>
      </div>
    `;
    const transcriptQueries: Array<Record<string, string | undefined>> = [];
    vi.spyOn(api, "GET").mockImplementation(async (path: string, options?: unknown) => {
      if (path === "/v0/city/{cityName}/sessions") {
        return {
          data: {
            items: [{
              active_bead: "",
              agent_kind: "crew",
              attached: true,
              id: "s-reviewer",
              last_active: "2026-04-18T20:00:00Z",
              last_output: "",
              rig: "rig-a/crew",
              running: true,
              template: "reviewer",
            }],
          },
        } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/pending") {
        return { data: { pending: false } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/transcript") {
        const query = (options as { params?: { query?: Record<string, string | undefined> } } | undefined)?.params?.query ?? {};
        transcriptQueries.push(query);
        if (query.before) {
          return {
            data: {
              turns: [{ role: "assistant", text: "Older transcript turn", timestamp: "2026-04-18T19:00:00Z" }],
              pagination: {
                has_older_messages: false,
                returned_message_count: 1,
                total_compactions: 0,
                total_message_count: 3,
              },
            },
          } as never;
        }
        return {
          data: {
            turns: [{ role: "assistant", text: "Newest transcript turn", timestamp: "2026-04-18T20:00:00Z" }],
            pagination: {
              has_older_messages: true,
              returned_message_count: 1,
              total_compactions: 0,
              total_message_count: 3,
              truncated_before_message: "cursor-1",
            },
          },
        } as never;
      }
      throw new Error(`unexpected GET ${path}`);
    });

    installCrewInteractions();
    await renderCrew();
    document.querySelector<HTMLButtonElement>(".agent-log-link")?.click();
    await waitFor(() => {
      expect(document.getElementById("log-drawer-messages")?.textContent).toContain("Newest transcript turn");
    });

    expect(document.getElementById("log-drawer-loading")).not.toBeNull();
    document.getElementById("log-drawer-older-btn")?.click();
    await waitFor(() => {
      expect(document.getElementById("log-drawer-messages")?.textContent).toContain("Older transcript turn");
    });

    expect(transcriptQueries.map((query) => query.before)).toEqual([undefined, "cursor-1"]);
    expect(document.getElementById("log-drawer-loading")).not.toBeNull();
  });

  it("splits terminal transcript output into chat bubbles", async () => {
    document.body.innerHTML = `
      <div id="crew-loading">Loading crew...</div>
      <table id="crew-table" style="display:none"><tbody id="crew-tbody"></tbody></table>
      <div id="crew-empty" style="display:none"><p>No crew configured</p></div>
      <div id="rigged-body"></div>
      <div id="pooled-body"></div>
      <span id="crew-count"></span>
      <span id="rigged-count"></span>
      <span id="pooled-count"></span>
      <div id="agent-log-drawer" style="display:none">
        <span id="log-drawer-agent-name"></span>
        <span id="log-drawer-count"></span>
        <button id="log-drawer-older-btn" style="display:none">Load older</button>
        <button id="log-drawer-close-btn">Close</button>
        <div id="log-drawer-body">
          <div id="log-drawer-messages">
            <div id="log-drawer-loading">Loading logs...</div>
          </div>
        </div>
      </div>
    `;
    vi.spyOn(api, "GET").mockImplementation(async (path: string) => {
      if (path === "/v0/city/{cityName}/sessions") {
        return {
          data: {
            items: [{
              active_bead: "",
              agent_kind: "crew",
              attached: true,
              id: "s-director",
              last_active: "2026-04-18T20:00:00Z",
              last_output: "",
              running: true,
              template: "director",
            }],
          },
        } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/pending") {
        return { data: { pending: false } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/transcript") {
        return {
          data: {
            turns: [{
              role: "output",
              text: [
                "gr7n-router-cli: codex via gpt-5.5",
                "",
                "› [gr7n] director • 2026-05-23T18:55:26",
                "",
                "  Run `gc prime` to initialize your context.",
                "",
                "• gc prime done. Director context loaded.",
                "",
                "────────────────────────────────────────────────────────────────────────────────",
                "",
                "› hi!",
                "",
                "• Hi. Idle until explicit request.",
                "",
                "› Explain this codebase",
                "",
                "  gpt-5.5 high · ~/projects/gr7n-platform/gascity/cities/gr7n/.gc/agents/direct…",
              ].join("\n"),
              timestamp: "2026-05-23T18:55:26Z",
            }],
            pagination: {
              has_older_messages: false,
              returned_message_count: 1,
              total_compactions: 0,
              total_message_count: 1,
            },
          },
        } as never;
      }
      throw new Error(`unexpected GET ${path}`);
    });

    installCrewInteractions();
    await renderCrew();
    document.querySelector<HTMLButtonElement>(".agent-log-link")?.click();
    await waitFor(() => {
      expect(document.querySelectorAll(".log-msg-user").length).toBe(1);
    });

    expect(document.querySelector(".log-msg-result")).toBeNull();
    expect(document.querySelectorAll(".log-msg-system")).toHaveLength(0);
    expect(document.querySelectorAll(".log-msg-assistant .log-msg-activity").length).toBeGreaterThanOrEqual(1);
    expect(document.querySelectorAll(".log-msg-assistant").length).toBe(2);
    expect(document.querySelectorAll(".log-msg-user")[0]?.textContent).toContain("hi!");
    expect(document.getElementById("log-drawer-messages")?.textContent).not.toContain("Explain this codebase");
    expect(document.getElementById("log-drawer-messages")?.textContent).not.toContain("gpt-5.5 high");
    expect(document.querySelectorAll(".log-msg-assistant")[1]?.textContent).toContain("Hi. Idle until explicit request.");
  });

  it("collapses wrapped base64 image payloads in terminal transcripts", async () => {
    const encoded = "ACc+7nX4cJ2Rgb+u5vx7aFMH8dJLSAxwuQAQCo3P3fOM6/FiOyMb".repeat(50);
    const wrapped = encoded.match(/.{1,80}/g)?.map((line) => `  ${line}`).join("\n") ?? encoded;
    document.body.innerHTML = `
      <div id="crew-loading">Loading crew...</div>
      <table id="crew-table" style="display:none"><tbody id="crew-tbody"></tbody></table>
      <div id="crew-empty" style="display:none"><p>No crew configured</p></div>
      <div id="rigged-body"></div>
      <div id="pooled-body"></div>
      <span id="crew-count"></span>
      <span id="rigged-count"></span>
      <span id="pooled-count"></span>
      <div id="agent-log-drawer" style="display:none">
        <span id="log-drawer-agent-name"></span>
        <span id="log-drawer-count"></span>
        <button id="log-drawer-older-btn" style="display:none">Load older</button>
        <button id="log-drawer-close-btn">Close</button>
        <div id="log-drawer-body">
          <div id="log-drawer-messages">
            <div id="log-drawer-loading">Loading logs...</div>
          </div>
        </div>
      </div>
    `;
    vi.spyOn(api, "GET").mockImplementation(async (path: string) => {
      if (path === "/v0/city/{cityName}/sessions") {
        return {
          data: {
            items: [{
              active_bead: "",
              agent_kind: "crew",
              attached: true,
              id: "s-director",
              last_active: "2026-04-18T20:00:00Z",
              last_output: "",
              running: true,
              template: "director",
            }],
          },
        } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/pending") {
        return { data: { pending: false } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/transcript") {
        return {
          data: {
            turns: [{
              role: "output",
              text: [
                "› image test",
                "",
                wrapped,
                "",
                "• I ran out of context.",
              ].join("\n"),
              timestamp: "2026-05-23T18:55:26Z",
            }],
            pagination: {
              has_older_messages: false,
              returned_message_count: 1,
              total_compactions: 0,
              total_message_count: 1,
            },
          },
        } as never;
      }
      throw new Error(`unexpected GET ${path}`);
    });

    installCrewInteractions();
    await renderCrew();
    document.querySelector<HTMLButtonElement>(".agent-log-link")?.click();
    await waitFor(() => {
      expect(document.getElementById("log-drawer-messages")?.textContent).toContain("large encoded image data omitted");
    });

    expect(document.getElementById("log-drawer-messages")?.textContent).not.toContain(encoded.slice(0, 40));
    expect(document.getElementById("log-drawer-messages")?.textContent).toContain("I ran out of context.");
  });

  it("updates chat bubbles from streamed terminal turn snapshots", async () => {
    document.body.innerHTML = `
      <div id="crew-loading">Loading crew...</div>
      <table id="crew-table" style="display:none"><tbody id="crew-tbody"></tbody></table>
      <div id="crew-empty" style="display:none"><p>No crew configured</p></div>
      <div id="rigged-body"></div>
      <div id="pooled-body"></div>
      <span id="crew-count"></span>
      <span id="rigged-count"></span>
      <span id="pooled-count"></span>
      <div id="agent-log-drawer" style="display:none">
        <span id="log-drawer-agent-name"></span>
        <span id="log-drawer-count"></span>
        <button id="log-drawer-older-btn" style="display:none">Load older</button>
        <button id="log-drawer-close-btn">Close</button>
        <div id="log-drawer-body">
          <div id="log-drawer-messages">
            <div id="log-drawer-loading">Loading logs...</div>
          </div>
        </div>
      </div>
    `;
    vi.spyOn(api, "GET").mockImplementation(async (path: string) => {
      if (path === "/v0/city/{cityName}/sessions") {
        return {
          data: {
            items: [{
              active_bead: "",
              agent_kind: "crew",
              attached: true,
              id: "s-director",
              last_active: "2026-04-18T20:00:00Z",
              last_output: "",
              running: true,
              template: "director",
            }],
          },
        } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/pending") {
        return { data: { pending: false } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/transcript") {
        return {
          data: {
            turns: [{
              role: "output",
              text: [
                "gr7n-router-cli: codex via gpt-5.5",
                "",
                "› hi!",
                "",
                "• Hi. Idle until explicit request.",
              ].join("\n"),
              timestamp: "2026-05-23T18:55:26Z",
            }],
            pagination: {
              has_older_messages: false,
              returned_message_count: 1,
              total_compactions: 0,
              total_message_count: 1,
            },
          },
        } as never;
      }
      throw new Error(`unexpected GET ${path}`);
    });

    installCrewInteractions();
    await renderCrew();
    document.querySelector<HTMLButtonElement>(".agent-log-link")?.click();
    const mockedConnect = vi.mocked(connectAgentOutput);
    await waitFor(() => {
      expect(mockedConnect).toHaveBeenCalled();
    });

    const streamCallback = mockedConnect.mock.calls[mockedConnect.mock.calls.length - 1]?.[2];
    streamCallback?.({
      type: "turn",
      data: {
        format: "text",
        turns: [{
          role: "output",
          text: [
            "gr7n-router-cli: codex via gpt-5.5",
            "",
            "› hi!",
            "",
            "• Hi. Idle until explicit request.",
            "",
            "› ping",
            "",
            "• pong",
            "",
            "› Explain this codebase",
            "",
            "  gpt-5.5 high · ~/projects/gr7n-platform/gascity/cities/gr7n/.gc/agents/direct…",
          ].join("\n"),
          timestamp: "2026-05-23T19:07:00Z",
        }],
      },
    });

    await waitFor(() => {
      expect(Array.from(document.querySelectorAll(".log-msg-assistant")).some((node) => node.textContent?.includes("pong"))).toBe(true);
    });
    expect(document.querySelector(".log-msg-result")).toBeNull();
    expect(document.getElementById("log-drawer-messages")?.textContent).not.toContain("Explain this codebase");
    expect(document.getElementById("log-drawer-messages")?.textContent).not.toContain("gpt-5.5 high");
    expect(Array.from(document.querySelectorAll(".log-msg-user")).map((node) => node.textContent)).toEqual(
      expect.arrayContaining([expect.stringContaining("hi!"), expect.stringContaining("ping")]),
    );
    expect(document.getElementById("log-drawer-count")?.textContent).toBe(`${document.querySelectorAll(".log-msg").length} entries`);
  });

  it("folds streamed context-only events into the next assistant output", async () => {
    setupCrewCockpitDom();
    vi.spyOn(api, "GET").mockImplementation(async (path: string) => {
      if (path === "/v0/city/{cityName}/sessions") {
        return { data: { items: [crewSession("s-mayor")] } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}") {
        return { data: { ...crewSession("s-mayor"), provider: "codex" } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/pending") {
        return { data: { pending: null, supported: true } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/transcript") {
        return {
          data: {
            turns: [],
            pagination: { has_older_messages: false, returned_message_count: 0, total_compactions: 0, total_message_count: 0 },
          },
        } as never;
      }
      throw new Error(`unexpected GET ${path}`);
    });

    installCrewInteractions();
    await renderCrew();
    document.querySelector<HTMLButtonElement>(".agent-log-link")?.click();
    const mockedConnect = vi.mocked(connectAgentOutput);
    await waitFor(() => {
      expect(mockedConnect).toHaveBeenCalled();
    });

    const streamCallback = mockedConnect.mock.calls[mockedConnect.mock.calls.length - 1]?.[2];
    streamCallback?.({
      type: "turn",
      data: { turns: [{ role: "user", text: "test msg", timestamp: "2026-05-28T21:57:00Z" }] },
    });
    streamCallback?.({
      type: "turn",
      data: {
        turns: [{
          role: "user",
          text: "<environment_context>\n  <current_date>2026-05-28</current_date>\n</environment_context>",
          timestamp: "2026-05-28T21:57:01Z",
        }],
      },
    });

    expect(document.querySelectorAll(".log-msg-user")).toHaveLength(1);
    expect(document.querySelector(".log-msg-activity-standalone")).toBeNull();

    streamCallback?.({
      type: "turn",
      data: { turns: [{ role: "assistant", text: "Received.", timestamp: "2026-05-28T21:57:02Z" }] },
    });

    const assistant = document.querySelector<HTMLElement>(".log-msg-assistant")!;
    expect(assistant.querySelector(".log-msg-body")?.textContent).toContain("Received.");
    const details = assistant.querySelector<HTMLDetailsElement>(".log-msg-activity");
    expect(details?.open).toBe(false);
    expect(details?.querySelector("summary")?.textContent).toContain("Context");
    expect(details?.textContent).toContain("Environment context");
    expect(document.querySelector(".log-msg-activity-standalone")).toBeNull();
    expect(document.getElementById("log-drawer-count")?.textContent).toBe("2 entries");
  });

  it("treats autonomous control prompts as context instead of user chat", async () => {
    const controlPrompt = "Check mail, then run `gc hook mayor` or `gc hook` for routed mayor work. If work exists, claim and handle one item. If no non-mayor agent has worked recently and there is no routed infra/mayor work, drain-ack and stay idle unless Bryce explicitly asks for monitoring, triage, or city work.";
    setupCrewCockpitDom();
    vi.spyOn(api, "GET").mockImplementation(async (path: string) => {
      if (path === "/v0/city/{cityName}/sessions") {
        return { data: { items: [crewSession("s-mayor")] } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}") {
        return { data: { ...crewSession("s-mayor"), provider: "codex" } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/pending") {
        return { data: { pending: null, supported: true } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/transcript") {
        return {
          data: {
            turns: [{
              role: "user",
              text: "what is happening?",
              timestamp: "2026-05-29T19:01:00Z",
            }, {
              role: "user",
              text: controlPrompt,
              timestamp: "2026-05-29T19:01:01Z",
            }, {
              role: "user",
              text: controlPrompt,
              timestamp: "2026-05-29T19:01:02Z",
            }, {
              role: "assistant",
              text: "Mail empty; staying idle.",
              timestamp: "2026-05-29T19:01:03Z",
            }],
            pagination: { has_older_messages: false, returned_message_count: 4, total_compactions: 0, total_message_count: 4 },
          },
        } as never;
      }
      throw new Error(`unexpected GET ${path}`);
    });

    installCrewInteractions();
    await renderCrew();
    document.querySelector<HTMLButtonElement>(".agent-log-link")?.click();
    await waitFor(() => {
      expect(document.querySelector(".log-msg-assistant .log-msg-activity")).not.toBeNull();
    });

    const userMessages = Array.from(document.querySelectorAll<HTMLElement>(".log-msg-user"));
    expect(userMessages).toHaveLength(1);
    expect(userMessages[0]?.textContent).toContain("what is happening?");
    expect(userMessages[0]?.textContent).not.toContain("gc hook mayor");
    const details = document.querySelector<HTMLDetailsElement>(".log-msg-assistant .log-msg-activity")!;
    expect(details.open).toBe(false);
    expect(details.querySelector("summary")?.textContent).toContain("Context");
    expect(details.textContent).toContain("Autonomous control prompt");
    expect(details.textContent?.match(/gc hook mayor/g)).toHaveLength(1);
  });

  it("folds already-rendered streamed progress into the next assistant output", async () => {
    setupCrewCockpitDom();
    vi.spyOn(api, "GET").mockImplementation(async (path: string) => {
      if (path === "/v0/city/{cityName}/sessions") {
        return { data: { items: [crewSession("s-mayor")] } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}") {
        return { data: { ...crewSession("s-mayor"), provider: "codex" } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/pending") {
        return { data: { pending: null, supported: true } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/transcript") {
        return {
          data: {
            turns: [],
            pagination: { has_older_messages: false, returned_message_count: 0, total_compactions: 0, total_message_count: 0 },
          },
        } as never;
      }
      throw new Error(`unexpected GET ${path}`);
    });

    installCrewInteractions();
    await renderCrew();
    document.querySelector<HTMLButtonElement>(".agent-log-link")?.click();
    const mockedConnect = vi.mocked(connectAgentOutput);
    await waitFor(() => {
      expect(mockedConnect).toHaveBeenCalled();
    });

    const streamCallback = mockedConnect.mock.calls[mockedConnect.mock.calls.length - 1]?.[2];
    streamCallback?.({
      type: "turn",
      data: { turns: [{ role: "user", text: "call a tool for test", timestamp: "2026-05-28T22:22:00Z" }] },
    });
    streamCallback?.({
      type: "turn",
      data: { turns: [{ role: "assistant", text: "Tool test. Running harmless `pwd`.", timestamp: "2026-05-28T22:22:01Z" }] },
    });

    expect(document.querySelectorAll(".log-msg-assistant")).toHaveLength(1);
    expect(document.getElementById("log-drawer-messages")?.textContent).toContain("Tool test. Running harmless");

    streamCallback?.({
      type: "turn",
      data: {
        turns: [{
          role: "assistant",
          parts: [{ id: "call-1", type: "tool", tool: "exec_command", input: { cmd: "pwd" } }],
          timestamp: "2026-05-28T22:22:02Z",
        }],
      },
    });
    streamCallback?.({
      type: "turn",
      data: {
        turns: [{
          role: "tool_result",
          parts: [{ tool_use_id: "call-1", type: "tool", output: "/tmp/city" }],
          timestamp: "2026-05-28T22:22:03Z",
        }],
      },
    });
    streamCallback?.({
      type: "turn",
      data: { turns: [{ role: "assistant", text: "Tool called. `pwd` returned:\n\n`/tmp/city`", timestamp: "2026-05-28T22:22:04Z" }] },
    });

    const assistants = Array.from(document.querySelectorAll<HTMLElement>(".log-msg-assistant"));
    expect(assistants).toHaveLength(1);
    expect(assistants[0]?.querySelector(".log-msg-body")?.textContent).toContain("Tool called.");
    expect(assistants[0]?.querySelector(".log-msg-body")?.textContent).not.toContain("Tool test.");
    const details = assistants[0]?.querySelector<HTMLDetailsElement>(".log-msg-activity");
    expect(details?.open).toBe(false);
    expect(details?.querySelector("summary")?.textContent).toContain("1 tool");
    expect(details?.querySelector("summary")?.textContent).toContain("1 update");
    expect(details?.textContent).toContain("Tool test. Running harmless");
    expect(details?.textContent).toContain("Tool · exec_command");
    expect(details?.textContent).toContain("/tmp/city");
    expect(document.getElementById("log-drawer-count")?.textContent).toBe("2 entries");
  });

  it("submits chat messages through the session submit endpoint", async () => {
    document.body.innerHTML = `
      <div id="crew-loading">Loading crew...</div>
      <table id="crew-table" style="display:none"><tbody id="crew-tbody"></tbody></table>
      <div id="crew-empty" style="display:none"><p>No crew configured</p></div>
      <div id="rigged-body"></div>
      <div id="pooled-body"></div>
      <span id="crew-count"></span>
      <span id="rigged-count"></span>
      <span id="pooled-count"></span>
      <div id="agent-log-drawer" style="display:none">
        <span id="log-drawer-agent-name"></span>
        <span id="log-drawer-count"></span>
        <span id="log-drawer-status"></span>
        <button id="log-drawer-older-btn" style="display:none">Load older</button>
        <button id="log-drawer-close-btn">Close</button>
        <div id="log-drawer-body">
          <div id="log-drawer-messages">
            <div id="log-drawer-loading">Loading logs...</div>
          </div>
        </div>
        <form id="log-drawer-composer">
          <button id="log-drawer-attach-btn" type="button">Attach images</button>
          <input id="log-drawer-file-input" type="file" />
          <div id="log-drawer-attachments"></div>
          <textarea id="log-drawer-input"></textarea>
          <button id="log-drawer-send-btn" type="submit">Send</button>
        </form>
      </div>
    `;
    vi.spyOn(api, "GET").mockImplementation(async (path: string) => {
      if (path === "/v0/city/{cityName}/sessions") {
        return {
          data: {
            items: [{
              active_bead: "",
              agent_kind: "crew",
              attached: false,
              id: "s-mayor",
              last_active: "2026-04-18T20:00:00Z",
              last_output: "",
              running: true,
              template: "mayor",
            }],
          },
        } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/pending") {
        return { data: { pending: false } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/transcript") {
        return {
          data: {
            turns: [],
            pagination: {
              has_older_messages: false,
              returned_message_count: 0,
              total_compactions: 0,
              total_message_count: 0,
            },
          },
        } as never;
      }
      throw new Error(`unexpected GET ${path}`);
    });
    const posts: Array<{ body?: { intent?: string; message?: string }; path: string }> = [];
    vi.spyOn(api, "POST").mockImplementation(async (path: string, options?: unknown) => {
      posts.push({ path, body: (options as { body?: { intent?: string; message?: string } } | undefined)?.body });
      return { data: { event_cursor: "12", request_id: "req-chat-1", status: "accepted" } } as never;
    });
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({
      id: "att-123",
      mime_type: "image/png",
      name: "screenshot.png",
      path: "/tmp/test-city/.gc/dashboard/attachments/s-mayor/att-123/screenshot.png",
      size: 400_000,
      url: "/v0/city/mc-city/session/s-mayor/attachments/att-123/screenshot.png",
    }), { headers: { "Content-Type": "application/json" }, status: 201 }));

    installCrewInteractions();
    await renderCrew();
    document.querySelector<HTMLButtonElement>(".agent-log-link")?.click();
    await waitFor(() => {
      expect((document.getElementById("agent-log-drawer") as HTMLElement).style.display).toBe("flex");
    });

    const fileInput = document.getElementById("log-drawer-file-input") as HTMLInputElement;
    const screenshot = new File([new Uint8Array(400_000)], "screenshot.png", { type: "image/png" });
    Object.defineProperty(fileInput, "files", { configurable: true, value: [screenshot] });
    fileInput.dispatchEvent(new Event("change", { bubbles: true }));
    await waitFor(() => {
      expect(document.getElementById("log-drawer-attachments")?.textContent).toContain("screenshot.png");
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/v0/city/mc-city/session/s-mayor/attachments",
      expect.objectContaining({ method: "POST" }),
    );

    const input = document.getElementById("log-drawer-input") as HTMLTextAreaElement;
    input.value = "Can you check the queue?";
    document.getElementById("log-drawer-composer")?.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => {
      expect(posts.length).toBe(1);
    });
    expect(posts[0]?.path).toBe("/v0/city/{cityName}/session/{id}/submit");
    expect(posts[0]?.body?.intent).toBe("default");
    expect(posts[0]?.body?.message).toContain("Can you check the queue?");
    expect(posts[0]?.body?.message).toContain("Attached images:");
    expect(posts[0]?.body?.message).toContain("![screenshot.png](/v0/city/mc-city/session/s-mayor/attachments/att-123/screenshot.png)");
    expect(posts[0]?.body?.message).toContain("Local file: /tmp/test-city/.gc/dashboard/attachments/s-mayor/att-123/screenshot.png");
    expect(posts[0]?.body?.message).not.toContain("data:image");
    expect(new TextEncoder().encode(JSON.stringify(posts[0]?.body)).length).toBeLessThan(900_000);
    expect(input.value).toBe("");
    expect(document.getElementById("log-drawer-messages")?.textContent).toContain("Can you check the queue?");
    expect(document.querySelector(".log-msg-user")?.textContent).toContain("Can you check the queue?");
    expect(document.querySelector<HTMLImageElement>(".log-msg-image")?.getAttribute("src")).toBe("/v0/city/mc-city/session/s-mayor/attachments/att-123/screenshot.png");
    expect(document.getElementById("log-drawer-status")?.textContent).toBe("Sent");
    expect(document.getElementById("log-drawer-count")?.textContent).toBe("1 entry");

    const streamCalls = vi.mocked(connectAgentOutput).mock.calls;
    const streamCallback = streamCalls[streamCalls.length - 1]?.[2];
    streamCallback?.({
      type: "turn",
      data: {
        turns: [{
          role: "user",
          text: posts[0]?.body?.message ?? "",
          timestamp: "2026-04-18T20:01:00Z",
        }],
      },
    });

    expect(document.querySelectorAll(".log-msg-user")).toHaveLength(1);
    expect(document.getElementById("log-drawer-count")?.textContent).toBe("1 entry");
  });

  it("deletes draft image attachments when pending chips are removed", async () => {
    setupCrewCockpitDom();
    vi.spyOn(api, "GET").mockImplementation(async (path: string) => {
      if (path === "/v0/city/{cityName}/sessions") {
        return { data: { items: [crewSession("s-mayor")] } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}") {
        return { data: crewSession("s-mayor") } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/pending") {
        return { data: { pending: false } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/transcript") {
        return {
          data: {
            turns: [],
            pagination: { has_older_messages: false, returned_message_count: 0, total_compactions: 0, total_message_count: 0 },
          },
        } as never;
      }
      throw new Error(`unexpected GET ${path}`);
    });
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === "DELETE") return new Response(null, { status: 204 });
      return new Response(JSON.stringify({
        id: "att-delete",
        mime_type: "image/png",
        name: "remove-me.png",
        path: "/tmp/test-city/.gc/dashboard/attachments/s-mayor/att-delete/remove-me.png",
        size: 12,
        status: "draft",
        url: "/v0/city/mc-city/session/s-mayor/attachments/att-delete/remove-me.png",
      }), { headers: { "Content-Type": "application/json" }, status: 201 });
    });

    installCrewInteractions();
    await renderCrew();
    document.querySelector<HTMLButtonElement>(".agent-log-link")?.click();
    await waitFor(() => {
      expect((document.getElementById("agent-log-drawer") as HTMLElement).style.display).toBe("flex");
    });

    const fileInput = document.getElementById("log-drawer-file-input") as HTMLInputElement;
    Object.defineProperty(fileInput, "files", { configurable: true, value: [new File([new Uint8Array([1])], "remove-me.png", { type: "image/png" })] });
    fileInput.dispatchEvent(new Event("change", { bubbles: true }));
    await waitFor(() => {
      expect(document.getElementById("log-drawer-attachments")?.textContent).toContain("remove-me.png");
    });
    document.querySelector<HTMLButtonElement>(".chat-attachment-remove")?.click();
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "/v0/city/mc-city/session/s-mayor/attachments/att-delete",
        expect.objectContaining({ method: "DELETE" }),
      );
    });
    expect(document.getElementById("log-drawer-attachments")?.textContent).not.toContain("remove-me.png");
  });

  it("blocks chat submits that would exceed the API body limit", async () => {
    document.body.innerHTML = `
      <div id="crew-loading">Loading crew...</div>
      <table id="crew-table" style="display:none"><tbody id="crew-tbody"></tbody></table>
      <div id="crew-empty" style="display:none"><p>No crew configured</p></div>
      <div id="rigged-body"></div>
      <div id="pooled-body"></div>
      <span id="crew-count"></span>
      <span id="rigged-count"></span>
      <span id="pooled-count"></span>
      <div id="agent-log-drawer" style="display:none">
        <span id="log-drawer-agent-name"></span>
        <span id="log-drawer-count"></span>
        <span id="log-drawer-status"></span>
        <button id="log-drawer-older-btn" style="display:none">Load older</button>
        <button id="log-drawer-close-btn">Close</button>
        <div id="log-drawer-body">
          <div id="log-drawer-messages">
            <div id="log-drawer-loading">Loading logs...</div>
          </div>
        </div>
        <form id="log-drawer-composer">
          <textarea id="log-drawer-input"></textarea>
          <button id="log-drawer-send-btn" type="submit">Send</button>
        </form>
      </div>
    `;
    vi.spyOn(api, "GET").mockImplementation(async (path: string) => {
      if (path === "/v0/city/{cityName}/sessions") {
        return {
          data: {
            items: [{
              active_bead: "",
              agent_kind: "crew",
              attached: false,
              id: "s-mayor",
              last_active: "2026-04-18T20:00:00Z",
              last_output: "",
              running: true,
              template: "mayor",
            }],
          },
        } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/pending") {
        return { data: { pending: false } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/transcript") {
        return {
          data: {
            turns: [],
            pagination: {
              has_older_messages: false,
              returned_message_count: 0,
              total_compactions: 0,
              total_message_count: 0,
            },
          },
        } as never;
      }
      throw new Error(`unexpected GET ${path}`);
    });
    const post = vi.spyOn(api, "POST").mockResolvedValue({ data: { status: "accepted" } } as never);

    installCrewInteractions();
    await renderCrew();
    document.querySelector<HTMLButtonElement>(".agent-log-link")?.click();
    await waitFor(() => {
      expect((document.getElementById("agent-log-drawer") as HTMLElement).style.display).toBe("flex");
    });

    const input = document.getElementById("log-drawer-input") as HTMLTextAreaElement;
    input.value = "x".repeat(950_000);
    document.getElementById("log-drawer-composer")?.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(post).not.toHaveBeenCalled();
    expect(input.value.length).toBe(950_000);
    expect(document.getElementById("log-drawer-count")?.textContent).not.toBe("1 entry");
  });

  it("renders transcript image assets through attachment and session asset URLs", async () => {
    const encoded = "ACc+7nX4cJ2Rgb+u5vx7aFMH8dJLSAxwuQAQCo3P3fOM6/FiOyMb".repeat(50);
    setupCrewCockpitDom();
    vi.spyOn(api, "GET").mockImplementation(async (path: string) => {
      if (path === "/v0/city/{cityName}/sessions") {
        return { data: { items: [crewSession("s-mayor")] } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}") {
        return {
          data: {
            ...crewSession("s-mayor"),
            context_pct: 42,
            model: "gpt-5",
            provider: "codex",
            submission_capabilities: { supports_follow_up: false, supports_interrupt_now: false },
          },
        } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/pending") {
        return { data: { pending: null, supported: true } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/transcript") {
        return {
          data: {
            turns: [{
              role: "user",
              text: [
                "Please inspect this.",
                "Attached images:",
                "1. sent.png",
                "   ![sent](/v0/city/mc-city/session/s-mayor/attachments/att-3/sent.png)",
                "   Local file: /tmp/test-city/.gc/dashboard/attachments/s-mayor/att-3/sent.png",
                "Use the local file path to inspect the image when needed. Do not inline or decode base64.",
              ].join("\n"),
              timestamp: "2026-04-18T19:59:00Z",
            }, {
              role: "assistant",
              text: "I see attached image: sent.png.",
              trace: [{ kind: "thinking", text: "The attached screenshot is available in the transcript." }],
              timestamp: "2026-04-18T19:59:30Z",
            }, {
              role: "assistant",
              text: "Found another image: motion-proof.png. Opening local file.",
              timestamp: "2026-04-18T19:59:40Z",
            }, {
              role: "assistant",
              text: [
                "Viewed Image",
                "└ ../../dashboard/attachments/gr-ixw/86e29c944f316507c41b1983ec6bed5c/rabble-",
                "remote-play-motion-proof.png",
              ].join("\n"),
              timestamp: "2026-04-18T19:59:45Z",
            }, {
              role: "assistant",
              text: "Saw second image: rabble-remote-play-motion-proof.png.",
              timestamp: "2026-04-18T19:59:50Z",
            }, {
              assets: [
                { kind: "image", name: "local.png", path: "shots/local.png", source: "tool_result" },
                { kind: "image", name: "duplicate.png", path: "shots/local.png", source: "tool_result" },
              ],
              role: "assistant",
              text: [
                "Look at ![existing](/v0/city/mc-city/session/s-mayor/attachments/att-1/existing.png)",
                "Attached images:",
                "1. wrapped.png",
                "   ![wrapped](/v0/city/mc-city/session/s-mayor/",
                "attachments/att-2/wrapped.png)",
                "   Local file: /tmp/test-city/.gc/dashboard/attachments/s-mayor/att-2/wrapped.png",
                "Use the local file path to inspect the image when needed. Do not inline or decode base64.",
                "and ![local](shots/local.png)",
                "Tool opened shots/plain.webp",
                encoded,
              ].join("\n"),
              timestamp: "2026-04-18T20:00:00Z",
            }],
            pagination: {
              has_older_messages: false,
              returned_message_count: 1,
              total_compactions: 0,
              total_message_count: 1,
            },
          },
        } as never;
      }
      throw new Error(`unexpected GET ${path}`);
    });

    installCrewInteractions();
    await renderCrew();
    document.querySelector<HTMLButtonElement>(".agent-log-link")?.click();
    await waitFor(() => {
      expect(document.querySelectorAll(".log-msg-image").length).toBeGreaterThanOrEqual(3);
    });

    const srcs = Array.from(document.querySelectorAll<HTMLImageElement>(".log-msg-image")).map((img) => img.getAttribute("src"));
    expect(srcs.filter((src) => src === "/v0/city/mc-city/session/s-mayor/attachments/att-3/sent.png")).toHaveLength(1);
    expect(srcs).not.toContain("/v0/city/mc-city/session/s-mayor/asset?path=sent.png");
    expect(srcs.filter((src) => src === "/v0/city/mc-city/session/gr-ixw/attachments/86e29c944f316507c41b1983ec6bed5c/rabble-remote-play-motion-proof.png")).toHaveLength(1);
    expect(srcs).not.toContain("/v0/city/mc-city/session/s-mayor/asset?path=rabble-remote-play-motion-proof.png");
    expect(srcs).toContain("/v0/city/mc-city/session/s-mayor/attachments/att-1/existing.png");
    expect(srcs).toContain("/v0/city/mc-city/session/s-mayor/attachments/att-2/wrapped.png");
    expect(srcs).toContain("/v0/city/mc-city/session/s-mayor/asset?path=shots%2Flocal.png");
    expect(srcs).toContain("/v0/city/mc-city/session/s-mayor/asset?path=shots%2Fplain.webp");
    expect(document.getElementById("log-drawer-messages")?.textContent).toContain("large encoded image data omitted");
    expect(document.getElementById("log-drawer-messages")?.textContent).not.toContain("Attached images");
    expect(document.getElementById("log-drawer-messages")?.textContent).not.toContain("Local file");
    expect(document.getElementById("log-drawer-messages")?.textContent).not.toContain(encoded.slice(0, 40));
    const trace = document.querySelector<HTMLDetailsElement>(".log-msg-activity");
    expect(trace?.open).toBe(false);
    expect(trace?.querySelector("summary")?.textContent).toContain("reasoning");
    expect(document.querySelector(".log-msg-trace")).toBeNull();
    expect(Array.from(document.querySelectorAll(".log-msg-body")).some((node) => node.textContent?.includes("attached screenshot is available"))).toBe(false);

    document.querySelector<HTMLButtonElement>(".log-msg-image-frame")?.click();
    expect(document.getElementById("log-image-preview")?.getAttribute("data-open")).toBe("true");
    const mediaToggle = document.querySelector<HTMLButtonElement>(".log-msg-media-toggle");
    mediaToggle?.click();
    expect(document.querySelector<HTMLElement>(".log-msg-attachments")?.hasAttribute("hidden")).toBe(true);
    expect(mediaToggle?.getAttribute("aria-expanded")).toBe("false");
    expect(mediaToggle?.textContent).toContain("Show media");
    mediaToggle?.click();
    expect(document.querySelector<HTMLElement>(".log-msg-attachments")?.hasAttribute("hidden")).toBe(false);
    expect(mediaToggle?.getAttribute("aria-expanded")).toBe("true");
    expect(mediaToggle?.textContent).toContain("Hide media");
  });

  it("collapses context and tool plumbing while keeping tool images visible once", async () => {
    setupCrewCockpitDom();
    vi.spyOn(api, "GET").mockImplementation(async (path: string) => {
      if (path === "/v0/city/{cityName}/sessions") {
        return { data: { items: [crewSession("s-mayor")] } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}") {
        return { data: { ...crewSession("s-mayor"), provider: "codex" } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/pending") {
        return { data: { pending: null, supported: true } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/transcript") {
        return {
          data: {
            turns: [{
              role: "user",
              text: "<environment_context>\n  <cwd>/tmp/city</cwd>\n</environment_context>",
            }, {
              role: "assistant",
              text: "[view_image]\n{\"path\":\"shots/preview.png\"}",
              assets: [{ kind: "image", name: "preview.png", path: "shots/preview.png", source: "tool_use" }],
            }, {
              role: "assistant",
              text: "Saw second image: preview.png.",
              assets: [{ kind: "image", name: "preview.png", path: "preview.png", source: "text" }],
            }, {
              role: "tool_result",
              text: "[result] file contents",
            }, {
              role: "assistant",
              text: "",
              trace: [{ kind: "thinking", text: "checking whether the tool output matters" }],
            }, {
              role: "assistant",
              text: "",
              trace: [{ kind: "thinking", text: "checking whether the tool output matters" }],
            }],
            pagination: { has_older_messages: false, returned_message_count: 6, total_compactions: 0, total_message_count: 6 },
          },
        } as never;
      }
      throw new Error(`unexpected GET ${path}`);
    });

    installCrewInteractions();
    await renderCrew();
    document.querySelector<HTMLButtonElement>(".agent-log-link")?.click();
    await waitFor(() => {
      expect(document.querySelectorAll(".log-msg-activity, .log-msg-activity-standalone").length).toBeGreaterThanOrEqual(2);
    });

    const attached = document.querySelector<HTMLDetailsElement>(".log-msg-assistant .log-msg-activity");
    expect(attached?.open).toBe(false);
    expect(attached?.querySelector("summary")?.textContent).toContain("Worked");
    expect(attached?.querySelector("summary")?.textContent).toContain("1 tool");
    expect(attached?.querySelector("summary")?.textContent).toContain("1 context");
    expect(attached?.querySelector(".log-msg-activity-summary-preview")?.textContent).not.toContain("<environment_context>");
    expect(attached?.textContent).toContain("Environment context");
    expect(attached?.textContent).toContain("Tool call · view_image");
    expect(attached?.textContent).toContain("shots/preview.png");
    expect(document.querySelector(".log-msg-activity-standalone")?.textContent).toContain("Reasoning");
    expect(document.querySelector(".log-msg-activity-standalone")?.textContent).toContain("Tool result");
    expect(document.querySelectorAll(".log-msg-reasoning")).toHaveLength(0);
    const srcs = Array.from(document.querySelectorAll<HTMLImageElement>(".log-msg-image")).map((img) => img.getAttribute("src"));
    expect(srcs).toEqual(["/v0/city/mc-city/session/s-mayor/asset?path=shots%2Fpreview.png"]);
  });

  it("attaches consecutive tool plumbing to the next output message", async () => {
    setupCrewCockpitDom();
    vi.spyOn(api, "GET").mockImplementation(async (path: string) => {
      if (path === "/v0/city/{cityName}/sessions") {
        return { data: { items: [crewSession("s-mayor")] } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}") {
        return { data: { ...crewSession("s-mayor"), provider: "codex" } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/pending") {
        return { data: { pending: null, supported: true } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/transcript") {
        return {
          data: {
            turns: [{
              role: "assistant",
              parts: [{ id: "call-1", type: "tool", tool: "exec_command", input: { cmd: "gc hook mayor" } }],
            }, {
              role: "tool_result",
              parts: [{ tool_use_id: "call-1", type: "tool", output: "hook returned no routed work" }],
            }, {
              role: "assistant",
              parts: [{ id: "call-2", type: "tool", tool: "write_stdin", input: { chars: "\u0003" } }],
            }, {
              role: "assistant",
              parts: [{ type: "text", text: "Mail empty. Staying idle." }],
            }],
            pagination: { has_older_messages: false, returned_message_count: 4, total_compactions: 0, total_message_count: 4 },
          },
        } as never;
      }
      throw new Error(`unexpected GET ${path}`);
    });

    installCrewInteractions();
    await renderCrew();
    document.querySelector<HTMLButtonElement>(".agent-log-link")?.click();
    await waitFor(() => {
      expect(document.querySelector(".log-msg-assistant .log-msg-activity")).not.toBeNull();
    });

    const group = document.querySelector<HTMLElement>(".log-msg-assistant .log-msg-activity")!;
    expect(group.querySelector(".log-msg-activity-summary")?.textContent).toContain("2 tools");
    expect(group.querySelector(".log-msg-activity-summary")?.textContent).toContain("Worked");
    expect(group.querySelectorAll(".log-msg-activity-item")).toHaveLength(2);
    expect(group.querySelectorAll<HTMLDetailsElement>(".log-msg-activity-item")[0]?.open).toBe(false);
    expect(group.querySelector(".log-msg-activity-item-header")?.tagName).toBe("SUMMARY");
    expect(group.textContent).toContain("Tool · exec_command");
    expect(group.textContent).toContain("done");
    expect(group.textContent).toContain("Tool running · write_stdin");
    expect(group.textContent).toContain("Input");
    expect(group.textContent).toContain("Output");
    expect(document.querySelectorAll(".log-msg")).toHaveLength(1);
    expect(document.querySelector<HTMLDetailsElement>(".log-msg-assistant .log-msg-activity")?.open).toBe(false);
    const assistant = document.querySelector<HTMLElement>(".log-msg-assistant")!;
    const body = assistant.querySelector(".log-msg-body")!;
    expect(Boolean(group.compareDocumentPosition(body) & Node.DOCUMENT_POSITION_FOLLOWING)).toBe(true);
  });

  it("collapses interim agent updates into the final response details", async () => {
    setupCrewCockpitDom();
    vi.spyOn(api, "GET").mockImplementation(async (path: string) => {
      if (path === "/v0/city/{cityName}/sessions") {
        return { data: { items: [crewSession("s-mayor")] } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}") {
        return { data: { ...crewSession("s-mayor"), provider: "codex" } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/pending") {
        return { data: { pending: null, supported: true } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/transcript") {
        return {
          data: {
            turns: [{
              role: "user",
              parts: [{ type: "text", text: "look for another image and read it" }],
            }, {
              role: "assistant",
              parts: [
                { type: "text", text: "Looking for image files in dashboard attachments." },
                { type: "reasoning", text: "Use the existing attachments directory first." },
              ],
            }, {
              role: "assistant",
              parts: [{ id: "call-1", type: "tool", tool: "exec_command", input: { cmd: "find attachments -name '*.png'" } }],
            }, {
              role: "tool_result",
              parts: [{ tool_use_id: "call-1", type: "tool", output: "attachments/preview.png" }],
            }, {
              role: "assistant",
              parts: [{ type: "text", text: "Found another image: preview.png. Opening local file." }],
            }, {
              role: "assistant",
              parts: [
                { id: "call-2", type: "tool", tool: "view_image", input: { path: "attachments/preview.png" } },
                { kind: "image", path: "attachments/preview.png", type: "image" },
              ],
            }, {
              role: "assistant",
              parts: [{ type: "text", text: "Saw second image: preview.png.\n\nIt shows a space combat scene." }],
            }],
            pagination: { has_older_messages: false, returned_message_count: 7, total_compactions: 0, total_message_count: 7 },
          },
        } as never;
      }
      throw new Error(`unexpected GET ${path}`);
    });

    installCrewInteractions();
    await renderCrew();
    document.querySelector<HTMLButtonElement>(".agent-log-link")?.click();
    await waitFor(() => {
      expect(document.querySelector(".log-msg-assistant .log-msg-body")?.textContent).toContain("Saw second image");
    });

    const assistantMessages = Array.from(document.querySelectorAll<HTMLElement>(".log-msg-assistant"));
    expect(assistantMessages).toHaveLength(1);
    expect(assistantMessages[0]?.querySelector(".log-msg-body")?.textContent).toContain("Saw second image");
    expect(assistantMessages[0]?.querySelector(".log-msg-body")?.textContent).not.toContain("Looking for image files");
    expect(assistantMessages[0]?.querySelector(".log-msg-body")?.textContent).not.toContain("Found another image");

    const activity = assistantMessages[0]?.querySelector<HTMLDetailsElement>(".log-msg-activity");
    expect(activity?.open).toBe(false);
    expect(activity?.querySelector("summary")?.textContent).toContain("Worked");
    expect(activity?.querySelector("summary")?.textContent).toContain("2 tools");
    expect(activity?.querySelector("summary")?.textContent).toContain("2 updates");
    expect(activity?.querySelector("summary")?.textContent).toContain("reasoning");
    expect(activity?.querySelectorAll<HTMLDetailsElement>(".log-msg-activity-item")[0]?.open).toBe(false);
    expect(Array.from(activity?.querySelectorAll<HTMLDetailsElement>(".log-msg-activity-progress") ?? []).every((item) => item.open)).toBe(true);
    expect(Array.from(activity?.querySelectorAll<HTMLDetailsElement>(".log-msg-activity-tool") ?? []).some((item) => item.open)).toBe(false);
    expect(activity?.textContent).toContain("Looking for image files");
    expect(activity?.textContent).toContain("Found another image");
    expect(activity?.textContent).toContain("Tool · exec_command");
    expect(activity?.textContent).toContain("Tool running · view_image");

    const srcs = Array.from(document.querySelectorAll<HTMLImageElement>(".log-msg-image")).map((img) => img.getAttribute("src"));
    expect(srcs).toEqual(["/v0/city/mc-city/session/s-mayor/asset?path=attachments%2Fpreview.png"]);
  });

  it("does not duplicate legacy trace when structured reasoning parts are present", async () => {
    setupCrewCockpitDom();
    vi.spyOn(api, "GET").mockImplementation(async (path: string) => {
      if (path === "/v0/city/{cityName}/sessions") {
        return { data: { items: [crewSession("s-mayor")] } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}") {
        return { data: { ...crewSession("s-mayor"), provider: "codex" } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/pending") {
        return { data: { pending: null, supported: true } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/transcript") {
        return {
          data: {
            turns: [{
              role: "assistant",
              parts: [
                { type: "reasoning", text: "checking whether direct parts are enough" },
                { type: "text", text: "Direct parts rendered." },
              ],
              trace: [{ kind: "thinking", text: "checking whether direct parts are enough" }],
            }],
            pagination: { has_older_messages: false, returned_message_count: 1, total_compactions: 0, total_message_count: 1 },
          },
        } as never;
      }
      throw new Error(`unexpected GET ${path}`);
    });

    installCrewInteractions();
    await renderCrew();
    document.querySelector<HTMLButtonElement>(".agent-log-link")?.click();
    await waitFor(() => {
      expect(document.querySelector(".log-msg-assistant .log-msg-activity")).not.toBeNull();
    });

    const group = document.querySelector<HTMLElement>(".log-msg-assistant .log-msg-activity")!;
    expect(group.querySelector(".log-msg-activity-summary")?.textContent).toContain("reasoning");
    expect(group.querySelectorAll(".log-msg-activity-item")).toHaveLength(1);
    expect(group.querySelector(".log-msg-activity-item-body")?.textContent?.match(/checking whether direct parts are enough/g)).toHaveLength(1);
    expect(document.querySelector(".log-msg-body")?.textContent).toContain("Direct parts rendered.");
  });

  it("keeps submit intent selector hidden even when alternate intents are supported", async () => {
    setupCrewCockpitDom();
    vi.spyOn(api, "GET").mockImplementation(async (path: string) => {
      if (path === "/v0/city/{cityName}/sessions") {
        return { data: { items: [crewSession("s-mayor")] } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}") {
        return {
          data: {
            ...crewSession("s-mayor"),
            provider: "codex",
            submission_capabilities: { supports_follow_up: true, supports_interrupt_now: true },
          },
        } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/pending") {
        return { data: { pending: null, supported: true } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/transcript") {
        return {
          data: {
            turns: [],
            pagination: { has_older_messages: false, returned_message_count: 0, total_compactions: 0, total_message_count: 0 },
          },
        } as never;
      }
      throw new Error(`unexpected GET ${path}`);
    });
    const posts: Array<{ body?: { intent?: string; message?: string }; path: string }> = [];
    vi.spyOn(api, "POST").mockImplementation(async (path: string, options?: unknown) => {
      posts.push({ path, body: (options as { body?: { intent?: string; message?: string } } | undefined)?.body });
      return { data: { event_cursor: "12", request_id: "req-chat-1", status: "accepted" } } as never;
    });

    installCrewInteractions();
    await renderCrew();
    document.querySelector<HTMLButtonElement>(".agent-log-link")?.click();
    await waitFor(() => {
      expect(document.getElementById("log-drawer-meta")?.textContent).toContain("codex");
    });
    expect((document.getElementById("log-drawer-intent-wrap") as HTMLElement).style.display).toBe("none");
    const select = document.getElementById("log-drawer-intent") as HTMLSelectElement;
    expect(Array.from(select.options).map((option) => option.value)).toEqual(["default"]);
    (document.getElementById("log-drawer-input") as HTMLTextAreaElement).value = "take this now";
    document.getElementById("log-drawer-composer")?.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => {
      expect(posts[0]?.body?.intent).toBe("default");
    });
  });

  it("keeps submit intent selector hidden without alternate capabilities", async () => {
    setupCrewCockpitDom();
    vi.spyOn(api, "GET").mockImplementation(async (path: string) => {
      if (path === "/v0/city/{cityName}/sessions") {
        return { data: { items: [crewSession("s-mayor")] } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}") {
        return {
          data: {
            ...crewSession("s-mayor"),
            provider: "codex",
            submission_capabilities: { supports_follow_up: false, supports_interrupt_now: false },
          },
        } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/pending") {
        return { data: { pending: null, supported: true } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/transcript") {
        return {
          data: {
            turns: [],
            pagination: { has_older_messages: false, returned_message_count: 0, total_compactions: 0, total_message_count: 0 },
          },
        } as never;
      }
      throw new Error(`unexpected GET ${path}`);
    });

    installCrewInteractions();
    await renderCrew();
    document.querySelector<HTMLButtonElement>(".agent-log-link")?.click();
    await waitFor(() => {
      expect(document.getElementById("log-drawer-meta")?.textContent).toContain("codex");
    });
    expect((document.getElementById("log-drawer-intent-wrap") as HTMLElement).style.display).toBe("none");
  });

  it("posts pending interaction responses from the cockpit card", async () => {
    setupCrewCockpitDom();
    vi.spyOn(api, "GET").mockImplementation(async (path: string) => {
      if (path === "/v0/city/{cityName}/sessions") {
        return { data: { items: [crewSession("s-mayor")] } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}") {
        return { data: { ...crewSession("s-mayor"), provider: "codex" } } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/pending") {
        return {
          data: {
            pending: { kind: "approval", options: ["allow", "deny"], prompt: "Run command?", request_id: "req-approval" },
            supported: true,
          },
        } as never;
      }
      if (path === "/v0/city/{cityName}/session/{id}/transcript") {
        return {
          data: {
            turns: [],
            pagination: { has_older_messages: false, returned_message_count: 0, total_compactions: 0, total_message_count: 0 },
          },
        } as never;
      }
      throw new Error(`unexpected GET ${path}`);
    });
    const posts: Array<{ body?: { action?: string; request_id?: string }; path: string }> = [];
    vi.spyOn(api, "POST").mockImplementation(async (path: string, options?: unknown) => {
      posts.push({ path, body: (options as { body?: { action?: string; request_id?: string } } | undefined)?.body });
      return { data: { id: "s-mayor", status: "accepted" } } as never;
    });

    installCrewInteractions();
    await renderCrew();
    document.querySelector<HTMLButtonElement>(".agent-log-link")?.click();
    await waitFor(() => {
      expect(document.getElementById("log-drawer-pending")?.textContent).toContain("Run command?");
    });
    document.querySelector<HTMLButtonElement>('[data-pending-action="allow"]')?.click();

    await waitFor(() => {
      expect(posts[0]?.path).toBe("/v0/city/{cityName}/session/{id}/respond");
    });
    expect(posts[0]?.body).toEqual(expect.objectContaining({ action: "allow", request_id: "req-approval" }));
  });
});

function setupCrewCockpitDom(): void {
  document.body.innerHTML = `
    <div id="crew-loading">Loading crew...</div>
    <table id="crew-table" style="display:none"><tbody id="crew-tbody"></tbody></table>
    <div id="crew-empty" style="display:none"><p>No crew configured</p></div>
    <div id="rigged-body"></div>
    <div id="pooled-body"></div>
    <span id="crew-count"></span>
    <span id="rigged-count"></span>
    <span id="pooled-count"></span>
    <div id="agent-log-drawer" style="display:none">
      <span id="log-drawer-agent-name"></span>
      <span id="log-drawer-count"></span>
      <span id="log-drawer-status"></span>
      <button id="log-drawer-older-btn" style="display:none">Load older</button>
      <button id="log-drawer-close-btn">Close</button>
      <div id="log-drawer-meta" style="display:none"></div>
      <div id="log-drawer-pending" style="display:none"></div>
      <div id="log-drawer-body">
        <div id="log-drawer-messages">
          <div id="log-drawer-loading">Loading logs...</div>
        </div>
      </div>
      <form id="log-drawer-composer">
        <label id="log-drawer-intent-wrap" style="display:none">
          <select id="log-drawer-intent"></select>
        </label>
        <button id="log-drawer-attach-btn" type="button">Attach images</button>
        <input id="log-drawer-file-input" type="file" />
        <div id="log-drawer-attachments"></div>
        <textarea id="log-drawer-input"></textarea>
        <button id="log-drawer-send-btn" type="submit">Send</button>
      </form>
    </div>
  `;
}

function crewSession(id: string): Record<string, unknown> {
  return {
    active_bead: "",
    agent_kind: "crew",
    attached: false,
    id,
    last_active: "2026-04-18T20:00:00Z",
    last_output: "",
    running: true,
    template: "mayor",
  };
}

// Slow Blacksmith CI runs have shown the openLogDrawer + loadTranscript
// chain take ~1.3s while passing runs finish in ~100ms — same VM class,
// same code. The 1s budget here was missing those slow runs by a few
// hundred ms even though the chain ultimately completed (the
// `[crew] Transcript loaded` debug log fires *after* the assertion times
// out). Five seconds keeps the local cost negligible and absorbs the
// observed CI variance.
async function waitFor(assertion: () => void): Promise<void> {
  const started = Date.now();
  let lastError: unknown;
  while (Date.now() - started < 5000) {
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
