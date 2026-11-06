import type { OutlineHeading } from "./types";

export interface Fence {
  /** Line number of the opening fence. */
  openLine: number;
  /** Line number of the closing fence, or -1 while it is not closed. */
  closeLine: number;
  /** language in the info string, lowercased; "" when there is none. */
  lang: string;
  /** title="..." of the info string, if any. */
  title: string;
  /** the code lines, excluding the fences. */
  code: string;
  /** true when the cursor was inside the block when it was parsed. */
  firstLine: number;
  lastLine: number;
}

const FENCE_RE = /^(\s{0,3})(`{3,}|~{3,})(.*)$/;

/** infoString returns the language and title of a fence's info string. */
export function infoString(rest: string): { lang: string; title: string } {
  const text = rest.trim();
  if (!text) return { lang: "", title: "" };
  let lang = text.split(/\s+/)[0] ?? "";
  lang = lang.replace(/^\{?\.?/, "").replace(/\}$/, "").toLowerCase();
  if (lang === "mmd") lang = "mermaid";
  let title = "";
  const m = /(?:^|\s)title\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s]+))/i.exec(text);
  if (m) title = m[1] ?? m[2] ?? m[3] ?? "";
  return { lang, title };
}

/** displayLang is the label Obsidian shows for a fence's language. */
export function displayLang(lang: string): string {
  const names: Record<string, string> = {
    go: "Go",
    golang: "Go",
    js: "JavaScript",
    javascript: "JavaScript",
    ts: "TypeScript",
    typescript: "TypeScript",
    jsx: "JSX",
    tsx: "TSX",
    py: "Python",
    python: "Python",
    sh: "Shell",
    shell: "Shell",
    bash: "Bash",
    zsh: "Zsh",
    sql: "SQL",
    json: "JSON",
    yaml: "YAML",
    yml: "YAML",
    toml: "TOML",
    html: "HTML",
    css: "CSS",
    scss: "SCSS",
    rust: "Rust",
    rs: "Rust",
    java: "Java",
    c: "C",
    cpp: "C++",
    "c++": "C++",
    csharp: "C#",
    md: "Markdown",
    markdown: "Markdown",
    mermaid: "Mermaid",
    text: "Text",
    txt: "Text",
  };
  return names[lang] ?? (lang ? lang.charAt(0).toUpperCase() + lang.slice(1) : "");
}

/** parseFences returns the fenced code blocks of lines. */
export function parseFences(lines: string[]): Fence[] {
  const out: Fence[] = [];
  for (let i = 0; i < lines.length; i++) {
    const m = FENCE_RE.exec(lines[i]);
    if (!m) continue;
    const marker = m[2][0];
    const size = m[2].length;
    const { lang, title } = infoString(m[3]);
    let close = -1;
    for (let j = i + 1; j < lines.length; j++) {
      const c = /^\s{0,3}(`{3,}|~{3,})\s*$/.exec(lines[j]);
      if (c && c[1][0] === marker && c[1].length >= size) {
        close = j;
        break;
      }
    }
    const code = lines.slice(i + 1, close === -1 ? lines.length : close).join("\n");
    out.push({
      openLine: i,
      closeLine: close,
      lang,
      title,
      code,
      firstLine: i + 1,
      lastLine: close === -1 ? lines.length - 1 : close - 1,
    });
    i = close === -1 ? lines.length : close;
  }
  return out;
}

/** fenceAt returns the fence holding line, if any. */
export function fenceAt(fences: Fence[], line: number): Fence | undefined {
  for (const f of fences) {
    if (line >= f.openLine && (f.closeLine === -1 || line <= f.closeLine)) return f;
  }
  return undefined;
}

export interface TableBlock {
  fromLine: number;
  toLine: number;
  header: string[];
  align: Array<"left" | "center" | "right" | "default">;
  rows: string[][];
}

const TABLE_DELIM = /^\s*\|?\s*:?-{1,}\s*(?::?\s*\|?\s*:?-{1,}\s*)*\|?\s*:?\s*$/;

