import "./obsidian.css";

import { EditorState, Compartment, Prec } from "@codemirror/state";
import {
  EditorView,
  drawSelection,
  dropCursor,
  highlightSpecialChars,
  keymap,
  placeholder,
  rectangularSelection,
  crosshairCursor,
} from "@codemirror/view";
import {
  defaultKeymap,
  history,
  historyKeymap,
  indentWithTab,
  redo as redoCommand,
  undo as undoCommand,
} from "@codemirror/commands";
import {
  HighlightStyle,
  bracketMatching,
  indentOnInput,
  syntaxHighlighting,
  syntaxTree,
} from "@codemirror/language";
import { markdown, markdownLanguage } from "@codemirror/lang-markdown";
import { languages } from "@codemirror/language-data";
import {
  closeSearchPanel,
  openSearchPanel,
  search,
  searchKeymap,
} from "@codemirror/search";
import { tags as t } from "@lezer/highlight";
import TurndownService from "turndown";
import { cachedMermaidSvg, renderMermaidSvg } from "./mermaid";
import { installDiagnostics } from "./diagnostics";

import {
  codeFenceDeletionRanges,
  countCharacters,
  countWords,
  fenceAt,
  outlineHeadings,
  parseFences,
  tableOnly,
} from "./markdown";
import { assetUrl, clipboardFiles, isImageFile } from "./runtime";
import {
  readBlockLine,
  transformBlockLines,
  type BlockKind,
} from "./lists";
import {
  attachView,
  holdCodeFences,
  livePreview,
  refreshPreview,
  reloadMermaid,
} from "./live-preview";
import type {
  AttachmentResult,
  CommandPayload,
  MyGoBridge,
  OpenPayload,
  OutlineHeading,
  ReloadPayload,
  RenameResult,
  SettingsPayload,
  StatusPayload,
  TextPayload,
} from "./types";

const EVENT_OPEN = "web-editor:open";
const EVENT_STATUS = "web-editor:status";
const EVENT_SETTINGS = "web-editor:settings";
const EVENT_COMMAND = "web-editor:command";
const EVENT_INSERT = "web-editor:insert-text";
const EVENT_TITLE = "web-editor:title";
const EVENT_RELOAD = "web-editor:reload";

const SERVICE = "SwiftEditor";

const $ = <T extends HTMLElement>(id: string): T => {
  const node = document.getElementById(id);
  if (!node) throw new Error(`missing #${id}`);
  return node as T;
};

const els = {
  app: $("app"),
  toolbar: $("toolbar"),
  main: $("main"),
  scroller: $("scroller"),
  page: $("page"),
  title: $("title"),
  editor: $("editor"),
  outline: $("outline"),
  outlineResizer: $("outline-resizer"),
  outlineList: $("outline-list"),
  status: $("status"),
  statusState: $("status-state"),
  statusCount: $("status-count"),
  statusMode: $("status-mode"),
  toast: $("toast"),
};

let current: OpenPayload | null = null;
let docId = "";
let currentMarkdown = "";
let readOnly = false;
let previewMode = false;
let loading = false;
let dirty = false;
let previousTitle = "";
let saveStateTimer = 0;
let countTimer = 0;
let outlineTimer = 0;
let scrollTimer = 0;
let scrollRestoreRaf = 0;
let toastTimer = 0;
let outlineWidth = 240;
let outlineResizePointer: number | null = null;
let outlineResizeStartX = 0;
let outlineResizeStartWidth = 0;
let slashOpen = false;
let slashStart = 0;
let slashItems: SlashItem[] = [];
let slashIndex = 0;
let blockMenu: HTMLElement | null = null;
let toolbarTimer = 0;
const toolbarButtons: Record<string, HTMLButtonElement> = {};
let blockLabel: HTMLElement | null = null;

const settings = {
  fontSize: 16,
  lineHeight: 1.75,
  readableLineWidth: false,
};

const bridge: MyGoBridge = window.mygo ?? createMockBridge();
installDiagnostics(bridge, SERVICE);

const turndown = new TurndownService({
  headingStyle: "atx",
  bulletListMarker: "-",
  codeBlockStyle: "fenced",
  emDelimiter: "*",
  strongDelimiter: "**",
});
turndown.addRule("strikethrough", {
  filter: ["del", "s"],
  replacement: (content) => `~~${content}~~`,
});
// Turndown leaves tables out of its Markdown by default. A table copied
// from a rendered page should arrive as a GitHub-style pipe table.
turndown.addRule("table", {
  filter: "table",
  replacement: (_content, node) => {
    const table = node as HTMLTableElement;
    const rows: string[][] = [];
    for (const row of Array.from(table.rows)) {
      const cells: string[] = [];
      for (const cell of Array.from(row.cells)) {
        cells.push((cell.textContent ?? "").replace(/\s+/g, " ").trim().replace(/\|/g, "\\|"));
      }
      if (cells.length > 0) rows.push(cells);
    }
    if (rows.length === 0) return "";
    const width = Math.max(...rows.map((row) => row.length));
    for (const row of rows) {
      while (row.length < width) row.push("");
    }
    const lines = [
      `| ${rows[0].join(" | ")} |`,
      `| ${rows[0].map(() => "---").join(" | ")} |`,
      ...rows.slice(1).map((row) => `| ${row.join(" | ")} |`),
    ];
    return "\n\n" + lines.join("\n") + "\n\n";
  },
});

const saveCompartment = new Compartment();

function copyText(text: string) {
  void (async () => {
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      try {
        await bridge.call<void>(`${SERVICE}.CopyText`, text);
      } catch {
        // ignored
      }
    }
    toast("已复制");
  })();
}

/** imageAsPngBlob rasterizes a picture the page can load into PNG. */
function imageAsPngBlob(src: string): Promise<Blob> {
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.onload = () => {
      try {
        const canvas = document.createElement("canvas");
        canvas.width = img.naturalWidth;
        canvas.height = img.naturalHeight;
        const ctx2d = canvas.getContext("2d");
        if (!ctx2d || !canvas.width || !canvas.height) {
          reject(new Error("canvas unavailable"));
          return;
        }
        ctx2d.drawImage(img, 0, 0);
        canvas.toBlob(
          (blob) => (blob ? resolve(blob) : reject(new Error("encode failed"))),
          "image/png",
        );
      } catch (err) {
        reject(err instanceof Error ? err : new Error(String(err)));
      }
    };
    img.onerror = () => reject(new Error("image failed"));
    img.src = src;
  });
}

/** copyImage puts the picture itself on the clipboard, not its path. The
 * page rasterizes the pictures it can load; the app reads the vault's
 * own files and writes them as PNG. */
function copyImage(src: string, ref: string) {
  void (async () => {
    try {
      const blob = await imageAsPngBlob(src);
      await navigator.clipboard.write([new ClipboardItem({ [blob.type]: blob })]);
      toast("已复制图片");
      return;
    } catch {
      // The page could not copy it; let the app read the file.
    }
    try {
      await bridge.call<void>(`${SERVICE}.CopyImage`, docId, ref || src);
      toast("已复制图片");
    } catch {
      toast("复制图片失败");
    }
  })();
}

/** imageDataURL returns a loaded picture as a portable data URL. */
function imageDataURL(img: HTMLImageElement): string | null {
  const src = img.currentSrc || img.src;
  if (!src) return null;
  // These formats are portable in HTML as they are; SVG is rasterized
  // below so apps that do not read SVG can still paste the picture.
  if (/^data:image\/(png|jpe?g|gif|webp);/i.test(src)) return src;
  if (!img.complete || !img.naturalWidth || !img.naturalHeight) return null;
  try {
    const canvas = document.createElement("canvas");
    canvas.width = img.naturalWidth;
    canvas.height = img.naturalHeight;
    const ctx = canvas.getContext("2d");
    if (!ctx) return null;
    ctx.drawImage(img, 0, 0);
    return canvas.toDataURL("image/png");
  } catch {
    // A picture the page may not read across origins stays as it is.
    return null;
  }
}

/** copyMixedSelection puts text and pictures on the clipboard together.
 * A browser copy of a page gives the receiving app the pictures' URLs,
 * which point at the vault's private scheme and cannot be shown there.
 * Replacing them with data URLs makes the pictures travel with the text. */
function copyMixedSelection(event: ClipboardEvent) {
  if (!event.clipboardData) return;
  const selection = window.getSelection();
  if (!selection || selection.isCollapsed || selection.rangeCount === 0) return;
  const range = selection.getRangeAt(0);
  if (!view.contentDOM.contains(range.commonAncestorContainer)) return;
  const fragment = range.cloneContents();
  if (!fragment.querySelector("img.swift-image-el")) return;

  const dataBySrc = new Map<string, string>();
  for (const img of Array.from(view.contentDOM.querySelectorAll<HTMLImageElement>("img.swift-image-el"))) {
    if (!range.intersectsNode(img)) continue;
    const data = imageDataURL(img);
    if (data) dataBySrc.set(img.src, data);
  }
  if (dataBySrc.size === 0) return;

  const holder = document.createElement("div");
  holder.appendChild(fragment);
  holder.querySelectorAll("img.cm-widgetBuffer, .code-block-flair, .swift-embed-hint").forEach((el) => el.remove());
  // The Markdown markers are visible while a line is edited; a copy
  // should carry the rendered text, not "**" around bold words.
  holder.querySelectorAll(".swift-formatting").forEach((el) => el.remove());

  let converted = 0;
  for (const img of Array.from(holder.querySelectorAll<HTMLImageElement>("img.swift-image-el"))) {
    const data = dataBySrc.get(img.src);
    if (data) {
      img.src = data;
      converted++;
    }
    img.removeAttribute("srcset");
    img.removeAttribute("loading");
    img.removeAttribute("title");
    img.setAttribute("alt", img.alt || "图片");
  }
  if (converted === 0) return;

  for (const el of Array.from(holder.querySelectorAll<HTMLElement>("*"))) {
    const classes = el.classList;
    if (classes.contains("swift-strong")) el.style.fontWeight = "700";
    if (classes.contains("swift-em")) el.style.fontStyle = "italic";
    if (classes.contains("swift-strike")) el.style.textDecoration = "line-through";
    if (classes.contains("swift-inline-code")) {
      el.style.fontFamily = "monospace";
      el.style.background = "#fafafa";
    }
    el.removeAttribute("class");
    el.removeAttribute("contenteditable");
    el.removeAttribute("data-href");
    el.removeAttribute("aria-hidden");
  }

  const pictures = Array.from(holder.querySelectorAll("img"))
    .map((img) => img.getAttribute("alt") || "")
    .filter(Boolean);
  const selectedText = selection.toString();
  const plain = selectedText.trim() ? selectedText : pictures.join("\n");
  try {
    event.clipboardData.setData("text/html", `<meta charset="utf-8">${holder.innerHTML}`);
    event.clipboardData.setData("text/plain", plain);
  } catch {
    // Leave the copy to the browser if this web view refuses the data.
    return;
  }
  event.preventDefault();
}

