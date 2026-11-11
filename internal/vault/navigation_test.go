package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNavigationRoundTrip(t *testing.T) {
	root := t.TempDir()
	v, err := OpenVault(root)
	if err != nil {
		t.Fatal(err)
	}
	v.SetNavigation("知识库 A", "知识库 A/第三篇.md", 520)
	v.SetNavigation("知识库 B", "知识库 B/第二篇.md", -12)
	if err := v.SaveNavigation(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenVault(root)
	if err != nil {
		t.Fatal(err)
	}
	a := reopened.NavigationFor("知识库 A")
	if a.Doc != "知识库 A/第三篇.md" || a.ScrollTop != 520 {
		t.Fatalf("knowledge base A = %+v", a)
	}
	b := reopened.NavigationFor("知识库 B")
	if b.Doc != "知识库 B/第二篇.md" || b.ScrollTop != 0 {
		t.Fatalf("knowledge base B = %+v", b)
	}
}

func TestNavigationIgnoresMissingOrBrokenFile(t *testing.T) {
	root := t.TempDir()
	v, err := OpenVault(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := v.NavigationFor("知识库 A"); got.Doc != "" {
		t.Fatalf("missing file has navigation %+v", got)
	}

	filename := filepath.Join(root, metaDirName, navigationFileName)
	if err := os.WriteFile(filename, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenVault(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.NavigationFor("知识库 A"); got.Doc != "" {
		t.Fatalf("broken file has navigation %+v", got)
	}
}
