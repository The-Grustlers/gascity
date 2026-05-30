import { cityScope } from "../api";
import { byId, clear, el } from "../util/dom";
import { formatTimestamp } from "../util/legacy";
import { apiURL, attachmentImageSrc, sessionAssetPath, sessionAttachmentFilePath } from "./session_paths";
import type {
  ActivityItem,
  ChatAttachment,
  DisplayTurn,
  RenderAttachment,
  TranscriptAsset,
  TranscriptPart,
  TranscriptTrace,
  TranscriptTurn,
  TurnPresentation,
  StreamTurnPayload,
} from "./session_types";

let transcriptSessionID = "";
let transcriptSessionLabel = "";
const knownTranscriptImagesByName = new Map<string, RenderAttachment>();
const renderedTranscriptImageSrcs = new Set<string>();
const TRANSCRIPT_INLINE_IMAGE_LIMIT_BYTES = 900_000;

export function setTranscriptRenderContext(sessionID: string, label: string): void {
  transcriptSessionID = sessionID;
  transcriptSessionLabel = label;
}

export function resetTranscriptRenderState(): void {
  knownTranscriptImagesByName.clear();
  renderedTranscriptImageSrcs.clear();
}

export function resetTranscriptRenderContext(): void {
  transcriptSessionID = "";
  transcriptSessionLabel = "";
  resetTranscriptRenderState();
}

export function clearRenderedTranscriptImages(): void {
  renderedTranscriptImageSrcs.clear();
}

export function appendDisplayTurns(container: Node, turns: DisplayTurn[]): number {
  for (const turn of turns) {
    if (isStandaloneActivityTurn(turn)) {
      container.appendChild(renderStandaloneActivityTurn(turn));
    } else {
      container.appendChild(renderTurn(turn.role, turn.text, turn.timestamp, [], turn.assets ?? [], turn.trace ?? [], turn.activity ?? []));
    }
  }
  return turns.length;
}

export function isStandaloneActivityDisplayTurn(turn: DisplayTurn): boolean {
  return isStandaloneActivityTurn(turn);
}

export function isOutputMessageDisplayTurn(turn: DisplayTurn): boolean {
  return isOutputMessageTurn(turn);
}

export function isUserMessageDisplayTurn(turn: DisplayTurn): boolean {
  return isUserMessageTurn(turn);
}

export function attachActivityTurnsToFirstOutput(turns: DisplayTurn[], activityTurns: DisplayTurn[]): { attached: boolean; turns: DisplayTurn[] } {
  if (activityTurns.length === 0) return { attached: false, turns };
  const outputIndex = turns.findIndex((turn) => isOutputMessageTurn(turn));
  if (outputIndex < 0) return { attached: false, turns };

  const activity = activityTurns.flatMap((turn) => {
    if (isOutputMessageTurn(turn) && turn.text.trim() !== "") {
      return appendActivityItems(turn.activity ?? [], [progressActivityItemFromTurn(turn)]);
    }
    return turn.activity ?? [];
  });
  const assets = activityTurns.flatMap((turn) => turn.assets ?? []);
  if (activity.length === 0 && assets.length === 0) return { attached: false, turns };

  const next = [...turns];
  const target = next[outputIndex]!;
  next[outputIndex] = {
    ...target,
    activity: appendActivityItems(activity, target.activity ?? []),
    assets: appendTranscriptAssets([...assets], target.assets ?? []),
  };
  return { attached: true, turns: next };
}

export function expandTranscriptTurns(turns: TranscriptTurn[]): DisplayTurn[] {
  const expanded = turns.flatMap((turn) => {
    if ((turn.parts ?? []).length > 0) {
      return expandStructuredTranscriptTurn(turn);
    }
    return expandTranscriptTurn(turn.role ?? "agent", turn.text ?? "", turn.timestamp, turn.assets ?? [], turn.trace ?? []);
  });
  return collapseAssistantRunIntermediates(attachActivityToOutputTurns(dedupeConsecutiveDisplayTurns(expanded)));
}

export function shouldReplaceWithStreamSnapshot(payload: StreamTurnPayload): boolean {
  const turns = payload.turns ?? [];
  return payload.format === "text" || turns.some((turn) => isTerminalTranscript(turn.role ?? "", turn.text ?? ""));
}

export function turnCountLabel(count: number): string {
  return `${count} ${count === 1 ? "entry" : "entries"}`;
}

function expandTranscriptTurn(role: string, text: string, timestamp: string | undefined, assets: TranscriptAsset[] = [], trace: TranscriptTrace[] = []): DisplayTurn[] {
  if (!isTerminalTranscript(role, text)) {
    return [{ assets, role, text, timestamp, trace }];
  }
  const parsed = parseTerminalTranscript(text, timestamp);
  if (parsed.length > 0) {
    if (assets.length > 0) {
      parsed[parsed.length - 1] = { ...parsed[parsed.length - 1], assets };
    }
    if (trace.length > 0) {
      parsed[parsed.length - 1] = { ...parsed[parsed.length - 1], trace };
    }
    return parsed;
  }
  return [{ assets, role, text, timestamp, trace }];
}

function expandStructuredTranscriptTurn(turn: TranscriptTurn): DisplayTurn[] {
  const role = turn.role ?? "agent";
  const timestamp = turn.timestamp;
  const parts = turn.parts ?? [];
  const textParts: string[] = [];
  const trace: TranscriptTrace[] = [];
  const activity: ActivityItem[] = [];
  let assets = appendTranscriptAssets([], turn.assets ?? []);

  for (const part of parts) {
    const type = (part.type ?? "").toLowerCase();
    if (type === "text") {
      const text = (part.text ?? "").trim();
      if (text !== "") textParts.push(text);
      continue;
    }
    if (type === "reasoning" || type === "thinking") {
      const text = (part.text ?? "").trim();
      if (text !== "") trace.push({ kind: "thinking", text });
      continue;
    }
    if (type === "file" || type === "image") {
      const asset = transcriptAssetFromPart(part);
      if (asset) assets = appendTranscriptAssets(assets, [asset]);
      continue;
    }
    if (type === "tool" || type === "tool_use" || type === "tool_result") {
      activity.push(toolActivityItemFromPart(part, timestamp));
      continue;
    }
    if (type === "interaction") {
      activity.push(interactionActivityItemFromPart(part, timestamp));
    }
  }

  if (trace.length === 0) {
    trace.push(...(turn.trace ?? []));
  }

  const text = textParts.join("\n\n");
  return [{
    activity,
    assets,
    role,
    text,
    timestamp,
    trace,
  }];
}