function openLink(url: string) {
  void bridge.call<void>(`${SERVICE}.OpenLink`, url).catch(() => undefined);
}

/** obsidianHighlightStyle maps syntax tokens to Obsidian's light palette.
 * The colors stay darker than Obsidian's faint tokens so code on the
 * near-white block background keeps a comfortable contrast; Bash in
 * particular shows many comments, strings and builtins at once. */
const obsidianHighlightStyle = HighlightStyle.define([
  { tag: t.comment, color: "#6b6b6b" },
  { tag: [t.keyword, t.modifier, t.operatorKeyword], color: "#b02060" },
  { tag: [t.string, t.special(t.string), t.regexp], color: "#0b7a38" },
  { tag: [t.number, t.bool, t.null, t.atom], color: "#5b3fc4" },
  { tag: [t.function(t.variableName), t.function(t.propertyName)], color: "#8a5a00" },
  { tag: [t.typeName, t.className, t.namespace], color: "#00706e" },
  { tag: [t.propertyName, t.attributeName], color: "#00706e" },
  { tag: [t.standard(t.variableName), t.definition(t.variableName)], color: "#1f5fbf" },
  { tag: [t.operator, t.compareOperator, t.arithmeticOperator, t.logicOperator], color: "#c41f36" },
  { tag: [t.punctuation, t.separator, t.bracket], color: "#4f4f4f" },
  { tag: [t.tagName, t.angleBracket], color: "#c41f36" },
  { tag: t.link, color: "#6d3fd4", textDecoration: "underline" },
  { tag: t.url, color: "#6d3fd4" },
  { tag: t.emphasis, fontStyle: "italic" },
  { tag: t.strong, fontWeight: "700" },
  { tag: t.strikethrough, textDecoration: "line-through" },
  { tag: t.heading, color: "var(--text-normal)", fontWeight: "inherit" },
  { tag: t.monospace, color: "var(--text-normal)" },
  { tag: t.invalid, color: "#c41f36" },
]);

function makeEditorState(doc: string): EditorState {
  return EditorState.create({
    doc,
    extensions: [
      highlightSpecialChars(),
      history(),
      drawSelection(),
      dropCursor(),
      EditorState.allowMultipleSelections.of(true),
      indentOnInput(),
      bracketMatching(),
      rectangularSelection(),
      crosshairCursor(),
      EditorView.lineWrapping,
      EditorState.tabSize.of(4),
      EditorState.phrases.of({
        Find: "查找",
        Replace: "替换",
        next: "下一个",
        previous: "上一个",
        all: "全部",
        "match case": "区分大小写",
        regexp: "正则",
        "by word": "全词",
        replace: "替换",
        "replace all": "全部替换",
        close: "关闭",
        "current match": "当前匹配",
        "on line": "位于第",
      }),
      EditorView.contentAttributes.of({
        spellcheck: "false",
        autocorrect: "off",
        autocapitalize: "off",
      }),
      placeholder("开始书写…"),
      markdown({
        base: markdownLanguage,
        codeLanguages: languages,
        addKeymap: true,
      }),
      syntaxHighlighting(obsidianHighlightStyle),
      saveCompartment.of(EditorView.editable.of(true)),
      EditorView.inputHandler.of((view, from, to, text) => fenceInput(view, from, to, text)),
      Prec.high(keymap.of([
        { key: "Backspace", run: (view) => deleteFencePair(view, false) },
        { key: "Delete", run: (view) => deleteFencePair(view, true) },
        { key: "Enter", run: fenceEnter },
        { key: "Mod-a", run: selectCodeBlock },
      ])),
      livePreview({
        get docId() {
          return docId;
        },
        assetUrl: (ref) => assetUrl(ref, docId, bridge.platform ?? "web"),
        copy: copyText,
        copyImage,
        openLink,
        reveal: revealPosition,
        preview: () => previewMode,
      }),
      search({ top: true }),
      keymap.of([
        indentWithTab,
        ...buildFormattingKeymap(),
        ...defaultKeymap,
        ...historyKeymap,
        ...searchKeymap,
      ]),
      EditorView.updateListener.of((update) => {
        if (update.docChanged && !loading) {
          currentMarkdown = update.state.doc.toString();
          markDirty();
          queueCount();
          queueOutline();
          scheduleMermaid();
          syncChanged();
        }
        if (update.selectionSet) {
          queueOutline();
          if (!loading) revealCursorSoon();
        }
        if (update.docChanged || update.selectionSet) {
          scheduleToolbarRefresh();
        }
        // The Edit > Select All menu uses the web view's own command,
        // which bypasses the editor's keymap. When it jumps from a caret
        // in a block of code straight to the whole document, keep the
        // selection inside that block instead.
        if (update.selectionSet && !update.docChanged && update.state.doc.length > 0) {
          const next = update.state.selection.main;
          if (next.from === 0 && next.to === update.state.doc.length) {
            const previous = update.startState.selection.main;
            const block = codeBlockSelection(update.startState, previous.head);
            if (block && (block.from !== 0 || block.to !== update.state.doc.length)) {
              const editor = update.view;
              queueMicrotask(() => {
                const now = editor.state.selection.main;
                if (now.from === 0 && now.to === editor.state.doc.length) {
                  editor.dispatch({ selection: { anchor: block.from, head: block.to }, scrollIntoView: true });
                }
              });
            }
          }
        }
        updateSlashMenu(update);
      }),
      EditorView.theme(
        {
          "&": {
            backgroundColor: "transparent",
            color: "var(--text-normal)",
            fontSize: "var(--font-text-size)",
            height: "auto",
          },
          "&.cm-focused": { outline: "none" },
          ".cm-scroller": {
            overflow: "visible",
            fontFamily: "var(--font-text)",
            lineHeight: "var(--line-height-normal)",
          },
          ".cm-content": {
            padding: "0",
            caretColor: "var(--caret-color)",
            fontFamily: "var(--font-text)",
            minHeight: "40vh",
          },
          ".cm-cursor, .cm-dropCursor": {
            borderLeftColor: "var(--caret-color)",
            borderLeftWidth: "1.5px",
          },
          "&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection":
            {
              backgroundColor: "var(--text-selection)",
            },
          ".cm-selectionMatch": { backgroundColor: "rgba(138, 92, 246, 0.12)" },
          ".cm-searchMatch": { backgroundColor: "rgba(255, 208, 0, 0.35)" },
          ".cm-searchMatch.cm-searchMatch-selected": {
            backgroundColor: "rgba(255, 150, 50, 0.5)",
          },
          ".cm-panels": {
            position: "fixed",
            zIndex: "20",
            top: "var(--toolbar-height)",
            right: "12px",
            left: "auto",
            backgroundColor: "transparent",
          },
          ".cm-panel.cm-search": {
            display: "flex",
            flexWrap: "wrap",
            alignItems: "center",
            gap: "4px",
            margin: "10px 14px",
            padding: "6px 8px",
            backgroundColor: "var(--background-primary)",
            border: "1px solid var(--background-modifier-border)",
            borderRadius: "8px",
            boxShadow: "0 4px 18px rgba(0, 0, 0, 0.12)",
            fontFamily: "var(--font-interface)",
            fontSize: "13px",
            color: "var(--text-normal)",
          },
          ".cm-panel.cm-search input": {
            height: "26px",
            padding: "2px 7px",
            border: "1px solid var(--background-modifier-border)",
            borderRadius: "5px",
            outline: "none",
            backgroundColor: "var(--background-primary)",
            color: "var(--text-normal)",
            fontSize: "13px",
            fontFamily: "var(--font-interface)",
          },
          ".cm-panel.cm-search input:focus": {
            borderColor: "var(--interactive-accent)",
          },
          ".cm-panel.cm-search button": {
            height: "26px",
            minWidth: "26px",
            padding: "0 6px",
            border: "0",
            borderRadius: "5px",
            background: "transparent",
            color: "var(--text-muted)",
            fontFamily: "var(--font-interface)",
            fontSize: "12px",
            cursor: "default",
          },
          ".cm-panel.cm-search button:hover": {
            backgroundColor: "var(--background-modifier-hover)",
            color: "var(--text-normal)",
          },
          ".cm-panel.cm-search label": {
            display: "inline-flex",
            alignItems: "center",
            gap: "2px",
            color: "var(--text-muted)",
            fontSize: "12px",
          },
          ".cm-panel.cm-search br": { display: "none" },
          ".cm-panel.cm-search [name=close]": {
            position: "absolute",
            top: "2px",
            right: "2px",
            fontSize: "14px",
            lineHeight: "14px",
          },
          ".cm-tooltip": {
            border: "1px solid var(--background-modifier-border)",
            backgroundColor: "var(--background-primary)",
            borderRadius: "6px",
            boxShadow: "0 4px 16px rgba(0, 0, 0, 0.1)",
          },
        },
        { dark: false },
      ),
    ],
  });
}

