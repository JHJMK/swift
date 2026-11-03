package app

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"
)

// A fenced code block keeps its language through Markdown and back.
func TestMarkdownKeepsFenceLanguage(t *testing.T) {
	const src = "```mermaid\ngraph TD\n  A --> B\n```\n"
	text := richtext.ParseMarkdown(src)
	md := text.Markdown()
	if !strings.Contains(md, "```mermaid") {
		t.Fatalf("language lost: %q", md)
	}
}

// Opening a document with a Mermaid block shows a rendered picture above
// the source, and saving keeps the source.
func TestMermaidPreview(t *testing.T) {
	if mermaidCLI() == "" {
		t.Skip("mmdc is not installed")
	}
	a, root, rel := newTestApp(t)
	const src = "# 图\n\n```mermaid\ngraph TD\n  A[开始] --> B[结束]\n```\n"
	if _, err := a.vault.WriteDoc(rel, src); err != nil {
		t.Fatal(err)
	}
	a.openDoc(rel)
	if a.doc == nil {
		t.Fatal("document not opened")
	}

	var url string
	for _, p := range a.doc.Doc.Text().Paragraphs() {
		for _, r := range p.Runs {
			if at := r.Attributes.Attachment; at != nil && at.Name == "Mermaid 图表" {
				url = at.URL
			}
		}
	}
	if url == "" {
		t.Fatalf("no Mermaid preview in the text: %q", a.doc.Doc.Text().Markdown())
	}
	img := filepath.Join(root, filepath.FromSlash(path.Dir(rel)), filepath.FromSlash(url))
	if _, err := os.Stat(img); err != nil {
		t.Fatalf("rendered diagram missing: %v", err)
	}

	a.markDocDirty()
	a.flushDoc()
	saved, _, err := a.vault.ReadDoc(rel)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(saved, "```mermaid") {
		t.Fatalf("saved Markdown lost the Mermaid block: %q", saved)
	}
	if strings.Contains(saved, "Mermaid 图表") {
		t.Fatalf("saved Markdown kept the preview: %q", saved)
	}
}

// Without mmdc, a Mermaid block shows the install hint and command.
func TestMermaidInstallHint(t *testing.T) {
	a, _, rel := newTestApp(t)
	const src = "```mermaid\ngraph TD\n  A --> B\n```\n"
	if _, err := a.vault.WriteDoc(rel, src); err != nil {
		t.Fatal(err)
	}

	mermaidCLIMu.Lock()
	oldPath, oldFound := mermaidCLIPath, mermaidCLIFound
	mermaidCLIPath, mermaidCLIFound = "", true
	mermaidCLIMu.Unlock()
	defer func() {
		mermaidCLIMu.Lock()
		mermaidCLIPath, mermaidCLIFound = oldPath, oldFound
		mermaidCLIMu.Unlock()
	}()

	a.openDoc(rel)
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	if !tt.HasText("npm i -g @mermaid-js/mermaid-cli") {
		t.Fatalf("no install hint: %q", tt.Texts())
	}
}
