/** inline.ts decides where the inline Markdown of one line begins and ends.
 * A backslash escapes ASCII punctuation, and emphasis runs only pair when
 * they are flanking. Only `*` delimits emphasis: notes are full of
 * identifiers like `lake_autovacuum_stale_partition_threshold`, so `_` is
 * always plain text and never needs escaping. */

const SPACE = /\s/;
const ESCAPABLE = /^[!-/:-@[-`{-~]$/;
const NO_ESCAPES = new Uint8Array(0);

/** isEscapable reports whether a backslash escapes ch. */
export function isEscapable(ch: string): boolean {
  return ESCAPABLE.test(ch);
}

/** escapeMask marks, for one line, the characters that a backslash escapes.
 * An escaped character is literal text, so it can neither open nor close
 * an emphasis run. Lines without a backslash share one empty mask. */
export function escapeMask(text: string): Uint8Array {
  if (!text.includes("\\")) return NO_ESCAPES;
  const mask = new Uint8Array(text.length);
  for (let i = 0; i + 1 < text.length; i++) {
    if (text[i] !== "\\" || !isEscapable(text[i + 1])) continue;
    mask[i + 1] = 1;
    // A backslash escapes the character right after it, never itself
    // again: `\\_` is a literal backslash followed by a live underscore.
    i++;
  }
  return mask;
}

/** delimiterRole reports whether the delimiter run at index can open or
 * close emphasis.
 *
 * Only `*` delimits emphasis, so underscores in identifiers stay plain
 * text. A run stops at spaces, because Chinese notes write
 * `**注意：**后面` and expect the label in bold. */
export function delimiterRole(
  text: string,
  index: number,
): { open: boolean; close: boolean } {
  if (text[index] !== "*") return { open: false, close: false };
  const before = index > 0 ? text[index - 1] : "";
  // The delimiters of one run act together, so the character after the
  // whole run decides the role.
  let after = index + 1;
  while (after < text.length && text[after] === "*") after++;
  const next = after < text.length ? text[after] : "";
  return {
    open: !(next === "" || SPACE.test(next)),
    close: !(before === "" || SPACE.test(before)),
  };
}

/** findRun returns where the first run of at least run mark characters
 * starts, or -1. Code span delimiters use this: a backslash is ordinary
 * text inside a span, so escapes do not hide a closing backtick. */
export function findRun(
  text: string,
  mark: string,
  start: number,
  end: number,
  run: number,
): number {
  for (let i = start; i + run <= end; i++) {
    if (text[i] !== mark) continue;
    let length = 0;
    while (i + length < end && text[i + length] === mark) length++;
    if (length >= run) return i;
    // A shorter run cannot hold this mark; skip what it has.
    i += length - 1;
  }
  return -1;
}

/** findUnescaped returns where the first run of at least run mark characters
 * starts, skipping runs a backslash escapes, or -1. */
export function findUnescaped(
  text: string,
  mark: string,
  start: number,
  end: number,
  run: number,
  escaped: Uint8Array,
): number {
  for (let i = start; i + run <= end; i++) {
    if (text[i] !== mark || escaped[i]) continue;
    let length = 0;
    while (i + length < end && text[i + length] === mark && !escaped[i + length]) length++;
    if (length >= run) return i;
    // A shorter run cannot hold this mark; skip what it has.
    i += length - 1;
  }
  return -1;
}

/** findEmphasisClose returns where a run of mark characters closes the run
 * that opened before start, or -1. Escaped characters and runs that cannot
 * close are skipped. */
export function findEmphasisClose(
  text: string,
  mark: string,
  start: number,
  end: number,
  run: number,
  escaped: Uint8Array,
): number {
  for (let i = start; i + run <= end; ) {
    const close = findUnescaped(text, mark, i, end, run, escaped);
    if (close < 0) return -1;
    if (delimiterRole(text, close).close) return close;
    i = close + 1;
  }
  return -1;
}