function dedupeConsecutiveDisplayTurns(turns: DisplayTurn[]): DisplayTurn[] {
  const deduped: DisplayTurn[] = [];
  let previousKey = "";
  for (const turn of turns) {
    const key = displayTurnKey(turn);
    if (key !== "" && key === previousKey) continue;
    deduped.push(turn);
    previousKey = key;
  }
  return deduped;
}

function attachActivityToOutputTurns(turns: DisplayTurn[]): DisplayTurn[] {
  const grouped: DisplayTurn[] = [];
  let pendingItems: ActivityItem[] = [];
  let pendingAssets: TranscriptAsset[] = [];
  const flush = () => {
    if (pendingItems.length > 0) {
      grouped.push({
        activity: pendingItems,
        assets: pendingAssets,
        role: "tool",
        text: "",
        timestamp: pendingItems[0]?.timestamp,
      });
    }
    pendingItems = [];
    pendingAssets = [];
  };

  for (const turn of turns) {
    const activity = activityItemsForTurn(turn);
    if (isCollapsedActivityTurn(turn, activity)) {
      pendingItems = appendActivityItems(pendingItems, activity);
      pendingAssets = appendTranscriptAssets(pendingAssets, turn.assets ?? []);
      continue;
    }
    const turnActivity = activity.length > 0 ? activity : [];
    if (pendingItems.length > 0 && isOutputMessageTurn(turn)) {
      grouped.push({
        ...turn,
        activity: appendActivityItems(pendingItems, turnActivity),
        assets: appendTranscriptAssets(pendingAssets, turn.assets ?? []),
        trace: [],
      });
      pendingItems = [];
      pendingAssets = [];
      continue;
    }
    if (pendingItems.length > 0 && isUserMessageTurn(turn)) {
      grouped.push({
        ...turn,
        activity: turnActivity,
        trace: turnActivity.length > 0 ? [] : turn.trace,
      });
      continue;
    }
    flush();
    grouped.push({
      ...turn,
      activity: turnActivity,
      trace: turnActivity.length > 0 ? [] : turn.trace,
    });
  }
  flush();
  return grouped;
}

function collapseAssistantRunIntermediates(turns: DisplayTurn[]): DisplayTurn[] {
  const collapsed: DisplayTurn[] = [];
  let run: DisplayTurn[] = [];
  let collectingRun = false;

  const flushRun = () => {
    if (run.length > 0) {
      collapsed.push(...collapseAssistantRun(run));
      run = [];
    }
  };

  for (const turn of turns) {
    if (isUserMessageTurn(turn)) {
      flushRun();
      collapsed.push(turn);
      collectingRun = true;
      continue;
    }
    if (!collectingRun) {
      collapsed.push(turn);
      continue;
    }
    run.push(turn);
  }

  flushRun();
  return collapsed;
}

function collapseAssistantRun(run: DisplayTurn[]): DisplayTurn[] {
  const outputIndexes = run
    .map((turn, index) => isOutputMessageTurn(turn) ? index : -1)
    .filter((index) => index >= 0);
  if (outputIndexes.length <= 1) return run;

  const finalOutputIndex = outputIndexes[outputIndexes.length - 1];
  const collapsed: DisplayTurn[] = [];
  let pendingItems: ActivityItem[] = [];
  let pendingAssets: TranscriptAsset[] = [];

  for (let index = 0; index < run.length; index += 1) {
    const turn = run[index]!;
    if (index < finalOutputIndex && isOutputMessageTurn(turn)) {
      pendingItems = appendActivityItems(pendingItems, turn.activity ?? [], [progressActivityItemFromTurn(turn)]);
      pendingAssets = appendTranscriptAssets(pendingAssets, turn.assets ?? []);
      pendingAssets = appendTranscriptAssets(pendingAssets, transcriptAssetsFromCollapsedTurn(turn));
      continue;
    }
    if (index === finalOutputIndex && isOutputMessageTurn(turn)) {
      collapsed.push({
        ...turn,
        activity: appendActivityItems(pendingItems, turn.activity ?? []),
        assets: appendTranscriptAssets(pendingAssets, turn.assets ?? []),
        trace: [],
      });
      pendingItems = [];
      pendingAssets = [];
      continue;
    }
    collapsed.push(turn);
  }

  return collapsed;
}

function progressActivityItemFromTurn(turn: DisplayTurn): ActivityItem {
  const text = turn.text.trim();
  return {
    detailText: text,
    kind: "progress",
    label: "update",
    preview: summarizeActivityText(text),
    summary: `${displayRoleLabel(turn.role)} update`,
    timestamp: turn.timestamp,
  };
}

function transcriptAssetsFromCollapsedTurn(turn: DisplayTurn): TranscriptAsset[] {
  const parsed = extractInlineImageAttachments(turn.text);
  return parsed.attachments.map((attachment) => ({
    kind: "image",
    name: attachment.name,
    source: "collapsed_text",
    url: attachment.src,
  }));
}

function isStandaloneActivityTurn(turn: DisplayTurn): boolean {
  return (turn.text ?? "").trim() === "" && (turn.activity ?? []).length > 0;
}

function isOutputMessageTurn(turn: DisplayTurn): boolean {
  const presentation = turnPresentation(turn.role, turn.text.trim(), turn.trace ?? []);
  if (presentation.kind !== "message") return false;
  const normalized = (turn.role ?? "").toLowerCase();
  return normalized === "assistant" || normalized === "agent";
}

function isUserMessageTurn(turn: DisplayTurn): boolean {
  const presentation = turnPresentation(turn.role, turn.text.trim(), turn.trace ?? []);
  return presentation.kind === "message" && (turn.role ?? "").toLowerCase() === "user";
}