const view = new EditorView({
  state: makeEditorState(""),
  parent: els.editor,
});
attachView(view);

// CodeMirror puts its search panel inside the scrolling editor, whose
// position: fixed is then relative to the document and scrolls away. The
// note's page scrolls in the outer scroller, so float the panel at the top
// of the view instead. The panel's own listeners travel with it.
const searchPanels = new MutationObserver(() => {
  const panels = els.editor.querySelector<HTMLElement>(".cm-panels");
  if (!panels || panels.parentElement === document.body) return;
  for (const old of document.querySelectorAll<HTMLElement>("body > .cm-panels")) {
    if (old !== panels) old.remove();
  }
  document.body.appendChild(panels);
  panels.querySelector<HTMLInputElement>('input[name="search"]')?.focus();
});
searchPanels.observe(els.editor, { childList: true, subtree: true });

els.editor.addEventListener("click", (event) => {
  const target = event.target as HTMLElement | null;
  const link = target?.closest<HTMLElement>("[data-href]");
  if (!link) return;
  if (!event.metaKey && !event.ctrlKey) return;
  event.preventDefault();
  const href = link.dataset.href ?? "";
  if (href) openLink(href);
});

els.editor.addEventListener("paste", handlePaste, true);
document.addEventListener("copy", copyMixedSelection, true);
document.addEventListener(
  "cut",
  (event) => {
    if (!isReadOnly()) return;
    const target = event.target;
    if (!(target instanceof Node)) return;
    if (!view.contentDOM.contains(target) && !els.title.contains(target)) return;
    event.preventDefault();
    event.stopPropagation();
  },
  true,
);

// Keep collapsed fences from moving text under the pointer while selecting.
// Plain clicks still expand a fence after mouseup.
let holdingCodeFences = false;
els.editor.addEventListener(
  "mousedown",
  (event) => {
    if (event.button !== 0 || previewMode || readOnly) return;
    const target = event.target as HTMLElement | null;
    let codeLine = target?.closest<HTMLElement>(".swift-codeblock-line") ?? null;
    while (codeLine && !codeLine.classList.contains("swift-codeblock-first")) {
      const previous = codeLine.previousElementSibling;
      codeLine = previous instanceof HTMLElement ? previous.closest(".swift-codeblock-line") : null;
    }
    if (!codeLine?.previousElementSibling?.classList.contains("swift-fence-spacer")) return;
    holdingCodeFences = true;
    view.dispatch({ effects: holdCodeFences.of(true) });
  },
  true,
);

window.addEventListener(
  "mouseup",
  () => {
    if (!holdingCodeFences) return;
    window.setTimeout(() => {
      if (!view.state.selection.main.empty) return;
      holdingCodeFences = false;
      view.dispatch({ effects: holdCodeFences.of(false) });
    }, 0);
  },
  true,
);

// The collapsed ``` lines are part of the code block's padding. A click
// there enters the code block instead of putting the caret in the fence.
els.editor.addEventListener(
  "mousedown",
  (event) => {
    const target = (event.target as HTMLElement | null)?.closest(".swift-fence-spacer");
    if (!(target instanceof HTMLElement)) return;
    event.preventDefault();
    event.stopPropagation();
    const isTop = target.classList.contains("swift-fence-top");
    const sibling = isTop ? target.nextElementSibling : target.previousElementSibling;
    if (sibling instanceof HTMLElement) {
      let pos = 0;
      try {
        pos = isTop
          ? view.posAtDOM(sibling, 0)
          : view.posAtDOM(sibling, sibling.textContent?.length ?? 0);
      } catch {
        pos = view.state.selection.main.head;
      }
      view.dispatch({ selection: { anchor: pos } });
    }
    view.focus();
  },
  true,
);

// Keep Edit > Paste enabled only while the page has the keyboard.
window.addEventListener("focus", () => {
  void bridge.call<void>(`${SERVICE}.SetFocused`, !isReadOnly()).catch(() => undefined);
});
window.addEventListener("blur", () => {
  void bridge.call<void>(`${SERVICE}.SetFocused`, false).catch(() => undefined);
});
els.scroller.addEventListener("dragover", (e) => {
  if (e.dataTransfer?.types.includes("Files")) e.preventDefault();
});
els.scroller.addEventListener("drop", (e) => {
  if (e.dataTransfer?.files.length) {
    e.preventDefault();
    void insertFiles(Array.from(e.dataTransfer.files));
  }
});

function markDirty() {
  dirty = true;
  setStatus("正在保存…", "saving");
  window.clearTimeout(saveStateTimer);
  saveStateTimer = window.setTimeout(() => {
    if (dirty) setStatus("等待保存", "dirty");
  }, 2500);
}

function syncChanged() {
  if (!docId) return;
  void bridge
    .call<void>(`${SERVICE}.Changed`, docId, currentMarkdown)
    .catch((err) => {
      console.warn("changed failed", err);
      setStatus("保存失败", "error");
    });
}

function flushScrollPosition() {
  window.clearTimeout(scrollTimer);
  scrollTimer = 0;
  if (!current || !docId || docId === "note" || loading) return;
  void bridge
    .call<void>(`${SERVICE}.Scroll`, docId, Math.max(0, els.scroller.scrollTop))
    .catch((err) => console.warn("scroll failed", err));
}

function queueScrollPosition() {
  if (!current || !docId || docId === "note" || loading) return;
  window.clearTimeout(scrollTimer);
  scrollTimer = window.setTimeout(flushScrollPosition, 220);
}

function restoreDocumentScroll(scrollTop: number) {
  const top = Number.isFinite(scrollTop) && scrollTop > 0 ? scrollTop : 0;
  window.cancelAnimationFrame(scrollRestoreRaf);
  els.scroller.scrollTop = top;
  // CodeMirror finishes its first layout on the next frame.
  scrollRestoreRaf = window.requestAnimationFrame(() => {
    scrollRestoreRaf = 0;
    els.scroller.scrollTop = top;
  });
}

function queueCount() {
  window.clearTimeout(countTimer);
  countTimer = window.setTimeout(updateCount, 120);
}

function queueOutline() {
  window.clearTimeout(outlineTimer);
  outlineTimer = window.setTimeout(renderOutline, 90);
}

function updateCount() {
  const words = countWords(currentMarkdown);
  const chars = countCharacters(currentMarkdown);
  els.statusCount.textContent = `${formatCount(words)} 个词 ${formatCount(chars)} 个字符`;
}

function formatCount(n: number): string {
  return n.toLocaleString("zh-CN");
}

function setStatus(text: string, state: StatusPayload["state"]) {
  els.statusState.textContent = text;
  els.statusState.dataset.state = state;
}

function setTitle(title: string, focus = false) {
  previousTitle = title;
  els.title.textContent = title;
  els.title.classList.toggle("is-empty", title.length === 0);
  if (focus) {
    els.title.focus();
    const range = document.createRange();
    range.selectNodeContents(els.title);
    range.collapse(false);
    const sel = window.getSelection();
    sel?.removeAllRanges();
    sel?.addRange(range);
  }
}

function setReadOnly(value: boolean) {
  readOnly = value;
  applyDocumentMode();
}

function isReadOnly(): boolean {
  return readOnly || previewMode;
}

function applyDocumentMode() {
  const value = isReadOnly();
  view.dispatch({
    effects: saveCompartment.reconfigure(EditorView.editable.of(!value)),
  });
  els.title.setAttribute("contenteditable", value ? "false" : "true");
  els.toolbar.hidden = previewMode;
  els.statusMode.hidden = !value;
  if (previewMode) els.statusMode.textContent = "预览";
  else if (readOnly) els.statusMode.textContent = "只读";
  refreshToolbar();
}

function setPreviewMode(value: boolean) {
  if (!current || previewMode === value) return;
  previewMode = value;
  current.previewMode = value;
  closeSlash();
  closeBlockMenu();
  applyDocumentMode();
  view.dispatch({ effects: refreshPreview.of(null) });
  if (previewMode) {
    setStatus("预览", "readonly");
    void bridge.call<void>(`${SERVICE}.SetFocused`, false).catch(() => undefined);
  } else {
    if (readOnly) {
      setStatus("只读", "readonly");
    } else if (dirty) {
      setStatus("正在保存…", "saving");
      view.focus();
      void bridge.call<void>(`${SERVICE}.SetFocused`, true).catch(() => undefined);
    } else {
      setStatus("已保存", "saved");
      view.focus();
      void bridge.call<void>(`${SERVICE}.SetFocused`, true).catch(() => undefined);
    }
  }
}

function applySettings(settingsValue: SettingsPayload) {
  settings.fontSize = settingsValue.fontSize > 0 ? settingsValue.fontSize : 16;
  settings.lineHeight = settingsValue.lineHeight > 0 ? settingsValue.lineHeight : 1.5;
  settings.readableLineWidth = settingsValue.readableLineWidth;
  document.documentElement.style.setProperty("--font-text-size", `${settings.fontSize}px`);
  document.documentElement.style.setProperty("--line-height-normal", String(settings.lineHeight));
  els.page.classList.toggle("is-readable-line-width", settings.readableLineWidth);
  // A new font size or line height moves every line; measure again so the
  // caret and clicks stay on the text.
  if (typeof view !== "undefined") view.requestMeasure();
}

