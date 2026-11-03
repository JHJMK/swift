package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Copying a document points its images at the destination, reusing the
// asset within a knowledge base and copying it to another one.
func TestPasteRewritesImages(t *testing.T) {
	a, root, rel := newTestApp(t)
	asset, err := a.vault.ImportAssetBytes(rel, "图.png", testPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.vault.WriteDoc(rel, "# 标题\n\n![图]("+asset+")\n"); err != nil {
		t.Fatal(err)
	}
	archive, err := a.vault.CreateFolder("我的知识库", "归档")
	if err != nil {
		t.Fatal(err)
	}
	a.refreshTree()

	a.copyNode(a.findNode(rel), false)
	a.pasteInto(a.findNode(archive))
	copied := "我的知识库/归档/开始.md"
	content, _, err := a.vault.ReadDoc(copied)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "../.assets/") {
		t.Fatalf("copy does not point back at the assets: %q", content)
	}
	mustExist(t, assetAbs(t, root, rel, asset))

	// Across knowledge bases the image is copied into the destination.
	other, err := a.vault.CreateKnowledgeBase("另一个")
	if err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.copyNode(a.findNode(rel), false)
	a.openKB(a.findNode(other))
	a.pasteAt(other)
	copied2 := "另一个/开始.md"
	content2, _, err := a.vault.ReadDoc(copied2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content2, ".assets/") {
		t.Fatalf("cross knowledge base copy has no asset reference: %q", content2)
	}
	assets, err := filepath.Glob(filepath.Join(root, "另一个", ".assets", "*-图.png"))
	if err != nil || len(assets) != 1 {
		t.Fatalf("destination assets are %v (%v)", assets, err)
	}
}

// Pasting cannot push a resource below the fourth level.
func TestPasteDepthLimit(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.copyNode(a.findNode(rel), false)
	parent := "我的知识库"
	for i := 0; i < 3; i++ {
		var err error
		parent, err = a.vault.CreateFolder(parent, "层"+string(rune('一'+i)))
		if err != nil {
			t.Fatal(err)
		}
	}
	deepest, err := a.vault.CreateFolder(parent, "层四")
	if err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.pasteAt(deepest)
	if a.errMsg == "" {
		t.Fatal("paste into the fourth level did not fail")
	}
	if _, err := os.Stat(filepath.Join(a.vault.Root, filepath.FromSlash(deepest), "开始.md")); !os.IsNotExist(err) {
		t.Fatal("paste into the fourth level created a document")
	}
}
