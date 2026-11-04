package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/richtext"
)

// An attachment is copied into the assets and linked from the document.
func TestInsertAttachment(t *testing.T) {
	a, root, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Doc.SetSelection(0, 0)

	src := filepath.Join(t.TempDir(), "说明.pdf")
	if err := os.WriteFile(src, []byte("%PDF-1.4"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.attachFile(src); err != nil {
		t.Fatal(err)
	}
	md := a.doc.Doc.Text().Markdown()
	if !strings.Contains(md, "[附件：说明.pdf](") {
		t.Fatalf("attachment link missing: %q", md)
	}
	a.flushDoc()
	saved, _, err := a.vault.ReadDoc(rel)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(saved, "[附件：说明.pdf](.assets/") {
		t.Fatalf("saved attachment link missing: %q", saved)
	}
	assets, _ := filepath.Glob(filepath.Join(root, "我的知识库", ".assets", "*-说明.pdf"))
	if len(assets) != 1 {
		t.Fatalf("attachment not copied: %v", assets)
	}
}

// Typing /附件 removes it and asks for the file.
func TestAttachmentSlash(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Doc.SetText(richtext.NewText("/附件"))
	a.doc.Doc.SetSelection(3, 3)
	if !a.maybeSlashAttachment() {
		t.Fatal("the slash command did not fire")
	}
	if got := a.doc.Doc.Text().PlainText(); got != "" {
		t.Fatalf("slash command left %q", got)
	}
	if a.deferred == nil {
		t.Fatal("the attachment picker was not asked for")
	}
	a.deferred = nil
}
