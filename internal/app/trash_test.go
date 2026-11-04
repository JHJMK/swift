package app

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func assetAbs(t *testing.T, root, docRel, assetURL string) string {
	t.Helper()
	return filepath.Join(root, filepath.FromSlash(path.Dir(docRel)), filepath.FromSlash(assetURL))
}

func mustNotExist(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("%s still exists (%v)", p, err)
	}
}

func mustExist(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("%s is missing: %v", p, err)
	}
}

// Purging a document deletes the images only it showed.
func TestPurgeTrashDeletesImages(t *testing.T) {
	a, root, rel := newTestApp(t)
	asset, err := a.vault.ImportAssetBytes(rel, "图.png", testPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.vault.WriteDoc(rel, "# 图\n\n![图]("+asset+")\n"); err != nil {
		t.Fatal(err)
	}
	p := assetAbs(t, root, rel, asset)
	mustExist(t, p)

	if err := a.vault.Trash(rel); err != nil {
		t.Fatal(err)
	}
	// Trashing keeps the images, so the document can be restored.
	mustExist(t, p)

	list, err := a.vault.ListTrash()
	if err != nil || len(list) != 1 {
		t.Fatalf("trash is %+v (%v)", list, err)
	}
	if err := a.vault.PurgeTrash(list[0].ID); err != nil {
		t.Fatal(err)
	}
	mustNotExist(t, p)
}

// An image another document still shows stays when its first user is
// purged, and goes with the last one.
func TestPurgeTrashKeepsSharedImages(t *testing.T) {
	a, root, rel := newTestApp(t)
	asset, err := a.vault.ImportAssetBytes(rel, "图.png", testPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.vault.WriteDoc(rel, "![图]("+asset+")\n"); err != nil {
		t.Fatal(err)
	}
	other, err := a.vault.CreateDoc("我的知识库", "另一篇")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.vault.WriteDoc(other, "![图]("+asset+")\n"); err != nil {
		t.Fatal(err)
	}
	p := assetAbs(t, root, rel, asset)

	if err := a.vault.Trash(rel); err != nil {
		t.Fatal(err)
	}
	list, _ := a.vault.ListTrash()
	if err := a.vault.PurgeTrash(list[0].ID); err != nil {
		t.Fatal(err)
	}
	mustExist(t, p)

	if err := a.vault.Trash(other); err != nil {
		t.Fatal(err)
	}
	list, _ = a.vault.ListTrash()
	if err := a.vault.PurgeTrash(list[0].ID); err != nil {
		t.Fatal(err)
	}
	mustNotExist(t, p)
}

// Attachments a document links to go with it too.
func TestPurgeTrashDeletesAttachments(t *testing.T) {
	a, root, rel := newTestApp(t)
	assets := filepath.Join(root, "我的知识库", ".assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	attach := filepath.Join(assets, "说明.txt")
	if err := os.WriteFile(attach, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.vault.WriteDoc(rel, "[附件](.assets/说明.txt)\n"); err != nil {
		t.Fatal(err)
	}

	if err := a.vault.Trash(rel); err != nil {
		t.Fatal(err)
	}
	list, _ := a.vault.ListTrash()
	if err := a.vault.PurgeTrash(list[0].ID); err != nil {
		t.Fatal(err)
	}
	mustNotExist(t, attach)
}

// Purging a document with children deletes the images of both.
func TestPurgeTrashDeletesChildImages(t *testing.T) {
	a, root, _ := newTestApp(t)
	kb := "我的知识库"
	parent, err := a.vault.CreateDoc(kb, "父")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := a.vault.EnsureDocDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	child, err := a.vault.CreateDoc(dir, "子")
	if err != nil {
		t.Fatal(err)
	}
	parentAsset, err := a.vault.ImportAssetBytes(parent, "父图.png", testPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	childAsset, err := a.vault.ImportAssetBytes(child, "子图.png", testPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.vault.WriteDoc(parent, "![图]("+parentAsset+")\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.vault.WriteDoc(child, "![图]("+childAsset+")\n"); err != nil {
		t.Fatal(err)
	}
	parentPath := assetAbs(t, root, parent, parentAsset)
	childPath := assetAbs(t, root, child, childAsset)

	if err := a.vault.Trash(parent); err != nil {
		t.Fatal(err)
	}
	list, _ := a.vault.ListTrash()
	if err := a.vault.PurgeTrash(list[0].ID); err != nil {
		t.Fatal(err)
	}
	mustNotExist(t, parentPath)
	mustNotExist(t, childPath)
}

// Emptying the trash purges every entry and its images.
func TestEmptyTrashDeletesAllImages(t *testing.T) {
	a, root, rel := newTestApp(t)
	assets := map[string]string{}
	for i, docRel := range []string{rel, "我的知识库/第二篇.md"} {
		if docRel != rel {
			if _, err := a.vault.CreateDoc("我的知识库", "第二篇"); err != nil {
				t.Fatal(err)
			}
		}
		asset, err := a.vault.ImportAssetBytes(docRel, "图"+string(rune('一'+i))+".png", testPNG(t))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.vault.WriteDoc(docRel, "![图]("+asset+")\n"); err != nil {
			t.Fatal(err)
		}
		assets[docRel] = assetAbs(t, root, docRel, asset)
		if err := a.vault.Trash(docRel); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range assets {
		mustExist(t, p)
	}

	if err := a.vault.EmptyTrash(); err != nil {
		t.Fatal(err)
	}
	for _, p := range assets {
		mustNotExist(t, p)
	}
	if list, _ := a.vault.ListTrash(); len(list) != 0 {
		t.Fatalf("trash is not empty: %+v", list)
	}
}

// The trash window offers emptying it.
func TestTrashModalEmptyButton(t *testing.T) {
	a, _, rel := newTestApp(t)
	if err := a.vault.Trash(rel); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(a.view, 1180, 760)
	if err := tt.Click("回收站"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("清空回收站") {
		t.Fatalf("trash window lacks the empty action: %q", tt.Texts())
	}
}
