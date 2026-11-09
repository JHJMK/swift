import assert from "node:assert/strict";
import test from "node:test";

import {
  installDiagnostics,
  type DiagnosticConsole,
  type DiagnosticTarget,
} from "../src/diagnostics.ts";
import type { MyGoBridge } from "../src/types.ts";

interface Call {
  method: string;
  args: unknown[];
}

function fakeBridge(calls: Call[]): Pick<MyGoBridge, "call"> {
  return {
    call: async <T>(method: string, ...args: unknown[]): Promise<T> => {
      calls.push({ method, args });
      return undefined as T;
    },
  };
}

function fakeTarget() {
  const listeners = new Map<string, (event: any) => void>();
  const target: DiagnosticTarget = {
    addEventListener(type, listener) {
      listeners.set(type, listener);
    },
  };
  return {
    target,
    dispatch(type: string, event: unknown) {
      listeners.get(type)?.(event);
    },
  };
}

function fakeConsole(calls: unknown[][]): DiagnosticConsole {
  return {
    warn(...data: unknown[]) {
      calls.push(["warn", ...data]);
    },
    error(...data: unknown[]) {
      calls.push(["error", ...data]);
    },
  };
}

test("reports console warnings and keeps the native console output", () => {
  const calls: Call[] = [];
  const output: unknown[][] = [];
  const consoleTarget = fakeConsole(output);
  const target = fakeTarget();
  installDiagnostics(fakeBridge(calls), "SwiftEditor", target.target, consoleTarget);

  consoleTarget.warn("warning", { code: 1 });

  assert.deepEqual(calls, [
    {
      method: "SwiftEditor.ReportLog",
      args: ["warn", 'warning {"code":1}', ""],
    },
  ]);
  assert.deepEqual(output, [["warn", "warning", { code: 1 }]]);
});

test("reports console errors with their stack", () => {
  const calls: Call[] = [];
  const output: unknown[][] = [];
  const consoleTarget = fakeConsole(output);
  const target = fakeTarget();
  installDiagnostics(fakeBridge(calls), "SwiftEditor", target.target, consoleTarget);

  consoleTarget.error(new Error("broken"));

  assert.equal(calls.length, 1);
  assert.equal(calls[0]?.method, "SwiftEditor.ReportLog");
  assert.equal(calls[0]?.args[0], "error");
  assert.match(String(calls[0]?.args[1]), /Error: broken/);
  assert.match(String(calls[0]?.args[2]), /Error: broken/);
  assert.deepEqual(output[0]?.[0], "error");
});

test("reports uncaught errors with their source location", () => {
  const calls: Call[] = [];
  const target = fakeTarget();
  installDiagnostics(fakeBridge(calls), "SwiftEditor", target.target, fakeConsole([]));

  target.dispatch("error", {
    message: "failed",
    filename: "editor.ts",
    lineno: 12,
    colno: 4,
    error: new Error("failed"),
  });

  assert.equal(calls.length, 1);
  assert.equal(calls[0]?.args[0], "error");
  assert.match(String(calls[0]?.args[1]), /editor\.ts:12:4/);
  assert.match(String(calls[0]?.args[2]), /Error: failed/);
});

test("reports unhandled promise rejections", () => {
  const calls: Call[] = [];
  const target = fakeTarget();
  installDiagnostics(fakeBridge(calls), "SwiftEditor", target.target, fakeConsole([]));

  target.dispatch("unhandledrejection", { reason: new Error("rejected") });

  assert.equal(calls.length, 1);
  assert.equal(calls[0]?.args[0], "error");
  assert.match(String(calls[0]?.args[1]), /Unhandled promise rejection/);
  assert.match(String(calls[0]?.args[1]), /Error: rejected/);
  assert.match(String(calls[0]?.args[2]), /Error: rejected/);
});

test("does not recurse through console when the bridge fails", async () => {
  const output: unknown[][] = [];
  const consoleTarget = fakeConsole(output);
  const target = fakeTarget();
  const bridge: Pick<MyGoBridge, "call"> = {
    call: async () => {
      throw new Error("bridge down");
    },
  };
  installDiagnostics(bridge, "SwiftEditor", target.target, consoleTarget);

  consoleTarget.error("problem");
  await Promise.resolve();

  assert.equal(output.length, 1);
});
