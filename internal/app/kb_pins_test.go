package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Vaults from before the 常用 list show every knowledge base by default.
func TestKnowledgeBasesDefaultToCommon(t *testing.T) {
	a, _, _ := newTestApp(t)
	if a.cfg.PinnedKBs != nil {
		t.Fatalf("new test settings already have pins: %v", a.cfg.PinnedKBs)
	}
	if !a.isKBPinned("我的知识库") {
		t.Fatal("the default knowledge base is not common")
	}
	if len(a.pinnedKBNodes()) != len(a.tree) {
		t.Fatalf("common rows = %d, knowledge bases = %d", len(a.pinnedKBNodes()), len(a.tree))
	}
}

// The 知识库 tab shows every knowledge base, and the pin control moves one
// in and out of the sidebar's 常用 list.
func TestKnowledgeBasesTabAndPinToggle(t *testing.T) {
	a, _, _ := newTestApp(t)
	if _, err := a.createKnowledgeBase("另一个知识库"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	tt := ui.NewTester(a.view, 1180, 760)

	if err := tt.Click("知识库"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.allKBs || a.selected != "" {
		t.Fatalf("all knowledge bases did not open: allKBs=%v selected=%q", a.allKBs, a.selected)
	}
	if !tt.HasText("3 个知识库") && !tt.HasText("2 个知识库") {
		t.Fatalf("the page has no knowledge base count: %q", tt.Texts())
	}

	if err := tt.Click("取消常用：另一个知识库"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.isKBPinned("另一个知识库") {
		t.Fatal("the knowledge base is still common")
	}
	if len(a.pinnedKBNodes()) != 1 || a.pinnedKBNodes()[0].Rel != "我的知识库" {
		t.Fatalf("common rows = %+v", a.pinnedKBNodes())
	}
	if !tt.HasText("固定到常用：另一个知识库") {
		t.Fatalf("the card does not offer pinning: %q", tt.Texts())
	}

	if err := tt.Click("固定到常用：另一个知识库"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.isKBPinned("另一个知识库") || len(a.pinnedKBNodes()) != 2 {
		t.Fatalf("the knowledge base was not pinned: %+v", a.pinnedKBNodes())
	}
}

// The row menu of a common knowledge base offers 取消常用.
func TestCommonKnowledgeBaseMenuUnpins(t *testing.T) {
	a, _, _ := newTestApp(t)
	pins := []string{"我的知识库"}
	a.cfg.PinnedKBs = &pins
	a.refreshTree()
	tt := ui.NewTester(a.view, 1180, 760)
	if err := tt.RightClick("我的知识库"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.rowMenu != "我的知识库" {
		t.Fatalf("row menu = %q", a.rowMenu)
	}
	if !tt.HasText("取消常用") {
		t.Fatalf("the menu lacks 取消常用: %q", tt.Texts())
	}
	if err := tt.Click("取消常用"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.isKBPinned("我的知识库") {
		t.Fatal("the knowledge base is still common")
	}
	if len(a.pinnedKBNodes()) != 0 {
		t.Fatalf("common rows = %+v", a.pinnedKBNodes())
	}
	if !tt.HasText("还没有常用知识库") {
		t.Fatalf("the empty common state is missing: %q", tt.Texts())
	}
}

// Renaming and deleting a knowledge base keeps the saved pin list true.
func TestKnowledgeBasePinsFollowRenameAndDelete(t *testing.T) {
	a, _, _ := newTestApp(t)
	if _, err := a.createKnowledgeBase("旧库"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	pins := []string{"旧库"}
	a.cfg.PinnedKBs = &pins
	if err := a.renameNode("旧库", "新库"); err != nil {
		t.Fatal(err)
	}
	if a.cfg.PinnedKBs == nil || len(*a.cfg.PinnedKBs) != 1 || (*a.cfg.PinnedKBs)[0] != "新库" {
		t.Fatalf("pins after rename = %v", a.cfg.PinnedKBs)
	}
	a.askDelete("新库")
	if a.confirm == nil {
		t.Fatal("no delete confirmation")
	}
	a.confirm.OnOK()
	if a.cfg.PinnedKBs == nil || len(*a.cfg.PinnedKBs) != 0 {
		t.Fatalf("pins after delete = %v", a.cfg.PinnedKBs)
	}
}

// An empty explicit pin list survives a settings reload: it means "none",
// not the old default of showing every knowledge base.
func TestEmptyKnowledgeBasePinListPersists(t *testing.T) {
	a, _, _ := newTestApp(t)
	pins := []string{"我的知识库"}
	a.cfg.PinnedKBs = &pins
	if err := a.setKBPinned("我的知识库", false); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig()
	if cfg.PinnedKBs == nil {
		t.Fatal("an empty 常用 list became the default all list")
	}
	if len(*cfg.PinnedKBs) != 0 {
		t.Fatalf("pins = %v", *cfg.PinnedKBs)
	}
}

// With an explicit common list, a newly created knowledge base joins it.
func TestNewKnowledgeBaseJoinsCommonList(t *testing.T) {
	a, _, _ := newTestApp(t)
	pins := []string{"我的知识库"}
	a.cfg.PinnedKBs = &pins
	if _, err := a.createKnowledgeBase("新库"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	if !a.isKBPinned("新库") {
		t.Fatal("a new knowledge base did not join the common list")
	}
	if a.cfg.PinnedKBs == nil || len(*a.cfg.PinnedKBs) != 2 {
		t.Fatalf("pins = %v", a.cfg.PinnedKBs)
	}
}

// A card on the all-knowledge-bases page opens its knowledge base.
func TestKnowledgeBaseCardOpens(t *testing.T) {
	a, _, _ := newTestApp(t)
	if _, err := a.createKnowledgeBase("另一个知识库"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	pins := []string{"我的知识库"}
	a.cfg.PinnedKBs = &pins
	tt := ui.NewTester(a.view, 1180, 760)
	if err := tt.Click("知识库"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("另一个知识库"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.allKBs {
		t.Fatalf("opening a card stayed on the all-knowledge-bases page: selected=%q doc=%v", a.selected, a.doc)
	}
	if a.selected != "另一个知识库" {
		t.Fatalf("selected = %q", a.selected)
	}
}

// The add button inside the 知识库 tab creates or imports a knowledge base
// without opening the all-knowledge-bases page.
func TestKnowledgeBaseTabAddButtonIsSeparate(t *testing.T) {
	a, _, _ := newTestApp(t)
	tt := ui.NewTester(a.view, 1180, 760)
	if err := tt.Click("新建或导入知识库"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.kbAddMenuOpen {
		t.Fatal("the add menu did not open")
	}
	if a.allKBs {
		t.Fatal("the add button opened the all-knowledge-bases page")
	}
}
