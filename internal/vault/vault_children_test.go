package vault

import (
	"testing"
)

// A document holds children in the folder beside it, which the tree shows
// under the document rather than as a row of its own.
func TestDocumentChildren(t *testing.T) {
	root := t.TempDir()
	v, err := OpenVault(root)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := v.CreateKnowledgeBase("知识库")
	if err != nil {
		t.Fatal(err)
	}
	parent, err := v.CreateDoc(kb, "父文档")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := v.EnsureDocDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if want := "知识库/父文档"; dir != want {
		t.Fatalf("children folder is %q, want %q", dir, want)
	}
	child, err := v.CreateDoc(dir, "子文档")
	if err != nil {
		t.Fatal(err)
	}

	tree, err := v.Tree()
	if err != nil {
		t.Fatal(err)
	}
	kbNode := tree[0]
	if len(kbNode.Children) != 1 {
		t.Fatalf("knowledge base has %d rows, want the document only: %+v", len(kbNode.Children), kbNode.Children)
	}
	doc := kbNode.Children[0]
	if doc.IsDir || doc.Rel != parent {
		t.Fatalf("row is %+v, want the document", doc)
	}
	if len(doc.Children) != 1 || doc.Children[0].Rel != child {
		t.Fatalf("document children are %+v, want %q", doc.Children, child)
	}

	// Renaming the document takes the children folder with it.
	renamed, err := v.Rename(parent, "新父")
	if err != nil {
		t.Fatal(err)
	}
	movedChild := "知识库/新父/子文档.md"
	if _, _, err := v.ReadDoc(movedChild); err != nil {
		t.Fatalf("child did not move with the document: %v", err)
	}
	if _, err := v.Tree(); err != nil {
		t.Fatal(err)
	}
	tree, _ = v.Tree()
	doc = tree[0].Children[0]
	if doc.Rel != renamed || len(doc.Children) != 1 || doc.Children[0].Rel != movedChild {
		t.Fatalf("renamed document is %+v", doc)
	}

	// Deleting the document takes its children to the trash, and restoring
	// brings both back.
	if err := v.Trash(renamed); err != nil {
		t.Fatal(err)
	}
	list, err := v.ListTrash()
	if err != nil || len(list) != 1 || !list[0].HasChildren {
		t.Fatalf("trash is %+v (%v)", list, err)
	}
	if err := v.RestoreTrash(list[0].ID); err != nil {
		t.Fatal(err)
	}
	tree, _ = v.Tree()
	doc = tree[0].Children[0]
	if doc.Rel != renamed || len(doc.Children) != 1 || doc.Children[0].Rel != movedChild {
		t.Fatalf("restored document is %+v", doc)
	}

	// Moving the document takes the children too.
	archive, err := v.CreateFolder(kb, "归档")
	if err != nil {
		t.Fatal(err)
	}
	moved, err := v.Move(renamed, archive, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := v.ReadDoc("知识库/归档/新父/子文档.md"); err != nil {
		t.Fatalf("child did not move with the document: %v", err)
	}
	tree, _ = v.Tree()
	var movedNode *Node
	var walk func([]*Node)
	walk = func(nodes []*Node) {
		for _, n := range nodes {
			if n.Rel == moved {
				movedNode = n
			}
			walk(n.Children)
		}
	}
	walk(tree)
	if movedNode == nil || len(movedNode.Children) != 1 {
		t.Fatalf("moved document is %+v", movedNode)
	}

	// Search finds the child under its document's folder.
	res, err := v.Search("子文档", 10)
	if err != nil || len(res) == 0 {
		t.Fatalf("search did not find the child: %+v (%v)", res, err)
	}
}
