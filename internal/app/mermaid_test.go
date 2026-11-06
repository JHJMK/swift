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
	cli := mermaidCLI()
	if cli == "" {
		t.Skip("mmdc is not installed")
	}
	// A copy of mermaid-cli can be on the machine and still not render,
	// as an npx cache whose dependencies are gone is. The preview test is
	// about the app, not about repairing that install.
	if _, ok := renderMermaidPNG(cli, "graph TD\n  A --> B"); !ok {
		t.Skip("mmdc is installed but cannot render")
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

// A Mermaid block shows its picture while the caret is elsewhere, and its
// source after the picture's edit button is pressed.
func TestMermaidPreviewTogglesSource(t *testing.T) {
	cli := mermaidCLI()
	if cli == "" {
		t.Skip("mmdc is not installed")
	}
	if _, ok := renderMermaidPNG(cli, "graph TD\n  A --> B"); !ok {
		t.Skip("mmdc is installed but cannot render")
	}
	a, _, rel := newTestApp(t)
	if _, err := a.vault.WriteDoc(rel, "# 图\n\n```mermaid\ngraph TD\n  A[开始] --> B[结束]\n```\n\n后文\n"); err != nil {
		t.Fatal(err)
	}
	a.openDoc(rel)
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	var id int
	var url string
	for _, p := range a.doc.Doc.Text().Paragraphs() {
		if p.Style.CodeBlock && p.Style.Language == "mermaid" {
			id = p.Style.CodeBlockID
		}
	}
	for u := range a.doc.mermaidPreviews {
		url = u
	}
	if id == 0 || url == "" {
		t.Fatalf("no Mermaid block or preview: id=%d previews=%v", id, a.doc.mermaidPreviews)
	}
	if !a.doc.Doc.CodeBlockHidden(id) || a.doc.Doc.PreviewHidden(url) {
		t.Fatal("the block should start as a rendered picture")
	}
	tt.Move(800, 300)
	tt.Frame()
	rect, ok := tt.Find("编辑 Mermaid")
	if !ok {
		t.Fatalf("no edit button on the picture: %q", tt.Texts())
	}
	tt.ClickAt(rect.X+rect.W/2, rect.Y+rect.H/2)
	tt.Frame()
	if a.doc.Doc.CodeBlockHidden(id) || !a.doc.Doc.PreviewHidden(url) {
		t.Fatal("the source did not replace the picture")
	}
	a.doc.Doc.SetSelection(0, 0)
	tt.Frame()
	if !a.doc.Doc.CodeBlockHidden(id) {
		t.Fatal("the source did not fold back into the picture")
	}
}
