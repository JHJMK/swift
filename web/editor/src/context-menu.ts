/** context-menu.ts draws the document's own right-click menu in the page.
 * The system menu of the web view is not made of the app's parts, so the
 * editor shows its own, as it shows the slash menu, and cancels the system
 * one. */

export interface ContextMenuAction {
  label: string;
  /** SVG markup of a stroked 24×24 icon. */
  icon?: string;
  shortcut?: string;
  disabled?: boolean;
  run(): void;
}

export interface ContextMenuSeparator {
  separator: true;
}

export type ContextMenuEntry = ContextMenuAction | ContextMenuSeparator;

/** SEPARATOR draws a rule between groups of actions. */
export const SEPARATOR: ContextMenuSeparator = { separator: true };

let menu: HTMLDivElement | null = null;
let buttons: HTMLButtonElement[] = [];

/** showContextMenu opens the menu at a position of the page. */
export function showContextMenu(x: number, y: number, entries: ContextMenuEntry[]): void {
  closeContextMenu();
  const el = document.createElement("div");
  el.className = "editor-menu";
  el.setAttribute("role", "menu");
  el.style.left = `${x}px`;
  el.style.top = `${y}px`;
  buttons = [];
  for (const entry of entries) {
    if ("separator" in entry) {
      const line = document.createElement("div");
      line.className = "editor-menu-sep";
      line.setAttribute("role", "separator");
      el.appendChild(line);
      continue;
    }
    const item = document.createElement("button");
    item.type = "button";
    item.className = "editor-menu-item";
    item.setAttribute("role", "menuitem");
    item.disabled = Boolean(entry.disabled);
    if (entry.icon) {
      const icon = document.createElement("span");
      icon.className = "editor-menu-icon";
      icon.innerHTML = entry.icon;
      item.appendChild(icon);
    }
    const label = document.createElement("span");
    label.className = "editor-menu-label";
    label.textContent = entry.label;
    item.appendChild(label);
    if (entry.shortcut) {
      const shortcut = document.createElement("span");
      shortcut.className = "editor-menu-shortcut";
      shortcut.textContent = entry.shortcut;
      item.appendChild(shortcut);
    }
    // Keep the editor's selection, and its focus, while the menu is up:
    // copy and cut act on it once one of them is chosen.
    item.addEventListener("mousedown", (event) => event.preventDefault());
    item.addEventListener("click", () => {
      closeContextMenu();
      entry.run();
    });
    el.appendChild(item);
    buttons.push(item);
  }
  document.body.appendChild(el);
  // Keep the menu on the page, next to the pointer rather than over the
  // edge it was opened at.
  const margin = 8;
  const rect = el.getBoundingClientRect();
  const left = Math.min(x, window.innerWidth - rect.width - margin);
  const top = Math.min(y, window.innerHeight - rect.height - margin);
  el.style.left = `${Math.max(margin, left)}px`;
  el.style.top = `${Math.max(margin, top)}px`;
  menu = el;
  window.addEventListener("pointerdown", onPointerDown, true);
  window.addEventListener("keydown", onKeyDown, true);
  window.addEventListener("blur", closeContextMenu);
  window.addEventListener("resize", closeContextMenu);
  window.addEventListener("scroll", closeContextMenu, true);
}

/** closeContextMenu hides the menu and drops its listeners. */
export function closeContextMenu(): void {
  if (!menu) return;
  menu.remove();
  menu = null;
  buttons = [];
  window.removeEventListener("pointerdown", onPointerDown, true);
  window.removeEventListener("keydown", onKeyDown, true);
  window.removeEventListener("blur", closeContextMenu);
  window.removeEventListener("resize", closeContextMenu);
  window.removeEventListener("scroll", closeContextMenu, true);
}

/** moveFocus puts the keyboard on an enabled action. */
function moveFocus(step: number): void {
  const current = buttons.indexOf(document.activeElement as HTMLButtonElement);
  for (let n = 1; n <= buttons.length; n++) {
    const index = current + step * n;
    if (index < 0 || index >= buttons.length) break;
    const next = buttons[index];
    if (!next.disabled) {
      next.focus({ preventScroll: true });
      return;
    }
  }
}

/** moveEdge puts the keyboard on the first or the last action. */
function moveEdge(last: boolean): void {
  const order = last ? [...buttons].reverse() : buttons;
  order.find((button) => !button.disabled)?.focus({ preventScroll: true });
}

function onPointerDown(event: PointerEvent): void {
  if (menu && event.target instanceof Node && menu.contains(event.target)) return;
  closeContextMenu();
}

function onKeyDown(event: KeyboardEvent): void {
  if (!menu) return;
  switch (event.key) {
    case "Escape":
      event.preventDefault();
      event.stopPropagation();
      closeContextMenu();
      break;
    case "ArrowDown":
      event.preventDefault();
      moveFocus(1);
      break;
    case "ArrowUp":
      event.preventDefault();
      moveFocus(-1);
      break;
    case "Home":
      event.preventDefault();
      moveEdge(false);
      break;
    case "End":
      event.preventDefault();
      moveEdge(true);
      break;
    case "Tab":
      closeContextMenu();
      break;
  }
}
