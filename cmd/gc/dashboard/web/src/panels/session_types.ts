export type SubmitIntent = "default" | "follow_up" | "interrupt_now";

export interface ChatAttachment {
  id: string;
  name: string;
  path: string;
  size: number;
  type: string;
  url: string;
}

export interface RenderAttachment {
  inferredFromFilename?: boolean;
  name: string;
  src: string;
}

export interface TranscriptAsset {
  kind?: string;
  name?: string;
  path?: string;
  source?: string;
  url?: string;
}

export interface TranscriptTrace {
  kind?: string;
  text?: string;
}

export interface TranscriptPart {
  action?: string;
  id?: string;
  input?: unknown;
  is_error?: boolean;
  kind?: string;
  mime?: string;
  name?: string;
  options?: string[] | null;
  output?: unknown;
  path?: string;
  prompt?: string;
  request_id?: string;
  source?: string;
  state?: string;
  text?: string;
  tool?: string;
  tool_use_id?: string;
  type?: string;
  url?: string;
}

export interface DisplayTurn {
  activity?: ActivityItem[] | null;
  assets?: TranscriptAsset[] | null;
  role: string;
  text: string;
  timestamp?: string;
  trace?: TranscriptTrace[] | null;
}

export interface ActivityItem {
  detailText: string;
  inputText?: string;
  label: string;
  kind: "context" | "progress" | "reasoning" | "tool";
  outputText?: string;
  preview?: string;
  status?: "completed" | "error" | "pending" | "running";
  summary: string;
  timestamp?: string;
  toolID?: string;
  toolName?: string;
}

export interface TurnPresentation {
  detailText: string;
  kind: "message" | "context" | "reasoning" | "reminder" | "tool";
  label: string;
  summary: string;
}

export interface TranscriptTurn {
  assets?: TranscriptAsset[] | null;
  parts?: TranscriptPart[] | null;
  role?: string;
  text?: string;
  timestamp?: string;
  trace?: TranscriptTrace[] | null;
}

export interface PendingInteraction {
  kind?: string;
  metadata?: Record<string, string>;
  options?: string[] | null;
  prompt?: string;
  request_id?: string;
}

export interface StreamTurnPayload {
  data?: { message?: TranscriptTurn };
  event?: string;
  format?: string;
  turns?: TranscriptTurn[];
}
