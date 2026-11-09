package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// Deleting a folder in the tree lands on its parent instead of the
// workbench.
func TestDeleteFolderKeepsParent(t *testing.T) {
	a, _, _ := newTestApp(t)
	folder, err := a.vault.CreateFolder("我的知识库", "新目录")
	if err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.selected = folder
	a.selectedIsDir = true
	a.askDelete(folder)
	if a.confirm == nil {
		t.Fatal("no delete confirmation")
	}
	a.confirm.OnOK()
	if a.selected != "我的知识库" || !a.selectedIsDir {
		t.Fatalf("selection %q dir=%v", a.selected, a.selectedIsDir)
	}
	if a.findNode(folder) != nil {
		t.Fatal("deleted folder still in tree")
	}
}

// Deleting the open document also leaves its knowledge base open.
func TestDeleteDocumentKeepsParent(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.askDelete(rel)
	if a.confirm == nil {
		t.Fatal("no delete confirmation")
	}
	a.confirm.OnOK()
	if a.doc != nil {
		t.Fatal("document still open")
	}
	if a.selected != "我的知识库" || !a.selectedIsDir {
		t.Fatalf("selection %q dir=%v", a.selected, a.selectedIsDir)
	}
}

// A folder removed outside the app selects its parent too.
func TestExternalFolderDeleteKeepsParent(t *testing.T) {
	a, root, _ := newTestApp(t)
	folder, err := a.vault.CreateFolder("我的知识库", "新目录")
	if err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.selected = folder
	a.selectedIsDir = true
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(folder))); err != nil {
		t.Fatal(err)
	}
	a.applyKBEvents(&kbWatch{rel: "我的知识库"}, []mygo.FileEvent{{
		Op: mygo.FileRemove, Path: "新目录", IsDir: true,
	}})
	if a.selected != "我的知识库" || !a.selectedIsDir {
		t.Fatalf("selection %q dir=%v", a.selected, a.selectedIsDir)
	}
}

// A folder renamed outside the app stays selected under its new name.
func TestExternalFolderRenameKeepsSelection(t *testing.T) {
	a, root, _ := newTestApp(t)
	folder, err := a.vault.CreateFolder("我的知识库", "旧目录")
	if err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.selected = folder
	a.selectedIsDir = true
	if err := os.Rename(filepath.Join(root, filepath.FromSlash(folder)), filepath.Join(root, "我的知识库", "新目录")); err != nil {
		t.Fatal(err)
	}
	a.applyKBEvents(&kbWatch{rel: "我的知识库"}, []mygo.FileEvent{{
		Op: mygo.FileRename, OldPath: "旧目录", Path: "新目录", IsDir: true,
	}})
	if a.selected != "我的知识库/新目录" || !a.selectedIsDir {
		t.Fatalf("selection %q dir=%v", a.selected, a.selectedIsDir)
	}
	if a.findNode(a.selected) == nil {
		t.Fatal("renamed folder missing from tree")
	}
}

// The watch reports the app's own autosave while the author keeps typing:
// the file holds exactly what the app last wrote, so the notice about a
// changed disk file stays away.
func TestAutosaveEchoStaysQuiet(t *testing.T) {
	a, root, rel := newTestApp(t)
	a.openDoc(rel)
	saved := "# 标题\n\n正文内容\n"
	a.doc.Saved = saved
	a.doc.Markdown = "保存之后又输入的内容"
	a.doc.Dirty = true
	writeExternal(t, root, rel, saved)
	a.applyKBEvents(&kbWatch{rel: "我的知识库"}, []mygo.FileEvent{{
		Op: mygo.FileWrite, Path: "开始.md",
	}})
	if a.toast != "" {
		t.Fatalf("autosave echo showed a notice: %q", a.toast)
	}
	if a.doc.externalConflict {
		t.Fatal("autosave echo noted as an external conflict")
	}
	if a.doc.Markdown != "保存之后又输入的内容" || !a.doc.Dirty {
		t.Fatalf("local edits replaced: %+v", a.doc)
	}
}

// A watch can report an older autosave after a newer one. That echo is
// still this app's own write, not an external edit, so it stays quiet.
func TestStaleAutosaveEchoStaysQuiet(t *testing.T) {
	a, root, rel := newTestApp(t)
	a.openDoc(rel)
	older := "# 旧的保存\n"
	a.doc.rememberWrite(older)
	a.doc.Saved = "# 新的保存\n"
	a.doc.Markdown = "保存之后又输入的内容"
	a.doc.Dirty = true
	a.doc.MTime = time.Now().Add(time.Hour)
	writeExternal(t, root, rel, older)
	a.applyKBEvents(&kbWatch{rel: "我的知识库"}, []mygo.FileEvent{{
		Op: mygo.FileWrite, Path: "开始.md",
	}})
	if a.toast != "" {
		t.Fatalf("stale autosave echo showed a notice: %q", a.toast)
	}
	if a.doc.externalConflict {
		t.Fatal("stale autosave echo noted as an external conflict")
	}
	if a.doc.Markdown != "保存之后又输入的内容" || !a.doc.Dirty {
		t.Fatalf("local edits replaced: %+v", a.doc)
	}
}