function isCollapsedActivityTurn(turn: DisplayTurn, activity: ActivityItem[]): boolean {
  if (activity.length === 0) return false;
  const presentation = turnPresentation(turn.role, turn.text.trim(), turn.trace ?? []);
  return (turn.text ?? "").trim() === "" || presentation.kind !== "message";
}

function activityItemsForTurn(turn: DisplayTurn): ActivityItem[] {
  const explicit = appendActivityItems(turn.activity ?? []);
  if (explicit.length > 0 && (turn.text ?? "").trim() === "" && (turn.trace ?? []).length === 0) {
    return explicit;
  }
  return appendActivityItems(explicit, derivedActivityItemsForTurn(turn));
}

function derivedActivityItemsForTurn(turn: DisplayTurn): ActivityItem[] {
  const items: ActivityItem[] = [];
  const text = turn.text.trim();
  const presentation = turnPresentation(turn.role, text, turn.trace ?? []);
  if (presentation.kind !== "message") {
    if (presentation.kind === "reasoning") {
      items.push(...reasoningActivityItems(turn.trace ?? [], turn.timestamp, displayRoleLabel(turn.role)));
    } else {
      items.push({
        detailText: presentation.detailText,
        kind: presentation.kind,
        label: presentation.label,
        preview: presentation.kind === "context" ? presentation.summary : summarizeActivityText(presentation.detailText),
        summary: presentation.summary || displayRoleLabel(turn.role),
        timestamp: turn.timestamp,
      });
    }
  }
  if (presentation.kind === "message") {
    items.push(...reasoningActivityItems(turn.trace ?? [], turn.timestamp, displayRoleLabel(turn.role)));
  }
  return items.filter((item) => item.detailText.trim() !== "" || item.summary.trim() !== "");
}

function reasoningActivityItems(trace: TranscriptTrace[], timestamp: string | undefined, label: string): ActivityItem[] {
  return traceThinkingTexts(trace).map((text, index, all) => ({
    detailText: text,
    kind: "reasoning",
    label,
    preview: summarizeActivityText(text),
    summary: all.length > 1 ? `Reasoning ${index + 1}` : "Reasoning",
    timestamp,
  }));
}

function appendActivityItems(...groups: (ActivityItem[] | null | undefined)[]): ActivityItem[] {
  const out: ActivityItem[] = [];
  for (const group of groups) {
    for (const item of group ?? []) {
      const existing = mergeableActivityItem(out, item);
      if (existing) {
        mergeActivityItem(existing, item);
      } else if (duplicateActivityItem(out, item)) {
        continue;
      } else {
        out.push({ ...item });
      }
    }
  }
  return out;
}

function duplicateActivityItem(items: ActivityItem[], item: ActivityItem): boolean {
  if (item.kind !== "context") return false;
  const key = activityItemKey(item);
  if (key === "") return false;
  return items.some((candidate) => candidate.kind === "context" && activityItemKey(candidate) === key);
}

function activityItemKey(item: ActivityItem): string {
  return JSON.stringify({
    detailText: item.detailText.trim(),
    kind: item.kind,
    label: item.label,
    summary: item.summary,
  });
}

function mergeableActivityItem(items: ActivityItem[], item: ActivityItem): ActivityItem | null {
  if (item.kind !== "tool" || !item.toolID) return null;
  for (let index = items.length - 1; index >= 0; index -= 1) {
    const candidate = items[index];
    if (candidate.kind !== "tool") continue;
    if (candidate.toolID === item.toolID) return candidate;
    if (candidate.toolID) break;
  }
  return null;
}

function mergeActivityItem(target: ActivityItem, incoming: ActivityItem): void {
  target.toolName ||= incoming.toolName;
  target.inputText ||= incoming.inputText;
  target.outputText ||= incoming.outputText;
  target.detailText = toolActivityDetailText(target.inputText, target.outputText) || target.detailText || incoming.detailText;
  target.preview = target.outputText ? summarizeActivityText(target.outputText) : target.preview || incoming.preview;
  target.status = mergeActivityStatus(target.status, incoming.status);
  target.summary = toolActivitySummary(target.toolName, target.status);
}

function mergeActivityStatus(a: ActivityItem["status"], b: ActivityItem["status"]): ActivityItem["status"] {
  if (a === "error" || b === "error") return "error";
  if (a === "completed" || b === "completed") return "completed";
  if (a === "running" || b === "running") return "running";
  return a ?? b;
}

function appendTranscriptAssets(existing: TranscriptAsset[], incoming: TranscriptAsset[]): TranscriptAsset[] {
  if (incoming.length === 0) return existing;
  const seen = new Set(existing.map(transcriptAssetKey));
  for (const asset of incoming) {
    const key = transcriptAssetKey(asset);
    if (key === "" || seen.has(key)) continue;
    seen.add(key);
    existing.push(asset);
  }
  return existing;
}

function transcriptAssetKey(asset: TranscriptAsset): string {
  if (asset.url) return `url:${asset.url}`;
  if (asset.path) return `path:${asset.path}`;
  return "";
}

function transcriptAssetFromPart(part: TranscriptPart): TranscriptAsset | null {
  const kind = (part.kind ?? "image").toLowerCase();
  if (kind !== "image") return null;
  if (!part.url && !part.path) return null;
  return {
    kind: "image",
    name: part.name,
    path: part.path,
    source: part.source ?? "part",
    url: part.url,
  };
}

function toolActivityItemFromPart(part: TranscriptPart, timestamp: string | undefined): ActivityItem {
  const toolName = (part.tool ?? part.name ?? "").trim();
  const hasOutput = typeof part.output !== "undefined" && part.output !== null;
  const input = formatPartValue(part.input);
  const output = formatPartValue(part.output);
  const status = hasOutput ? (part.is_error ? "error" : "completed") : "running";
  return {
    detailText: toolActivityDetailText(input, output),
    inputText: input || undefined,
    kind: "tool",
    label: "tool",
    outputText: output || undefined,
    preview: summarizeActivityText(output || input),
    status,
    summary: toolActivitySummary(toolName, status),
    timestamp,
    toolID: (part.tool_use_id ?? part.id ?? "").trim() || undefined,
    toolName: toolName || undefined,
  };
}

