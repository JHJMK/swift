package app

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Importing a Markdown file copies the images it refers to, in Markdown
// and HTML, into the knowledge base's assets, and leaves remote images
// alone.
func TestImportMarkdownWithImages(t *testing.T) {
	a, root, _ := newTestApp(t)
	src := t.TempDir()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "pixel.png"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	const md = "# 导入\n\n![图片](pixel.png)\n\n![远程](https://example.com/x.png)\n\n<img src=\"pixel.png\">\n"
	mdPath := filepath.Join(src, "导入文档.md")
	if err := os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := a.importMarkdownFile("我的知识库", mdPath); err != nil {
		t.Fatal(err)
	}
	if a.doc == nil || a.doc.Title != "导入文档" || a.doc.Rel != "我的知识库/导入文档.md" {
		t.Fatalf("imported document is %+v", a.doc)
	}
	saved := a.doc.Saved
	if strings.Contains(saved, "![图片](pixel.png)") || strings.Contains(saved, `<img src="pixel.png">`) {
		t.Fatalf("local images were not rewritten: %q", saved)
	}
	if !strings.Contains(saved, ".assets/") {
		t.Fatalf("no asset reference: %q", saved)
	}
	if !strings.Contains(saved, "https://example.com/x.png") {
		t.Fatalf("remote image changed: %q", saved)
	}
	assets, err := filepath.Glob(filepath.Join(root, "我的知识库", ".assets", "*-pixel.png"))
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 2 {
		t.Fatalf("imported assets are %v", assets)
	}
}

// A Typora image reference is an absolute path with spaces and no angle
// brackets; the importer still finds, copies and rewrites it.
func TestImportMarkdownWithSpacedImagePath(t *testing.T) {
	a, root, _ := newTestApp(t)
	src := t.TempDir()
	imgDir := filepath.Join(src, "typora user images")
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	imgPath := filepath.Join(imgDir, "image-20261103102841513.png")
	if err := os.WriteFile(imgPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	md := "# 哈哈哈\n\n这是一张图片\n\n![image-20261103102841513](" + imgPath + ")\n"
	mdPath := filepath.Join(src, "哈哈哈.md")
	if err := os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := a.importMarkdownFile("我的知识库", mdPath); err != nil {
		t.Fatal(err)
	}
	saved := a.doc.Saved
	if strings.Contains(saved, "typora user images") {
		t.Fatalf("absolute image path was not rewritten: %q", saved)
	}
	if !strings.Contains(saved, ".assets/") {
		t.Fatalf("no asset reference: %q", saved)
	}
	assets, err := filepath.Glob(filepath.Join(root, "我的知识库", ".assets", "*-image-20261103102841513.png"))
	if err != nil || len(assets) != 1 {
		t.Fatalf("imported assets are %v (%v)", assets, err)
	}
}
