import { Range, StateEffect, StateField, type EditorState, type Extension } from "@codemirror/state";
import {
  Decoration,
  EditorView,
  type DecorationSet,
} from "@codemirror/view";
import {
  displayLang,
  parseFences,
  parseTables,
  type Fence,
  type TableBlock,
} from "./markdown";
import { cachedMermaidSvg } from "./mermaid";
import {
  CodeFlairWidget,
  HrWidget,
  ImageWidget,
  LineBreakWidget,
  ListMarkerWidget,
  MermaidWidget,
  TableWidget,
  TaskWidget,
  type WidgetContext,
} from "./widgets";

export interface LivePreviewOptions {
  docId: string;
  assetUrl(ref: string): string;
  copy(text: string): void;
  copyImage(src: string, ref: string): void;
  openLink(url: string): void;
  reveal(pos: number): void;
  preview(): boolean;
}

export const reloadMermaid = StateEffect.define<null>();
export const refreshPreview = StateEffect.define<null>();
export const holdCodeFences = StateEffect.define<boolean>();

interface PreviewValue {
  decorations: DecorationSet;
  atomic: DecorationSet;
  held: boolean;
}

let viewRef: EditorView | null = null;

/** attachView gives the widgets of the decorations a view to dispatch on. */
export function attachView(view: EditorView) {
  viewRef = view;
}

const hiddenMark = Decoration.mark({ class: "swift-token-hidden" });

function lineActive(active: Set<number>, n: number): boolean {
  return active.has(n);
}

function inlineAssetUrl(url: string): string {
  const trimmed = url.trim().replace(/^<|>$/g, "");
  if (/^(https?:|data:|blob:|swiftassets:|file:)/i.test(trimmed)) return trimmed;
  return "";
}

/** isImageRef reports whether a wiki embed names a picture. */
function isImageRef(ref: string): boolean {
  const clean = ref.split("|")[0].split("#")[0].trim().toLowerCase();
  return /\.(png|jpe?g|gif|webp|svg|bmp|avif)$/.test(clean);
}

