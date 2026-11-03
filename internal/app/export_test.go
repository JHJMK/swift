package app

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/richtext"
	docx "github.com/mmonterroca/docxgo/v2"
)

func TestWriteDOCX(t *testing.T) {
	root := t.TempDir()
	v, err := OpenVault(root)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := v.CreateKnowledgeBase("知识库")
	if err != nil {
		t.Fatal(err)
	}
	rel, err := v.CreateDoc(kb, "示例")
	if err != nil {
		t.Fatal(err)
	}
	md := "# 标题\n\n正文 **粗体** 和 `代码`\n\n- 项目\n\n1. 第一\n2. 第二\n\n> 引用\n\n```\nx := 1\n```\n"
	if _, err := v.WriteDoc(rel, md); err != nil {
		t.Fatal(err)
	}

	a := &app{vault: v}
	a.openDoc(rel)
	path := filepath.Join(t.TempDir(), "out.docx")
	if err := a.writeDOCX(path); err != nil {
		t.Fatal(err)
	}

	doc, err := docx.OpenDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	var body, bullet, number, code bool
	for _, p := range doc.Paragraphs() {
		text := p.Text()
		switch {
		case strings.HasPrefix(text, "正文"):
			body = true
			var bold, mono bool
			for _, r := range p.Runs() {
				if strings.Contains(r.Text(), "粗体") && r.Bold() {
					bold = true
				}
				if strings.Contains(r.Text(), "代码") && r.Font().Name == "Menlo" {
					mono = true
				}
			}
			if !bold || !mono {
				t.Errorf("body runs: bold=%v mono=%v", bold, mono)
			}
		case text == "• 项目":
			bullet = true
		case text == "1. 第一":
			number = true
		case text == "x := 1":
			for _, r := range p.Runs() {
				if r.Font().Name == "Menlo" {
					code = true
				}
			}
		}
	}
	if !body || !bullet || !number || !code {
		t.Fatalf("missing content: body=%v bullet=%v number=%v code=%v", body, bullet, number, code)
	}

	// The reader leaves paragraph styles to the XML, so check them there.
	xml := docxDocumentXML(t, path)
	for _, want := range []string{`w:pStyle w:val="Heading1"`, `w:pStyle w:val="Quote"`} {
		if !strings.Contains(xml, want) {
			t.Errorf("document.xml lacks %s", want)
		}
	}
}

func TestExportTextEmbedsImages(t *testing.T) {
	root := t.TempDir()
	v, err := OpenVault(root)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := v.CreateKnowledgeBase("知识库")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := v.CreateFolder(kb, "子目录")
	if err != nil {
		t.Fatal(err)
	}
	rel, err := v.CreateDoc(dir, "图")
	if err != nil {
		t.Fatal(err)
	}
	asset := filepath.Join(root, kb, ".assets", "pic.png")
	if err := os.MkdirAll(filepath.Dir(asset), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(asset, []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	url, err := filepath.Rel(filepath.Join(root, filepath.FromSlash(rel), ".."), asset)
	if err != nil {
		t.Fatal(err)
	}

	text := richtext.TextOf(richtext.Paragraph{Runs: []richtext.Run{{
		Text:       string(richtext.ObjectReplacement),
		Attributes: richtext.Attributes{Attachment: &richtext.Attachment{URL: filepath.ToSlash(url), Name: "pic"}},
	}}})
	a := &app{vault: v}
	a.doc = &docState{Rel: rel, Title: "图", Doc: richtext.NewDocument(text)}

	html := a.exportText().HTML()
	if !strings.Contains(html, `src="data:image/png;base64,`) {
		t.Fatalf("image not embedded: %s", html)
	}
}

func docxDocumentXML(t *testing.T, path string) string {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		b, err := io.ReadAll(rc)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	t.Fatal("word/document.xml not found")
	return ""
}

// zipTree writes the knowledge base with its assets under its own name,
// and leaves the Finder's .DS_Store out.
func TestZipKnowledgeBase(t *testing.T) {
	root := t.TempDir()
	v, err := OpenVault(root)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := v.CreateKnowledgeBase("知识库")
	if err != nil {
		t.Fatal(err)
	}
	rel, err := v.CreateDoc(kb, "文档")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.WriteDoc(rel, "# 标题\n\n正文\n"); err != nil {
		t.Fatal(err)
	}
	assets := filepath.Join(root, kb, ".assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "图.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, kb, ".DS_Store"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "out.zip")
	if err := zipTree(filepath.Join(root, kb), kb, dst); err != nil {
		t.Fatal(err)
	}
	r, err := zip.OpenReader(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	names := map[string]bool{}
	for _, f := range r.File {
		names[f.Name] = true
	}
	for _, want := range []string{"知识库/", "知识库/文档.md", "知识库/.assets/", "知识库/.assets/图.png"} {
		if !names[want] {
			t.Errorf("archive lacks %q: %v", want, names)
		}
	}
	if names["知识库/.DS_Store"] {
		t.Error("archive includes .DS_Store")
	}
}
