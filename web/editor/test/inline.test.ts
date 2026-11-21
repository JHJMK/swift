import assert from "node:assert/strict";
import test from "node:test";

import {
  bareUrlAt,
  delimiterRole,
  escapeMask,
  findEmphasisClose,
  findRun,
  findUnescaped,
  isEscapable,
} from "../src/inline.ts";

test("escaped underscores stay literal text", () => {
  const line = "vacuum / vacuum\\_full / delete\\_txn\\_log 共用的线程池";
  const escaped = escapeMask(line);
  for (let i = 0; i < line.length; i++) {
    if (line[i] !== "_") continue;
    // Every underscore is escaped, so none of them is a delimiter the
    // editor may pair.
    assert.equal(escaped[i], 1, `escaped at ${i}`);
    assert.equal(findEmphasisClose(line, "_", i + 1, line.length, 1, escaped), -1);
  }
});

test("underscores inside words never emphasize", () => {
  const line = "lake_autovacuum_parallel_partitions 8";
  const escaped = escapeMask(line);
  for (let i = 0; i < line.length; i++) {
    if (line[i] !== "_") continue;
    assert.equal(delimiterRole(line, i).open, false);
    assert.equal(delimiterRole(line, i).close, false);
    assert.equal(findEmphasisClose(line, "_", i + 1, line.length, 1, escaped), -1);
  }
});

test("asterisks still emphasize inside a word", () => {
  const line = "a*b*c";
  const escaped = escapeMask(line);
  assert.equal(delimiterRole(line, 1).open, true);
  assert.equal(findEmphasisClose(line, "*", 2, line.length, 1, escaped), 3);
});

test("underscores never delimit emphasis, even around a word", () => {
  const line = "_斜体_ and __加粗__";
  for (let i = 0; i < line.length; i++) {
    if (line[i] !== "_") continue;
    assert.equal(delimiterRole(line, i).open, false);
    assert.equal(delimiterRole(line, i).close, false);
  }
});

test("asterisk emphasis survives Chinese punctuation, as in **注意：**后面", () => {
  const line = "**注意：**后面";
  const escaped = escapeMask(line);
  assert.equal(delimiterRole(line, 0).open, true);
  assert.equal(findEmphasisClose(line, "*", 2, line.length, 2, escaped), 5);
  assert.equal(delimiterRole(line, 5).close, true);
});

test("spaces stop asterisk emphasis", () => {
  assert.equal(delimiterRole("* 前面有空格", 0).open, false);
  const line = "2 * 3 * 4";
  assert.equal(delimiterRole(line, 2).open, false);
  assert.equal(findEmphasisClose(line, "*", 3, line.length, 1, escapeMask(line)), -1);
});

test("a delimiter run cannot open or close next to a space", () => {
  const open = "* 有空格";
  assert.equal(delimiterRole(open, 0).open, false);
  const close = "*斜体 *";
  const escaped = escapeMask(close);
  assert.equal(findEmphasisClose(close, "*", 1, close.length, 1, escaped), -1);
});

test("an escaped backslash leaves the next delimiter free", () => {
  const line = "\\\\*x*";
  const escaped = escapeMask(line);
  assert.equal(escaped[1], 1);
  assert.equal(escaped[2], 0);
  assert.equal(findEmphasisClose(line, "*", 3, line.length, 1, escaped), 4);
});

test("a code span closes on a run as long as its own", () => {
  const line = "``使用 ` 反引号 ` 的代码`` 结束";
  assert.equal(findRun(line, "`", 2, line.length, 2), line.lastIndexOf("``"));
  assert.equal(findRun(line, "`", 2, line.length, 1), line.indexOf("`", 2));
  assert.equal(findRun("`a`", "`", 1, 3, 1), 2);
});

test("escaped runs are skipped when looking for a close", () => {
  const line = "~~a \\~~ b~~";
  const escaped = escapeMask(line);
  assert.equal(findUnescaped(line, "~", 2, line.length, 2, escaped), line.lastIndexOf("~~"));
});

test("a line without backslashes needs no escape mask", () => {
  assert.equal(escapeMask("lake_autovacuum 8 *不是斜体*").length, 0);
  assert.equal(escapeMask("a \\_b")[3], 1);
});

test("only ASCII punctuation can be escaped", () => {
  assert.equal(isEscapable("!"), true);
  assert.equal(isEscapable("\\"), true);
  assert.equal(isEscapable("。"), false);
  assert.equal(isEscapable("5"), false);
});

test("bare URL keeps sentence punctuation out", () => {
  const line = "见 https://leetcode.cn/problems/remove-element/description/。";
  const at = line.indexOf("https");
  assert.equal(
    bareUrlAt(line, at),
    "https://leetcode.cn/problems/remove-element/description/",
  );
});

test("bare URL drops an unmatched closing parenthesis", () => {
  assert.equal(bareUrlAt("(https://example.com)", 1), "https://example.com");
  assert.equal(
    bareUrlAt("https://en.wikipedia.org/wiki/Foo_(bar)", 0),
    "https://en.wikipedia.org/wiki/Foo_(bar)",
  );
});

test("bare URL needs a host", () => {
  assert.equal(bareUrlAt("https://", 0), "");
  assert.equal(bareUrlAt("http://a", 0), "http://a");
  assert.equal(bareUrlAt("https://example.com.", 0), "https://example.com");
});
