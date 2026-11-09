import type { MyGoBridge } from "./types.ts";

export interface DiagnosticTarget {
  addEventListener(type: string, listener: (event: any) => void): void;
}

export interface DiagnosticConsole {
  warn(...data: unknown[]): void;
  error(...data: unknown[]): void;
}

interface Description {
  message: string;
  stack: string;
}

const installedTargets = new WeakSet<object>();

function describe(value: unknown): Description {
  if (value instanceof Error) {
    return {
      message: `${value.name}: ${value.message}`,
      stack: value.stack ?? "",
    };
  }
  if (typeof value === "string") {
    return { message: value, stack: "" };
  }
  try {
    const json = JSON.stringify(value);
    return { message: json ?? String(value), stack: "" };
  } catch {
    return { message: String(value), stack: "" };
  }
}

function describeAll(values: unknown[]): Description {
  const descriptions = values.map(describe);
  return {
    message: descriptions.map((item) => item.message).join(" "),
    stack: descriptions.map((item) => item.stack).filter(Boolean).join("\n"),
  };
}

export function installDiagnostics(
  bridge: Pick<MyGoBridge, "call">,
  service: string,
  target: DiagnosticTarget = window,
  consoleTarget: DiagnosticConsole = console,
): void {
  if (installedTargets.has(target as object)) return;
  installedTargets.add(target as object);

  const report = (level: "warn" | "error", message: string, stack = "") => {
    try {
      void bridge
        .call<void>(`${service}.ReportLog`, level, message, stack)
        .catch(() => undefined);
    } catch {
      // Logging must never become another source of uncaught errors.
    }
  };

  const originalWarn = consoleTarget.warn.bind(consoleTarget);
  const originalError = consoleTarget.error.bind(consoleTarget);

  consoleTarget.warn = (...data: unknown[]) => {
    const description = describeAll(data);
    report("warn", description.message, description.stack);
    originalWarn(...data);
  };
  consoleTarget.error = (...data: unknown[]) => {
    const description = describeAll(data);
    report("error", description.message, description.stack);
    originalError(...data);
  };

  target.addEventListener("error", (event: any) => {
    const description = describe(event?.error);
    const filename = typeof event?.filename === "string" ? event.filename : "";
    const line = Number(event?.lineno ?? 0);
    const column = Number(event?.colno ?? 0);
    let message = String(event?.message ?? description.message ?? "Uncaught error");
    let stack = description.stack;
    if (filename) {
      const location = `${filename}:${line}:${column}`;
      message += ` (${location})`;
      if (!stack) stack = `at ${location}`;
    }
    report("error", message, stack);
  });

  target.addEventListener("unhandledrejection", (event: any) => {
    const description = describe(event?.reason);
    report("error", `Unhandled promise rejection: ${description.message}`, description.stack);
  });
}
