import assert from "node:assert/strict";
import test from "node:test";

import { codeFenceDeletionRanges } from "../src/markdown.ts";

test("backspacing an opening fence also removes its closing fence", () => {
  const text = "```\ncode\n```\nafter";
  assert.deepEqual(codeFenceDeletionRanges(text, 3, 3, false), [
    { from: 0, to: 3 },
    { from: 9, to: 12 },
  ]);
});

test("deleting an opening fence and code body leaves no orphaned closing fence", () => {
  const text = "```\n222\n```\n333";
  assert.deepEqual(codeFenceDeletionRanges(text, 0, 7, false), [
    { from: 0, to: 7 },
    { from: 8, to: 11 },
  ]);
});

test("deleting a closing fence also removes its opening fence", () => {
  const text = "```\ncode\n```\nafter";
  assert.deepEqual(codeFenceDeletionRanges(text, 9, 9, true), [
    { from: 0, to: 3 },
    { from: 9, to: 12 },
  ]);
});

test("deleting code text keeps its fences", () => {
  const text = "```\ncode\n```\nafter";
  assert.equal(codeFenceDeletionRanges(text, 5, 8, false), null);
});

test("deleting a complete fenced block uses the normal deletion", () => {
  const text = "```\ncode\n```\nafter";
  assert.equal(codeFenceDeletionRanges(text, 0, 12, false), null);
});

test("an unclosed fence is left alone so it can be removed", () => {
  const text = "```\ncode\nafter";
  assert.equal(codeFenceDeletionRanges(text, 3, 3, false), null);
});
