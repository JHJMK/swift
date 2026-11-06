package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo"
)

func writeExternal(t *testing.T, root, rel, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWatchIgnore(t *testing.T) {
	cases := []struct {
		rel   string
		isDir bool
		want  bool
	}{
		{"我的知识库/.assets/a.png", false, true},
		{"我的知识库/.swift/sync-state.json", false, true},
		{"我的知识库/.git/config", false, true},
		{"我的知识库/开始.md", false, false},
		{"我的知识库/目录", true, false},
		{"我的知识库/readme.txt", false, true},
	}
	for _, c := range cases {
		if got := watchIgnore(c.rel, c.isDir); got != c.want {
			t.Errorf("watchIgnore(%q, %v) = %v, want %v", c.rel, c.isDir, got, c.want)
		}
	}
}

// A write from another editor updates the open document.
func TestExternalWriteReloadsDocument(t *testing.T) {
	a, root, rel := newTestApp(t)
	a.openDoc(rel)
	writeExternal(t, root, rel, "# 外部修改\n\n新正文\n")
	w := &kbWatch{rel: "我的知识库"}
	a.applyKBEvents(w, []mygo.FileEvent{{Op: mygo.FileWrite, Path: "开始.md"}})

	if a.doc == nil {
		t.Fatal("document closed")
	}
	if a.doc.Markdown != "# 外部修改\n\n新正文\n" {
		t.Fatalf("markdown %q", a.doc.Markdown)
	}
	if !strings.Contains(a.doc.Doc.Text().PlainText(), "新正文") {
		t.Fatalf("document text %q", a.doc.Doc.Text().PlainText())
	}
}

// A document dropped into the knowledge base appears in the tree.
func TestExternalCreateRefreshesTree(t *testing.T) {
	a, root, rel := newTestApp(t)
	a.openDoc(rel)
	writeExternal(t, root, "我的知识库/新文档.md", "新文档")
	a.applyKBEvents(&kbWatch{rel: "我的知识库"}, []mygo.FileEvent{{
		Op: mygo.FileCreate, Path: "新文档.md",
	}})
	if a.findNode("我的知识库/新文档.md") == nil {
		t.Fatalf("new document missing from tree: %+v", a.tree)
	}
	if a.doc == nil || a.doc.Rel != rel {
		t.Fatalf("open document changed: %+v", a.doc)
	}
}

// Removing the open file closes it and shows the knowledge base home.
func TestExternalDeleteClosesDocument(t *testing.T) {
	a, root, rel := newTestApp(t)
	a.openDoc(rel)
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
		t.Fatal(err)
	}
	a.applyKBEvents(&kbWatch{rel: "我的知识库"}, []mygo.FileEvent{{
		Op: mygo.FileRemove, Path: "开始.md",
	}})
	if a.doc != nil {
		t.Fatalf("document still open: %+v", a.doc)
	}
	if a.selected != "我的知识库" || !a.selectedIsDir {
		t.Fatalf("selection %q dir=%v", a.selected, a.selectedIsDir)
	}
}

// Moving the open file keeps it open under its new name.
func TestExternalRenameFollowsDocument(t *testing.T) {
	a, root, rel := newTestApp(t)
	a.openDoc(rel)
	oldPath := filepath.Join(root, filepath.FromSlash(rel))
	newPath := filepath.Join(root, "我的知识库", "改名.md")
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	a.applyKBEvents(&kbWatch{rel: "我的知识库"}, []mygo.FileEvent{{
		Op: mygo.FileRename, OldPath: "开始.md", Path: "改名.md",
	}})
	if a.doc == nil || a.doc.Rel != "我的知识库/改名.md" {
		t.Fatalf("document %+v", a.doc)
	}
	if a.doc.Title != "改名" || a.selected != a.doc.Rel {
		t.Fatalf("title/selection %q %q", a.doc.Title, a.selected)
	}
	if a.findNode(a.doc.Rel) == nil {
		t.Fatalf("renamed document missing from tree: %+v", a.tree)
	}
}

// Local unsaved edits are kept when the file changes outside.
func TestExternalWriteKeepsDirtyDocument(t *testing.T) {
	a, root, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Markdown = "本地修改"
	a.doc.Dirty = true
	writeExternal(t, root, rel, "外部内容")
	a.applyKBEvents(&kbWatch{rel: "我的知识库"}, []mygo.FileEvent{{
		Op: mygo.FileWrite, Path: "开始.md",
	}})
	if a.doc.Markdown != "本地修改" || !a.doc.Dirty {
		t.Fatalf("local edits replaced: %+v", a.doc)
	}
	if !a.doc.externalConflict {
		t.Fatal("external conflict not noted")
	}
}

// A deleted file with unsaved edits stays open until the author saves.
func TestExternalDeleteKeepsDirtyDocument(t *testing.T) {
	a, root, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Markdown = "本地修改"
	a.doc.Dirty = true
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
		t.Fatal(err)
	}
	a.applyKBEvents(&kbWatch{rel: "我的知识库"}, []mygo.FileEvent{{
		Op: mygo.FileRemove, Path: "开始.md",
	}})
	if a.doc == nil || a.doc.Markdown != "本地修改" {
		t.Fatalf("dirty document closed: %+v", a.doc)
	}
	if !a.doc.externalDeleted {
		t.Fatal("external deletion not noted")
	}
}

// An atomic save that looks like a removal and a new file keeps the open
// document open.
func TestExternalRemoveAndRecreateKeepsDocument(t *testing.T) {
	a, root, rel := newTestApp(t)
	a.openDoc(rel)
	writeExternal(t, root, rel, "# 替换后的内容\n")
	a.applyKBEvents(&kbWatch{rel: "我的知识库", root: filepath.Join(root, "我的知识库")}, []mygo.FileEvent{
		{Op: mygo.FileRemove, Path: "开始.md"},
		{Op: mygo.FileCreate, Path: "开始.md"},
	})
	if a.doc == nil {
		t.Fatal("document closed after replacement")
	}
	if !strings.Contains(a.doc.Doc.Text().PlainText(), "替换后的内容") {
		t.Fatalf("document text %q", a.doc.Doc.Text().PlainText())
	}
}

// The polling fallback and the first snapshot after enrolling a watch use
// the same coarse refresh.
func TestKBSnapshotRefreshesTreeAndDocument(t *testing.T) {
	a, root, rel := newTestApp(t)
	a.openDoc(rel)
	writeExternal(t, root, "我的知识库/快照新文件.md", "新文件")
	writeExternal(t, root, rel, "# 快照修改\n")
	w := &kbWatch{rel: "我的知识库", root: filepath.Join(root, "我的知识库")}
	a.applyKBSnapshot(w)
	if a.findNode("我的知识库/快照新文件.md") == nil {
		t.Fatal("new document missing from tree")
	}
	if !strings.Contains(a.doc.Doc.Text().PlainText(), "快照修改") {
		t.Fatalf("document not reloaded: %q", a.doc.Doc.Text().PlainText())
	}
}