function clampOutlineWidth(width: number): number {
  if (!Number.isFinite(width) || width <= 0) return 240;
  return Math.min(Math.max(width, 180), 420);
}

function applyOutlineWidth(width: number) {
  outlineWidth = clampOutlineWidth(width);
  document.documentElement.style.setProperty("--outline-width", `${outlineWidth}px`);
}

function openDocument(payload: OpenPayload) {
  if (current && payload.id !== docId) flushScrollPosition();
  loading = true;
  closeSlash();
  closeBlockMenu();
  closeSearchPanel(view);
  current = payload;
  previewMode = Boolean(payload.previewMode);
  docId = payload.id;
  currentMarkdown = payload.content;
  dirty = false;
  applySettings({
    fontSize: payload.fontSize,
    lineHeight: payload.lineHeight,
    readableLineWidth: payload.readableLineWidth,
  });
  setTitle(payload.title);
  const modeText = previewMode ? "预览" : payload.readOnly ? "只读" : "已保存";
  setStatus(modeText, previewMode || payload.readOnly ? "readonly" : "saved");
  applyOutlineWidth(payload.outlineWidth);
  els.outline.hidden = !payload.outlineOpen;
  els.outlineResizer.hidden = !payload.outlineOpen;
  view.setState(makeEditorState(payload.content));
  // A page that is only a table opens with the caret on the blank line
  // after it, so the table is shown rendered instead of as the active
  // source block. Clicking the table still enters its source.
  if (payload.content.endsWith("\n")) {
    const lines = payload.content.split("\n");
    if (tableOnly(lines, parseFences(lines))) {
      const end = view.state.doc.length;
      view.dispatch({ selection: { anchor: end, head: end } });
    }
  }
  setReadOnly(payload.readOnly);
  loading = false;
  const focusTitle = Boolean(payload.focusTitle) && !isReadOnly();
  if (focusTitle) {
    // A new, untitled page wants its title first; its body is one Tab or
    // Down press away.
    setTitle(payload.title, true);
    void bridge.call<void>(`${SERVICE}.SetFocused`, false).catch(() => undefined);
  } else {
    view.focus();
    void bridge.call<void>(`${SERVICE}.SetFocused`, !isReadOnly()).catch(() => undefined);
  }
  restoreDocumentScroll(payload.scrollTop);
  updateCount();
  renderOutline();
  refreshToolbar();
  scheduleMermaid();
}

let mermaidTimer = 0;
function scheduleMermaid() {
  window.clearTimeout(mermaidTimer);
  mermaidTimer = window.setTimeout(prefetchMermaid, 260);
}

async function prefetchMermaid() {
  const lines = currentMarkdown.split("\n");
  const fences = parseFences(lines);
  let rendered = false;
  for (const fence of fences) {
    if (fence.lang !== "mermaid") continue;
    if (cachedMermaidSvg(fence.code)) continue;
    const svg = await renderMermaidSvg(fence.code);
    if (svg) rendered = true;
  }
  if (rendered) view.dispatch({ effects: reloadMermaid.of(null) });
}

let revealRaf = 0;
function revealCursorSoon() {
  if (revealRaf) return;
  revealRaf = window.requestAnimationFrame(() => {
    revealRaf = 0;
    const head = view.state.selection.main.head;
    revealPosition(head);
  });
}

/** revealPosition scrolls the page so pos is comfortably in view. */
function revealPosition(pos: number) {
  window.requestAnimationFrame(() => {
    const coords = view.coordsAtPos(pos);
    const rect = els.scroller.getBoundingClientRect();
    const margin = 60;
    if (!coords) {
      // In preview mode CodeMirror cannot scroll its own overflow-visible
      // scroller when the selection is outside the rendered viewport.
      const block = view.lineBlockAt(pos);
      els.scroller.scrollTop += view.documentTop + block.top - (rect.top + margin);
      return;
    }
    if (coords.top < rect.top + margin) {
      els.scroller.scrollTop -= rect.top + margin - coords.top;
    } else if (coords.bottom > rect.bottom - margin) {
      els.scroller.scrollTop += coords.bottom - (rect.bottom - margin);
    }
  });
}

/** reloadDocument replaces the page's text after an external edit. */
function reloadDocument(payload: ReloadPayload) {
  if (!current || payload.id !== docId || loading) return;
  const selection = view.state.selection.main;
  const anchor = Math.min(selection.anchor, payload.content.length);
  const head = Math.min(selection.head, payload.content.length);
  const scrollTop = els.scroller.scrollTop;
  const hadFocus = view.hasFocus;
  loading = true;
  dirty = false;
  closeSlash();
  closeBlockMenu();
  currentMarkdown = payload.content;
  if (current) current.content = payload.content;
  view.setState(makeEditorState(payload.content));
  setReadOnly(readOnly);
  view.dispatch({ selection: { anchor, head } });
  loading = false;
  if (hadFocus && !isReadOnly()) view.focus();
  els.scroller.scrollTop = scrollTop;
  updateCount();
  renderOutline();
  refreshToolbar();
  scheduleMermaid();
  if (previewMode) {
    setStatus("预览", "readonly");
  } else {
    setStatus(readOnly ? "只读" : "已保存", readOnly ? "readonly" : "saved");
  }
  toast("已从磁盘更新");
}

function insertText(text: string, opts: { block?: boolean; atCursor?: boolean } = {}) {
  if (isReadOnly()) return;
  const { from, to } = view.state.selection.main;
  let insert = text;
  if (opts.block) {
    const before = view.state.doc.sliceString(0, from);
    const after = view.state.doc.sliceString(to);
    if (before.length > 0 && !before.endsWith("\n")) insert = "\n" + insert;
    if (!after.startsWith("\n") && after.length > 0) insert = insert + "\n";
  }
  view.dispatch({
    changes: { from, to, insert },
    selection: { anchor: from + insert.length },
    scrollIntoView: true,
  });
  view.focus();
}

function wrapSelection(before: string, after = before) {
  if (isReadOnly()) return;
  const { from, to } = view.state.selection.main;
  const selected = view.state.sliceDoc(from, to);
  if (selected.startsWith(before) && selected.endsWith(after) && selected.length >= before.length + after.length) {
    const inner = selected.slice(before.length, selected.length - after.length);
    view.dispatch({
      changes: { from, to, insert: inner },
      selection: { anchor: from, head: from + inner.length },
    });
    return;
  }
  const insert = before + selected + after;
  view.dispatch({
    changes: { from, to, insert },
    selection: selected
      ? { anchor: from + before.length, head: from + before.length + selected.length }
      : { anchor: from + before.length },
  });
  view.focus();
}

/** hasNode reports whether pos sits inside one of the Markdown nodes. */
function hasNode(pos: number, names: string[]): boolean {
  let node: any = syntaxTree(view.state).resolveInner(pos, -1);
  while (node) {
    if (names.includes(node.name)) return true;
    node = node.parent;
  }
  return false;
}

/** codeBlockSelection returns the text inside the fenced block holding
 * pos, without its opening and closing fences. It returns null outside a
 * block and for an empty block, where there is no inner text to select. */
function codeBlockSelection(state: EditorState, pos: number): { from: number; to: number } | null {
  const doc = state.doc;
  if (doc.length === 0) return null;
  const line = doc.lineAt(Math.max(0, Math.min(pos, doc.length)));
  const lines: string[] = [];
  for (let i = 1; i <= doc.lines; i++) lines.push(doc.line(i).text);
  const fence = fenceAt(parseFences(lines), line.number - 1);
  if (!fence) return null;
  // An empty block has its closing fence on the line after the opening
  // one; selecting no text would make Select All appear broken.
  if (fence.closeLine !== -1 && fence.closeLine === fence.openLine + 1) return null;
  const firstBodyLine = fence.openLine + 2;
  if (firstBodyLine > doc.lines) return null;
  const from = doc.line(firstBodyLine).from;
  const to = fence.closeLine === -1
    ? doc.length
    : Math.max(from, doc.line(fence.closeLine + 1).from - 1);
  return to > from ? { from, to } : null;
}

/** selectCodeBlock handles Mod-a inside a fenced code block: it selects
 * the block's inner text. Outside one the default Select All runs. */
function selectCodeBlock(view: EditorView): boolean {
  const block = codeBlockSelection(view.state, view.state.selection.main.head);
  if (!block) return false;
  view.dispatch({ selection: { anchor: block.from, head: block.to }, scrollIntoView: true });
  return true;
}

/** fenceInput closes the code fence when a third backtick begins one.
 * The closing fence goes two lines below, so the language can still be
 * typed after the opening fence before the caret enters the body. */
function fenceInput(view: EditorView, from: number, to: number, text: string): boolean {
  if (text !== "`" || from !== to) return false;
  const line = view.state.doc.lineAt(from);
  if (from !== line.to) return false;
  const before = view.state.doc.sliceString(line.from, from);
  const match = /^(\s{0,3})``$/.exec(before);
  if (!match) return false;
  let node: any = syntaxTree(view.state).resolveInner(from, -1);
  while (node) {
    if (node.name === "FencedCode" || node.name === "CodeBlock") return false;
    node = node.parent;
  }
  const indent = match[1];
  view.dispatch({
    changes: { from, to, insert: "`\n\n" + indent + "```" },
    // Keep the caret after the opening fence, where a language goes.
    selection: { anchor: from + 1 },
    scrollIntoView: true,
  });
  return true;
}