function interactionActivityItemFromPart(part: TranscriptPart, timestamp: string | undefined): ActivityItem {
  const prompt = (part.prompt ?? part.text ?? "").trim();
  const optionText = (part.options ?? []).filter(Boolean).join(", ");
  const detailText = [
    prompt,
    optionText ? `Options: ${optionText}` : "",
    part.action ? `Action: ${part.action}` : "",
    part.request_id ? `Request: ${part.request_id}` : "",
  ].filter(Boolean).join("\n");
  return {
    detailText,
    kind: "tool",
    label: "interaction",
    summary: part.kind ? `Interaction · ${part.kind}` : "Interaction",
    timestamp,
  };
}

function formatPartValue(value: unknown): string {
  if (typeof value === "undefined" || value === null) return "";
  if (typeof value === "string") return value.trim();
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

function toolActivitySummary(toolName: string | undefined, status: ActivityItem["status"]): string {
  const prefix = status === "error" ? "Tool failed" : status === "completed" ? "Tool" : "Tool running";
  return toolName ? `${prefix} · ${toolName}` : prefix;
}

function toolActivityDetailText(input: string | undefined, output: string | undefined): string {
  return [
    input ? `Input\n${input}` : "",
    output ? `Output\n${output}` : "",
  ].filter(Boolean).join("\n\n");
}

function summarizeActivityText(text: string | undefined, max = 96): string {
  const cleaned = stripActivityMarkdown(text ?? "").replace(/\s+/g, " ").trim();
  if (cleaned.length <= max) return cleaned;
  const cut = cleaned.lastIndexOf(" ", max);
  return `${cleaned.slice(0, cut > 32 ? cut : max).trimEnd()}...`;
}

function stripActivityMarkdown(text: string): string {
  return text
    .replace(/```[\w-]*\n?([\s\S]*?)```/g, (_match, inner: string) => inner.trim())
    .replace(/`([^`]+)`/g, "$1")
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
    .replace(/^>\s?/gm, "")
    .replace(/^#{1,6}\s+/gm, "")
    .replace(/\*{1,3}([^*]+)\*{1,3}/g, "$1")
    .replace(/_{1,3}([^_]+)_{1,3}/g, "$1")
    .trim();
}

function displayTurnKey(turn: DisplayTurn): string {
  const assetKeys = (turn.assets ?? [])
    .map((asset) => `${asset.kind ?? ""}:${asset.url ?? ""}:${asset.path ?? ""}:${asset.name ?? ""}:${asset.source ?? ""}`)
    .sort();
  const traceTexts = traceThinkingTexts(turn.trace ?? []);
  const activityKeys = (turn.activity ?? []).map((item) => `${item.kind}:${item.label}:${item.summary}:${item.detailText}`);
  return JSON.stringify({
    activity: activityKeys,
    assets: assetKeys,
    role: (turn.role ?? "").toLowerCase(),
    text: (turn.text ?? "").trim(),
    trace: traceTexts,
  });
}

function isTerminalTranscript(role: string, text: string): boolean {
  if ((role ?? "").toLowerCase() !== "output") return false;
  return text.includes("\n› ") || text.startsWith("› ") || text.includes("\n• ") || text.startsWith("• ");
}

function parseTerminalTranscript(text: string, timestamp: string | undefined): DisplayTurn[] {
  const turns: DisplayTurn[] = [];
  let current: { dropIfTerminalPrompt: boolean; role: string; lines: string[] } | null = null;

  const flush = (atEnd = false) => {
    if (!current) return;
    const body = trimBlankLines(current.lines).join("\n").trimEnd();
    if (body !== "" && !(atEnd && current.dropIfTerminalPrompt)) {
      turns.push({ role: current.role, text: body, timestamp });
    }
    current = null;
  };
  const startTurn = (role: string, firstLine: string) => {
    flush();
    current = { dropIfTerminalPrompt: false, role, lines: [firstLine] };
  };

  for (const rawLine of text.replace(/\r\n/g, "\n").split("\n")) {
    const line = rawLine.replace(/\s+$/g, "");
    if (isTerminalSeparatorLine(line)) {
      flush();
      continue;
    }
    if (line.startsWith("› ")) {
      startTurn(roleForTerminalPrompt(line.slice(2)), line.slice(2));
      continue;
    }
    if (line.startsWith("• ")) {
      startTurn("assistant", line.slice(2));
      continue;
    }
    if (isTerminalStatusLine(line)) {
      if (current?.role === "user") current.dropIfTerminalPrompt = true;
      continue;
    }
    if (!current) {
      current = { dropIfTerminalPrompt: false, role: "system", lines: [] };
    }
    current.lines.push(line.startsWith("  ") ? line.slice(2) : line);
  }
  flush(true);
  return turns;
}

function roleForTerminalPrompt(prompt: string): string {
  const trimmed = prompt.trim();
  if (
    trimmed.startsWith("<system-reminder>") ||
    /^\[[^\]]+\]\s+\S+\s+•/.test(trimmed) ||
    trimmed.startsWith("Stay idle.")
  ) {
    return "system";
  }
  return "user";
}

function isTerminalSeparatorLine(line: string): boolean {
  return /^[─━═-]{20,}$/.test(line.trim());
}

function isTerminalStatusLine(line: string): boolean {
  const trimmed = line.trim();
  if (/^[\w.-]+-router-cli:\s+/.test(trimmed)) return true;
  return /^(gpt|claude|gemini|kimi|codex|openai|opencode)[\w.-]*(\s+\w+)*\s+·\s+/.test(trimmed);
}

function trimBlankLines(lines: string[]): string[] {
  let start = 0;
  let end = lines.length;
  while (start < end && lines[start]?.trim() === "") start += 1;
  while (end > start && lines[end - 1]?.trim() === "") end -= 1;
  return lines.slice(start, end);
}

export function renderTurn(
  role: string,
  text: string,
  timestamp: string | undefined,
  localAttachments: ChatAttachment[] = [],
  assets: TranscriptAsset[] = [],
  trace: TranscriptTrace[] = [],
  activity: ActivityItem[] = [],
): HTMLElement {
  const className = roleClass(role);
  const parsed = extractInlineImageAttachments(text);
  const bodyText = parsed.text.trim();
  const attachments = suppressRepeatedFilenameAttachments(dedupeRenderAttachments([
    ...parsed.attachments,
    ...assets.map(assetToRenderAttachment).filter((attachment): attachment is RenderAttachment => attachment !== null),
    ...localAttachments.map((attachment) => ({ name: attachment.name, src: attachmentImageSrc(attachment.url) })),
  ]));
  rememberRenderAttachments(attachments);
  attachments.forEach((attachment) => renderedTranscriptImageSrcs.add(attachment.src));
  const mediaBlock = attachments.length > 0 ? renderMediaBlock(attachments) : null;
  const activityBlock = renderActivityBlock(appendActivityItems(activity, reasoningActivityItems(trace, timestamp, displayRoleLabel(role))));
  const presentation = turnPresentation(role, bodyText, trace);
  if (presentation.kind !== "message") {
    return renderCompactTurn(className, presentation, timestamp, activityBlock, mediaBlock);
  }
  return el("div", { class: `log-msg log-msg-${className}` }, [
    el("div", { class: "log-msg-header" }, [
      el("span", { class: `log-msg-type log-msg-type-${className}` }, [displayRoleLabel(role)]),
      el("span", { class: "log-msg-time" }, [formatTimestamp(timestamp)]),
    ]),
    activityBlock,
    bodyText ? el("div", { class: "log-msg-body" }, [bodyText]) : null,
    mediaBlock,
  ]);
}

function renderCompactTurn(
  className: string,
  presentation: TurnPresentation,
  timestamp: string | undefined,
  activityBlock: HTMLElement | null,
  mediaBlock: HTMLElement | null,
): HTMLElement {
  const details = el("details", { class: "log-msg-detail" }, [
    el("summary", { class: "log-msg-detail-summary", title: "Click to expand" }, [
      el("span", { class: `log-msg-type log-msg-type-${className}` }, [presentation.label]),
      el("span", { class: "log-msg-detail-title" }, [presentation.summary]),
      el("span", { class: "log-msg-time" }, [formatTimestamp(timestamp)]),
    ]),
    presentation.detailText ? el("div", { class: "log-msg-body log-msg-detail-body" }, [presentation.detailText]) : null,
    activityBlock,
  ]);
  return el("div", { class: `log-msg log-msg-${className} log-msg-compact log-msg-${presentation.kind}` }, [
    details,
    mediaBlock,
  ]);
}

function renderStandaloneActivityTurn(turn: DisplayTurn): HTMLElement {
  const items = turn.activity ?? [];
  const attachments = suppressRepeatedFilenameAttachments(dedupeRenderAttachments(
    (turn.assets ?? []).map(assetToRenderAttachment).filter((attachment): attachment is RenderAttachment => attachment !== null),
  ));
  rememberRenderAttachments(attachments);
  attachments.forEach((attachment) => renderedTranscriptImageSrcs.add(attachment.src));
  const mediaBlock = attachments.length > 0 ? renderMediaBlock(attachments) : null;
  const details = el("details", { class: "log-msg-detail" }, [
    el("summary", { class: "log-msg-detail-summary", title: "Click to expand" }, [
      el("span", { class: "log-msg-type log-msg-type-result" }, ["details"]),
      el("span", { class: "log-msg-detail-title" }, [activitySummary(items)]),
      el("span", { class: "log-msg-time" }, [formatTimestamp(turn.timestamp)]),
    ]),
    renderActivityList(items),
  ]);
  return el("div", { class: "log-msg log-msg-result log-msg-compact log-msg-activity-standalone" }, [
    details,
    mediaBlock,
  ]);
}

function renderActivityBlock(items: ActivityItem[]): HTMLElement | null {
  if (items.length === 0) return null;
  return el("details", { class: "log-msg-activity" }, [
    el("summary", { class: "log-msg-activity-summary", title: "Click to expand" }, [
      el("span", { class: "log-msg-activity-summary-main" }, [activitySummary(items)]),
      activityPreview(items) ? el("span", { class: "log-msg-activity-summary-preview" }, [activityPreview(items)]) : null,
    ]),
    renderActivityList(items),
  ]);
}

function renderActivityList(items: ActivityItem[]): HTMLElement {
  return el("div", { class: "log-msg-activity-list" }, items.map(renderActivityItem));
}

function renderActivityItem(item: ActivityItem): HTMLElement {
  const body = renderActivityItemBody(item);
  return el("details", { class: `log-msg-activity-item log-msg-activity-${item.kind}`, open: body ? item.kind === "progress" : undefined }, [
    el("summary", { class: "log-msg-activity-item-header", title: body ? "Click to expand" : "" }, [
      el("span", { "aria-hidden": "true", class: "log-msg-activity-item-caret" }, [body ? ">" : ""]),
      el("span", { class: "log-msg-activity-item-label" }, [item.label]),
      el("span", { class: "log-msg-activity-item-title" }, [item.summary]),
      el("span", { class: "log-msg-activity-item-meta" }, [
        item.status ? el("span", { class: `log-msg-activity-status log-msg-activity-status-${item.status}` }, [activityStatusLabel(item.status)]) : null,
        el("span", { class: "log-msg-time" }, [formatTimestamp(item.timestamp)]),
      ]),
    ]),
    body,
  ]);
}

function renderActivityItemBody(item: ActivityItem): HTMLElement | null {
  if (item.inputText || item.outputText) {
    return el("div", { class: "log-msg-activity-item-body log-msg-activity-item-body-structured" }, [
      item.inputText ? renderActivitySection("Input", item.inputText) : null,
      item.outputText ? renderActivitySection(item.status === "error" ? "Error" : "Output", item.outputText) : null,
    ]);
  }
  if (!item.detailText) return null;
  return el("div", { class: "log-msg-activity-item-body" }, [item.detailText]);
}

function renderActivitySection(label: string, text: string): HTMLElement {
  return el("div", { class: "log-msg-activity-section" }, [
    el("div", { class: "log-msg-activity-section-label" }, [label]),
    el("pre", { class: "log-msg-activity-section-body" }, [text]),
  ]);
}

function activitySummary(items: ActivityItem[]): string {
  const count = items.length;
  if (count === 0) return "Activity";
  const reasoning = items.filter((item) => item.kind === "reasoning").length;
  const tools = items.filter((item) => item.kind === "tool").length;
  const context = items.filter((item) => item.kind === "context").length;
  const progress = items.filter((item) => item.kind === "progress").length;
  const workParts = [
    reasoning > 0 ? `${reasoning} reasoning` : "",
    tools > 0 ? `${tools} ${tools === 1 ? "tool" : "tools"}` : "",
    progress > 0 ? `${progress} ${progress === 1 ? "update" : "updates"}` : "",
  ].filter(Boolean);
  const contextParts = [
    context > 0 ? `${context} context` : "",
  ].filter(Boolean);
  const duration = activityDuration(items);
  if (tools > 0 || reasoning > 0 || progress > 0) {
    const suffix = [...workParts, ...contextParts].join(", ");
    return `${duration ? `Worked for ${duration}` : "Worked"} · ${suffix}`;
  }
  if (context > 0) {
    return `Context · ${context} ${context === 1 ? "item" : "items"}`;
  }
  return `Activity · ${count} ${count === 1 ? "item" : "items"}`;
}

function activityPreview(items: ActivityItem[]): string {
  const workPreview = items
    .filter((item) => item.kind !== "context")
    .map((item) => item.preview ?? summarizeActivityText(item.detailText))
    .find(Boolean);
  if (workPreview) return workPreview;
  return items.map((item) => item.preview).find(Boolean) ?? "";
}

function activityDuration(items: ActivityItem[]): string {
  const times = items
    .map((item) => Date.parse(item.timestamp ?? ""))
    .filter((value) => Number.isFinite(value))
    .sort((a, b) => a - b);
  if (times.length < 2) return "";
  const elapsed = times[times.length - 1] - times[0];
  if (elapsed < 1000) return "";
  return formatActivityDuration(elapsed);
}

function formatActivityDuration(milliseconds: number): string {
  const totalSeconds = Math.max(1, Math.round(milliseconds / 1000));
  if (totalSeconds < 60) return `${totalSeconds}s`;
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes < 60) return seconds > 0 ? `${minutes}m ${seconds}s` : `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  const restMinutes = minutes % 60;
  return restMinutes > 0 ? `${hours}h ${restMinutes}m` : `${hours}h`;
}

