export type BlockKind = "paragraph" | "heading" | "bullet" | "ordered" | "task" | "quote";

export interface BlockInfo {
  indent: string;
  prefix: string;
  content: string;
  kind: BlockKind;
  level: number;
}

/** readBlockLine splits a line into its indent and one block prefix. */
export function readBlockLine(text: string): BlockInfo {
  const match = /^(\s*)(#{1,6}[ \t]+|>[ \t]?|[-*+][ \t]+(?:\[[ xX]\][ \t]+)?|\d{1,9}[.)][ \t]+)/.exec(text);
  if (!match) {
    const indent = /^\s*/.exec(text)?.[0] ?? "";
    return { indent, prefix: "", content: text.slice(indent.length), kind: "paragraph", level: 0 };
  }
  const indent = match[1];
  const prefix = match[2];
  const content = text.slice(match[0].length);
  if (prefix.startsWith("#")) {
    return { indent, prefix, content, kind: "heading", level: prefix.trim().length };
  }
  if (prefix.startsWith(">")) {
    return { indent, prefix, content, kind: "quote", level: 0 };
  }
  if (/^[-*+][ \t]+\[[ xX]\][ \t]+$/.test(prefix)) {
    return { indent, prefix, content, kind: "task", level: 0 };
  }
  if (/^[-*+][ \t]+$/.test(prefix)) {
    return { indent, prefix, content, kind: "bullet", level: 0 };
  }
  return { indent, prefix, content, kind: "ordered", level: 0 };
}

export function blockPrefix(kind: BlockKind, level: number): string {
  switch (kind) {
    case "heading":
      return "#".repeat(Math.max(1, Math.min(6, level))) + " ";
    case "bullet":
      return "- ";
    case "ordered":
      return "1. ";
    case "task":
      return "- [ ] ";
    case "quote":
      return "> ";
    default:
      return "";
  }
}

/** linesMatchKind reports whether every non-blank line already has the
 * kind, so pressing the toolbar again removes it from all. */
function linesMatchKind(lines: readonly string[], kind: BlockKind, level: number): boolean {
  const nonBlank = lines.filter((text) => text.trim() !== "");
  const inspect = nonBlank.length > 0 ? nonBlank : lines;
  return inspect.every((text) => {
    const info = readBlockLine(text);
    if (kind === "paragraph") return info.kind === "paragraph";
    if (kind === "heading") return info.kind === "heading" && info.level === level;
    return info.kind === kind;
  });
}

/** transformBlockLines applies a block kind to every selected line, or
 * removes it from all when every line already has it. Ordered lists are
 * numbered from 1 in the selection. */
export function transformBlockLines(
  lines: readonly string[],
  kind: BlockKind,
  level = 0,
): string[] {
  const same = linesMatchKind(lines, kind, level);
  let counter = 1;
  return lines.map((text) => {
    const info = readBlockLine(text);
    if (!same && text.trim() === "") return text;
    const prefix = same
      ? ""
      : kind === "ordered"
        ? `${counter++}. `
        : blockPrefix(kind, level);
    return info.indent + prefix + info.content;
  });
}

/** Ordered list line parts: indentation, number, delimiter, gap, content. */
export interface OrderedLine {
  indent: string;
  number: number;
  delimiter: string;
  gap: string;
  content: string;
}

const orderedLine = /^(\s*)(\d{1,9})([.)])(\s+)(.*)$/;

export function parseOrderedLine(text: string): OrderedLine | null {
  const match = orderedLine.exec(text);
  if (!match) return null;
  return {
    indent: match[1],
    number: Number(match[2]),
    delimiter: match[3],
    gap: match[4],
    content: match[5],
  };
}

/**
 * renumberOrderedFrom renumbers the ordered item at index and the items
 * after it at the same indentation, starting at start. The items before
 * index keep their numbers: changing 3 in 1 2 3 4 5 to 1 makes it
 * 1 2 1 2 3.
 */
export function renumberOrderedFrom(
  lines: readonly string[],
  index: number,
  start: number,
): string[] {
  const first = parseOrderedLine(lines[index] ?? "");
  if (!first) return [...lines];
  const indent = first.indent.length;
  const number = Math.max(1, Math.min(999999999, Math.trunc(start)));
  let next = number;
  const out = [...lines];
  for (let i = index; i < out.length; i++) {
    const item = parseOrderedLine(out[i]);
    if (!item || item.indent.length !== indent) break;
    out[i] = `${item.indent}${next}${item.delimiter}${item.gap}${item.content}`;
    next++;
  }
  return out;
}
