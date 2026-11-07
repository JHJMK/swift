import { WidgetType } from "@codemirror/view";
import type { TableBlock } from "./markdown";

export interface WidgetContext {
  docId: string;
  assetUrl(ref: string): string;
  copy(text: string): void;
  toggleTask(pos: number): void;
  enter(pos: number): void;
}

function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  className?: string,
  text?: string,
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) => {
    switch (c) {
      case "&":
        return "&amp;";
      case "<":
        return "&lt;";
      case ">":
        return "&gt;";
      case '"':
        return "&quot;";
      default:
        return "&#39;";
    }
  });
}

function restoreInlineTags(html: string): string {
  return html.replace(/&lt;(\/?)(u|strong|b|em|i|del|s|strike|ins)&gt;/gi, (_tag, closing: string, name: string) => {
    const canonical =
      name === "b" ? "strong" :
      name === "i" ? "em" :
      name === "s" || name === "strike" ? "del" :
      name === "ins" ? "u" :
      name;
    return `<${closing}${canonical}>`;
  }).replace(/&lt;br\s*\/?&gt;/gi, "<br>");
}

/** inlineHtml renders the small amount of inline Markdown a table cell uses. */
export function inlineHtml(text: string): string {
  let out = restoreInlineTags(escapeHtml(text));
  out = out.replace(/`([^`]+)`/g, "<code>$1</code>");
  out = out.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  out = out.replace(/__([^_]+)__/g, "<strong>$1</strong>");
  out = out.replace(/(^|[^*])\*([^*]+)\*/g, "$1<em>$2</em>");
  out = out.replace(/~~([^~]+)~~/g, "<del>$1</del>");
  out = out.replace(
    /\[([^\]]+)\]\(([^)]+)\)/g,
    '<a href="$2" target="_blank" rel="noreferrer">$1</a>',
  );
  return out;
}

/** LineBreakWidget shows a DOCX line break without exposing its HTML tag. */
export class LineBreakWidget extends WidgetType {
  toDOM(): HTMLElement {
    return el("br", "swift-line-break");
  }

  ignoreEvent(): boolean {
    return true;
  }
}

/** ListMarkerWidget draws the bullet or number Obsidian shows for a list. */
export class ListMarkerWidget extends WidgetType {
  constructor(
    readonly kind: "bullet" | "number",
    readonly text: string,
    readonly depth: number,
  ) {
    super();
  }

  eq(other: ListMarkerWidget): boolean {
    return other.kind === this.kind && other.text === this.text && other.depth === this.depth;
  }

  toDOM(): HTMLElement {
    const span = el("span", `swift-list-marker swift-list-${this.kind}`);
    span.textContent = this.kind === "number" ? this.text : "";
    span.setAttribute("aria-hidden", "true");
    if (this.kind === "number") span.style.minWidth = `${this.text.length + 0.5}ch`;
    return span;
  }

  ignoreEvent(): boolean {
    return true;
  }
}

/** TaskWidget is the clickable checkbox of a task list item. */
export class TaskWidget extends WidgetType {
  constructor(
    readonly checked: boolean,
    readonly pos: number,
    readonly ctx: WidgetContext,
  ) {
    super();
  }

  eq(other: TaskWidget): boolean {
    return other.checked === this.checked && other.pos === this.pos && other.ctx.docId === this.ctx.docId;
  }

  toDOM(): HTMLElement {
    const wrap = el("span", "swift-task");
    const box = el("button", `swift-checkbox${this.checked ? " is-checked" : ""}`);
    box.type = "button";
    box.setAttribute("role", "checkbox");
    box.setAttribute("aria-checked", String(this.checked));
    box.title = this.checked ? "标记为未完成" : "标记为完成";
    box.addEventListener("mousedown", (e) => e.preventDefault());
    box.addEventListener("click", (e) => {
      e.preventDefault();
      e.stopPropagation();
      this.ctx.toggleTask(this.pos);
    });
    wrap.appendChild(box);
    return wrap;
  }

  ignoreEvent(): boolean {
    return true;
  }
}

/** CodeFlairWidget is the language label and copy button at a block's top right. */
export class CodeFlairWidget extends WidgetType {
  constructor(
    readonly lang: string,
    readonly title: string,
    readonly code: string,
    readonly ctx: WidgetContext,
  ) {
    super();
  }

  eq(other: CodeFlairWidget): boolean {
    return other.lang === this.lang && other.title === this.title && other.code === this.code;
  }

  toDOM(): HTMLElement {
    const wrap = el("span", "code-block-flair");
    if (this.title || this.lang) {
      const label = el("span", "code-block-lang", this.title || this.lang);
      if (this.title && this.lang) label.title = this.lang;
      wrap.appendChild(label);
    }
    const copy = el("button", "code-block-copy");
    copy.type = "button";
    copy.title = "复制代码";
    copy.setAttribute("aria-label", "复制代码");
    copy.innerHTML =
      '<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/></svg>';
    copy.addEventListener("mousedown", (e) => e.preventDefault());
    copy.addEventListener("click", (e) => {
      e.preventDefault();
      e.stopPropagation();
      this.ctx.copy(this.code);
      copy.classList.add("is-copied");
      window.setTimeout(() => copy.classList.remove("is-copied"), 900);
    });
    wrap.appendChild(copy);
    return wrap;
  }

  ignoreEvent(): boolean {
    return true;
  }
}

/** ImageWidget shows a Markdown image that is not being edited. */
export class ImageWidget extends WidgetType {
  constructor(
    readonly src: string,
    readonly alt: string,
    readonly pos: number,
    readonly block: boolean,
    readonly ctx: WidgetContext,
  ) {
    super();
  }

  eq(other: ImageWidget): boolean {
    return other.src === this.src && other.alt === this.alt && other.block === this.block;
  }

  toDOM(): HTMLElement {
    const wrap = el("span", this.block ? "swift-image-block" : "swift-image");
    const img = el("img", "swift-image-el");
    img.src = this.src;
    img.alt = this.alt;
    img.loading = "lazy";
    img.title = this.alt ? `${this.alt}（点击编辑）` : "点击编辑图片";
    img.addEventListener("error", () => {
      wrap.classList.add("is-missing");
      wrap.setAttribute("title", "图片无法加载");
    });
    img.addEventListener("mousedown", (e) => e.preventDefault());
    img.addEventListener("click", (e) => {
      e.preventDefault();
      e.stopPropagation();
      this.ctx.enter(this.pos);
    });
    wrap.appendChild(img);
    return wrap;
  }

  ignoreEvent(): boolean {
    return true;
  }
}

/** MermaidWidget shows a rendered diagram; clicking edits its source. */
export class MermaidWidget extends WidgetType {
  constructor(
    readonly svg: string,
    readonly source: string,
    readonly pos: number,
    readonly ctx: WidgetContext,
  ) {
    super();
  }

  eq(other: MermaidWidget): boolean {
    return other.svg === this.svg && other.pos === this.pos;
  }

  toDOM(): HTMLElement {
    const wrap = el("div", "swift-mermaid");
    const body = el("div", "swift-mermaid-body");
    body.innerHTML = this.svg;
    body.addEventListener("mousedown", (e) => e.preventDefault());
    body.addEventListener("click", (e) => {
      e.preventDefault();
      e.stopPropagation();
      this.ctx.enter(this.pos);
    });
    wrap.appendChild(body);
    const hint = el("div", "swift-embed-hint", "Mermaid · 点击编辑源码");
    wrap.appendChild(hint);
    return wrap;
  }

  ignoreEvent(): boolean {
    return true;
  }
}

/** TableWidget shows a pipe table in its rendered form. */
export class TableWidget extends WidgetType {
  constructor(
    readonly table: TableBlock,
    readonly pos: number,
    readonly ctx: WidgetContext,
  ) {
    super();
  }

  eq(other: TableWidget): boolean {
    return other.pos === this.pos && JSON.stringify(other.table) === JSON.stringify(this.table);
  }

  toDOM(): HTMLElement {
    const wrap = el("div", "swift-table-wrap");
    const table = el("table", "swift-table");
    const thead = el("thead");
    const headRow = el("tr");
    this.table.header.forEach((cell, i) => {
      const th = el("th");
      th.innerHTML = inlineHtml(cell);
      const align = this.table.align[i];
      if (align && align !== "default") th.style.textAlign = align;
      headRow.appendChild(th);
    });
    thead.appendChild(headRow);
    table.appendChild(thead);
    const tbody = el("tbody");
    for (const row of this.table.rows) {
      const tr = el("tr");
      this.table.header.forEach((_, i) => {
        const td = el("td");
        td.innerHTML = inlineHtml(row[i] ?? "");
        const align = this.table.align[i];
        if (align && align !== "default") td.style.textAlign = align;
        tr.appendChild(td);
      });
      tbody.appendChild(tr);
    }
    table.appendChild(tbody);
    wrap.appendChild(table);
    wrap.addEventListener("mousedown", (e) => e.preventDefault());
    wrap.addEventListener("click", (e) => {
      e.preventDefault();
      e.stopPropagation();
      this.ctx.enter(this.pos);
    });
    return wrap;
  }

  ignoreEvent(): boolean {
    return true;
  }
}

/** HrWidget renders a horizontal rule. */
export class HrWidget extends WidgetType {
  constructor(
    readonly pos: number,
    readonly ctx: WidgetContext,
  ) {
    super();
  }

  eq(other: HrWidget): boolean {
    return other.pos === this.pos;
  }

  toDOM(): HTMLElement {
    const wrap = el("div", "swift-hr");
    const hr = el("hr");
    wrap.appendChild(hr);
    wrap.addEventListener("mousedown", (e) => e.preventDefault());
    wrap.addEventListener("click", (e) => {
      e.preventDefault();
      e.stopPropagation();
      this.ctx.enter(this.pos);
    });
    return wrap;
  }

  ignoreEvent(): boolean {
    return true;
  }
}