/** fenceEnter enters an empty auto-closed code block instead of adding
 * another blank line between its fences. */
function fenceEnter(view: EditorView): boolean {
  if (isReadOnly()) return false;
  const { from, to } = view.state.selection.main;
  if (from !== to) return false;
  const doc = view.state.doc;
  const line = doc.lineAt(from);
  if (from !== line.to) return false;
  const open = /^(\s{0,3})(`{3,}|~{3,})(.*)$/.exec(line.text);
  if (!open) return false;
  if (line.number + 2 > doc.lines) return false;
  const body = doc.line(line.number + 1);
  if (body.text.trim() !== "") return false;
  const close = doc.line(line.number + 2);
  const closing = /^(\s{0,3})(`{3,}|~{3,})\s*$/.exec(close.text);
  if (!closing || closing[2][0] !== open[2][0] || closing[2].length < open[2].length) return false;
  view.dispatch({ selection: { anchor: body.from }, scrollIntoView: true });
  return true;
}

/** deleteFencePair removes both sides of a closed fence when a normal
 * Backspace or Delete would leave only one side behind. */
function deleteFencePair(view: EditorView, forward: boolean): boolean {
  if (isReadOnly() || view.state.selection.ranges.length !== 1) return false;
  const { from, to } = view.state.selection.main;
  const ranges = codeFenceDeletionRanges(view.state.doc.toString(), from, to, forward);
  if (!ranges) return false;
  view.dispatch({
    changes: ranges,
    userEvent: from === to ? (forward ? "delete.forward" : "delete.backward") : "delete.selection",
    scrollIntoView: true,
  });
  return true;
}

/** selectedLines returns the lines a selection covers; a selection that
 * ends at a line's start does not include that line. */
function selectedLines() {
  const { from, to } = view.state.selection.main;
  const first = view.state.doc.lineAt(from);
  let last = view.state.doc.lineAt(to);
  if (to > from && last.from === to) last = view.state.doc.line(last.number - 1);
  return { first, last };
}

/** setBlockKind changes every selected line to a heading, list or quote.
 * A second press removes the style from all of them. */
function setBlockKind(kind: BlockKind, level = 0) {
  if (isReadOnly()) return;
  if (hasNode(view.state.selection.main.head, ["FencedCode", "CodeBlock"])) return;
  const { from, to } = view.state.selection.main;
  const { first, last } = selectedLines();
  const lines: string[] = [];
  for (let n = first.number; n <= last.number; n++) lines.push(view.state.doc.line(n).text);
  const nextLines = transformBlockLines(lines, kind, level);
  const newText = nextLines.join("\n");
  if (newText === view.state.doc.sliceString(first.from, last.to)) {
    view.focus();
    return;
  }

  const starts: number[] = [];
  let at = first.from;
  for (const text of nextLines) {
    starts.push(at);
    at += text.length + 1;
  }
  const mapPosition = (pos: number): number => {
    if (pos > last.to) return starts[starts.length - 1] + nextLines[nextLines.length - 1].length;
    const line = view.state.doc.lineAt(pos);
    const index = line.number - first.number;
    const oldInfo = readBlockLine(line.text);
    const newInfo = readBlockLine(nextLines[index]);
    const oldContentStart = line.from + oldInfo.indent.length + oldInfo.prefix.length;
    const newContentStart = starts[index] + newInfo.indent.length + newInfo.prefix.length;
    if (pos <= oldContentStart) return newContentStart;
    return newContentStart + Math.min(pos - oldContentStart, newInfo.content.length);
  };
  view.dispatch({
    changes: { from: first.from, to: last.to, insert: newText },
    selection: { anchor: mapPosition(from), head: mapPosition(to) },
  });
  view.focus();
}

function toggleLinePrefix(prefix: string) {
  switch (prefix) {
    case "- ":
      setBlockKind("bullet");
      return;
    case "1. ":
      setBlockKind("ordered");
      return;
    case "- [ ] ":
      setBlockKind("task");
      return;
    case "> ":
      setBlockKind("quote");
      return;
  }
  setBlockKind("paragraph");
}

function setHeading(level: number) {
  if (level <= 0) setBlockKind("paragraph");
  else setBlockKind("heading", level);
}

/** toggleInline removes the markers around the caret, or wraps the selection. */
function toggleInline(marker: string, nodeName: string) {
  if (isReadOnly()) return;
  const { from, to } = view.state.selection.main;
  const head = view.state.selection.main.head;
  let node: any = syntaxTree(view.state).resolveInner(head, -1);
  let found: { from: number; to: number } | null = null;
  while (node) {
    if (node.name === nodeName) {
      found = { from: node.from, to: node.to };
      break;
    }
    node = node.parent;
  }
  if (found) {
    const text = view.state.sliceDoc(found.from, found.to);
    if (text.startsWith(marker) && text.endsWith(marker) && text.length >= marker.length * 2) {
      const innerFrom = found.from;
      const innerTo = found.to - marker.length;
      const anchor = Math.max(innerFrom, Math.min(innerTo, from >= found.from + marker.length ? from - marker.length : from));
      const headPos = Math.max(innerFrom, Math.min(innerTo, to >= found.from + marker.length ? to - marker.length : to));
      view.dispatch({
        changes: [
          { from: found.from, to: found.from + marker.length, insert: "" },
          { from: found.to - marker.length, to: found.to, insert: "" },
        ],
        selection: { anchor, head: headPos },
      });
      view.focus();
      return;
    }
  }
  wrapSelection(marker);
}

function buildFormattingKeymap() {
  return [
  { key: "Mod-b", run: () => (toggleInline("**", "StrongEmphasis"), true) },
  { key: "Mod-i", run: () => (toggleInline("*", "Emphasis"), true) },
  { key: "Mod-Shift-x", run: () => (toggleInline("~~", "Strikethrough"), true) },
  { key: "Mod-e", run: () => (toggleInline("`", "InlineCode"), true) },
  { key: "Mod-Shift-k", run: () => (wrapLink(), true) },
  { key: "Mod-1", run: () => (setHeading(1), true) },
  { key: "Mod-2", run: () => (setHeading(2), true) },
  { key: "Mod-3", run: () => (setHeading(3), true) },
  { key: "Mod-4", run: () => (setHeading(4), true) },
  { key: "Mod-5", run: () => (setHeading(5), true) },
  { key: "Mod-6", run: () => (setHeading(6), true) },
  { key: "Mod-0", run: () => (setHeading(0), true) },
  { key: "Mod-Shift-7", run: () => (toggleLinePrefix("1. "), true) },
  { key: "Mod-Shift-8", run: () => (toggleLinePrefix("- "), true) },
  { key: "Mod-Shift-9", run: () => (toggleLinePrefix("- [ ] "), true) },
  { key: "Mod-f", run: () => (openSearchPanel(view), true) },
  { key: "Escape", run: () => escapeEditor() },
  ];
}

function wrapLink() {
  if (isReadOnly()) return;
  const { from, to } = view.state.selection.main;
  const selected = view.state.sliceDoc(from, to) || "链接文字";
  const insert = `[${selected}](https://)`;
  view.dispatch({
    changes: { from, to, insert },
    selection: { anchor: from + selected.length + 3, head: from + selected.length + 11 },
  });
  view.focus();
}

function escapeEditor(): boolean {
  if (slashOpen) {
    closeSlash();
    return true;
  }
  const panel = document.querySelector(".cm-panel.cm-search");
  if (panel) {
    closeSearchPanel(view);
    return true;
  }
  return false;
}

async function handlePaste(event: ClipboardEvent) {
  if (isReadOnly() || !event.clipboardData) return;
  const data = event.clipboardData;
  const files = clipboardFiles(data);
  if (files.length) {
    event.preventDefault();
    await insertFiles(files);
    return;
  }
  const html = data.getData("text/html");
  if (html && html.trim()) {
    event.preventDefault();
    const markdownText = turndown.turndown(html).trim();
    if (markdownText) insertPastedMarkdown(markdownText);
    return;
  }
  const text = data.getData("text/plain");
  if (text) {
    // A plain Markdown table is inserted with a blank line after it, so
    // its page opens with the table rendered rather than as the active
    // source block. Other text keeps CodeMirror's own paste behavior.
    if (isMarkdownTable(text)) {
      event.preventDefault();
      insertText(text.trimEnd() + "\n", { block: true });
      return;
    }
    // CodeMirror handles plain text, but a URL pasted over a selection
    // becomes a Markdown link in Obsidian.
    const { from, to } = view.state.selection.main;
    const selected = view.state.sliceDoc(from, to);
    if (selected && /^https?:\/\/\S+$/.test(text.trim())) {
      event.preventDefault();
      insertText(`[${selected}](${text.trim()})`);
    }
  }
}

/** isMarkdownTable reports text that is one complete pipe table. */
function isMarkdownTable(text: string): boolean {
  const lines = text.replace(/\r\n/g, "\n").split("\n");
  return tableOnly(lines, parseFences(lines)) !== null;
}

/** insertPastedMarkdown gives a pasted table its own blank line, so the
 * caret starts outside the table and the table is rendered. */
function insertPastedMarkdown(markdown: string): void {
  if (isMarkdownTable(markdown)) {
    insertText(markdown.trimEnd() + "\n", { block: true });
    return;
  }
  insertText(markdown, { block: false });
}

