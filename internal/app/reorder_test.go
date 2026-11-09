package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// treeChildNames lists the entry names of a node's children.
func treeChildNames(n *Node) []string {
	var names []string
	for _, child := range n.Children {
		names = append(names, child.Name)
	}
	return names
}

// dragRow drags the row showing fromName onto the row showing toName,
// releasing at the top or bottom strip of the target row.
func dragRow(t *testing.T, tt *ui.Tester, fromName, toName, edge string) {
	t.Helper()
	from, ok := tt.Find(fromName)
	if !ok {
		t.Fatalf("no row shows %q", fromName)
	}
	to, ok := tt.Find(toName)
	if !ok {
		t.Fatalf("no row shows %q", toName)
	}
	x := to.X + to.W/2
	y := to.Y - 3
	if edge == "bottom" {
		y = to.Y + to.H + 3
	}
	tt.Press(from.X+from.W/2, from.Y+from.H/2)
	tt.Move(from.X+from.W/2, from.Y+from.H/2+8)
	tt.Move(x, y)
	tt.Release(x, y)
	tt.Frame()
	tt.Frame()
}

// A document dropped on another row's top strip lands above it; the
// bottom strip lands it below.
func TestTreeDragReordersDocuments(t *testing.T) {
	a, _, _ := newTestApp(t)
	if _, err := a.vault.CreateDoc("我的知识库", "第二篇"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.vault.CreateDoc("我的知识库", "第三篇"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	tt := ui.NewTester(a.view, 1180, 760)
	if err := tt.Click("我的知识库"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	dragRow(t, tt, "第三篇", "第二篇", "top")
	kb := a.findNode("我的知识库")
	names := treeChildNames(kb)
	want := []string{"开始.md", "第三篇.md", "第二篇.md"}
	if len(names) != len(want) || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Fatalf("order after dragging up = %v, want %v", names, want)
	}

	dragRow(t, tt, "开始", "第三篇", "bottom")
	names = treeChildNames(a.findNode("我的知识库"))
	want = []string{"第三篇.md", "开始.md", "第二篇.md"}
	if len(names) != len(want) || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Fatalf("order after dragging down = %v, want %v", names, want)
	}
}

// A document dropped on the middle of a folder row still nests into it.
func TestTreeDragMiddleStillNests(t *testing.T) {
	a, _, _ := newTestApp(t)
	if _, err := a.vault.CreateDoc("我的知识库", "另一篇"); err != nil {
		t.Fatal(err)
	}
	folder, err := a.vault.CreateFolder("我的知识库", "分组")
	if err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	tt := ui.NewTester(a.view, 1180, 760)
	if err := tt.Click("我的知识库"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	from, ok := tt.Find("另一篇")
	if !ok {
		t.Fatal("no row shows 另一篇")
	}
	to, ok := tt.Find("分组")
	if !ok {
		t.Fatal("no row shows 分组")
	}
	x, y := to.X+to.W/2, to.Y+to.H/2
	tt.Press(from.X+from.W/2, from.Y+from.H/2)
	tt.Move(from.X+from.W/2, from.Y+from.H/2+8)
	tt.Move(x, y)
	tt.Release(x, y)
	tt.Frame()
	tt.Frame()

	nested := a.findNode(folder + "/另一篇.md")
	if nested == nil {
		t.Fatalf("document not nested into the folder: %v", treeChildNames(a.findNode("我的知识库")))
	}
}

// Knowledge bases reorder by dragging one onto another in the sidebar.
func TestSidebarDragReordersKnowledgeBases(t *testing.T) {
	a, _, _ := newTestApp(t)
	if _, err := a.vault.CreateKnowledgeBase("另一个知识库"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	tt := ui.NewTester(a.view, 1180, 760)

	dragRow(t, tt, "另一个知识库", "我的知识库", "top")
	if len(a.tree) != 2 || a.tree[0].Rel != "另一个知识库" || a.tree[1].Rel != "我的知识库" {
		var rels []string
		for _, n := range a.tree {
			rels = append(rels, n.Rel)
		}
		t.Fatalf("knowledge base order = %v", rels)
	}
}

// The first document of an empty vault names its knowledge base and
// itself as both are made.
func TestFirstDocumentDialog(t *testing.T) {
	oldConfigDir := configDirOverride
	configDirOverride = t.TempDir()
	t.Cleanup(func() { configDirOverride = oldConfigDir })
	root := t.TempDir()
	v, err := OpenVault(root)
	if err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: &Config{Workspace: root}, vault: v, openDirs: map[string]*bool{}}
	a.refreshTree()
	tt := ui.NewTester(a.view, 1180, 760)

	if err := tt.Click("新建文档"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.firstDoc == nil {
		t.Fatal("the first-document dialog did not open")
	}
	if !tt.HasText("知识库名称") || !tt.HasText("文档标题") {
		t.Fatalf("the dialog lacks its fields: %q", tt.Texts())
	}
	a.firstDoc.KB = "读书笔记"
	a.firstDoc.Title = "欢迎"
	tt.Frame()
	if err := tt.Click("创建"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.firstDoc != nil {
		t.Fatal("the dialog stayed open")
	}
	if len(a.tree) != 1 || a.tree[0].Rel != "读书笔记" {
		t.Fatalf("knowledge base not created: %v", a.tree)
	}
	if a.doc == nil || a.doc.Rel != "读书笔记/欢迎.md" {
		t.Fatalf("document not opened: %+v", a.doc)
	}
	if a.doc.Title != "欢迎" {
		t.Fatalf("document title = %q", a.doc.Title)
	}

	// With a knowledge base in place the picker takes over again.
	if err := tt.Click("工作台"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("新建文档"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.firstDoc != nil {
		t.Fatal("the first-document dialog opened with a knowledge base present")
	}
	if a.picker == nil {
		t.Fatal("the location picker did not open")
	}
}

// Dropping a node on the strip just below its previous sibling, where it
// already is, keeps its place instead of moving it to the end.
func TestTreeDragSamePlaceKeepsOrder(t *testing.T) {
	a, _, _ := newTestApp(t)
	if _, err := a.vault.CreateDoc("我的知识库", "第二篇"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.vault.CreateDoc("我的知识库", "第三篇"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	tt := ui.NewTester(a.view, 1180, 760)
	if err := tt.Click("我的知识库"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	dragRow(t, tt, "第二篇", "开始", "bottom")
	names := treeChildNames(a.findNode("我的知识库"))
	want := []string{"开始.md", "第二篇.md", "第三篇.md"}
	if len(names) != len(want) || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Fatalf("order after a same-place drop = %v, want %v", names, want)
	}
}

// Folders reorder among their siblings by dragging their rows.
func TestTreeDragReordersFolders(t *testing.T) {
	a, _, _ := newTestApp(t)
	if _, err := a.vault.CreateFolder("我的知识库", "一班"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.vault.CreateFolder("我的知识库", "二班"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	tt := ui.NewTester(a.view, 1180, 760)
	if err := tt.Click("我的知识库"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	dragRow(t, tt, "二班", "一班", "top")
	names := treeChildNames(a.findNode("我的知识库"))
	first, second := -1, -1
	for i, name := range names {
		switch name {
		case "二班":
			first = i
		case "一班":
			second = i
		}
	}
	if first < 0 || second < 0 || first > second {
		t.Fatalf("folder order = %v, want 二班 before 一班", names)
	}
}

// The tree can still be reordered when the global sidebar is collapsed
// to its icon rail, where the sidebar's own list does not run.
func TestTreeDragWithCollapsedSidebar(t *testing.T) {
	a, _, _ := newTestApp(t)
	if _, err := a.vault.CreateDoc("我的知识库", "第二篇"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.sidebarCollapsed = true
	a.selected = "我的知识库"
	a.selectedIsDir = true
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()

	dragRow(t, tt, "第二篇", "开始", "top")
	names := treeChildNames(a.findNode("我的知识库"))
	if len(names) < 2 || names[0] != "第二篇.md" || names[1] != "开始.md" {
		t.Fatalf("order with the sidebar collapsed = %v", names)
	}
}

// Dropping a knowledge base just below its previous one keeps its place.
func TestSidebarDragSamePlaceKeepsOrder(t *testing.T) {
	a, _, _ := newTestApp(t)
	if _, err := a.vault.CreateKnowledgeBase("另一个知识库"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	tt := ui.NewTester(a.view, 1180, 760)

	dragRow(t, tt, "另一个知识库", "我的知识库", "bottom")
	if len(a.tree) != 2 || a.tree[0].Rel != "我的知识库" || a.tree[1].Rel != "另一个知识库" {
		t.Fatalf("knowledge base order changed by a same-place drop: %v", a.tree)
	}
}