function activityStatusLabel(status: NonNullable<ActivityItem["status"]>): string {
  switch (status) {
    case "completed":
      return "done";
    case "error":
      return "error";
    case "pending":
      return "pending";
    case "running":
      return "running";
  }
}

function turnPresentation(role: string, text: string, trace: TranscriptTrace[]): TurnPresentation {
  const normalized = (role ?? "").toLowerCase();
  const thinkingCount = traceThinkingTexts(trace).length;
  if (text === "" && thinkingCount > 0) {
    return {
      detailText: "",
      kind: "reasoning",
      label: displayRoleLabel(role),
      summary: `Reasoning (${thinkingCount})`,
    };
  }
  if (isEnvironmentContextText(text)) {
    return {
      detailText: text,
      kind: "context",
      label: "context",
      summary: "Environment context",
    };
  }
  if (isSessionPrimerText(text)) {
    return {
      detailText: text,
      kind: "context",
      label: "context",
      summary: "Session primer",
    };
  }
  if (isAutonomousControlPromptText(text)) {
    return {
      detailText: text,
      kind: "context",
      label: "control",
      summary: "Autonomous control prompt",
    };
  }
  if (normalized === "system") {
    return {
      detailText: text,
      kind: "context",
      label: "system",
      summary: firstNonEmptyLine(text) || "System instruction",
    };
  }
  const toolCall = toolCallParts(text);
  if (toolCall.name) {
    return {
      detailText: toolCall.detail,
      kind: "tool",
      label: "tool",
      summary: `Tool call · ${toolCall.name}`,
    };
  }
  if (normalized === "tool" || normalized === "tool_result" || normalized === "result" || text.startsWith("[result]")) {
    return {
      detailText: text.replace(/^\[result\]\s*/i, ""),
      kind: "tool",
      label: "tool",
      summary: text.includes("Viewed Image") ? "Viewed image" : "Tool result",
    };
  }
  return {
    detailText: text,
    kind: "message",
    label: displayRoleLabel(role),
    summary: "",
  };
}

