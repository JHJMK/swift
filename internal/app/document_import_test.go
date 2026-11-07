package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/richtext"
	docx "github.com/mmonterroca/docxgo/v2"
	"github.com/mmonterroca/docxgo/v2/domain"
)

func TestImportTextDocument(t *testing.T) {
	a, _, _ := newTestApp(t)
	src := filepath.Join(t.TempDir(), "纯文本.txt")
	if err := os.WriteFile(src, []byte("\xef\xbb\xbf第一行\r\n第二行\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := a.importDocumentFile("我的知识库", src); err != nil {
		t.Fatal(err)
	}
	if a.doc == nil || a.doc.Title != "纯文本" || a.doc.Rel != "我的知识库/纯文本.md" {
		t.Fatalf("imported document is %+v", a.doc)
	}
	if want := "第一行  \n第二行"; a.doc.Markdown != want {
		t.Fatalf("imported markdown = %q, want %q", a.doc.Markdown, want)
	}
}

func TestImportTextDocumentKeepsMarkdownSyntaxLiteral(t *testing.T) {
	a, _, _ := newTestApp(t)
	src := filepath.Join(t.TempDir(), "普通文本.txt")
	if err := os.WriteFile(src, []byte("# 不是标题\nfoo_bar\n- 不是列表\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := a.importDocumentFile("我的知识库", src); err != nil {
		t.Fatal(err)
	}
	if got, want := a.doc.Doc.Text().PlainText(), "# 不是标题\nfoo_bar\n- 不是列表"; got != want {
		t.Fatalf("imported text = %q, want %q", got, want)
	}
}

func TestImportTextDocumentKeepsNumberedLinesLiteral(t *testing.T) {
	a, _, _ := newTestApp(t)
	src := filepath.Join(t.TempDir(), "普通文本.txt")
	if err := os.WriteFile(src, []byte("test\nssss\n\n1111\n1.dd\n2.ddd\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := a.importDocumentFile("我的知识库", src); err != nil {
		t.Fatal(err)
	}
	if got, want := a.doc.Markdown, "test  \nssss  \n\n1111  \n1.dd  \n2.ddd"; got != want {
		t.Fatalf("imported markdown = %q, want %q", got, want)
	}
}

func TestImportGB18030TextDocument(t *testing.T) {
	a, _, _ := newTestApp(t)
	src := filepath.Join(t.TempDir(), "中文.txt")
	// GB18030 bytes for "中文".
	if err := os.WriteFile(src, []byte{0xd6, 0xd0, 0xce, 0xc4}, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := a.importDocumentFile("我的知识库", src); err != nil {
		t.Fatal(err)
	}
	if a.doc == nil || a.doc.Saved != "中文" {
		t.Fatalf("imported text = %+v", a.doc)
	}
}

func TestImportWordDocument(t *testing.T) {
	a, root, _ := newTestApp(t)
	src := filepath.Join(t.TempDir(), "Word 导入.docx")
	doc := docx.NewDocument()

	heading, err := doc.AddParagraph()
	if err != nil {
		t.Fatal(err)
	}
	if err := heading.SetStyle(domain.StyleIDHeading1); err != nil {
		t.Fatal(err)
	}
	run, err := heading.AddRun()
	if err != nil {
		t.Fatal(err)
	}
	if err := run.SetText("一级标题"); err != nil {
		t.Fatal(err)
	}

	body, err := doc.AddParagraph()
	if err != nil {
		t.Fatal(err)
	}
	run, err = body.AddRun()
	if err != nil {
		t.Fatal(err)
	}
	if err := run.SetText("粗体"); err != nil {
		t.Fatal(err)
	}
	if err := run.SetBold(true); err != nil {
		t.Fatal(err)
	}
	run, err = body.AddRun()
	if err != nil {
		t.Fatal(err)
	}
	if err := run.SetText("与斜体"); err != nil {
		t.Fatal(err)
	}
	if err := run.SetItalic(true); err != nil {
		t.Fatal(err)
	}

	picture, err := doc.AddParagraph()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := picture.AddImageFromBytes(testPNG(t), domain.ImageFormatPNG); err != nil {
		t.Fatal(err)
	}

	table, err := doc.AddTable(2, 2)
	if err != nil {
		t.Fatal(err)
	}
	header, err := table.Row(0)
	if err != nil {
		t.Fatal(err)
	}
	for col, text := range []string{"名称", "数量"} {
		cell, err := header.Cell(col)
		if err != nil {
			t.Fatal(err)
		}
		paragraph, err := cell.AddParagraph()
		if err != nil {
			t.Fatal(err)
		}
		run, err := paragraph.AddRun()
		if err != nil {
			t.Fatal(err)
		}
		if err := run.SetText(text); err != nil {
			t.Fatal(err)
		}
	}
	row, err := table.Row(1)
	if err != nil {
		t.Fatal(err)
	}
	for col, text := range []string{"测试", "2"} {
		cell, err := row.Cell(col)
		if err != nil {
			t.Fatal(err)
		}
		paragraph, err := cell.AddParagraph()
		if err != nil {
			t.Fatal(err)
		}
		run, err := paragraph.AddRun()
		if err != nil {
			t.Fatal(err)
		}
		if err := run.SetText(text); err != nil {
			t.Fatal(err)
		}
	}
	if err := doc.SaveAs(src); err != nil {
		t.Fatal(err)
	}

	if err := a.importDocumentFile("我的知识库", src); err != nil {
		t.Fatal(err)
	}
	saved := a.doc.Saved
	for _, want := range []string{
		"# 一级标题",
		"![图片](.assets/",
		"粗体",
		"与斜体",
		"名称\t数量",
		"测试\t2",
	} {
		if !strings.Contains(saved, want) {
			t.Fatalf("Word Markdown lacks %q: %q", want, saved)
		}
	}
	var bold, italic bool
	for _, paragraph := range a.doc.Doc.Text().Paragraphs() {
		for _, run := range paragraph.Runs {
			switch run.Text {
			case "粗体":
				bold = run.Attributes.Bold()
			case "与斜体":
				italic = run.Attributes.Italic
			}
		}
	}
	if !bold || !italic {
		t.Fatalf("Word formatting was lost: %q", saved)
	}
	images, err := filepath.Glob(filepath.Join(root, "我的知识库", ".assets", "*-image1.png"))
	if err != nil || len(images) != 1 {
		t.Fatalf("imported Word images are %v (%v)", images, err)
	}
}

func TestWordInlineFormattingUsesMarkdown(t *testing.T) {
	doc := docx.NewDocument()
	a := &app{}
	tests := []struct {
		name   string
		text   string
		format func(domain.Run) error
		want   string
	}{
		{"bold", "粗体", func(run domain.Run) error { return run.SetBold(true) }, "**粗体**"},
		{"italic", "斜体", func(run domain.Run) error { return run.SetItalic(true) }, "*斜体*"},
		{"strike", "删除", func(run domain.Run) error { return run.SetStrike(true) }, "~~删除~~"},
		{"underline", "下划线", func(run domain.Run) error { return run.SetUnderline(domain.UnderlineSingle) }, "<u>下划线</u>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := doc.AddParagraph()
			if err != nil {
				t.Fatal(err)
			}
			run, err := p.AddRun()
			if err != nil {
				t.Fatal(err)
			}
			if err := run.SetText(tt.text); err != nil {
				t.Fatal(err)
			}
			if err := tt.format(run); err != nil {
				t.Fatal(err)
			}
			got, err := a.wordParagraphInline(p, "")
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("Word inline Markdown = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWordInlineFormattingCombinesAdjacentRuns(t *testing.T) {
	doc := docx.NewDocument()
	a := &app{}
	p, err := doc.AddParagraph()
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"权限动态", "申请"} {
		run, err := p.AddRun()
		if err != nil {
			t.Fatal(err)
		}
		if err := run.SetText(text); err != nil {
			t.Fatal(err)
		}
		if err := run.SetBold(true); err != nil {
			t.Fatal(err)
		}
	}

	got, err := a.wordParagraphInline(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := "**权限动态申请**"; got != want {
		t.Fatalf("Word inline Markdown = %q, want %q", got, want)
	}
}

func TestWordListMarkdownKeepsNestedLevels(t *testing.T) {
	doc := docx.NewDocument()
	parent, err := doc.AddParagraph()
	if err != nil {
		t.Fatal(err)
	}
	run, err := parent.AddRun()
	if err != nil {
		t.Fatal(err)
	}
	if err := run.SetText("一级"); err != nil {
		t.Fatal(err)
	}
	child, err := doc.AddParagraph()
	if err != nil {
		t.Fatal(err)
	}
	run, err = child.AddRun()
	if err != nil {
		t.Fatal(err)
	}
	if err := run.SetText("二级"); err != nil {
		t.Fatal(err)
	}

	a := &app{}
	parentMD, err := a.wordParagraphMarkdown(
		wordNumberedParagraph{Paragraph: parent, ref: domain.NumberingReference{ID: 1, Level: 0}},
		map[wordNumberingKey]string{{ID: 1, Level: 0}: "bullet"},
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	childMD, err := a.wordParagraphMarkdown(
		wordNumberedParagraph{Paragraph: child, ref: domain.NumberingReference{ID: 1, Level: 1}},
		map[wordNumberingKey]string{{ID: 1, Level: 1}: "bullet"},
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if parentMD != "- 一级" || childMD != "  - 二级" {
		t.Fatalf("Word list Markdown = %q, %q", parentMD, childMD)
	}

	parsed := richtext.ParseMarkdown(parentMD + "\n\n" + childMD)
	var styles []richtext.ParagraphStyle
	for _, p := range parsed.Paragraphs() {
		styles = append(styles, p.Style)
	}
	if len(styles) != 2 ||
		styles[0].List != richtext.Bullets || styles[0].Indent != 0 ||
		styles[1].List != richtext.Bullets || styles[1].Indent != 1 {
		t.Fatalf("parsed Word list styles = %+v", styles)
	}
}

func TestWordNumberingFormats(t *testing.T) {
	data := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:abstractNum w:abstractNumId="0">
    <w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/></w:lvl>
    <w:lvl w:ilvl="1"><w:numFmt w:val="decimal"/></w:lvl>
  </w:abstractNum>
  <w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>
</w:numbering>`)
	doc := wordNumberingDocument{Document: docx.NewDocument(), data: data}
	formats := wordNumberingFormats(doc)
	if formats[wordNumberingKey{ID: 1, Level: 0}] != "bullet" {
		t.Fatalf("level 0 format = %q", formats[wordNumberingKey{ID: 1, Level: 0}])
	}
	if formats[wordNumberingKey{ID: 1, Level: 1}] != "decimal" {
		t.Fatalf("level 1 format = %q", formats[wordNumberingKey{ID: 1, Level: 1}])
	}
}

type wordNumberedParagraph struct {
	domain.Paragraph
	ref domain.NumberingReference
}

func (p wordNumberedParagraph) Numbering() (domain.NumberingReference, bool) {
	return p.ref, true
}

type wordNumberingDocument struct {
	domain.Document
	data []byte
}

func (d wordNumberingDocument) NumberingPartInfo() ([]byte, string) {
	return d.data, "numbering.xml"
}