async function insertFiles(files: File[]) {
  if (isReadOnly()) return;
  for (const file of files) {
    try {
      const base64 = await readBase64(file);
      if (isImageFile(file)) {
        const result = await bridge.call<AttachmentResult>(
          `${SERVICE}.InsertImageData`,
          docId,
          file.name || "图片.png",
          base64,
        );
        if (result?.text) insertText(result.text);
      } else {
        const result = await bridge.call<AttachmentResult>(
          `${SERVICE}.InsertAttachmentData`,
          docId,
          file.name || "附件",
          base64,
        );
        if (result?.text) insertText(result.text);
      }
    } catch (err) {
      toast(`插入失败：${String(err)}`);
    }
  }
}

function readBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const value = String(reader.result ?? "");
      const comma = value.indexOf(",");
      resolve(comma >= 0 ? value.slice(comma + 1) : value);
    };
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(file);
  });
}

function toast(message: string) {
  els.toast.textContent = message;
  els.toast.hidden = false;
  els.toast.classList.add("is-shown");
  window.clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => {
    els.toast.classList.remove("is-shown");
    window.setTimeout(() => {
      els.toast.hidden = true;
    }, 160);
  }, 1600);
}


// ---------------------------------------------------------------- toolbar

const toolbarIcons: Record<string, string> = {
  undo: '<path d="M9 14 4 9l5-5"/><path d="M4 9h10.5a5.5 5.5 0 0 1 0 11H11"/>',
  redo: '<path d="m15 14 5-5-5-5"/><path d="M20 9H9.5a5.5 5.5 0 0 0 0 11H13"/>',
  link: '<path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>',
  bullet: '<path d="M9 6h12M9 12h12M9 18h12"/><path d="M4 6h.01M4 12h.01M4 18h.01"/>',
  ordered: '<path d="M10 6h11M10 12h11M10 18h11"/><path d="M4 5h1v4M3.5 9H6"/><path d="M6 18H3.5c0-1 2.5-1.6 2.5-3s-2.5-1.2-2.5 0"/>',
  task: '<rect x="3" y="5" width="5" height="5" rx="1"/><path d="m4 7 1 1 2-2"/><path d="M11 6h10M11 12h10M11 18h10"/><rect x="3" y="13" width="5" height="5" rx="1"/>',
  quote: '<path d="M10 11H6a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h3a1 1 0 0 1 1 1v8a4 4 0 0 1-4 4"/><path d="M20 11h-4a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h3a1 1 0 0 1 1 1v8a4 4 0 0 1-4 4"/>',
  code: '<path d="m16 18 6-6-6-6M8 6l-6 6 6 6"/>',
  codeblock: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="m9 10-2 2 2 2M15 10l2 2-2 2"/>',
  table: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 10h18M9 4v16M15 4v16"/>',
  mermaid: '<rect x="3" y="3" width="6" height="6" rx="1"/><rect x="15" y="15" width="6" height="6" rx="1"/><path d="M9 6h3a3 3 0 0 1 3 3v6"/><path d="m15 9-3 3 3 3" transform="translate(0 -3)"/>',
  image: '<rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="9" cy="9" r="2"/><path d="m21 15-5-5L5 21"/>',
  attachment: '<path d="m21.44 11.05-9.19 9.19a6 6 0 0 1-8.49-8.49l8.57-8.57A4 4 0 1 1 18 8.84l-8.59 8.57a2 2 0 0 1-2.83-2.83l8.49-8.48"/>',
  search: '<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>',
  chevron: '<path d="m6 9 6 6 6-6"/>',
};

function toolbarIcon(name: string): string {
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${toolbarIcons[name] ?? ""}</svg>`;
}

function toolbarButton(
  key: string,
  title: string,
  content: string,
  run: () => void,
  extraClass = "",
): HTMLButtonElement {
  const button = document.createElement("button");
  button.type = "button";
  button.className = `toolbar-btn${extraClass ? " " + extraClass : ""}`;
  button.title = title;
  button.setAttribute("aria-label", title);
  button.innerHTML = content;
  button.addEventListener("click", (event) => {
    event.preventDefault();
    if (button.disabled) return;
    run();
    refreshToolbar();
  });
  toolbarButtons[key] = button;
  return button;
}

function toolbarSeparator(): HTMLElement {
  const sep = document.createElement("span");
  sep.className = "toolbar-sep";
  sep.setAttribute("aria-hidden", "true");
  return sep;
}

let blockButton: HTMLButtonElement | null = null;

function buildToolbar() {
  if (!els.toolbar) return;
  els.toolbar.replaceChildren();
  const group = (...nodes: Node[]) => {
    const wrap = document.createElement("div");
    wrap.className = "toolbar-group";
    for (const node of nodes) wrap.appendChild(node);
    els.toolbar.appendChild(wrap);
    return wrap;
  };

  group(
    toolbarButton("undo", "撤销（⌘Z）", toolbarIcon("undo"), () => {
      undoCommand(view);
      view.focus();
    }),
    toolbarButton("redo", "重做（⇧⌘Z）", toolbarIcon("redo"), () => {
      redoCommand(view);
      view.focus();
    }),
  );
  els.toolbar.appendChild(toolbarSeparator());

  blockButton = document.createElement("button");
  blockButton.type = "button";
  blockButton.id = "toolbar-block";
  blockButton.className = "toolbar-block";
  blockButton.title = "段落样式";
  blockButton.setAttribute("aria-label", "段落样式");
  blockLabel = document.createElement("span");
  blockLabel.textContent = "正文";
  blockButton.appendChild(blockLabel);
  blockButton.insertAdjacentHTML("beforeend", toolbarIcon("chevron"));
  blockButton.addEventListener("click", (event) => {
    event.preventDefault();
    toggleBlockMenu();
  });
  group(blockButton);
  els.toolbar.appendChild(toolbarSeparator());

  group(
    toolbarButton("bold", "加粗（⌘B）", "<span>B</span>", () => toggleInline("**", "StrongEmphasis"), "is-bold"),
    toolbarButton("italic", "斜体（⌘I）", "<span>I</span>", () => toggleInline("*", "Emphasis"), "is-italic"),
    toolbarButton("strike", "删除线（⇧⌘X）", "<span>S</span>", () => toggleInline("~~", "Strikethrough"), "is-strike"),
    toolbarButton("code", "行内代码（⌘E）", toolbarIcon("code"), () => toggleInline("`", "InlineCode")),
    toolbarButton("link", "链接（⇧⌘K）", toolbarIcon("link"), () => wrapLink()),
  );
  els.toolbar.appendChild(toolbarSeparator());

  group(
    toolbarButton("bullet", "无序列表", toolbarIcon("bullet"), () => setBlockKind("bullet")),
    toolbarButton("ordered", "有序列表", toolbarIcon("ordered"), () => setBlockKind("ordered")),
    toolbarButton("task", "任务列表", toolbarIcon("task"), () => setBlockKind("task")),
    toolbarButton("quote", "引用", toolbarIcon("quote"), () => setBlockKind("quote")),
  );
  els.toolbar.appendChild(toolbarSeparator());

  group(
    toolbarButton("codeblock", "代码块", toolbarIcon("codeblock"), () => {
      insertText("```\n\n```", { block: true });
    }),
    toolbarButton("table", "表格", toolbarIcon("table"), () => {
      insertText("| 列 1 | 列 2 |\n| --- | --- |\n|  |  |", { block: true });
    }),
    toolbarButton("mermaid", "Mermaid 图表", toolbarIcon("mermaid"), () => {
      insertText("```mermaid\nflowchart LR\n    A[开始] --> B[结束]\n```", { block: true });
    }),
  );
  els.toolbar.appendChild(toolbarSeparator());

  group(
    toolbarButton("image", "插入图片", toolbarIcon("image"), () => {
      void pickImage();
    }),
    toolbarButton("attachment", "插入附件", toolbarIcon("attachment"), () => {
      void pickAttachment();
    }),
  );
  els.toolbar.appendChild(toolbarSeparator());

  group(
    toolbarButton("search", "查找（⌘F）", toolbarIcon("search"), () => {
      openSearchPanel(view);
    }),
  );

  els.toolbar.addEventListener("mousedown", (event) => {
    const target = event.target as HTMLElement | null;
    if (target?.closest("button")) event.preventDefault();
  });
}

function scheduleToolbarRefresh() {
  window.clearTimeout(toolbarTimer);
  toolbarTimer = window.setTimeout(refreshToolbar, 40);
}

function setToolbarActive(key: string, active: boolean) {
  toolbarButtons[key]?.classList.toggle("is-active", active);
}

function refreshToolbar() {
  const editingKeys = [
    "undo", "redo", "bold", "italic", "strike", "code", "link",
    "bullet", "ordered", "task", "quote", "codeblock", "table", "mermaid",
    "image", "attachment",
  ];
  for (const key of editingKeys) {
    const button = toolbarButtons[key];
    if (button) button.disabled = !current || isReadOnly();
  }
  if (toolbarButtons.search) toolbarButtons.search.disabled = !current;
  if (blockButton) blockButton.disabled = !current || isReadOnly();
  if (!current) {
    if (blockLabel) blockLabel.textContent = "正文";
    for (const key of editingKeys) setToolbarActive(key, false);
    return;
  }
  const head = view.state.selection.main.head;
  const info = readBlockLine(view.state.doc.lineAt(head).text);
  if (blockLabel) {
    blockLabel.textContent = info.kind === "heading" ? `标题 ${info.level}` : "正文";
  }
  const { first, last } = selectedLines();
  const selected: { text: string }[] = [];
  for (let n = first.number; n <= last.number; n++) selected.push(view.state.doc.line(n));
  const nonBlank = selected.filter((line) => line.text.trim() !== "");
  const inspect = nonBlank.length > 0 ? nonBlank : selected;
  const allKind = (kind: BlockKind) => inspect.length > 0 && inspect.every((line) => readBlockLine(line.text).kind === kind);
  setToolbarActive("bold", hasNode(head, ["StrongEmphasis"]));
  setToolbarActive("italic", hasNode(head, ["Emphasis"]));
  setToolbarActive("strike", hasNode(head, ["Strikethrough"]));
  setToolbarActive("code", hasNode(head, ["InlineCode"]));
  setToolbarActive("link", hasNode(head, ["Link"]));
  setToolbarActive("bullet", allKind("bullet"));
  setToolbarActive("ordered", allKind("ordered"));
  setToolbarActive("task", allKind("task"));
  setToolbarActive("quote", allKind("quote"));
  setToolbarActive("codeblock", hasNode(head, ["FencedCode", "CodeBlock"]));
}

