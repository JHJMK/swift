export interface OpenPayload {
  id: string;
  title: string;
  content: string;
  readOnly: boolean;
  previewMode: boolean;
  note: boolean;
  fontSize: number;
  lineHeight: number;
  readableLineWidth: boolean;
  outlineOpen: boolean;
  outlineWidth: number;
  focusTitle?: boolean;
  savedAt: string;
  scrollTop: number;
  message?: string;
}

export interface StatusPayload {
  state: "dirty" | "saving" | "saved" | "readonly" | "error";
  text: string;
}

export interface SettingsPayload {
  fontSize: number;
  lineHeight: number;
  readableLineWidth: boolean;
}

export type CommandName =
  | "search"
  | "close-search"
  | "slash"
  | "outline"
  | "preview"
  | "focus"
  | "image"
  | "attachment"
  | "save";

export interface CommandPayload {
  command: CommandName;
  open?: boolean;
  message?: string;
}

/** ClipboardResult is what the page's Paste menu gets from the app. */
export interface ClipboardResult {
  html: string;
  markdown: string;
  text: string;
  /** done says the app already inserted the clipboard, a picture it saved. */
  done: boolean;
}

export interface ReloadPayload {
  id: string;
  content: string;
  savedAt: string;
}

export interface RenamePayload {
  id: string;
  title: string;
}

export interface TextPayload {
  text: string;
  /** Plain form, used when the target is a page field rather than the note. */
  plain?: string;
}

export interface RenameResult {
  id: string;
  title: string;
}

export interface AttachmentResult {
  text: string;
}

/** The slice of MyGo's bridge the editor uses. */
export interface MyGoBridge {
  call<T>(method: string, ...args: unknown[]): Promise<T>;
  on(name: string, listener: (payload: unknown) => void): () => void;
  platform?: string;
}

declare global {
  interface Window {
    mygo?: MyGoBridge;
  }
}

export interface OutlineHeading {
  level: number;
  text: string;
  line: number;
  offset: number;
}