function isEnvironmentContextText(text: string): boolean {
  const trimmed = text.trim();
  return trimmed.startsWith("<environment_context>") && trimmed.endsWith("</environment_context>");
}

function isSessionPrimerText(text: string): boolean {
  const trimmed = text.trim();
  return /^\[[^\]]+\]\s+\S+\s+•\s+\d{4}-\d{2}-\d{2}T/.test(trimmed) && trimmed.includes("Run `gc prime`");
}

function isAutonomousControlPromptText(text: string): boolean {
  const trimmed = text.trim();
  if (trimmed.startsWith("Stay idle. Do not run commands or inspect state until")) return true;
  return (
    /^Check mail, then run `gc hook(?: [^`]+)?`/.test(trimmed)
    && trimmed.includes("claim and handle one item")
    && trimmed.includes("stay idle unless")
  );
}

function toolCallParts(text: string): { detail: string; name: string } {
  const trimmed = text.trim();
  const match = trimmed.match(/^\[([A-Za-z_][\w.-]*)\](?:\n([\s\S]*))?$/);
  return { detail: (match?.[2] ?? "").trim(), name: match?.[1] ?? "" };
}

function firstNonEmptyLine(text: string): string {
  return text.split(/\r?\n/).map((line) => line.trim()).find(Boolean) ?? "";
}

function traceThinkingTexts(trace: TranscriptTrace[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const item of trace) {
    if ((item.kind ?? "").toLowerCase() !== "thinking") continue;
    const text = (item.text ?? "").trim();
    if (text === "" || seen.has(text)) continue;
    seen.add(text);
    out.push(text);
  }
  return out;
}

function renderMediaBlock(attachments: RenderAttachment[]): HTMLElement {
  const grid = el("div", { class: "log-msg-attachments" }, attachments.map((attachment) => (
    renderAttachmentImage(attachment)
  )));
  const toggle = el("button", { "aria-expanded": "true", class: "log-msg-media-toggle", type: "button" }, [`Hide media (${attachments.length})`]);
  toggle.addEventListener("click", () => {
    grid.hidden = !grid.hidden;
    const hidden = grid.hidden;
    toggle.setAttribute("aria-expanded", hidden ? "false" : "true");
    toggle.textContent = hidden ? `Show media (${attachments.length})` : `Hide media (${attachments.length})`;
  });
  return el("div", { class: "log-msg-media" }, [toggle, grid]);
}

function assetToRenderAttachment(asset: TranscriptAsset): RenderAttachment | null {
  if (asset.kind && asset.kind !== "image") return null;
  if (asset.url) {
    return { name: asset.name || filenameFromPath(asset.url), src: attachmentImageSrc(asset.url) };
  }
  if (!asset.path) return null;
  if (isBareImageFilename(asset.path)) return null;
  const dashboardAttachment = dashboardAttachmentPathAttachment(asset.path, asset.name || filenameFromPath(asset.path));
  if (dashboardAttachment) return dashboardAttachment;
  const city = cityScope();
  if (
    !city
    || !transcriptSessionID
    || asset.path.includes(".gc/dashboard/attachments/")
    || asset.path.includes(".gc\\dashboard\\attachments\\")
  ) return null;
  return {
    name: asset.name || filenameFromPath(asset.path),
    src: apiURL(sessionAssetPath(city, transcriptSessionID, asset.path)),
  };
}

function renderAttachmentImage(attachment: RenderAttachment): HTMLElement {
  const button = el("button", { class: "log-msg-image-frame", title: "Preview image", type: "button" }, [
    el("img", { alt: attachment.name, class: "log-msg-image", src: attachment.src }),
  ]);
  button.addEventListener("click", () => showImagePreview(attachment));
  return button;
}

function dedupeRenderAttachments(attachments: RenderAttachment[]): RenderAttachment[] {
  const seen = new Set<string>();
  const out: RenderAttachment[] = [];
  for (const attachment of attachments) {
    if (!attachment.src || seen.has(attachment.src)) continue;
    seen.add(attachment.src);
    out.push(attachment);
  }
  return out;
}

function suppressRepeatedFilenameAttachments(attachments: RenderAttachment[]): RenderAttachment[] {
  return attachments.filter((attachment) => {
    return !attachment.inferredFromFilename || !renderedTranscriptImageSrcs.has(attachment.src);
  });
}

function extractInlineImageAttachments(text: string): { attachments: RenderAttachment[]; text: string } {
  const attachments: RenderAttachment[] = [];
  let cleaned = text.replace(/!\[([^\]]*)\]\(([\s\S]*?)\)/g, (match, name: string, rawRef: string) => {
    const ref = rawRef.replace(/\s+/g, "");
    const label = name || filenameFromPath(ref);
    if (/^data:image\/[^;)]+;base64,/i.test(ref)) {
      if (ref.length > TRANSCRIPT_INLINE_IMAGE_LIMIT_BYTES) {
        return `[inline image data omitted: ${label || "image"}]`;
      }
      attachments.push({ name: label || "image", src: ref });
      return "";
    }
    if (/^\/v0\/city\/[^/]+\/session\/[^/]+\/attachments\/.+/i.test(ref)) {
      attachments.push({ name: label || filenameFromPath(ref), src: attachmentImageSrc(ref) });
      rememberRenderAttachments(attachments);
      return "";
    }
    if (/\.(?:png|jpe?g|gif|webp)(?:[?#].*)?$/i.test(ref)) {
      const attachment = localImagePathAttachment(ref, label || "image", attachments);
      if (!attachment) return match;
      attachments.push(attachment);
      return "";
    }
    return match;
  });
  collectPlainImagePathAttachments(cleaned, attachments);
  cleaned = cleanDashboardAttachmentBoilerplate(cleaned);
  cleaned = collapseLooseDataImagePayloads(cleaned);
  cleaned = collapseLooseBase64Payloads(cleaned);
  return { attachments, text: cleaned };
}

function cleanDashboardAttachmentBoilerplate(text: string): string {
  const out: string[] = [];
  const lines = text.replace(/\r\n/g, "\n").split("\n");
  let inAttachmentBlock = false;
  let skippingLocalFile = false;
  for (const line of lines) {
    const trimmed = line.trim();
    if (/^Attached images:\s*$/i.test(trimmed)) {
      inAttachmentBlock = true;
      skippingLocalFile = false;
      continue;
    }
    if (inAttachmentBlock) {
      if (trimmed === "") continue;
      if (/^\d+\.\s+.+\.(?:png|jpe?g|gif|webp)\s*$/i.test(trimmed)) continue;
      if (/^Local file:\s*/i.test(trimmed)) {
        skippingLocalFile = true;
        continue;
      }
      if (/^Use the local file path to inspect the image/i.test(trimmed)) {
        inAttachmentBlock = false;
        skippingLocalFile = false;
        continue;
      }
      if (skippingLocalFile && (
        /^[/~.]/.test(trimmed)
        || /\.(?:png|jpe?g|gif|webp)$/i.test(trimmed)
      )) {
        continue;
      }
      inAttachmentBlock = false;
      skippingLocalFile = false;
    }
    out.push(line);
  }
  return out.join("\n");
}

function collectPlainImagePathAttachments(text: string, attachments: RenderAttachment[]): void {
  const searchable = unwrapDashboardAttachmentPaths(text);
  const matches = searchable.matchAll(/(?:^|[\s([{"'`])((?:~[/\\]|\.{1,2}[/\\]|\/|[A-Za-z0-9_.@%+-]+[/\\])[A-Za-z0-9_./\\@%:+-]+\.(?:png|jpe?g|gif|webp))(?=$|[\s)\]}"'`.,;:])/gi);
  for (const match of matches) {
    const imagePath = match[1];
    if (!imagePath) continue;
    const attachment = localImagePathAttachment(imagePath, filenameFromPath(imagePath), attachments);
    if (attachment) attachments.push(attachment);
  }
}

