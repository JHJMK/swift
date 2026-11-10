import assert from "node:assert/strict";
import test from "node:test";

import { parseOrderedLine, renumberOrderedFrom } from "../src/lists.ts";

test("renumbers an item and the items after it", () => {
  assert.deepEqual(
    renumberOrderedFrom(
      ["1. 一", "2. 二", "3. 三", "4. 四", "5. 五"],
      2,
      1,
    ),
    ["1. 一", "2. 二", "1. 三", "2. 四", "3. 五"],
  );
});

test("keeps a parenthesized delimiter and stops at a nested item", () => {
  assert.deepEqual(
    renumberOrderedFrom(
      ["3) three", "4) four", "  9) nested", "5) five"],
      0,
      7,
    ),
    ["7) three", "8) four", "  9) nested", "5) five"],
  );
});

test("parses indentation and content", () => {
  assert.deepEqual(parseOrderedLine("  12. hello"), {
    indent: "  ",
    number: 12,
    delimiter: ".",
    gap: " ",
    content: "hello",
  });
  assert.equal(parseOrderedLine("paragraph"), null);
});

import { transformBlockLines } from "../src/lists.ts";

test("turns every selected line into an ordered list", () => {
  assert.deepEqual(
    transformBlockLines(["第一行", "- 第二行", "3. 第三行"], "ordered"),
    ["1. 第一行", "2. 第二行", "3. 第三行"],
  );
});

test("a second press removes the ordered list from every line", () => {
  assert.deepEqual(
    transformBlockLines(["1. 第一行", "2. 第二行", "5. 第三行"], "ordered"),
    ["第一行", "第二行", "第三行"],
  );
});

test("blank lines stay blank when a selection gets a block kind", () => {
  assert.deepEqual(
    transformBlockLines(["第一行", "", "第二行"], "bullet"),
    ["- 第一行", "", "- 第二行"],
  );
});
