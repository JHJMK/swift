package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/richtext"
)

// Importing a folder copies its Markdown hierarchy and the pictures and
// files it refers to.
func TestImportKnowledgeBaseCopy(t *testing.T) {
	a, root, _ := newTestApp(t)
	src := filepath.Join(t.TempDir(), "旧笔记")
	for _, dir := range []string{src, filepath.Join(src, "图片"), filepath.Join(src, "附件"), filepath.Join(src, "子目录")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(src, "图片", "图.png"), testPNG(t), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "附件", "手册.pdf"), []byte("%PDF-1.4"), 0o644); err != nil {
		t.Fatal(err)
	}
	index := "# 首页\n\n![图](图片/图.png)\n\n[手册](附件/手册.pdf)\n\n[子页](子目录/子页.md)\n"
	if err := os.WriteFile(filepath.Join(src, "首页.md"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	child := "## 子页\n\n![图](../图片/图.png)\n"
	if err := os.WriteFile(filepath.Join(src, "子目录", "子页.md"), []byte(child), 0o644); err != nil {
		t.Fatal(err)
	}

	kb, err := a.importKnowledgeBaseCopy(src)
	if err != nil {
		t.Fatal(err)
	}
	if kb != "旧笔记" {
		t.Fatalf("knowledge base is %q", kb)
	}
	a.refreshTree()
	if a.findNode(kb+"/首页.md") == nil || a.findNode(kb+"/子目录/子页.md") == nil {
		t.Fatalf("imported documents missing: %+v", a.findNode(kb))
	}
	if a.findNode(kb+"/图片") != nil || a.findNode(kb+"/附件") != nil {
		t.Fatalf("asset-only folders were copied: %+v", a.findNode(kb))
	}

	got, _, err := a.vault.ReadDoc(kb + "/首页.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "![图](.assets/") {
		t.Fatalf("image reference not rewritten: %q", got)
	}
	if !strings.Contains(got, "[手册](.assets/") {
		t.Fatalf("attachment reference not rewritten: %q", got)
	}
	if !strings.Contains(got, "[子页](子目录/子页.md)") {
		t.Fatalf("document link changed: %q", got)
	}
	sub, _, err := a.vault.ReadDoc(kb + "/子目录/子页.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sub, "![图](../.assets/") {
		t.Fatalf("nested image reference not rewritten: %q", sub)
	}
	assets := filepath.Join(root, "旧笔记", ".assets")
	if files, _ := filepath.Glob(filepath.Join(assets, "*-图.png")); len(files) != 2 {
		t.Fatalf("images copied %v", files)
	}
	if files, _ := filepath.Glob(filepath.Join(assets, "*-手册.pdf")); len(files) != 1 {
		t.Fatalf("attachment copied %v", files)
	}
}

// An external knowledge base reads and writes where its folder is.
func TestExternalKnowledgeBase(t *testing.T) {
	a, _, _ := newTestApp(t)
	ext := filepath.Join(t.TempDir(), "外部笔记")
	if err := os.MkdirAll(ext, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ext, "图.png"), testPNG(t), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ext, "hello.md"), []byte("# 外部\n\n![图](图.png)\n\n内容\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	name, err := a.vault.AddExternal("外部笔记", ext)
	if err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	if a.findNode(name) == nil || a.findNode(name+"/hello.md") == nil {
		t.Fatalf("external knowledge base missing from the tree")
	}
	a.openKB(a.findNode(name))
	if a.doc == nil || a.doc.Rel != name+"/hello.md" {
		t.Fatalf("external document not opened: %+v", a.doc)
	}
	if a.doc.ReadOnly {
		t.Fatal("external document is read-only")
	}

	// The picture is read from the external folder.
	loaded := false
	for _, p := range a.doc.Doc.Text().Paragraphs() {
		for _, r := range p.Runs {
			if at := r.Attributes.Attachment; at != nil && at.Image != nil {
				loaded = true
			}
		}
	}
	if !loaded {
		t.Fatal("the external picture was not loaded")
	}

	renamable := false
	for _, act := range a.nodeActions(a.findNode(a.doc.Rel)) {
		if act.label == "重命名" {
			renamable = true
		}
	}
	if !renamable {
		t.Fatal("external document cannot be renamed")
	}

	// Editing the page writes the external file.
	a.doc.Doc.SetText(richtext.NewText("改过的内容"))
	a.markDocDirty()
	a.flushDoc()
	if b, err := os.ReadFile(filepath.Join(ext, "hello.md")); err != nil || !strings.Contains(string(b), "改过的内容") {
		t.Fatalf("external file not saved: %q (%v)", b, err)
	}

	// Resources can be created in the external knowledge base too.
	if _, err := a.vault.CreateDoc(name, "新文档"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ext, "新文档.md")); err != nil {
		t.Fatal("a document was not created in the external folder")
	}
}
