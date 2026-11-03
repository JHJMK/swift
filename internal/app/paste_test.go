package app

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPasteImageAddsAsset(t *testing.T) {
	a, root, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Doc.SetSelection(0, 0)

	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	a.pasteImage(buf.Bytes())
	if a.errMsg != "" {
		t.Fatalf("paste failed: %s", a.errMsg)
	}
	if md := a.doc.Doc.Text().Markdown(); !strings.Contains(md, "![图片](") {
		t.Fatalf("image not inserted: %q", md)
	}
	a.flushDoc()
	if a.doc.Dirty {
		t.Fatal("document not saved")
	}

	saved, _, err := a.vault.ReadDoc(rel)
	if err != nil {
		t.Fatal(err)
	}
	const open = "![图片]("
	i := strings.Index(saved, open)
	if i < 0 {
		t.Fatalf("image missing from saved markdown: %q", saved)
	}
	rest := saved[i+len(open):]
	j := strings.Index(rest, ")")
	if j < 0 {
		t.Fatalf("malformed image markdown: %q", saved)
	}
	url := rest[:j]
	asset := filepath.Join(root, filepath.Dir(rel), filepath.FromSlash(url))
	if _, err := os.Stat(asset); err != nil {
		t.Fatalf("asset not written: %v", err)
	}
}
