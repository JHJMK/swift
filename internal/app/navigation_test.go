package app

import (
	"testing"
)

func TestKnowledgeBaseRestoresDocumentAndScroll(t *testing.T) {
	a, _, _ := newTestApp(t)
	third, err := a.vault.CreateDoc("我的知识库", "第三篇")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.vault.CreateKnowledgeBase("知识库 B"); err != nil {
		t.Fatal(err)
	}
	secondB, err := a.vault.CreateDoc("知识库 B", "第二篇")
	if err != nil {
		t.Fatal(err)
	}
	a.refreshTree()

	a.openDoc(third)
	a.webScroll(third, 520)
	a.openKB(a.findNode("知识库 B"))
	a.openDoc(secondB)
	a.webScroll(secondB, 240)

	a.openKB(a.findNode("我的知识库"))
	if a.doc == nil || a.doc.Rel != third || a.doc.ScrollTop != 520 {
		t.Fatalf("knowledge base A restored %+v", a.doc)
	}
	if got := a.webOpenPayload().ScrollTop; got != 520 {
		t.Fatalf("knowledge base A payload scroll = %v", got)
	}

	a.openKB(a.findNode("知识库 B"))
	if a.doc == nil || a.doc.Rel != secondB || a.doc.ScrollTop != 240 {
		t.Fatalf("knowledge base B restored %+v", a.doc)
	}
}

func TestKnowledgeBaseFallsBackFromStaleNavigation(t *testing.T) {
	a, _, _ := newTestApp(t)
	first := a.findNode("我的知识库").Children[0].Rel
	if _, err := a.vault.CreateKnowledgeBase("知识库 B"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.vault.CreateDoc("知识库 B", "另一篇"); err != nil {
		t.Fatal(err)
	}
	other, err := a.vault.CreateDoc("知识库 B", "第二篇")
	if err != nil {
		t.Fatal(err)
	}
	a.refreshTree()

	for _, record := range []string{"我的知识库/不存在.md", other} {
		a.vault.SetNavigation("我的知识库", record, 900)
		a.openDoc(other)
		a.openKB(a.findNode("我的知识库"))
		if a.doc == nil || a.doc.Rel != first || a.doc.ScrollTop != 0 {
			t.Fatalf("record %q restored %+v, want %q", record, a.doc, first)
		}
	}
}

func TestNavigationSurvivesWorkbenchAndRestart(t *testing.T) {
	a, root, _ := newTestApp(t)
	doc, err := a.vault.CreateDoc("我的知识库", "第三篇")
	if err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.openDoc(doc)
	a.webScroll(doc, 640)
	a.goHome()

	reopened, err := OpenVault(root)
	if err != nil {
		t.Fatal(err)
	}
	a.vault = reopened
	a.openKB(a.findNode("我的知识库"))
	if a.doc == nil || a.doc.Rel != doc || a.doc.ScrollTop != 640 {
		t.Fatalf("workbench round trip restored %+v", a.doc)
	}
}

func TestRenamingFolderKeepsNavigation(t *testing.T) {
	a, _, _ := newTestApp(t)
	folder, err := a.vault.CreateFolder("我的知识库", "分组")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := a.vault.CreateDoc(folder, "文章")
	if err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.openDoc(doc)
	a.webScroll(doc, 360)

	a.renameNode(folder, "新分组")
	newDoc := "我的知识库/新分组/文章.md"
	nav := a.vault.NavigationFor("我的知识库")
	if nav.Doc != newDoc || nav.ScrollTop != 360 {
		t.Fatalf("navigation after rename = %+v, want %q at 360", nav, newDoc)
	}
	if a.doc == nil || a.doc.Rel != newDoc || a.doc.ScrollTop != 360 {
		t.Fatalf("open document after rename = %+v", a.doc)
	}
}