function closeBlockMenu() {
  blockMenu?.remove();
  blockMenu = null;
}

function toggleBlockMenu() {
  if (blockMenu) {
    closeBlockMenu();
    return;
  }
  openBlockMenu();
}

function openBlockMenu() {
  if (!blockButton || !current || isReadOnly()) return;
  const head = view.state.selection.main.head;
  const info = readBlockLine(view.state.doc.lineAt(head).text);
  const menu = document.createElement("div");
  menu.className = "toolbar-menu";
  const items: Array<{ label: string; kind: BlockKind; level: number }> = [
    { label: "正文", kind: "paragraph", level: 0 },
    { label: "标题 1", kind: "heading", level: 1 },
    { label: "标题 2", kind: "heading", level: 2 },
    { label: "标题 3", kind: "heading", level: 3 },
    { label: "标题 4", kind: "heading", level: 4 },
    { label: "标题 5", kind: "heading", level: 5 },
    { label: "标题 6", kind: "heading", level: 6 },
  ];
  for (const item of items) {
    const row = document.createElement("button");
    row.type = "button";
    row.className = "toolbar-menu-item";
    row.textContent = item.label;
    if (
      (item.kind === "paragraph" && info.kind === "paragraph") ||
      (item.kind === "heading" && info.kind === "heading" && info.level === item.level)
    ) {
      row.classList.add("is-active");
    }
    row.addEventListener("mousedown", (event) => event.preventDefault());
    row.addEventListener("click", () => {
      closeBlockMenu();
      setBlockKind(item.kind, item.level);
      refreshToolbar();
    });
    menu.appendChild(row);
  }
  const rect = blockButton.getBoundingClientRect();
  menu.style.left = `${Math.min(rect.left, window.innerWidth - 172)}px`;
  menu.style.top = `${rect.bottom + 4}px`;
  document.body.appendChild(menu);
  blockMenu = menu;
}

buildToolbar();

// ---------------------------------------------------------------- title

function commitTitle() {
  if (!current || isReadOnly()) return;
  const next = els.title.textContent?.replace(/\n/g, " ").trim() ?? "";
  if (!next) {
    setTitle(previousTitle);
    return;
  }
  if (next === previousTitle) return;
  view.focus();
  void bridge
    .call<RenameResult>(`${SERVICE}.Rename`, docId, next)
    .then((result) => {
      if (!result) return;
      docId = result.id;
      if (current) {
        current.id = result.id;
        current.title = result.title;
      }
      previousTitle = result.title;
      els.title.textContent = result.title;
      setStatus("已保存", "saved");
      toast("已重命名");
    })
    .catch((err) => {
      console.warn(err);
      setTitle(previousTitle);
      toast("重命名失败");
    });
}

els.title.addEventListener("keydown", (event) => {
  if (event.isComposing) return;
  if (event.key === "Enter") {
    event.preventDefault();
    els.title.blur();
    view.focus();
  } else if (event.key === "Escape") {
    event.preventDefault();
    setTitle(previousTitle);
    els.title.blur();
    view.focus();
  } else if (event.key === "ArrowDown") {
    event.preventDefault();
    view.focus();
  }
});
els.title.addEventListener("blur", commitTitle);
els.title.addEventListener("input", () => {
  els.title.classList.toggle("is-empty", (els.title.textContent ?? "").length === 0);
});
els.title.addEventListener("paste", (event) => {
  event.preventDefault();
  const text = event.clipboardData?.getData("text/plain") ?? "";
  document.execCommand("insertText", false, text.replace(/\n/g, " "));
});

// ---------------------------------------------------------------- outline

function renderOutline() {
  if (!current) return;
  if (els.outline.hidden) return;
  const lines = currentMarkdown.split("\n");
  const fences = parseFences(lines);
  const headings = outlineHeadings(lines, fences);
  const activeLine = view.state.doc.lineAt(view.state.selection.main.head).number - 1;
  let currentHeading: OutlineHeading | null = null;
  for (const heading of headings) {
    if (heading.line <= activeLine) currentHeading = heading;
  }
  els.outlineList.replaceChildren();
  if (headings.length === 0) {
    const empty = document.createElement("div");
    empty.className = "outline-empty";
    empty.textContent = "标题会显示在这里";
    els.outlineList.appendChild(empty);
    return;
  }
  for (const heading of headings) {
    const item = document.createElement("button");
    item.type = "button";
    item.className = `outline-item outline-level-${heading.level}`;
    item.textContent = heading.text;
    if (currentHeading === heading) item.classList.add("is-active");
    item.addEventListener("click", () => {
      const line = view.state.doc.line(Math.min(heading.line + 1, view.state.doc.lines));
      view.dispatch({ selection: { anchor: line.from } });
      revealPosition(line.from);
      view.focus();
    });
    els.outlineList.appendChild(item);
  }
}

function setOutlineOpen(open: boolean) {
  els.outline.hidden = !open;
  els.outlineResizer.hidden = !open;
  if (open) renderOutline();
}

els.outlineResizer.addEventListener("pointerdown", (event) => {
  if (event.button !== 0 || els.outline.hidden) return;
  event.preventDefault();
  outlineResizePointer = event.pointerId;
  outlineResizeStartX = event.clientX;
  outlineResizeStartWidth = els.outline.getBoundingClientRect().width;
  els.outlineResizer.setPointerCapture(event.pointerId);
  document.body.classList.add("is-resizing-outline");
});

els.outlineResizer.addEventListener("pointermove", (event) => {
  if (outlineResizePointer !== event.pointerId) return;
  event.preventDefault();
  applyOutlineWidth(outlineResizeStartWidth - (event.clientX - outlineResizeStartX));
});

function finishOutlineResize(event: PointerEvent) {
  if (outlineResizePointer !== event.pointerId) return;
  const pointerId = outlineResizePointer;
  outlineResizePointer = null;
  document.body.classList.remove("is-resizing-outline");
  if (els.outlineResizer.hasPointerCapture(pointerId)) {
    els.outlineResizer.releasePointerCapture(pointerId);
  }
  void bridge.call<void>(`${SERVICE}.SetOutlineWidth`, outlineWidth).catch((err) => {
    console.warn("outline width save failed", err);
  });
}

els.outlineResizer.addEventListener("pointerup", finishOutlineResize);
els.outlineResizer.addEventListener("pointercancel", finishOutlineResize);

// ---------------------------------------------------------------- slash menu

interface SlashItem {
  label: string;
  hint: string;
  run: () => void;
}

function slashCommands(): SlashItem[] {
  return [
    { label: "标题 1", hint: "一级标题", run: () => setHeading(1) },
    { label: "标题 2", hint: "二级标题", run: () => setHeading(2) },
    { label: "标题 3", hint: "三级标题", run: () => setHeading(3) },
    { label: "无序列表", hint: "圆点列表", run: () => toggleLinePrefix("- ") },
    { label: "有序列表", hint: "编号列表", run: () => toggleLinePrefix("1. ") },
    { label: "任务列表", hint: "可勾选的事项", run: () => toggleLinePrefix("- [ ] ") },
    { label: "引用", hint: "引用一段话", run: () => toggleLinePrefix("> ") },
    {
      label: "代码块",
      hint: "围栏代码块",
      run: () => insertText("```\n\n```", { block: true }),
    },
    {
      label: "表格",
      hint: "两列表格",
      run: () => insertText("| 列 1 | 列 2 |\n| --- | --- |\n|  |  |", { block: true }),
    },
    {
      label: "Mermaid 图表",
      hint: "流程图、时序图",
      run: () => insertText("```mermaid\nflowchart LR\n    A[开始] --> B[结束]\n```", { block: true }),
    },
    {
      label: "图片",
      hint: "从文件插入图片",
      run: () => void pickImage(),
    },
    {
      label: "附件",
      hint: "从文件插入附件",
      run: () => void pickAttachment(),
    },
    { label: "分割线", hint: "水平分割线", run: () => insertText("---", { block: true }) },
  ];
}

function slashElement(): HTMLDivElement | null {
  return document.querySelector<HTMLDivElement>("#slash-menu");
}

function openSlash(start: number, query: string) {
  closeBlockMenu();
  slashStart = start;
  slashItems = slashCommands().filter((item) => {
    if (!query) return true;
    const q = query.toLowerCase();
    return item.label.toLowerCase().includes(q) || item.hint.toLowerCase().includes(q);
  });
  slashIndex = 0;
  slashOpen = true;
  const menu = slashElement() ?? document.createElement("div");
  menu.id = "slash-menu";
  menu.className = "slash-menu";
  menu.replaceChildren();
  if (slashItems.length === 0) {
    const empty = document.createElement("div");
    empty.className = "slash-empty";
    empty.textContent = "没有匹配的命令";
    menu.appendChild(empty);
  } else {
    slashItems.forEach((item, index) => {
      const row = document.createElement("button");
      row.type = "button";
      row.className = "slash-item";
      if (index === slashIndex) row.classList.add("is-selected");
      const label = document.createElement("span");
      label.className = "slash-label";
      label.textContent = item.label;
      const hint = document.createElement("span");
      hint.className = "slash-hint";
      hint.textContent = item.hint;
      row.append(label, hint);
      row.addEventListener("mousedown", (e) => e.preventDefault());
      row.addEventListener("click", () => runSlash(index));
      menu.appendChild(row);
    });
  }
  if (!menu.isConnected) document.body.appendChild(menu);
  positionSlashMenu(menu, start);
}

