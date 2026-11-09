import assert from "node:assert/strict";
import test from "node:test";

import {
  assetUrl,
  clipboardFiles,
  isImageFile,
} from "../src/runtime.ts";

test("uses the WebView2 asset URL on Windows", () => {
  assert.equal(
    assetUrl(".assets/图片.png", "知识库/文档.md", "win32"),
    "http://swiftassets.localhost/asset?doc=%E7%9F%A5%E8%AF%86%E5%BA%93%2F%E6%96%87%E6%A1%A3.md&ref=.assets%2F%E5%9B%BE%E7%89%87.png",
  );
});

test("keeps the custom asset scheme on other platforms", () => {
  assert.equal(
    assetUrl(".assets/图片.png", "知识库/文档.md", "darwin"),
    "swiftassets://localhost/asset?doc=%E7%9F%A5%E8%AF%86%E5%BA%93%2F%E6%96%87%E6%A1%A3.md&ref=.assets%2F%E5%9B%BE%E7%89%87.png",
  );
});

test("reads clipboard files from items when files is empty", () => {
  const picture = new File(["png"], "image.png", { type: "image/png" });
  const files = clipboardFiles({
    files: [],
    items: [
      { kind: "string", getAsFile: () => null },
      { kind: "file", getAsFile: () => picture },
    ],
  });

  assert.deepEqual(files, [picture]);
});

test("recognizes image files with a missing MIME type", () => {
  assert.equal(
    isImageFile(new File(["png"], "截图.PNG", { type: "" })),
    true,
  );
  assert.equal(
    isImageFile(new File(["text"], "说明.txt", { type: "" })),
    false,
  );
});