function localImagePathAttachment(imagePath: string, name: string, localAttachments: RenderAttachment[] = []): RenderAttachment | null {
  const known = knownAttachmentForImagePath(imagePath, localAttachments);
  if (isBareImageFilename(imagePath)) return null;
  const dashboardAttachment = dashboardAttachmentPathAttachment(imagePath, name);
  if (dashboardAttachment) return dashboardAttachment;
  if (known && (imagePath.includes(".gc/dashboard/attachments/") || imagePath.includes(".gc\\dashboard\\attachments\\"))) return known;
  const city = cityScope();
  if (!city || !transcriptSessionID) return null;
  if (/^(?:https?:|data:|\/v0\/city\/)/i.test(imagePath)) return null;
  if (imagePath.includes(".gc/") || imagePath.includes(".gc\\")) return null;
  return {
    name: name || filenameFromPath(imagePath),
    src: apiURL(sessionAssetPath(city, transcriptSessionID, imagePath)),
  };
}

function unwrapDashboardAttachmentPaths(text: string): string {
  const joined = text
    .replace(/([/\\])\s+/g, "$1")
    .replace(/-\s+([A-Za-z0-9])/g, "-$1");
  return joined.replace(
    /([^\s)\]}"'`]*(?:\.gc[\\/]+)?dashboard[\\/]+attachments[\\/]+[^\s)\]}"'`]+[\\/]+[^\s)\]}"'`]+[\\/]+)\s+([A-Za-z0-9_.@%+-]+\.(?:png|jpe?g|gif|webp))/gi,
    "$1$2",
  );
}

function dashboardAttachmentPathAttachment(imagePath: string, name: string): RenderAttachment | null {
  const city = cityScope();
  if (!city) return null;
  const unwrapped = unwrapDashboardAttachmentPaths(imagePath).replace(/\\/g, "/");
  const match = unwrapped.match(/(?:^|\/)(?:\.gc\/)?dashboard\/attachments\/([^/\s)\]}"'`]+)\/([^/\s)\]}"'`]+)\/([^/\s)\]}"'`]+\.(?:png|jpe?g|gif|webp))(?:[?#].*)?$/i);
  if (!match) return null;
  const [, sessionID, attachmentID, fileName] = match;
  if (!sessionID || !attachmentID || !fileName) return null;
  return {
    name: name || filenameFromPath(fileName),
    src: apiURL(sessionAttachmentFilePath(city, sessionID, attachmentID, fileName)),
  };
}