function positionSlashMenu(menu: HTMLElement, pos: number) {
  const coords = view.coordsAtPos(pos);
  if (!coords) return;
  const scrollerRect = els.scroller.getBoundingClientRect();
  const left = coords.left;
  let top = coords.bottom + 4;
  if (top + 320 > scrollerRect.bottom) top = Math.max(scrollerRect.top + 8, coords.top - 320);
  menu.style.left = `${Math.min(left, window.innerWidth - 320)}px`;
  menu.style.top = `${top}px`;
}

function closeSlash() {
  slashOpen = false;
  slashElement()?.remove();
}

function runSlash(index: number) {
  const item = slashItems[index];
  closeSlash();
  if (!item) return;
  const { from } = view.state.selection.main;
  view.dispatch({
    changes: { from: slashStart, to: from, insert: "" },
    selection: { anchor: slashStart },
  });
  window.setTimeout(() => item.run(), 0);
}

function updateSlashMenu(update: { docChanged: boolean; selectionSet: boolean }) {
  if (isReadOnly()) return;
  if (!update.docChanged && !update.selectionSet) return;
  const { from } = view.state.selection.main;
  const before = view.state.sliceDoc(Math.max(0, from - 80), from);
  const match = /(^|\s)\/([^\s/]*)$/.exec(before);
  if (!match) {
    if (slashOpen) closeSlash();
    return;
  }
  const query = match[2] ?? "";
  const start = from - query.length - 1;
  openSlash(start, query);
}

window.addEventListener(
  "keydown",
  (event) => {
    if (event.key !== "Escape" || event.isComposing) return;
    if (slashOpen) {
      event.preventDefault();
      closeSlash();
    }
    if (blockMenu) {
      event.preventDefault();
      closeBlockMenu();
    }
  },
  true,
);

document.addEventListener(
  "mousedown",
  (event) => {
    const target = event.target as Element | null;
    if (slashOpen) {
      const menu = slashElement();
      if (menu && target && !menu.contains(target)) closeSlash();
    }
    if (blockMenu && target && !blockMenu.contains(target) && !target.closest("#toolbar-block")) {
      closeBlockMenu();
    }
  },
  true,
);

els.scroller.addEventListener(
  "scroll",
  () => {
    closeBlockMenu();
    queueScrollPosition();
  },
  { passive: true },
);

view.dom.addEventListener("keydown", (event) => {
  if (!slashOpen || event.isComposing) return;
  if (event.key === "ArrowDown" || event.key === "ArrowUp") {
    event.preventDefault();
    if (slashItems.length === 0) return;
    const dir = event.key === "ArrowDown" ? 1 : -1;
    slashIndex = (slashIndex + dir + slashItems.length) % slashItems.length;
    const menu = slashElement();
    if (menu) {
      Array.from(menu.querySelectorAll(".slash-item")).forEach((node, i) => {
        node.classList.toggle("is-selected", i === slashIndex);
      });
      menu.querySelectorAll(".slash-item")[slashIndex]?.scrollIntoView({ block: "nearest" });
    }
  } else if (event.key === "Enter") {
    event.preventDefault();
    runSlash(slashIndex);
  } else if (event.key === "Escape") {
    event.preventDefault();
    closeSlash();
  }
});

// ---------------------------------------------------------------- files

async function pickImage() {
  if (isReadOnly()) return;
  try {
    const result = await bridge.call<AttachmentResult>(`${SERVICE}.PickImage`, docId);
    if (result?.text) insertText(result.text);
  } catch (err) {
    toast(`插入图片失败：${String(err)}`);
  }
}

async function pickAttachment() {
  if (isReadOnly()) return;
  try {
    const result = await bridge.call<AttachmentResult>(`${SERVICE}.PickAttachment`, docId);
    if (result?.text) insertText(result.text);
  } catch (err) {
    toast(`插入附件失败：${String(err)}`);
  }
}

// ---------------------------------------------------------------- commands

function handleCommand(payload: CommandPayload) {
  if (payload.command !== "slash") {
    closeSlash();
    closeBlockMenu();
  }
  switch (payload.command) {
    case "search":
      openSearchPanel(view);
      break;
    case "close-search":
      closeSearchPanel(view);
      break;
    case "slash":
      if (isReadOnly()) return;
      openSlash(view.state.selection.main.from, "");
      break;
    case "preview":
      setPreviewMode(Boolean(payload.open));
      break;
    case "outline":
      setOutlineOpen(Boolean(payload.open));
      break;
    case "focus":
      view.focus();
      break;
    case "image":
      void pickImage();
      break;
    case "attachment":
      void pickAttachment();
      break;
    case "save":
      syncChanged();
      break;
  }
}

// ---------------------------------------------------------------- bridge

bridge.on(EVENT_OPEN, (payload) => {
  openDocument(payload as OpenPayload);
});
bridge.on(EVENT_STATUS, (payload) => {
  const status = payload as StatusPayload;
  if (!previewMode) setStatus(status.text, status.state);
  if (status.state === "saved") {
    dirty = false;
    window.clearTimeout(saveStateTimer);
  }
});
bridge.on(EVENT_SETTINGS, (payload) => {
  applySettings(payload as SettingsPayload);
});
bridge.on(EVENT_COMMAND, (payload) => {
  handleCommand(payload as CommandPayload);
});
bridge.on(EVENT_INSERT, (payload) => {
  const text = (payload as TextPayload).text;
  if (text) insertText(text);
});
bridge.on(EVENT_TITLE, (payload) => {
  const meta = payload as { id?: string; title: string };
  if (meta.id) {
    docId = meta.id;
    if (current) current.id = meta.id;
  }
  setTitle(meta.title);
});
bridge.on(EVENT_RELOAD, (payload) => {
  reloadDocument(payload as ReloadPayload);
});

window.addEventListener("pagehide", flushScrollPosition);
window.addEventListener("beforeunload", flushScrollPosition);

function timeNow(): string {
  return new Date().toLocaleTimeString("zh-CN", { hour12: false });
}

void bridge.call<void>(`${SERVICE}.Ready`).catch((err) => {
  console.warn("editor ready failed", err);
});

// Expose the editor for screenshots, tests and the web dev server.
(window as unknown as { __swiftEditor: unknown }).__swiftEditor = {
  view,
  open: openDocument,
  getMarkdown: () => view.state.doc.toString(),
};

// ---------------------------------------------------------------- dev mock

function createMockBridge(): MyGoBridge {
  const listeners = new Map<string, Set<(payload: unknown) => void>>();
  const emit = (name: string, payload: unknown) => {
    for (const fn of listeners.get(name) ?? []) fn(payload);
  };
  let title = "Obsidian 风格编辑器";
  let id = "mock/preview.md";
  const sample = `# 编辑器预览

这是一段正文，包含 **加粗**、*斜体*、~~删除线~~、\`行内代码\` 和 [链接](https://example.com)。Obsidian 的正文使用系统字体，行距舒适。

## 二级标题

> 这是一段引用文字。引用左侧有一条竖线。

- 无序列表项一
- 无序列表项二
  - 嵌套列表
- [ ] 待办事项
- [x] 已完成事项

1. 有序列表一
2. 有序列表二

\`\`\`go
package main

import "fmt"

func main() {
	fmt.Println("Hello, Obsidian")
}
\`\`\`

\`\`\`mermaid
flowchart LR
    A[开始] --> B{判断}
    B -->|是| C[结束]
\`\`\`

| 名称 | 说明 |
| --- | --- |
| 语燕 | 本地优先 |
| Obsidian | 知识库 |

---
`;
  const saved = localStorage.getItem("swift-editor-mock");
  const initial = saved ?? sample;
  window.setTimeout(() => {
    emit(EVENT_OPEN, {
      id,
      title,
      content: initial,
      readOnly: false,
      previewMode: false,
      note: false,
      fontSize: 16,
      lineHeight: 1.75,
      readableLineWidth: false,
      outlineOpen: false,
      outlineWidth: 240,
      savedAt: "",
      scrollTop: 0,
    } satisfies OpenPayload);
  }, 0);
  return {
    call: async <T>(method: string, ...args: unknown[]): Promise<T> => {
      if (method === `${SERVICE}.Changed`) {
        localStorage.setItem("swift-editor-mock", String(args[1] ?? ""));
        window.setTimeout(() => emit(EVENT_STATUS, { state: "saved", text: `已保存 ${timeNow()}` }), 250);
        return undefined as T;
      }
      if (method === `${SERVICE}.Rename`) {
        title = String(args[1] ?? title);
        id = id.replace(/[^/]+\.md$/, `${title}.md`);
        return { id, title } as T;
      }
      if (method === `${SERVICE}.Ready`) return undefined as T;
      if (method === `${SERVICE}.CopyText`) {
        await navigator.clipboard.writeText(String(args[0] ?? ""));
        return undefined as T;
      }
      return undefined as T;
    },
    on: (name, listener) => {
      let set = listeners.get(name);
      if (!set) {
        set = new Set();
        listeners.set(name, set);
      }
      set.add(listener);
      return () => set?.delete(listener);
    },
    platform: "web",
  };
}

