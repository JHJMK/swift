package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestVault(t *testing.T) *Vault {
	t.Helper()
	v, err := OpenVault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestVaultCreateAndTree(t *testing.T) {
	v := newTestVault(t)
	kb, err := v.CreateKnowledgeBase("产品")
	if err != nil {
		t.Fatal(err)
	}
	folder, err := v.CreateFolder(kb, "需求")
	if err != nil {
		t.Fatal(err)
	}
	docRel, err := v.CreateDoc(folder, "登录")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.WriteDoc(docRel, "# 登录\n\n内容"); err != nil {
		t.Fatal(err)
	}

	tree, err := v.Tree()
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 1 || tree[0].Name != "产品" {
		t.Fatalf("tree = %#v", tree)
	}
	child := tree[0].Children[0]
	if child.Title() != "需求" || !child.IsDir {
		t.Fatalf("folder = %#v", child)
	}
	if got := child.Children[0].Title(); got != "登录" {
		t.Fatalf("doc title = %q", got)
	}

	// Order survives reopening the vault.
	reopened, err := OpenVault(v.Root)
	if err != nil {
		t.Fatal(err)
	}
	tree2, err := reopened.Tree()
	if err != nil {
		t.Fatal(err)
	}
	if tree2[0].Children[0].Name != "需求" {
		t.Fatalf("order not saved: %#v", tree2)
	}
}

func TestVaultRename(t *testing.T) {
	v := newTestVault(t)
	kb, _ := v.CreateKnowledgeBase("知识库")
	doc, _ := v.CreateDoc(kb, "旧标题")

	newDoc, err := v.Rename(doc, "新标题")
	if err != nil {
		t.Fatal(err)
	}
	if newDoc != "知识库/新标题.md" {
		t.Fatalf("newDoc = %q", newDoc)
	}
	if _, err := os.Stat(filepath.Join(v.Root, "知识库", "旧标题.md")); !os.IsNotExist(err) {
		t.Fatal("old file still exists")
	}
	if _, err := os.Stat(filepath.Join(v.Root, "知识库", "新标题.md")); err != nil {
		t.Fatal(err)
	}
}

func TestVaultMoveAndOrder(t *testing.T) {
	v := newTestVault(t)
	kb, _ := v.CreateKnowledgeBase("库")
	a, _ := v.CreateFolder(kb, "A")
	b, _ := v.CreateFolder(kb, "B")
	doc, _ := v.CreateDoc(kb, "文档")

	// Move the document into A.
	moved, err := v.Move(doc, a, "")
	if err != nil {
		t.Fatal(err)
	}
	if moved != "库/A/文档.md" {
		t.Fatalf("moved = %q", moved)
	}
	if _, err := os.Stat(filepath.Join(v.Root, "库", "A", "文档.md")); err != nil {
		t.Fatal(err)
	}

	// Move B into A, before the document.
	movedB, err := v.Move(b, a, "文档.md")
	if err != nil {
		t.Fatal(err)
	}
	if movedB != "库/A/B" {
		t.Fatalf("movedB = %q", movedB)
	}
	tree, _ := v.Tree()
	names := []string{}
	for _, n := range tree[0].Children[0].Children {
		names = append(names, n.Name)
	}
	if len(names) != 2 || names[0] != "B" || names[1] != "文档.md" {
		t.Fatalf("children order = %v", names)
	}

	// Reorder within A: document before B.
	if _, err := v.Move("库/A/文档.md", a, "B"); err != nil {
		t.Fatal(err)
	}
	tree, _ = v.Tree()
	if got := tree[0].Children[0].Children[0].Name; got != "文档.md" {
		t.Fatalf("first child = %q", got)
	}

	// A folder cannot move into itself.
	if _, err := v.Move(a, "库/A/B", ""); err == nil {
		t.Fatal("expected an error moving a folder into itself")
	}
}

func TestVaultTrashAndRestore(t *testing.T) {
	v := newTestVault(t)
	kb, _ := v.CreateKnowledgeBase("库")
	doc, _ := v.CreateDoc(kb, "草稿")
	if err := v.Trash(doc); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(v.Root, "库", "草稿.md")); !os.IsNotExist(err) {
		t.Fatal("document still exists")
	}
	list, err := v.ListTrash()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].OriginalRel != "库/草稿.md" {
		t.Fatalf("trash = %#v", list)
	}
	if err := v.RestoreTrash(list[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(v.Root, "库", "草稿.md")); err != nil {
		t.Fatal(err)
	}
	if list, _ := v.ListTrash(); len(list) != 0 {
		t.Fatalf("trash not empty: %#v", list)
	}
}

func TestVaultSearch(t *testing.T) {
	v := newTestVault(t)
	kb, _ := v.CreateKnowledgeBase("库")
	doc, _ := v.CreateDoc(kb, "部署说明")
	_, _ = v.WriteDoc(doc, "服务器使用 Go 编写\n支持 macOS")

	results, err := v.Search("go", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Rel != doc {
		t.Fatalf("results = %#v", results)
	}
	if results, _ := v.Search("macos", 10); len(results) != 1 {
		t.Fatalf("content search = %#v", results)
	}
	if results, _ := v.Search("不存在", 10); len(results) != 0 {
		t.Fatalf("unexpected results = %#v", results)
	}
}

func TestVaultUniqueNames(t *testing.T) {
	v := newTestVault(t)
	kb, _ := v.CreateKnowledgeBase("库")
	first, _ := v.CreateDoc(kb, "同名")
	second, _ := v.CreateDoc(kb, "同名")
	if first == second {
		t.Fatalf("duplicate names: %q and %q", first, second)
	}
}