function rememberRenderAttachments(attachments: RenderAttachment[]): void {
  for (const attachment of attachments) {
    const key = imageNameKey(attachment.name) || imageNameKey(attachment.src);
    if (!key || !attachment.src) continue;
    knownTranscriptImagesByName.set(key, attachment);
  }
}

function knownAttachmentForImagePath(imagePath: string, localAttachments: RenderAttachment[]): RenderAttachment | null {
  const key = imageNameKey(imagePath);
  if (!key) return null;
  for (let index = localAttachments.length - 1; index >= 0; index -= 1) {
    const attachment = localAttachments[index];
    if (imageNameKey(attachment.name || filenameFromPath(attachment.src)) === key) return attachment;
  }
  return knownTranscriptImagesByName.get(key) ?? null;
}

function imageNameKey(value: string): string {
  const name = filenameFromPath(value).trim().toLowerCase();
  return /\.(?:png|jpe?g|gif|webp)$/i.test(name) ? name : "";
}

function isBareImageFilename(value: string): boolean {
  return imageNameKey(value) !== "" && !/[\\/]/.test(value);
}

function filenameFromPath(path: string): string {
  const withoutQuery = path.split(/[?#]/, 1)[0] ?? path;
  const normalized = withoutQuery.replace(/\\/g, "/");
  return normalized.split("/").filter(Boolean).pop() || "image";
}

function showImagePreview(attachment: RenderAttachment): void {
  let modal = byId("log-image-preview");
  if (!modal) {
    modal = el("div", { class: "log-image-preview", id: "log-image-preview" });
    document.body.append(modal);
  }
  clear(modal);
  const close = el("button", { class: "log-image-preview-close", type: "button" }, ["Close"]);
  close.addEventListener("click", hideImagePreview);
  const backdrop = el("button", { "aria-label": "Close image preview", class: "log-image-preview-backdrop", type: "button" });
  backdrop.addEventListener("click", hideImagePreview);
  modal.append(
    backdrop,
    el("div", { class: "log-image-preview-stage" }, [
      close,
      el("img", { alt: attachment.name, class: "log-image-preview-img", src: attachment.src }),
    ]),
  );
  modal.setAttribute("data-open", "true");
}

function hideImagePreview(): void {
  const modal = byId("log-image-preview");
  if (!modal) return;
  modal.removeAttribute("data-open");
}

function collapseLooseDataImagePayloads(text: string): string {
  return text.replace(/data:image\/[A-Za-z0-9.+-]+;base64,[A-Za-z0-9+/=\s]{2000,}/g, "[inline image data omitted]");
}

function collapseLooseBase64Payloads(text: string): string {
  return text.replace(/[A-Za-z0-9+/=\s]{2000,}/g, (chunk) => {
    if (!looksLikeBase64Payload(chunk)) return chunk;
    return "[large encoded image data omitted from transcript]";
  });
}

function looksLikeBase64Payload(chunk: string): boolean {
  const compact = chunk.replace(/\s+/g, "");
  return compact.length > 1500 && /^[A-Za-z0-9+/=]+$/.test(compact) && /[+/]/.test(compact);
}

export function scrollLogDrawerToBottom(): void {
  const body = byId("log-drawer-body");
  if (!body) return;
  window.requestAnimationFrame(() => {
    body.scrollTop = body.scrollHeight;
  });
}

function roleClass(role: string): string {
  switch ((role ?? "").toLowerCase()) {
    case "assistant":
    case "agent":
      return "assistant";
    case "system":
      return "system";
    case "output":
    case "result":
    case "tool":
    case "tool_result":
      return "result";
    default:
      return "user";
  }
}

function displayRoleLabel(role: string): string {
  const normalized = (role ?? "").toLowerCase();
  if ((normalized === "assistant" || normalized === "agent") && transcriptSessionLabel) return transcriptSessionLabel;
  return role;
}