/** build makes the decorations of a document in one pass. */
function build(
  state: EditorState,
  options: LivePreviewOptions,
  held = false,
): PreviewValue {
  const doc = state.doc;
  const lines: string[] = [];
  for (let i = 1; i <= doc.lines; i++) lines.push(doc.line(i).text);
  const fences = parseFences(lines);
  const tables = parseTables(lines, fences);

  const active = new Set<number>();
  if (!options.preview()) {
    for (const range of state.selection.ranges) {
      const from = doc.lineAt(range.from).number - 1;
      const to = doc.lineAt(range.to).number - 1;
      for (let n = from; n <= to; n++) active.add(n);
    }
  }

  const decos: Range<Decoration>[] = [];
  const atomicRanges: Range<Decoration>[] = [];
  const ctx: WidgetContext = {
    docId: options.docId,
    assetUrl: options.assetUrl,
    copy: options.copy,
    copyImage: options.copyImage,
    remeasure() {
      viewRef?.requestMeasure();
    },
    toggleTask(pos) {
      if (options.preview()) return;
      const view = viewRef;
      if (!view) return;
      const current = view.state.doc.sliceString(pos, pos + 1);
      const checked = current.toLowerCase() === "x";
      view.dispatch({
        changes: { from: pos, to: pos + 1, insert: checked ? " " : "x" },
      });
    },
    enter(pos) {
      const view = viewRef;
      if (!view) return;
      view.dispatch({ selection: { anchor: pos } });
      options.reveal(pos);
      view.focus();
    },
  };

  const addLine = (line: number, cls: string, attributes?: Record<string, string>) => {
    const l = doc.line(line + 1);
    decos.push(
      Decoration.line({ class: cls, attributes }).range(l.from),
    );
  };
  const hide = (from: number, to: number, list: Range<Decoration>[] = atomicRanges) => {
    if (to <= from) return;
    decos.push(hiddenMark.range(from, to));
    list.push(Decoration.mark({}).range(from, to));
  };
  const style = (from: number, to: number, cls: string, attrs?: Record<string, string>) => {
    if (to <= from) return;
    decos.push(
      Decoration.mark({ class: cls, attributes: attrs }).range(from, to),
    );
  };

  /** inline decorates emphasis, code, links and images within one line. */
  const inline = (base: number, text: string, isActive: boolean, fromOffset = 0, toOffset = text.length) => {
    let i = fromOffset;
    const n = toOffset;
    const lower = text.toLowerCase();
    while (i < n) {
      const ch = text[i];
      if (ch === "`") {
        const end = text.indexOf("`", i + 1);
        if (end > i) {
          if (!isActive) hide(base + i, base + end + 1);
          else style(base + i, base + i + 1, "swift-formatting"), style(base + end, base + end + 1, "swift-formatting");
          style(base + i + 1, base + end, "swift-inline-code");
          i = end + 1;
          continue;
        }
      }
      if (ch === "!" && text[i + 1] === "[" && text[i + 2] === "[") {
        const end = text.indexOf("]]", i + 3);
        if (end > 0) {
          const inner = text.slice(i + 3, end);
          const target = inner.split("|")[0].trim();
          const alias = inner.split("|")[1]?.trim() ?? "";
          if (isImageRef(target)) {
            const resolved = inlineAssetUrl(target) || options.assetUrl(target);
            decos.push(
              Decoration.replace({
                widget: new ImageWidget(resolved, alias || target, base + i, false, target, ctx),
              }).range(base + i, base + end + 2),
            );
            atomicRanges.push(Decoration.mark({}).range(base + i, base + end + 2));
            i = end + 2;
            continue;
          }
          // An embedded note has no inline preview here; show its title
          // as a link so the source stays out of the way.
          if (!isActive) {
            hide(base + i, base + i + 3);
            hide(base + end, base + end + 2);
          } else {
            style(base + i, base + i + 3, "swift-formatting");
            style(base + end, base + end + 2, "swift-formatting");
          }
          style(base + i + 3, base + end, "swift-link", {
            "data-href": target,
            title: `嵌入：${target}`,
          });
          i = end + 2;
          continue;
        }
      }
      if (ch === "!" && text[i + 1] === "[") {
        const close = text.indexOf("](", i + 2);
        if (close > 0) {
          const end = text.indexOf(")", close + 2);
          if (end > 0) {
            const alt = text.slice(i + 2, close);
            const url = text.slice(close + 2, end);
            const resolved = inlineAssetUrl(url) || options.assetUrl(url);
            if (resolved) {
              decos.push(
                Decoration.replace({
                  widget: new ImageWidget(resolved, alt, base + i, false, url.trim().replace(/^<|>$/g, ""), ctx),
                }).range(base + i, base + end + 1),
              );
              if (end + 1 > i) {
                atomicRanges.push(Decoration.mark({}).range(base + i, base + end + 1));
              }
              i = end + 1;
              continue;
            }
          }
        }
      }
      if (ch === "<") {
        const lineBreak = /^<br\s*\/?>/i.exec(text.slice(i));
        if (lineBreak) {
          if (!isActive) {
            decos.push(
              Decoration.replace({
                widget: new LineBreakWidget(),
              }).range(base + i, base + i + lineBreak[0].length),
            );
          } else {
            style(base + i, base + i + lineBreak[0].length, "swift-formatting");
          }
          i += lineBreak[0].length;
          continue;
        }
        const tag = /^<(strong|b|em|i|del|s|strike|u|ins)>/i.exec(text.slice(i));
        if (tag) {
          const name = tag[1].toLowerCase();
          const closeTag = `</${name}>`;
          const closeAt = lower.indexOf(closeTag, i + tag[0].length);
          if (closeAt >= i + tag[0].length) {
            if (!isActive) {
              hide(base + i, base + i + tag[0].length);
              hide(base + closeAt, base + closeAt + closeTag.length);
            } else {
              style(base + i, base + i + tag[0].length, "swift-formatting");
              style(base + closeAt, base + closeAt + closeTag.length, "swift-formatting");
            }
            const cls =
              name === "strong" || name === "b" ? "swift-strong" :
              name === "em" || name === "i" ? "swift-em" :
              name === "del" || name === "s" || name === "strike" ? "swift-strike" :
              "swift-underline";
            style(base + i + tag[0].length, base + closeAt, cls);
            inline(base, text, isActive, i + tag[0].length, closeAt);
            i = closeAt + closeTag.length;
            continue;
          }
        }
        const end = text.indexOf(">", i + 1);
        if (end > i) {
          const url = text.slice(i + 1, end);
          if (/^(https?:\/\/|mailto:)/i.test(url)) {
            if (!isActive) {
              hide(base + i, base + i + 1);
              hide(base + end, base + end + 1);
            } else {
              style(base + i, base + i + 1, "swift-formatting");
              style(base + end, base + end + 1, "swift-formatting");
            }
            style(base + i + 1, base + end, "swift-link", {
              "data-href": url,
              title: url,
            });
            i = end + 1;
            continue;
          }
        }
      }
      if (ch === "[" && text[i + 1] === "[") {
        const end = text.indexOf("]]", i + 2);
        if (end > 0) {
          const inner = text.slice(i + 2, end);
          const [target, alias] = inner.split("|");
          const title = (alias ?? target).trim();
          const href = target.trim();
          if (!isActive) {
            hide(base + i, base + i + 2);
            hide(base + end, base + end + 2);
          } else {
            style(base + i, base + i + 2, "swift-formatting");
            style(base + end, base + end + 2, "swift-formatting");
          }
          style(base + i + 2, base + end, "swift-link", {
            "data-href": href,
            title: href,
          });
          if (!title) {
            // keep the raw text of an empty link visible
          }
          i = end + 2;
          continue;
        }
      }
      if (ch === "[") {
        const close = text.indexOf("](", i + 1);
        if (close > 0) {
          const end = text.indexOf(")", close + 2);
          if (end > 0) {
            const url = text.slice(close + 2, end);
            if (!isActive) {
              hide(base + i, base + i + 1);
              hide(base + close, base + end + 1);
            } else {
              style(base + i, base + i + 1, "swift-formatting");
              style(base + close, base + close + 2, "swift-formatting");
              style(base + close + 2, base + end, "swift-url");
              style(base + end, base + end + 1, "swift-formatting");
            }
            style(base + i + 1, base + close, "swift-link", {
              "data-href": url,
              title: url,
            });
            i = end + 1;
            continue;
          }
        }
      }
      if (text.startsWith("**", i) || text.startsWith("__", i)) {
        const mark = text.slice(i, i + 2);
        const end = text.indexOf(mark, i + 2);
        if (end > i) {
          if (!isActive) {
            hide(base + i, base + i + 2);
            hide(base + end, base + end + 2);
          } else {
            style(base + i, base + i + 2, "swift-formatting");
            style(base + end, base + end + 2, "swift-formatting");
          }
          style(base + i + 2, base + end, "swift-strong");
          inline(base, text, isActive, i + 2, end);
          i = end + 2;
          continue;
        }
      }
      if (text.startsWith("~~", i)) {
        const end = text.indexOf("~~", i + 2);
        if (end > i) {
          if (!isActive) {
            hide(base + i, base + i + 2);
            hide(base + end, base + end + 2);
          } else {
            style(base + i, base + i + 2, "swift-formatting");
            style(base + end, base + end + 2, "swift-formatting");
          }
          style(base + i + 2, base + end, "swift-strike");
          inline(base, text, isActive, i + 2, end);
          i = end + 2;
          continue;
        }
      }
      if (text.startsWith("==", i)) {
        const end = text.indexOf("==", i + 2);
        if (end > i) {
          if (!isActive) {
            hide(base + i, base + i + 2);
            hide(base + end, base + end + 2);
          } else {
            style(base + i, base + i + 2, "swift-formatting");
            style(base + end, base + end + 2, "swift-formatting");
          }
          style(base + i + 2, base + end, "swift-highlight");
          inline(base, text, isActive, i + 2, end);
          i = end + 2;
          continue;
        }
      }
      if (ch === "*" || ch === "_") {
        const end = text.indexOf(ch, i + 1);
        if (end > i + 1 && !text.startsWith(ch + ch, i)) {
          if (!isActive) {
            hide(base + i, base + i + 1);
            hide(base + end, base + end + 1);
          } else {
            style(base + i, base + i + 1, "swift-formatting");
            style(base + end, base + end + 1, "swift-formatting");
          }
          style(base + i + 1, base + end, "swift-em");
          inline(base, text, isActive, i + 1, end);
          i = end + 1;
          continue;
        }
      }
      i++;
    }
  };

  const covered = new Set<number>();
  const fenceByLine = new Map<number, { fence: Fence; role: "open" | "code" | "close" }>();
  for (const fence of fences) {
    for (let n = fence.openLine; n <= (fence.closeLine === -1 ? lines.length - 1 : fence.closeLine); n++) {
      const role = n === fence.openLine ? "open" : n === fence.closeLine ? "close" : "code";
      fenceByLine.set(n, { fence, role });
    }
  }
  const tableByLine = new Map<number, TableBlock>();
  for (const table of tables) {
    for (let n = table.fromLine; n <= table.toLine; n++) tableByLine.set(n, table);
  }

  // Rendered embeds replace their whole block while the cursor is outside.
  for (const fence of fences) {
    if (fence.lang !== "mermaid") continue;
    let touched = false;
    for (let n = fence.openLine; n <= (fence.closeLine === -1 ? lines.length - 1 : fence.closeLine); n++) {
      if (active.has(n)) touched = true;
    }
    if (touched) continue;
    const svg = cachedMermaidSvg(fence.code);
    if (svg) {
      const from = doc.line(fence.openLine + 1).from;
      const last = doc.line(fence.closeLine === -1 ? lines.length : fence.closeLine + 1);
      decos.push(
        Decoration.replace({
          widget: new MermaidWidget(svg, fence.code, from, ctx),
          block: true,
        }).range(from, last.to),
      );
      for (let n = fence.openLine; n <= (fence.closeLine === -1 ? lines.length - 1 : fence.closeLine); n++) {
        covered.add(n);
      }
    }
  }
  for (const table of tables) {
    let touched = false;
    for (let n = table.fromLine; n <= table.toLine; n++) if (active.has(n)) touched = true;
    if (touched) continue;
    const from = doc.line(table.fromLine + 1).from;
    const to = doc.line(table.toLine + 1).to;
    decos.push(
      Decoration.replace({
        widget: new TableWidget(table, from, ctx),
        block: true,
      }).range(from, to),
    );
    for (let n = table.fromLine; n <= table.toLine; n++) covered.add(n);
  }

  for (let n = 0; n < lines.length; n++) {
    if (covered.has(n)) continue;
    const line = doc.line(n + 1);
    const text = line.text;
    const isActive = lineActive(active, n);
    const fenceInfo = fenceByLine.get(n);
    if (fenceInfo) {
      const { fence, role } = fenceInfo;
      const activeBlock = active.has(fence.openLine) ||
        active.has(fence.closeLine) ||
        Array.from(active).some((a) => a >= fence.openLine && a <= (fence.closeLine === -1 ? lines.length : fence.closeLine));
      if (role === "open" || role === "close") {
        if (!activeBlock || held) {
          addLine(n, role === "open" ? "swift-fence-spacer swift-fence-top" : "swift-fence-spacer swift-fence-bottom");
          continue;
        }
        addLine(n, "swift-fence-line swift-fence-active");
        const re = /^(\s{0,3}[`~]{3,})(.*)$/.exec(text);
        if (re) {
          style(line.from, line.from + re[1].length, "swift-formatting");
          const info = re[2];
          if (info.trim()) style(line.from + re[1].length, line.to, "swift-fence-info");
        }
        continue;
      }
      // code line
      const first = fence.firstLine;
      const last = fence.lastLine;
      const cls = ["swift-codeblock-line", "swift-code-line"];
      if (n === first) cls.push("swift-codeblock-first");
      if (n === last) cls.push("swift-codeblock-last");
      addLine(n, cls.join(" "));
      style(line.from, line.to, "swift-code-text");
      // The flair is positioned at the end of the first code line. On an
      // empty line being edited, that is also the caret's anchor, and the
      // absolutely positioned flair drags the caret to the far right.
      // Show it once the line has text, or while the caret is elsewhere.
      if (n === first && (line.length > 0 || !isActive)) {
        decos.push(
          Decoration.widget({
            widget: new CodeFlairWidget(displayLang(fence.lang) || fence.lang, fence.title, fence.code, ctx),
            side: 1,
          }).range(line.to),
        );
      }
      continue;
    }

    if (tableByLine.has(n)) {
      addLine(n, "swift-table-source");
      inline(line.from, text, true);
      continue;
    }

    if (text.trim() === "") {
      addLine(n, isActive ? "swift-blank-line swift-active-line" : "swift-blank-line");
      continue;
    }

    // A line that is only an image becomes a block image.
    const imageOnly = /^\s*!\[([^\]]*)\]\(([^)]+)\)\s*$/.exec(text);
    if (imageOnly && !isActive) {
      const url = imageOnly[2].trim();
      const resolved = inlineAssetUrl(url) || options.assetUrl(url);
      if (resolved) {
        decos.push(
          Decoration.replace({
            widget: new ImageWidget(resolved, imageOnly[1], line.from, true, url, ctx),
            block: true,
          }).range(line.from, line.to),
        );
        continue;
      }
    }
    const wikiImageOnly = /^\s*!\[\[([^\]]+)\]\]\s*$/.exec(text);
    if (wikiImageOnly && !isActive) {
      const inner = wikiImageOnly[1];
      const target = inner.split("|")[0].trim();
      const alias = inner.split("|")[1]?.trim() ?? "";
      if (isImageRef(target)) {
        const resolved = inlineAssetUrl(target) || options.assetUrl(target);
        decos.push(
          Decoration.replace({
            widget: new ImageWidget(resolved, alias || target, line.from, true, target, ctx),
            block: true,
          }).range(line.from, line.to),
        );
        continue;
      }
    }

    const hr = /^\s{0,3}((\*\s*){3,}|(-\s*){3,}|(_\s*){3,})\s*$/.exec(text);
    if (hr && !isActive) {
      decos.push(
        Decoration.replace({
          widget: new HrWidget(line.from, ctx),
          block: true,
        }).range(line.from, line.to),
      );
      continue;
    }
    if (hr) {
      addLine(n, "swift-hr-source");
      style(line.from, line.to, "swift-formatting");
      continue;
    }

    const heading = /^(#{1,6})(\s+)(.*)$/.exec(text);
    if (heading) {
      const level = heading[1].length;
      addLine(n, `swift-heading swift-h${level}${isActive ? " swift-active-line" : ""}`);
      if (isActive) {
        style(line.from, line.from + heading[1].length, "swift-formatting");
        style(line.from + heading[1].length, line.from + heading[1].length + heading[2].length, "swift-formatting");
      } else {
        hide(line.from, line.from + heading[1].length + heading[2].length);
      }
      inline(line.from, text, isActive, heading[1].length + heading[2].length);
      continue;
    }

    const task = /^(\s*)([-*+])(\s+)\[([ xX])\](\s+)(.*)$/.exec(text);
    if (task) {
      const indent = task[1].length;
      const markerStart = indent;
      const contentStart = indent + 1 + task[3].length + 3 + task[5].length;
      addLine(n, `swift-list-line swift-task-line${isActive ? " swift-active-line" : ""}`);
      const boxPos = line.from + indent + 1 + task[3].length + 1;
      const checkbox = new TaskWidget(task[4].toLowerCase() === "x", boxPos, ctx);
      const from = line.from + markerStart;
      const to = line.from + contentStart;
      decos.push(Decoration.replace({ widget: checkbox }).range(from, to));
      atomicRanges.push(Decoration.mark({}).range(from, to));
      inline(line.from, text, isActive, contentStart);
      continue;
    }

    const list = /^(\s*)([-*+]|\d{1,9}[.)])(\s+)(.*)$/.exec(text);
    if (list) {
      const indent = list[1].length;
      const numbered = /\d/.test(list[2]);
      const contentStart = indent + list[2].length + list[3].length;
      const depth = Math.min(8, Math.floor(indent / 2));
      addLine(n, `swift-list-line swift-list-depth-${depth}${isActive ? " swift-active-line" : ""}`);
      const marker = numbered
        ? new ListMarkerWidget("number", list[2], depth)
        : new ListMarkerWidget("bullet", "", depth);
      const from = line.from + indent;
      const to = line.from + contentStart;
      decos.push(Decoration.replace({ widget: marker }).range(from, to));
      atomicRanges.push(Decoration.mark({}).range(from, to));
      inline(line.from, text, isActive, contentStart);
      continue;
    }

    const quote = /^(\s{0,3}>\s?)(.*)$/.exec(text);
    if (quote) {
      addLine(n, `swift-blockquote${isActive ? " swift-active-line" : ""}`);
      if (isActive) {
        style(line.from, line.from + quote[1].length, "swift-formatting");
      } else {
        hide(line.from, line.from + quote[1].length);
      }
      inline(line.from, text, isActive, quote[1].length);
      continue;
    }

    const prevBlank = n > 0 && lines[n - 1].trim() === "";
    addLine(n, `swift-paragraph${prevBlank && n > 0 ? " swift-block-start" : ""}${isActive ? " swift-active-line" : ""}`);
    inline(line.from, text, isActive);
  }

  const decorations = Decoration.set(decos, true);
  const atomic = Decoration.set(atomicRanges, true);
  return { decorations, atomic, held };
}

/** livePreview is the Obsidian-style live preview of a Markdown document. */
export function livePreview(options: LivePreviewOptions): Extension {
  const field = StateField.define<PreviewValue>({
    create(state) {
      return build(state, options);
    },
    update(value, tr) {
      let held = value.held;
      let heldChanged = false;
      for (const effect of tr.effects) {
        if (effect.is(holdCodeFences)) {
          held = effect.value;
          heldChanged = true;
        }
      }
      if (
        tr.docChanged ||
        tr.selection ||
        heldChanged ||
        tr.effects.some((effect) => effect.is(reloadMermaid) || effect.is(refreshPreview))
      ) {
        return build(tr.state, options, held);
      }
      return value;
    },
    provide(f) {
      return [
        EditorView.decorations.from(f, (value) => value.decorations),
        EditorView.atomicRanges.of((view) => view.state.field(f).atomic),
      ];
    },
  });
  return field;
}
