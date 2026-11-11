package app

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"
)

func newTestApp(t *testing.T) (*app, string, string) {
	t.Helper()
	// Keep the settings the tests save out of the user's own config.
	oldConfigDir := configDirOverride
	configDirOverride = t.TempDir()
	t.Cleanup(func() { configDirOverride = oldConfigDir })
	root := t.TempDir()
	v, err := OpenVault(root)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := v.CreateKnowledgeBase("我的知识库")
	if err != nil {
		t.Fatal(err)
	}
	rel, err := v.CreateDoc(kb, "开始")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.WriteDoc(rel, "# 标题\n\n正文内容\n"); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: &Config{Workspace: root}, vault: v, openDirs: map[string]*bool{}}
	a.refreshTree()
	return a, root, rel
}

func TestUIViewOpensDocument(t *testing.T) {
	a, _, rel := newTestApp(t)
	tt := ui.NewTester(a.view, 1180, 760)

	if !tt.HasText("知识库") || !tt.HasText("工作台") {
		t.Fatalf("main view not shown: %q", tt.Texts())
	}
	// The knowledge base column holds the document tree.
	if err := tt.Click("我的知识库"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("开始"); err != nil {
		t.Fatal(err)
	}
	if a.doc == nil || a.doc.Rel != rel {
		t.Fatalf("document not opened: %+v", a.doc)
	}
	if !tt.HasText("已保存") {
		t.Fatalf("editor not shown: %q", tt.Texts())
	}

	if !tt.HasText("预览") || tt.HasText("重命名") {
		t.Fatalf("document actions are wrong: %q", tt.Texts())
	}
	if err := tt.Click("预览"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	texts := tt.Texts()
	previewLabels := 0
	for _, text := range texts {
		if text == "预览" {
			previewLabels++
		}
	}
	if !a.previewMode || !tt.HasText("编辑") || tt.HasText("加粗（⌘B）") || previewLabels != 0 {
		t.Fatalf("preview mode not shown: preview=%v previewLabels=%d texts=%q", a.previewMode, previewLabels, texts)
	}
	if err := tt.Click("编辑"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	texts = tt.Texts()
	editLabels := 0
	for _, text := range texts {
		if text == "编辑" {
			editLabels++
		}
	}
	if a.previewMode || !tt.HasText("加粗（⌘B）") || editLabels != 0 {
		t.Fatalf("edit mode not restored: preview=%v editLabels=%d texts=%q", a.previewMode, editLabels, texts)
	}

	if err := tt.Click("工作台"); err != nil {
		t.Fatal(err)
	}
	createDocFromWorkbench(t, tt)
	if a.doc == nil || !a.doc.ShowTitle || !untitledTitle(a.doc.Title) {
		t.Fatalf("untitled page not shown: %+v", a.doc)
	}

	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	if !a.settingsOpen {
		t.Fatal("settings not opened")
	}
	if err := tt.Click("完成"); err != nil {
		t.Fatal(err)
	}
	if a.settingsOpen {
		t.Fatal("settings not closed")
	}
}

func TestPasteMenuFollowsEditorFocus(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.pasteItem = &mygo.MenuItem{Label: "粘贴", Disabled: true}
	tt := ui.NewTester(a.view, 1180, 760)

	if err := tt.Click("我的知识库"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("开始"); err != nil {
		t.Fatal(err)
	}
	if a.pasteItem.IsEnabled() {
		t.Fatal("paste enabled before the editor has the focus")
	}
	tt.ClickAt(700, 300) // the editor's page
	if !a.pasteItem.IsEnabled() {
		t.Fatal("paste not enabled while the editor has the focus")
	}
	if err := tt.Click("工作台"); err != nil {
		t.Fatal(err)
	}
	if a.pasteItem.IsEnabled() {
		t.Fatal("paste still enabled after leaving the editor")
	}
}

// Selecting a knowledge base opens its first document, and 首页 shows the
// knowledge base's own page again.
func TestSelectingKnowledgeBaseOpensFirstDoc(t *testing.T) {
	a, _, rel := newTestApp(t)
	tt := ui.NewTester(a.view, 1180, 760)

	if err := tt.Click("我的知识库"); err != nil {
		t.Fatal(err)
	}
	if a.doc == nil || a.doc.Rel != rel {
		t.Fatalf("first document not opened: %+v", a.doc)
	}
	if !tt.HasText("已保存") {
		t.Fatalf("editor not shown: %q", tt.Texts())
	}

	if err := tt.Click("首页"); err != nil {
		t.Fatal(err)
	}
	if a.doc != nil {
		t.Fatalf("document still open: %+v", a.doc)
	}
	if a.selected != "我的知识库" || !a.selectedIsDir {
		t.Fatalf("knowledge base not selected: %q %v", a.selected, a.selectedIsDir)
	}
	if !tt.HasText("新建文档") {
		t.Fatalf("knowledge base home not shown: %q", tt.Texts())
	}
}

// The plus button of the knowledge base opens a menu drawn in the window,
// whose items run their actions and close it.
func TestKnowledgeBaseMenu(t *testing.T) {
	a, _, _ := newTestApp(t)
	tt := ui.NewTester(a.view, 1180, 760)

	if err := tt.Click("我的知识库"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("新建"); err != nil {
		t.Fatal(err)
	}
	if !a.kbMenuOpen {
		t.Fatal("menu not open")
	}
	for _, label := range []string{"新建文档", "新建目录", "导入", "重命名知识库"} {
		if !tt.HasText(label) {
			t.Fatalf("menu lacks %q: %q", label, tt.Texts())
		}
	}

	if err := tt.Click("新建目录"); err != nil {
		t.Fatal(err)
	}
	if a.kbMenuOpen {
		t.Fatal("menu closed on choosing an item")
	}
	if a.prompt == nil || a.prompt.Title != "新建目录" {
		t.Fatalf("prompt not shown: %+v", a.prompt)
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}

	// A press elsewhere closes the menu.
	if err := tt.Click("新建"); err != nil {
		t.Fatal(err)
	}
	tt.ClickAt(900, 500)
	if a.kbMenuOpen {
		t.Fatal("menu did not close on a press outside it")
	}
}

func TestUnifiedImportMenus(t *testing.T) {
	a, _, _ := newTestApp(t)
	tt := ui.NewTester(a.view, 1180, 760)
	importItems := []string{"复制导入知识库", "引用外部知识库", "导入语雀 Lakebook"}

	if err := tt.Click("新建或导入知识库"); err != nil {
		t.Fatal(err)
	}
	if !a.kbAddMenuOpen || a.kbImportMenuOpen {
		t.Fatalf("sidebar menu state = %v %v", a.kbAddMenuOpen, a.kbImportMenuOpen)
	}
	if !tt.HasText("新建知识库") || !tt.HasText("导入") {
		t.Fatalf("sidebar menu lacks its top-level actions: %q", tt.Texts())
	}
	for _, item := range importItems {
		if tt.HasText(item) {
			t.Fatalf("sidebar top-level menu already shows %q", item)
		}
	}

	if err := tt.Click("导入"); err != nil {
		t.Fatal(err)
	}
	if a.kbAddMenuOpen || !a.kbImportMenuOpen {
		t.Fatalf("sidebar import menu state = %v %v", a.kbAddMenuOpen, a.kbImportMenuOpen)
	}
	assertLabelsOnce(t, tt.Texts(), importItems)

	tt.ClickAt(900, 500)
	if a.kbImportMenuOpen {
		t.Fatal("sidebar import menu stayed open after a press outside")
	}
	if err := tt.Click("导入知识库"); err != nil {
		t.Fatal(err)
	}
	if !a.importMenuOpen {
		t.Fatal("workbench import menu did not open")
	}
	assertLabelsOnce(t, tt.Texts(), importItems)
}

func assertLabelsOnce(t *testing.T, texts, labels []string) {
	t.Helper()
	for _, label := range labels {
		count := 0
		for _, text := range texts {
			if text == label {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("%q appears %d times: %q", label, count, texts)
		}
	}
}

// Renaming the knowledge base of the open document keeps it open, saves
// its edits first, and leaves no copy behind under the old name.
func TestRenameSavesBeforeMoving(t *testing.T) {
	a, root, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Saved = "" // an edit to write
	a.doc.Dirty = true

	a.askRename("我的知识库")
	if a.prompt == nil {
		t.Fatal("rename prompt not shown")
	}
	if err := a.prompt.OnOK("新名字"); err != nil {
		t.Fatal(err)
	}
	if a.doc == nil || !strings.HasPrefix(a.doc.Rel, "新名字/") {
		t.Fatalf("document not moved: %+v", a.doc)
	}
	if a.selected != a.doc.Rel {
		t.Fatalf("selected is %q, want %q", a.selected, a.doc.Rel)
	}
	if _, _, err := a.vault.ReadDoc(a.doc.Rel); err != nil {
		t.Fatalf("moved document unreadable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "我的知识库")); !os.IsNotExist(err) {
		t.Fatalf("old knowledge base stayed behind: %v", err)
	}
}

// A new document is untitled and shows the large title field of 语雀's
// page; the title typed there renames it.
func TestNewDocumentTitle(t *testing.T) {
	a, _, _ := newTestApp(t)
	tt := ui.NewTester(a.view, 1180, 760)

	createDocFromWorkbench(t, tt)
	if a.doc == nil || !a.doc.ShowTitle || !untitledTitle(a.doc.Title) {
		t.Fatalf("untitled page not shown: %+v", a.doc)
	}
	if a.doc.TitleDraft != "" {
		t.Fatalf("title field is not empty: %q", a.doc.TitleDraft)
	}

	tt.Type("我的新文档")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.doc.Title != "我的新文档" {
		t.Fatalf("title not applied: %q", a.doc.Title)
	}
	if want := "我的知识库/我的新文档.md"; a.doc.Rel != want {
		t.Fatalf("document is at %q, want %q", a.doc.Rel, want)
	}
	if a.selected != a.doc.Rel {
		t.Fatalf("selected is %q, want %q", a.selected, a.doc.Rel)
	}
	if _, _, err := a.vault.ReadDoc(a.doc.Rel); err != nil {
		t.Fatalf("renamed document unreadable: %v", err)
	}
	if !a.doc.ShowTitle {
		t.Fatal("title field closed after the title was applied")
	}
}

// Leaving the title field applies the title too, without Enter.
func TestTitleCommitsOnBlur(t *testing.T) {
	a, _, _ := newTestApp(t)
	tt := ui.NewTester(a.view, 1180, 760)

	createDocFromWorkbench(t, tt)
	tt.Type("标题二号")
	tt.ClickAt(900, 420) // the page below the title
	tt.Frame()
	if a.doc == nil || a.doc.Title != "标题二号" {
		t.Fatalf("title not applied on blur: %+v", a.doc)
	}
}

// A title typed on the page is saved before another document opens.
func TestTitleSavesWhenLeavingDocument(t *testing.T) {
	a, _, _ := newTestApp(t)
	tt := ui.NewTester(a.view, 1180, 760)

	createDocFromWorkbench(t, tt)
	tt.Type("离开前标题")
	if err := tt.Click("开始"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.findNode("我的知识库/离开前标题.md") == nil {
		t.Fatalf("typed title not saved: %q", tt.Texts())
	}
	if a.doc == nil || a.doc.Rel != "我的知识库/开始.md" {
		t.Fatalf("other document not opened: %+v", a.doc)
	}
}

// Renaming an untitled document from the header keeps its large title
// field showing the new name, not the placeholder.
func TestRenameUntitledDocument(t *testing.T) {
	a, _, _ := newTestApp(t)
	tt := ui.NewTester(a.view, 1180, 760)

	createDocFromWorkbench(t, tt)
	a.askRename(a.doc.Rel)
	tt.Frame()
	if a.prompt == nil {
		t.Fatal("rename prompt not shown")
	}
	a.prompt.Value = "正式标题"
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.doc == nil || a.doc.Title != "正式标题" {
		t.Fatalf("document not renamed: %+v", a.doc)
	}
	if !a.doc.ShowTitle || a.doc.TitleDraft != "正式标题" {
		t.Fatalf("title field lost the name: %+v", a.doc)
	}
}

// A document's row has its own actions, which differ from a folder's.
func TestDocumentRowMenu(t *testing.T) {
	a, _, rel := newTestApp(t)
	tt := ui.NewTester(a.view, 1180, 760)

	if err := tt.Click("我的知识库"); err != nil {
		t.Fatal(err)
	}
	if a.doc == nil || a.doc.Rel != rel {
		t.Fatalf("document not opened: %+v", a.doc)
	}
	if err := tt.Click("更多操作"); err != nil {
		t.Fatal(err)
	}
	if a.rowMenu != rel {
		t.Fatalf("row menu not open: %q", a.rowMenu)
	}
	for _, want := range []string{"重命名", "复制链接", "置顶文档", "导出 Markdown（含图片）", "导出为 PDF", "在 Finder 中显示", "删除"} {
		if !tt.HasText(want) {
			t.Fatalf("document menu lacks %q", want)
		}
	}
	if tt.HasText("新建目录") {
		t.Fatal("document menu has a folder action")
	}
	if err := tt.Click("重命名"); err != nil {
		t.Fatal(err)
	}
	if a.prompt == nil || a.prompt.Title != "重命名" {
		t.Fatalf("rename prompt not shown: %+v", a.prompt)
	}
	if a.rowMenu != "" {
		t.Fatalf("row menu stayed open: %q", a.rowMenu)
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
}

// A folder's row menu offers no document-only actions, and its add menu
// creates a document, a group or imports a file.
func TestFolderRowMenu(t *testing.T) {
	a, _, _ := newTestApp(t)
	folder, err := a.vault.CreateFolder("我的知识库", "目录甲")
	if err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	tt := ui.NewTester(a.view, 1180, 760)

	if err := tt.Click("我的知识库"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("目录甲"); err != nil {
		t.Fatal(err)
	}
	if a.selected != folder || a.doc != nil {
		t.Fatalf("folder not chosen: %q %+v", a.selected, a.doc)
	}
	if err := tt.Click("更多操作"); err != nil {
		t.Fatal(err)
	}
	if a.rowMenu != folder {
		t.Fatalf("row menu not open: %q", a.rowMenu)
	}
	for _, want := range []string{"重命名", "在 Finder 中显示", "删除"} {
		if !tt.HasText(want) {
			t.Fatalf("folder menu lacks %q", want)
		}
	}
	for _, unwanted := range []string{"复制链接", "置顶文档", "移出目录"} {
		if tt.HasText(unwanted) {
			t.Fatalf("folder menu has %q", unwanted)
		}
	}
	if err := tt.Click("更多操作"); err != nil {
		t.Fatal(err)
	}

	if err := tt.Click("添加"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"文档", "分组", "导入"} {
		if !tt.HasText(want) {
			t.Fatalf("add menu lacks %q", want)
		}
	}
	if err := tt.Click("文档"); err != nil {
		t.Fatal(err)
	}
	if a.doc == nil || !untitledTitle(a.doc.Title) || !strings.HasPrefix(a.doc.Rel, folder+"/") {
		t.Fatalf("document not created in the folder: %+v", a.doc)
	}
}

// Pinning a document moves it to the top of its folder, and moving it out
// takes it up one level.
func TestMoveAndPinDocument(t *testing.T) {
	a, _, _ := newTestApp(t)
	folder, err := a.vault.CreateFolder("我的知识库", "目录甲")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.vault.CreateDoc(folder, "甲"); err != nil {
		t.Fatal(err)
	}
	second, err := a.vault.CreateDoc(folder, "乙")
	if err != nil {
		t.Fatal(err)
	}
	a.refreshTree()

	if n := a.findNode(second); n != nil {
		a.pinDoc(n)
	}
	a.refreshTree()
	dir := a.findNode(folder)
	if dir == nil || len(dir.Children) < 2 || dir.Children[0].Name != "乙.md" {
		t.Fatalf("pin failed: %+v", dir)
	}

	if n := a.findNode(folder + "/乙.md"); n != nil {
		a.moveOut(n)
	}
	a.refreshTree()
	if a.findNode("我的知识库/乙.md") == nil {
		t.Fatalf("move out failed: %+v", a.findNode(folder))
	}
}

// A document can be a parent: its row menu creates a child document that
// the tree shows under it.
func TestDocumentAsParent(t *testing.T) {
	a, _, rel := newTestApp(t)
	tt := ui.NewTester(a.view, 1180, 760)

	if err := tt.Click("我的知识库"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("添加"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"文档", "分组", "导入"} {
		if !tt.HasText(want) {
			t.Fatalf("add menu lacks %q: %q", want, tt.Texts())
		}
	}
	if err := tt.Click("文档"); err != nil {
		t.Fatal(err)
	}
	if a.doc == nil || !untitledTitle(a.doc.Title) {
		t.Fatalf("child document not created: %+v", a.doc)
	}
	if want := "我的知识库/开始/"; !strings.HasPrefix(a.doc.Rel, want) {
		t.Fatalf("child is at %q, want under %q", a.doc.Rel, want)
	}
	parent := a.findNode(rel)
	if parent == nil || len(parent.Children) != 1 {
		t.Fatalf("parent document children are %+v", parent)
	}
	if !tt.HasText("无标题文档") {
		t.Fatalf("child document not shown in the tree: %q", tt.Texts())
	}
}

// The tree goes four levels deep, and no further.
func TestTreeDepthLimit(t *testing.T) {
	a, _, _ := newTestApp(t)
	kb := "我的知识库"
	first, err := a.vault.CreateDoc(kb, "一级")
	if err != nil {
		t.Fatal(err)
	}
	d1, _ := a.vault.EnsureDocDir(first)
	second, _ := a.vault.CreateDoc(d1, "二级")
	d2, _ := a.vault.EnsureDocDir(second)
	third, _ := a.vault.CreateDoc(d2, "三级")
	d3, _ := a.vault.EnsureDocDir(third)
	fourth, _ := a.vault.CreateDoc(d3, "四级")
	a.refreshTree()

	if !a.canNest(a.findNode(third)) {
		t.Fatal("third level cannot hold children")
	}
	if a.canNest(a.findNode(fourth)) {
		t.Fatal("fourth level can hold children")
	}
	for _, act := range a.nodeActions(a.findNode(fourth)) {
		if act.label == "新建子文档" {
			t.Fatal("fourth level offers a child action")
		}
	}
	before := a.findNode(fourth).Children
	a.newChildDoc(a.findNode(fourth))
	if len(a.findNode(fourth).Children) != len(before) {
		t.Fatal("fourth level created a child")
	}
}

// The knowledge base's row in the sidebar offers renaming, exporting and
// deleting it.
func TestSidebarKnowledgeBaseMenu(t *testing.T) {
	a, _, _ := newTestApp(t)
	tt := ui.NewTester(a.view, 1180, 760)

	if err := tt.Click("我的知识库"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("知识库操作"); err != nil {
		t.Fatal(err)
	}
	if a.rowMenu != "我的知识库" {
		t.Fatalf("knowledge base menu not open: %q", a.rowMenu)
	}
	for _, want := range []string{"重命名", "导出", "删除"} {
		if !tt.HasText(want) {
			t.Fatalf("knowledge base menu lacks %q", want)
		}
	}
	if err := tt.Click("删除"); err != nil {
		t.Fatal(err)
	}
	if a.confirm == nil || !strings.Contains(a.confirm.Title, "我的知识库") {
		t.Fatalf("knowledge base delete confirm not shown: %+v", a.confirm)
	}
	if a.rowMenu != "" {
		t.Fatalf("knowledge base menu stayed open: %q", a.rowMenu)
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
}

// The add menu's group item asks for a group, in the words it shows.
func TestAddGroup(t *testing.T) {
	a, _, _ := newTestApp(t)
	tt := ui.NewTester(a.view, 1180, 760)

	if err := tt.Click("我的知识库"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("添加"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("分组"); err != nil {
		t.Fatal(err)
	}
	if a.prompt == nil || a.prompt.Title != "新建分组" {
		t.Fatalf("group prompt not shown: %+v", a.prompt)
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
}

// The tree menu copies a resource, shallow or deep, and pastes it under a
// folder.
func TestCopyPasteMenu(t *testing.T) {
	a, _, _ := newTestApp(t)
	if _, err := a.vault.CreateFolder("我的知识库", "归档"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	tt := ui.NewTester(a.view, 1180, 760)

	if err := tt.Click("我的知识库"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("更多操作"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"仅复制本资源", "复制全部子资源"} {
		if !tt.HasText(want) {
			t.Fatalf("row menu lacks %q", want)
		}
	}
	if err := tt.Click("仅复制本资源"); err != nil {
		t.Fatal(err)
	}
	if a.clipboard == nil || a.clipboard.deep {
		t.Fatalf("shallow copy not put on the clipboard: %+v", a.clipboard)
	}

	if err := tt.Click("归档"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("更多操作"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("粘贴") {
		t.Fatalf("folder menu lacks paste: %q", tt.Texts())
	}
	if err := tt.Click("粘贴"); err != nil {
		t.Fatal(err)
	}
	if a.findNode("我的知识库/归档/开始.md") == nil {
		t.Fatalf("pasted document missing: %+v", a.findNode("我的知识库/归档"))
	}
}

// Right clicking a row opens the same menu as its More button.
func TestRightClickOpensRowMenu(t *testing.T) {
	a, _, rel := newTestApp(t)
	tt := ui.NewTester(a.view, 1180, 760)
	if err := tt.Click("我的知识库"); err != nil {
		t.Fatal(err)
	}
	if err := tt.RightClick("开始"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.rowMenu != rel {
		t.Fatalf("right click did not open the row menu: %q", a.rowMenu)
	}
	if !tt.HasText("仅复制本资源") {
		t.Fatalf("right click menu lacks the row actions: %q", tt.Texts())
	}
}

// Enter in the page's title puts the caret at the start of the text.
func TestTitleEnterFocusesBody(t *testing.T) {
	a, _, _ := newTestApp(t)
	tt := ui.NewTester(a.view, 1180, 760)
	createDocFromWorkbench(t, tt)
	tt.Type("新标题")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.doc == nil || a.doc.Title != "新标题" {
		t.Fatalf("title not applied: %+v", a.doc)
	}
	if !a.editorFocused {
		t.Fatal("the text is not focused after the title")
	}
	if anchor, caret := a.doc.Doc.Selection(); anchor != 0 || caret != 0 {
		t.Fatalf("caret is at %d, %d, want the start", anchor, caret)
	}
}

// The empty page's prompt goes away once a block is inserted, even with
// no text yet.
func TestBodyPlaceholderHidesForBlocks(t *testing.T) {
	a, _, _ := newTestApp(t)
	tt := ui.NewTester(a.view, 1180, 760)
	createDocFromWorkbench(t, tt)
	if bodyPlaceholder(a.doc) == "" {
		t.Fatal("empty page has no prompt")
	}
	a.doc.Doc.ToggleList(richtext.Tasks)
	if bodyPlaceholder(a.doc) != "" {
		t.Fatal("prompt stays over a task list")
	}
	a.doc.Doc.ToggleList(richtext.Tasks)
	if bodyPlaceholder(a.doc) == "" {
		t.Fatal("prompt did not come back")
	}
	a.doc.Doc.ToggleQuote()
	if bodyPlaceholder(a.doc) != "" {
		t.Fatal("prompt stays over a quotation")
	}
}

// createDocFromWorkbench creates a document through the workbench card
// and its location picker, choosing the default knowledge base.
func createDocFromWorkbench(t *testing.T, tt *ui.Tester) {
	t.Helper()
	if err := tt.Click("新建文档"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("创建"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
}

// Exporting Markdown makes a zip of the document and the pictures it
// shows, with the Mermaid source kept and its preview left out.
func TestExportMarkdownZip(t *testing.T) {
	a, _, rel := newTestApp(t)
	asset, err := a.vault.ImportAssetBytes(rel, "图.png", testPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.vault.Root, "我的知识库", ".assets", "手册.pdf"), []byte("%PDF-1.4"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.vault.WriteDoc(rel, "# 图\n\n![图]("+asset+")\n\n[附件](.assets/手册.pdf)\n\n```mermaid\ngraph TD\n  A --> B\n```\n"); err != nil {
		t.Fatal(err)
	}
	a.openDoc(rel)
	dst := filepath.Join(t.TempDir(), "out.zip")
	if err := a.writeMarkdownZip(dst, "开始"); err != nil {
		t.Fatal(err)
	}
	r, err := zip.OpenReader(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	names := map[string]bool{}
	var md string
	for _, f := range r.File {
		names[f.Name] = true
		if f.Name == "开始/开始.md" {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			b, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatal(err)
			}
			md = string(b)
		}
	}
	if !names["开始/开始.md"] {
		t.Fatalf("zip lacks the Markdown: %v", names)
	}
	if !strings.Contains(md, "开始.assets/") {
		t.Fatalf("Markdown does not point at the exported assets: %q", md)
	}
	if !strings.Contains(md, "```mermaid") {
		t.Fatalf("Markdown lost the Mermaid source: %q", md)
	}
	if !strings.Contains(md, "[附件](开始.assets/手册.pdf)") {
		t.Fatalf("Markdown lost the attachment link: %q", md)
	}
	if !names["开始/开始.assets/手册.pdf"] {
		t.Fatalf("zip lacks the attachment: %v", names)
	}
	if strings.Contains(md, "Mermaid 图表") {
		t.Fatalf("Markdown kept the Mermaid preview: %q", md)
	}
	found := false
	for name := range names {
		if strings.HasPrefix(name, "开始/开始.assets/") && strings.HasSuffix(name, ".png") {
			found = true
		}
	}
	if !found {
		t.Fatalf("zip lacks the image: %v", names)
	}
}

// The insert menu can also open from the menu bar shortcut, without the
// slash key.
func TestInsertMenuShortcut(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.openInsertMenu()
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	if !a.insertMenuOpen || a.insertSlash {
		t.Fatalf("insert menu did not open: %v %v", a.insertMenuOpen, a.insertSlash)
	}
	if !tt.HasText("附件") || !tt.HasText("Mermaid 图表") {
		t.Fatalf("insert menu has no commands: %q", tt.Texts())
	}
}

// A slash after text opens the block menu too: the user may type it at
// the end of a line, not only at its first character.
func TestSlashMenuAfterText(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.focusBody = true
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	n := a.doc.Doc.Text().Len()
	a.doc.Doc.SetSelection(n, n)
	tt.Frame()
	tt.Type("/")
	tt.Frame()
	if !a.insertMenuOpen || !a.insertSlash {
		t.Fatalf("no menu after a slash at the end: %q", a.doc.Doc.Text().Markdown())
	}
	if strings.HasSuffix(a.doc.Doc.Text().Markdown(), "/") {
		t.Fatalf("the slash was kept: %q", a.doc.Doc.Text().Markdown())
	}
	if !tt.HasText("附件") {
		t.Fatalf("the block menu is empty: %q", tt.Texts())
	}
	// A URL keeps its slashes.
	a.insertMenuOpen = false
	a.insertSlash = false
	a.insertQuery = ""
	a.doc.Doc.SetSelection(0, 0)
	tt.Frame()
	tt.Type("https://")
	tt.Frame()
	if a.insertMenuOpen {
		t.Fatal("a URL opened the block menu")
	}
}

// A new, untitled document opens with an empty title and asks the web
// editor to put the caret there.
func TestNewDocumentFocusesTitleInWebEditor(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	if p := a.webOpenPayload(); p.FocusTitle || p.Title != "开始" {
		t.Fatalf("normal document payload = %+v", p)
	}
	untitled, err := a.vault.CreateDoc("我的知识库", titleUntitled)
	if err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.openDoc(untitled)
	p := a.webOpenPayload()
	if !p.FocusTitle {
		t.Fatal("new document did not ask to focus the title")
	}
	if p.Title != "" {
		t.Fatalf("new document title = %q, want empty", p.Title)
	}
}

// A web document keeps no rich text model until an export asks for one.
func TestWebDocumentBuildsRichTextOnDemand(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Web = true
	a.doc.Doc = nil
	a.refreshDocFromMarkdown()
	if a.doc.Doc == nil {
		t.Fatal("the export did not build the rich text model")
	}
}

// A web document falling back to the native editor gets its rich text
// model built first.
func TestEnsureNativeDoc(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Web = true
	a.doc.Doc = nil
	a.ensureNativeDoc()
	if a.doc.Doc == nil {
		t.Fatal("the native fallback has no document")
	}
	if a.doc.Doc.Text().PlainText() == "" {
		t.Fatal("the fallback document is empty")
	}
}