function splitRow(line: string): string[] {
  let text = line.trim();
  if (text.startsWith("|")) text = text.slice(1);
  if (text.endsWith("|")) text = text.slice(0, -1);
  const cells: string[] = [];
  let cell = "";
  let escaped = false;
  for (const ch of text) {
    if (escaped) {
      cell += ch;
      escaped = false;
      continue;
    }
    if (ch === "\\") {
      escaped = true;
      continue;
    }
    if (ch === "|") {
      cells.push(cell.trim());
      cell = "";
      continue;
    }
    cell += ch;
  }
  cells.push(cell.trim());
  return cells;
}

/** parseTables returns GitHub-style pipe tables outside fenced code. */
export function parseTables(lines: string[], fences: Fence[]): TableBlock[] {
  const out: TableBlock[] = [];
  const inFence = new Set<number>();
  for (const f of fences) {
    for (let i = f.openLine; i <= (f.closeLine === -1 ? lines.length - 1 : f.closeLine); i++) {
      inFence.add(i);
    }
  }
  for (let i = 0; i + 1 < lines.length; i++) {
    if (inFence.has(i) || inFence.has(i + 1)) continue;
    if (!/\|/.test(lines[i]) || !TABLE_DELIM.test(lines[i + 1])) continue;
    const header = splitRow(lines[i]);
    const delims = splitRow(lines[i + 1]);
    if (header.length < 1 || delims.length !== header.length) continue;
    const align = delims.map((d) => {
      const t = d.trim();
      const left = t.startsWith(":");
      const right = t.endsWith(":");
      if (left && right) return "center" as const;
      if (right) return "right" as const;
      if (left) return "left" as const;
      return "default" as const;
    });
    let j = i + 2;
    const rows: string[][] = [];
    while (j < lines.length && /\|/.test(lines[j]) && lines[j].trim() !== "") {
      rows.push(splitRow(lines[j]));
      j++;
    }
    out.push({ fromLine: i, toLine: j - 1, header, align, rows });
    i = j - 1;
  }
  return out;
}

/** outlineHeadings lists ATX headings outside code blocks. */
export function outlineHeadings(lines: string[], fences: Fence[]): OutlineHeading[] {
  const out: OutlineHeading[] = [];
  const inFence = new Set<number>();
  for (const f of fences) {
    for (let i = f.openLine; i <= (f.closeLine === -1 ? lines.length - 1 : f.closeLine); i++) {
      inFence.add(i);
    }
  }
  let offset = 0;
  for (let i = 0; i < lines.length; i++) {
    if (!inFence.has(i)) {
      const m = /^(#{1,6})\s+(.*)$/.exec(lines[i]);
      if (m) {
        const text = m[2].replace(/\s+#+\s*$/, "").trim();
        if (text) out.push({ level: m[1].length, text, line: i, offset });
      }
    }
    offset += lines[i].length + 1;
  }
  return out;
}

/** countWords counts Han characters and runs of Latin letters or digits. */
export function countWords(text: string): number {
  let count = 0;
  let inWord = false;
  for (const ch of text) {
    const code = ch.codePointAt(0) ?? 0;
    const han =
      (code >= 0x3400 && code <= 0x4dbf) ||
      (code >= 0x4e00 && code <= 0x9fff) ||
      (code >= 0xf900 && code <= 0xfaff) ||
      (code >= 0x3040 && code <= 0x30ff) ||
      (code >= 0xac00 && code <= 0xd7af);
    if (han) {
      count++;
      inWord = false;
    } else if (/\s/.test(ch) || /[，。！？、；：“”‘’（）【】《》,.!?;:'"()[\]{}<>/\\|`~@#$%^&*+=_-]/.test(ch)) {
      inWord = false;
    } else if (!inWord) {
      count++;
      inWord = true;
    }
  }
  return count;
}

/** countCharacters is Obsidian's character count: every code point. */
export function countCharacters(text: string): number {
  return Array.from(text).length;
}
