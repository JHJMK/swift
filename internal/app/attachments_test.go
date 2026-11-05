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

// Typing / opens the block palette and removes the slash.
func TestSlashMenu(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Doc.SetText(richtext.NewText("/"))
	a.doc.Doc.SetSelection(1, 1)
	if !a.maybeSlashMenu() {
		t.Fatal("the slash menu did not fire")
	}
	if got := a.doc.Doc.Text().PlainText(); got != "" {
		t.Fatalf("slash command left %q", got)
	}
	if !a.insertMenuOpen || a.insertQuery != "" {
		t.Fatalf("the insert palette did not open: %v %q", a.insertMenuOpen, a.insertQuery)
	}
	a.insertMenuOpen = false
}

// The outline lists the page's headings in order.
func TestOutlineEntries(t *testing.T) {
	text := richtext.ParseMarkdown("# 一\n\n正文\n\n## 二\n\n### 三\n")
	entries := outlineEntries(text)
	if len(entries) != 3 {
		t.Fatalf("outline has %d entries: %+v", len(entries), entries)
	}
	for i, want := range []struct {
		level int
		text  string
	}{{1, "一"}, {2, "二"}, {3, "三"}} {
		if entries[i].Level != want.level || entries[i].Text != want.text {
			t.Fatalf("entry %d is %+v", i, entries[i])
		}
	}
}

// The quick note keeps its own attachments, and moves them into the
// knowledge base when it is saved as a document.
func TestNoteAttachment(t *testing.T) {
	a, root, _ := newTestApp(t)
	a.openNote()
	src := filepath.Join(t.TempDir(), "说明.txt")
	if err := os.WriteFile(src, []byte("note file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.attachFile(src); err != nil {
		t.Fatal(err)
	}
	md := a.doc.Doc.Text().Markdown()
	if !strings.Contains(md, "[附件：说明.txt](.swift/note-assets/") {
		t.Fatalf("note attachment link missing: %q", md)
	}
	i := strings.Index(md, "](.swift/note-assets/") + 2
	j := strings.Index(md[i:], ")") + i
	url := md[i:j]
	p, ok := a.vault.ResolveAsset("", url)
	if !ok {
		t.Fatalf("note attachment did not resolve: %q", url)
	}
	if b, err := os.ReadFile(p); err != nil || string(b) != "note file" {
		t.Fatalf("note attachment content: %q %v", b, err)
	}
	if err := a.saveNoteTo("我的知识库"); err != nil {
		t.Fatal(err)
	}
	saved, _, err := a.vault.ReadDoc("我的知识库/小记.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(saved, ".swift/note-assets/") || !strings.Contains(saved, "(.assets/") {
		t.Fatalf("note attachment was not moved into the knowledge base: %q", saved)
	}
	assets, _ := filepath.Glob(filepath.Join(root, "我的知识库", ".assets", "*-说明.txt"))
	if len(assets) != 1 {
		t.Fatalf("note attachment not copied: %v", assets)
	}
	if _, err := os.Stat(filepath.Join(root, ".swift", "note-assets")); !os.IsNotExist(err) {
		t.Fatalf("note assets were not cleaned up: %v", err)
	}
}

// The slash menu works in the quick note as well, offering the four block
// types.
func TestNoteSlashMenu(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.openNote()
	a.doc.Doc.SetText(richtext.NewText("/"))
	a.doc.Doc.SetSelection(1, 1)
	if !a.maybeSlashMenu() {
		t.Fatal("the slash menu did not fire in the note")
	}
	if !a.insertMenuOpen || !a.insertSlash {
		t.Fatalf("note slash menu did not open: %v %v", a.insertMenuOpen, a.insertSlash)
	}
	cmds := a.slashCommands(a.doc, func() {})
	want := []string{"附件", "画板", "流程图", "思维导图"}
	if len(cmds) != len(want) {
		t.Fatalf("slash menu has %d commands: %+v", len(cmds), cmds)
	}
	for i, label := range want {
		if cmds[i].Label != label {
			t.Fatalf("slash command %d is %q, want %q", i, cmds[i].Label, label)
		}
	}
}
