import { supervisorBaseURL } from "../api";

export function sessionAttachmentsPath(city: string, sessionID: string): string {
  return `/v0/city/${encodeURIComponent(city)}/session/${encodeURIComponent(sessionID)}/attachments`;
}

export function sessionAttachmentFilePath(city: string, sessionID: string, attachmentID: string, name: string): string {
  return `${sessionAttachmentsPath(city, sessionID)}/${encodeURIComponent(attachmentID)}/${encodeURIComponent(name)}`;
}

export function sessionAttachmentDeletePath(city: string, sessionID: string, attachmentID: string): string {
  return `${sessionAttachmentsPath(city, sessionID)}/${encodeURIComponent(attachmentID)}`;
}

export function sessionAssetPath(city: string, sessionID: string, assetPath: string): string {
  return `/v0/city/${encodeURIComponent(city)}/session/${encodeURIComponent(sessionID)}/asset?path=${encodeURIComponent(assetPath)}`;
}

export function apiURL(path: string): string {
  return `${supervisorBaseURL()}${path}`;
}

export function attachmentImageSrc(path: string): string {
  if (/^https?:\/\//i.test(path) || path.startsWith("data:")) return path;
  return apiURL(path.startsWith("/") ? path : `/${path}`);
}
