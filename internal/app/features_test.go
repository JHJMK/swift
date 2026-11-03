package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"
)

// The workbench's new document asks for a folder first.
func TestNewDocPicker(t *testing.T) {
	a, _, _ := newTestApp(t)
	folder, err := a.vault.CreateFolder("我的知识库", "归档")
	if err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	tt := ui.NewTester(a.view, 1180, 760)

	if err := tt.Click("新建文档"); err != nil {
		t.Fatal(err)
	}
	if a.picker == nil {
		t.Fatal("the picker did not open")
	}
	a.picker.Selected = folder
	if err := tt.Click("创建"); err != nil {
		t.Fatal(err)
	}
	if a.picker != nil {
		t.Fatal("the picker stayed open")
	}
	if a.doc == nil || !strings.HasPrefix(a.doc.Rel, folder+"/") {
		t.Fatalf("document is at %+v, want under %q", a.doc, folder)
	}
}

// The quick note is edited outside the knowledge bases, then saved into
// one of them.
func TestQuickNoteSaveTo(t *testing.T) {
	a, _, _ := newTestApp(t)
	tt := ui.NewTester(a.view, 1180, 760)

	if err := tt.Click("小记"); err != nil {
		t.Fatal(err)
	}
	if a.doc == nil || !a.doc.Note {
		t.Fatalf("the note did not open: %+v", a.doc)
	}
	if err := tt.Click("保存到"); err != nil {
		t.Fatal(err)
	}
	if a.picker == nil {
		t.Fatal("save-to did not open the picker")
	}
	a.picker.Selected = "我的知识库"
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	if a.picker != nil {
		t.Fatal("the picker stayed open")
	}
	if a.doc == nil || a.doc.Note {
		t.Fatalf("the note was not saved as a document: %+v", a.doc)
	}
	if a.findNode("我的知识库/小记.md") == nil {
		t.Fatalf("saved document missing: %+v", a.findNode("我的知识库"))
	}
}

// The quick note is kept when another page opens, and comes back.
func TestQuickNotePersists(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.openNote()
	a.doc.Doc.SetText(richtext.NewText("笔记内容"))
	a.markDocDirty()
	a.flushDoc()
	a.goHome()
	a.openNote()
	if a.doc == nil || a.doc.Doc.Text().PlainText() != "笔记内容" {
		t.Fatalf("note did not persist: %+v", a.doc)
	}
}

// Searching can be narrowed to a knowledge base or to a resource.
func TestSearchScopes(t *testing.T) {
	root := t.TempDir()
	v, err := OpenVault(root)
	if err != nil {
		t.Fatal(err)
	}
	first, err := v.CreateKnowledgeBase("甲库")
	if err != nil {
		t.Fatal(err)
	}
	second, err := v.CreateKnowledgeBase("乙库")
	if err != nil {
		t.Fatal(err)
	}
	one, _ := v.CreateDoc(first, "文档一")
	if _, err := v.WriteDoc(one, "苹果 香蕉\n"); err != nil {
		t.Fatal(err)
	}
	two, _ := v.CreateDoc(second, "文档二")
	if _, err := v.WriteDoc(two, "苹果 梨\n"); err != nil {
		t.Fatal(err)
	}
	parent, _ := v.CreateDoc(first, "父")
	dir, _ := v.EnsureDocDir(parent)
	child, _ := v.CreateDoc(dir, "子")
	if _, err := v.WriteDoc(child, "苹果 子\n"); err != nil {
		t.Fatal(err)
	}

	all, err := v.SearchIn("苹果", 20, "")
	if err != nil || len(all) != 3 {
		t.Fatalf("all: %d results (%v)", len(all), err)
	}
	onlyFirst, err := v.SearchIn("苹果", 20, first)
	if err != nil || len(onlyFirst) != 2 {
		t.Fatalf("first knowledge base: %+v (%v)", onlyFirst, err)
	}
	underParent, err := v.SearchIn("苹果", 20, parent)
	if err != nil || len(underParent) != 1 || underParent[0].Rel != child {
		t.Fatalf("parent scope: %+v (%v)", underParent, err)
	}
	if res, _ := v.SearchIn("苹果", 20, second); len(res) != 1 || res[0].Rel != two {
		t.Fatalf("second knowledge base: %+v", res)
	}
}

// The search window has a quiet Enter hint and a scope, without a big
// search button.
func TestSearchWindow(t *testing.T) {
	a, _, _ := newTestApp(t)
	tt := ui.NewTester(a.view, 1180, 760)
	a.openSearch()
	tt.Frame()
	if !tt.HasText("回车") {
		t.Fatalf("no Enter hint: %q", tt.Texts())
	}
	if !tt.HasText("全部知识库") {
		t.Fatalf("no scope: %q", tt.Texts())
	}
	tt.Type("正文")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if len(a.searchResults) == 0 {
		t.Fatalf("search found nothing: %q", a.searchNote)
	}
}

// Cmd+F's find bar finds the text and highlights the match.
func TestFindBar(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	tt := ui.NewTester(a.view, 1180, 760)
	a.openFind()
	tt.Frame()
	if !a.findOpen {
		t.Fatal("the find bar did not open")
	}
	tt.Type("正文")
	tt.Frame()
	if len(a.findMatches) != 1 {
		t.Fatalf("matches are %v", a.findMatches)
	}
	if !tt.HasText("1/1") {
		t.Fatalf("no match count: %q", tt.Texts())
	}
	anchor, caret := a.doc.Doc.Selection()
	if anchor >= caret {
		t.Fatalf("match not selected: %d, %d", anchor, caret)
	}
	a.closeFind()
	tt.Frame()
	if a.findOpen {
		t.Fatal("the find bar stayed open")
	}
}

// A note's title is kept with it too.
func TestQuickNoteTitlePersists(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.openNote()
	a.doc.TitleDraft = "记事本"
	a.flushTitle()
	a.flushDoc()
	a.goHome()
	a.openNote()
	if a.doc == nil || a.doc.Title != "记事本" {
		t.Fatalf("note title did not persist: %+v", a.doc)
	}
}

// Cancelling the location picker closes it, without a crash.
func TestPickerCancel(t *testing.T) {
	a, _, _ := newTestApp(t)
	tt := ui.NewTester(a.view, 1180, 760)
	if err := tt.Click("新建文档"); err != nil {
		t.Fatal(err)
	}
	if a.picker == nil {
		t.Fatal("the picker did not open")
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	if a.picker != nil {
		t.Fatal("the picker stayed open")
	}
	tt.Frame()
}

// A note can be saved under a parent document, as one of its children.
func TestQuickNoteSaveUnderDocument(t *testing.T) {
	a, _, _ := newTestApp(t)
	parent, err := a.vault.CreateDoc("我的知识库", "父文档")
	if err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	tt := ui.NewTester(a.view, 1180, 760)

	if err := tt.Click("小记"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("保存到"); err != nil {
		t.Fatal(err)
	}
	if a.picker == nil {
		t.Fatal("save-to did not open the picker")
	}
	a.picker.Selected = parent
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSuffix(parent, ".md") + "/小记.md"
	if a.doc == nil || a.doc.Rel != want {
		t.Fatalf("note saved at %+v, want %q", a.doc, want)
	}
	if a.findNode(want) == nil {
		t.Fatalf("saved child missing under the document: %+v", a.findNode(parent))
	}
}
