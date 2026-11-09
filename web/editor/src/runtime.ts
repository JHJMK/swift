type ClipboardData = Pick<DataTransfer, "files" | "items">;

const imageExtension = /\.(avif|bmp|gif|jpe?g|png|svg|webp)$/i;

export function assetUrl(
  ref: string,
  docId: string,
  platform: string,
): string {
  const trimmed = ref.trim().replace(/^<|>$/g, "");
  if (!trimmed) return "";
  if (/^(https?:|data:|blob:|file:)/i.test(trimmed)) return trimmed;
  const origin =
    platform === "win32"
      ? "http://swiftassets.localhost"
      : "swiftassets://localhost";
  return `${origin}/asset?doc=${encodeURIComponent(docId)}&ref=${encodeURIComponent(trimmed)}`;
}

/** clipboardFiles also reads items, which WebView2 can provide alone. */
export function clipboardFiles(data: ClipboardData): File[] {
  const files = Array.from(data.files);
  if (files.length > 0) return files;
  const fromItems: File[] = [];
  for (const item of Array.from(data.items)) {
    if (item.kind !== "file") continue;
    const file = item.getAsFile();
    if (file) fromItems.push(file);
  }
  return fromItems;
}

/** isImageFile handles image clipboard data that arrives without a MIME. */
export function isImageFile(file: Pick<File, "name" | "type">): boolean {
  const type = file.type.toLowerCase();
  if (type.startsWith("image/")) return true;
  return (
    (type === "" || type === "application/octet-stream") &&
    imageExtension.test(file.name)
  );
}
