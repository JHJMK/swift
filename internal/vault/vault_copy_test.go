package vault

import (
	"os"
	"path/filepath"
	"testing"
)

// Copying a document either takes its children or leaves them behind.
func TestVaultCopyDocument(t *testing.T) {
	root := t.TempDir()
	v, err := OpenVault(root)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := v.CreateKnowledgeBase("知识库")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := v.CreateDoc(kb, "文档")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.WriteDoc(doc, "# 文档\n\n正文\n"); err != nil {
		t.Fatal(err)
	}
	dir, err := v.EnsureDocDir(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.CreateDoc(dir, "子文档"); err != nil {
		t.Fatal(err)
	}
	archive, err := v.CreateFolder(kb, "归档")
	if err != nil {
		t.Fatal(err)
	}

	shallow, err := v.Copy(doc, archive, false)
	if err != nil {
		t.Fatal(err)
	}
	if shallow != "知识库/归档/文档.md" {
		t.Fatalf("shallow copy is %q", shallow)
	}
	if _, err := os.Stat(filepath.Join(root, "知识库", "归档", "文档")); !os.IsNotExist(err) {
		t.Fatal("shallow copy made a children folder")
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(shallow)))
	if err != nil || string(b) != "# 文档\n\n正文\n" {
		t.Fatalf("copy content is %q (%v)", b, err)
	}

	deep, err := v.Copy(doc, archive, true)
	if err != nil {
		t.Fatal(err)
	}
	if deep != "知识库/归档/文档 2.md" {
		t.Fatalf("deep copy is %q", deep)
	}
	if _, _, err := v.ReadDoc("知识库/归档/文档 2/子文档.md"); err != nil {
		t.Fatalf("deep copy lacks the child: %v", err)
	}

	// A folder copies empty or with everything below it.
	folder, err := v.CreateFolder(kb, "目录")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.CreateDoc(folder, "甲"); err != nil {
		t.Fatal(err)
	}
	sub, err := v.CreateFolder(folder, "子")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.CreateDoc(sub, "乙"); err != nil {
		t.Fatal(err)
	}
	empty, err := v.Copy(folder, kb, false)
	if err != nil {
		t.Fatal(err)
	}
	if empty != "知识库/目录 2" {
		t.Fatalf("empty folder copy is %q", empty)
	}
	if entries, err := os.ReadDir(filepath.Join(root, "知识库", "目录 2")); err != nil || len(entries) != 0 {
		t.Fatalf("empty copy has %d entries (%v)", len(entries), err)
	}
	full, err := v.Copy(folder, kb, true)
	if err != nil {
		t.Fatal(err)
	}
	if full != "知识库/目录 3" {
		t.Fatalf("deep folder copy is %q", full)
	}
	for _, rel := range []string{"知识库/目录 3/甲.md", "知识库/目录 3/子/乙.md"} {
		if _, _, err := v.ReadDoc(rel); err != nil {
			t.Fatalf("deep folder copy lacks %q: %v", rel, err)
		}
	}

	if _, err := v.Copy(folder, folder, true); err == nil {
		t.Fatal("copied a folder into itself")
	}
	if _, err := v.Copy(folder, "知识库/目录/子", true); err == nil {
		t.Fatal("copied a folder into its child")
	}
}
