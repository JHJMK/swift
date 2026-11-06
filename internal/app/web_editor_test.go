package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The page's Markdown is what a document holds once the web editor has
// it, so a save must not write the stale rich text model.
func TestWebEditorSavesItsMarkdown(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Web = true
	a.doc.Markdown = "# 新的标题\n\n来自网页的正文\n"
	a.doc.Dirty = true
	a.flushDoc()

	got, _, err := a.vault.ReadDoc(rel)
	if err != nil {
		t.Fatal(err)
	}
	if got != a.doc.Markdown {
		t.Fatalf("saved %q, want %q", got, a.doc.Markdown)
	}
	if a.doc.Dirty {
		t.Fatal("document still dirty after saving")
	}
}

// Editing the page's inline title renames the file and its tree entry.
func TestWebEditorRenamesDocument(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Web = true
	a.doc.Markdown = "正文\n"

	result, err := a.webRename(rel, "网页改名")
	if err != nil {
		t.Fatal(err)
	}
	if result.Title != "网页改名" {
		t.Fatalf("title %q", result.Title)
	}
	if result.ID == rel || !strings.HasSuffix(result.ID, "网页改名.md") {
		t.Fatalf("id %q", result.ID)
	}
	if _, err := os.Stat(filepath.Join(a.vault.Root, filepath.FromSlash(result.ID))); err != nil {
		t.Fatalf("renamed document missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(a.vault.Root, filepath.FromSlash(rel))); !os.IsNotExist(err) {
		t.Fatalf("old document still there: %v", err)
	}
	if a.doc.Rel != result.ID || a.doc.Title != result.Title {
		t.Fatalf("document not renamed in place: %+v", a.doc)
	}
}

// The page inserts pictures and attachments through the same asset code
// as the native editor.
func TestWebEditorAssetMarkdown(t *testing.T) {
	a, root, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Web = true

	result, err := a.webSaveAsset("截图.png", []byte("png bytes"), true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.Text, "![截图.png](") || !strings.HasSuffix(result.Text, ")") {
		t.Fatalf("picture Markdown %q", result.Text)
	}
	ref := strings.TrimSuffix(strings.TrimPrefix(result.Text, "![截图.png]("), ")")
	path, ok := a.resolveWebAsset(rel, ref)
	if !ok {
		t.Fatalf("asset %q does not resolve", ref)
	}
	if !strings.HasPrefix(path, filepath.Join(root, "我的知识库", ".assets")+string(filepath.Separator)) {
		t.Fatalf("asset path %q", path)
	}
}

// Asset requests stay inside the vault.
func TestWebAssetStaysInVault(t *testing.T) {
	a, _, rel := newTestApp(t)
	if _, ok := a.resolveWebAsset(rel, "../../../../etc/passwd"); ok {
		t.Fatal("path outside the vault resolved")
	}
	if _, ok := a.resolveWebAsset(rel, ""); ok {
		t.Fatal("empty reference resolved")
	}
	if _, ok := a.resolveWebAsset(rel, "https://example.com/x.png"); ok {
		t.Fatal("remote reference resolved to a local file")
	}
}

// The quick note saves the page's Markdown, and its own title.
func TestWebEditorSavesNote(t *testing.T) {
	a, _, _ := newTestApp(t)
	if err := a.vault.WriteNote("小记", "旧内容"); err != nil {
		t.Fatal(err)
	}
	a.openNote()
	a.doc.Web = true
	a.doc.Title = "新小记"
	a.doc.Markdown = "网页里的小记\n"
	a.doc.Dirty = true
	a.flushDoc()

	title, body, err := a.vault.ReadNote()
	if err != nil {
		t.Fatal(err)
	}
	if title != "新小记" || body != a.doc.Markdown {
		t.Fatalf("note %q %q", title, body)
	}
}

// Note pictures are linked by their vault-relative .swift/note-assets
// path.
func TestWebNoteAssetResolves(t *testing.T) {
	a, root, _ := newTestApp(t)
	a.openNote()
	ref, err := a.vault.ImportNoteAssetBytes("图片.png", []byte("png"))
	if err != nil {
		t.Fatal(err)
	}
	if a.doc == nil {
		t.Fatal("note not open")
	}
	path, ok := a.resolveWebAsset("note", ref)
	if !ok {
		t.Fatalf("note asset %q does not resolve", ref)
	}
	want := filepath.Join(root, ".swift", "note-assets", filepath.Base(ref))
	if path != want {
		t.Fatalf("note asset path %q, want %q", path, want)
	}
	if _, ok := a.resolveWebAsset("note", "../config.json"); ok {
		t.Fatal("note asset escaped its folder")
	}
}
