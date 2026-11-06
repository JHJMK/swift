package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The left sidebar folds to an icon rail and opens again.
func TestSidebarCollapse(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	tt := ui.NewTester(a.view, 1180, 760)
	if err := tt.Click("收起侧栏"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.sidebarCollapsed {
		t.Fatal("the sidebar did not collapse")
	}
	if tt.HasText("语燕") {
		t.Fatalf("the expanded brand stayed: %q", tt.Texts())
	}
	if err := tt.Click("展开侧栏"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.sidebarCollapsed {
		t.Fatal("the sidebar did not open")
	}
	if !tt.HasText("语燕") {
		t.Fatalf("the brand did not return: %q", tt.Texts())
	}
}

// Folding the whole left side leaves only the page, and opening it keeps
// the sidebar as it was: folded here.
func TestTreeCollapseKeepsSidebarCollapsed(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	tt := ui.NewTester(a.view, 1180, 760)
	if err := tt.Click("收起侧栏"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("收起目录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.treeCollapsed {
		t.Fatal("the tree did not collapse")
	}
	if tt.HasText("我的知识库") {
		t.Fatalf("a side panel stayed: %q", tt.Texts())
	}
	if err := tt.Click("展开目录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.treeCollapsed {
		t.Fatal("the tree did not open")
	}
	if !a.sidebarCollapsed {
		t.Fatal("opening the tree opened the sidebar too")
	}
	if !tt.HasText("我的知识库") {
		t.Fatalf("the tree is not back: %q", tt.Texts())
	}
}

// Hovering the book icon of the rail lists the knowledge bases.
func TestSidebarRailKnowledgeBases(t *testing.T) {
	a, _, _ := newTestApp(t)
	if _, err := a.vault.CreateKnowledgeBase("另一个知识库"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	tt := ui.NewTester(a.view, 1180, 760)
	if err := tt.Click("收起侧栏"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	rect, ok := tt.Find("知识库")
	if !ok {
		t.Fatalf("the rail has no knowledge base icon: %q", tt.Texts())
	}
	tt.Move(rect.X+rect.W/2, rect.Y+rect.H/2)
	tt.Frame()
	if !a.kbRailOpen {
		t.Fatal("hovering the icon did not open the knowledge bases")
	}
	if err := tt.Click("另一个知识库"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.kbRailOpen {
		t.Fatal("the flyout stayed open")
	}
	if a.selected != "另一个知识库" {
		t.Fatalf("the knowledge base was not chosen: %q", a.selected)
	}
}
