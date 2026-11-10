import assert from "node:assert/strict";
import test from "node:test";

import { parseFences, parseTables, tableOnly } from "../src/markdown.ts";

test("parses a long table without backtracking over every row", () => {
  const lines = ["| Metric | Task | Value | Unit |", "|---:|---:|---:|---:|"];
  for (let i = 0; i < 500; i++) {
    lines.push(`| metric ${i} | task | ${i / 10} | ms |`);
  }
  lines.push("");
  const tables = parseTables(lines, parseFences(lines));
  assert.equal(tables.length, 1);
  assert.equal(tables[0].header.length, 4);
  assert.equal(tables[0].rows.length, 500);
  assert.deepEqual(tables[0].align, ["right", "right", "right", "right"]);
});

test("opens a table-only document after the table", () => {
  const lines = ["| A | B |", "|---|---|", "| 1 | 2 |", ""];
  assert.ok(tableOnly(lines, parseFences(lines)));
  assert.equal(tableOnly(["text", ...lines], parseFences(["text", ...lines])), null);
});

test("accepts delimiter cells with colons and spaces", () => {
  const lines = ["| A | B | C |", "| :--- | :---: | ---: |", "| 1 | 2 | 3 |"];
  const tables = parseTables(lines, parseFences(lines));
  assert.equal(tables.length, 1);
  assert.deepEqual(tables[0].align, ["left", "center", "right"]);
});
