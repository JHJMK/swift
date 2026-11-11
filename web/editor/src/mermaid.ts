let mermaidPromise: Promise<typeof import("mermaid").default> | null = null;
let initialized = false;
let sequence = 0;
const cache = new Map<string, string>();
// A long note can hold many diagrams; keep only the ones recently shown,
// so their SVGs do not grow without bound.
const cacheLimit = 128;

async function loadMermaid(): Promise<typeof import("mermaid").default> {
  if (!mermaidPromise) {
    mermaidPromise = import("mermaid").then((mod) => mod.default);
  }
  const mermaid = await mermaidPromise;
  if (!initialized) {
    mermaid.initialize({
      startOnLoad: false,
      securityLevel: "strict",
      theme: "default",
      fontFamily:
        'ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif',
      flowchart: { useMaxWidth: false, htmlLabels: true, curve: "basis" },
      sequence: { useMaxWidth: false },
      gantt: { useMaxWidth: true },
      journey: { useMaxWidth: true },
      class: { useMaxWidth: true },
      state: { useMaxWidth: true },
      er: { useMaxWidth: false },
      pie: { useMaxWidth: true },
    });
    initialized = true;
  }
  return mermaid;
}

/** renderMermaidSvg renders a diagram once per source and returns its SVG. */
export async function renderMermaidSvg(source: string): Promise<string | null> {
  const cached = cache.get(source);
  if (cached) {
    cache.delete(source);
    cache.set(source, cached);
    return cached;
  }
  try {
    const mermaid = await loadMermaid();
    const id = `swift-mermaid-${++sequence}`;
    const { svg } = await mermaid.render(id, source);
    cache.set(source, svg);
    if (cache.size > cacheLimit) {
      const oldest = cache.keys().next().value;
      if (oldest !== undefined) cache.delete(oldest);
    }
    return svg;
  } catch (err) {
    console.warn("mermaid render failed", err);
    return null;
  }
}

export function cachedMermaidSvg(source: string): string | undefined {
  const svg = cache.get(source);
  if (svg) {
    cache.delete(source);
    cache.set(source, svg);
  }
  return svg;
}
