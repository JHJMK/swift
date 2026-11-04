package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An external knowledge base is listed and searched where its folder is.
func TestExternalKnowledgeBaseVault(t *testing.T) {
	ext := filepath.Join(t.TempDir(), "外部")
	if err := os.MkdirAll(filepath.Join(ext, "子目录"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ext, "图.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ext, "子目录", "笔记.md"), []byte("外部内容 苹果\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	v, err := OpenVault(root)
	if err != nil {
		t.Fatal(err)
	}
	name, err := v.AddExternal("外部", ext)
	if err != nil {
		t.Fatal(err)
	}
	if name != "外部" {
		t.Fatalf("name is %q", name)
	}
	if !v.IsExternal(name + "/子目录/笔记.md") {
		t.Fatal("document is not external")
	}
	content, _, err := v.ReadDoc(name + "/子目录/笔记.md")
	if err != nil || !strings.Contains(content, "苹果") {
		t.Fatalf("external document unreadable: %q (%v)", content, err)
	}
	if _, err := v.WriteDoc(name+"/子目录/笔记.md", "改过的内容 苹果\n"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(ext, "子目录", "笔记.md")); string(b) != "改过的内容 苹果\n" {
		t.Fatalf("external file not written: %q", b)
	}
	newRel, err := v.CreateDoc(name, "新文档")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ext, "新文档.md")); err != nil {
		t.Fatalf("created document %q is missing: %v", newRel, err)
	}
	if p, ok := v.ResolveAsset(name+"/子目录/笔记.md", "../图.png"); !ok || p != filepath.Join(ext, "图.png") {
		t.Fatalf("external asset resolves to %q (%v)", p, ok)
	}
	renamed, err := v.Rename(newRel, "改名")
	if err != nil || renamed != name+"/改名.md" {
		t.Fatalf("rename external document: %q (%v)", renamed, err)
	}
	if _, err := os.Stat(filepath.Join(ext, "改名.md")); err != nil {
		t.Fatal("the external file was not renamed")
	}
	if err := v.Trash(renamed); err != nil {
		t.Fatal(err)
	}
	list, err := v.ListTrash()
	if err != nil || len(list) != 1 {
		t.Fatalf("external document trash: %+v (%v)", list, err)
	}
	if err := v.RestoreTrash(list[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ext, "改名.md")); err != nil {
		t.Fatal("the external file was not restored")
	}
	if _, err := v.CreateFolder(name, "资料"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(ext, "资料")); err != nil || !info.IsDir() {
		t.Fatalf("external folder not created: %v", err)
	}
	results, err := v.SearchIn("苹果", 10, "")
	if err != nil || len(results) != 1 || results[0].Rel != name+"/子目录/笔记.md" {
		t.Fatalf("external search: %+v (%v)", results, err)
	}

	// The reference survives reopening the vault.
	reopened, err := OpenVault(root)
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.IsExternal(name + "/子目录/笔记.md") {
		t.Fatal("external reference was not saved")
	}
	if err := reopened.RemoveExternal(name); err != nil {
		t.Fatal(err)
	}
	if reopened.IsExternal(name) {
		t.Fatal("external reference was not removed")
	}
	if _, err := os.Stat(filepath.Join(ext, "子目录", "笔记.md")); err != nil {
		t.Fatal("removing the reference deleted the file")
	}
}
