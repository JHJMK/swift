package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"
)

// Typing ```sql and Enter makes a code block of SQL, not a plain block
// with the fence text left in it.
func TestFenceShortcutSetsLanguage(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Doc.SetText(richtext.NewText(""))
	a.focusBody = true
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()

	tt.Type("```sql")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()

	var code *richtext.ParagraphStyle
	for _, p := range a.doc.Doc.Text().Paragraphs() {
		if p.Style.CodeBlock {
			style := p.Style
			code = &style
			break
		}
	}
	if code == nil {
		t.Fatal("the fence shortcut made no block of code")
	}
	if code.Language != "sql" {
		t.Fatalf("code block language is %q, want sql", code.Language)
	}
	if !strings.Contains(a.doc.Doc.Text().Markdown(), "```sql") {
		t.Fatalf("Markdown lost the language: %q", a.doc.Doc.Text().Markdown())
	}
	if strings.Contains(a.doc.Doc.Text().PlainText(), "```") {
		t.Fatalf("the fence stayed in the text: %q", a.doc.Doc.Text().PlainText())
	}
}

// The header above a block of code edits its title and language, and the
// fence keeps both.
func TestCodeBlockHeaderTitleAndLanguage(t *testing.T) {
	a, _, rel := newTestApp(t)
	if _, err := a.vault.WriteDoc(rel, "```sql title=\"查询\"\nSELECT 1\n```\n"); err != nil {
		t.Fatal(err)
	}
	a.openDoc(rel)
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	if !tt.HasText("查询") || !tt.HasText("SQL") {
		t.Fatalf("the code header is missing: %q", tt.Texts())
	}

	// The title reads as text; clicking it opens a field of its own, and
	// Enter closes it into text again.
	if err := tt.Click("查询"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(ui.Super, ui.KeyA)
	tt.Type("新标题")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.codeTitleEditing != 0 {
		t.Fatal("the title field stayed open after Enter")
	}
	if !tt.HasText("新标题") {
		t.Fatalf("the title is not shown as text: %q", tt.Texts())
	}
	if !strings.Contains(a.doc.Doc.Text().Markdown(), `title="新标题"`) {
		t.Fatalf("the title is not in the Markdown: %q", a.doc.Doc.Text().Markdown())
	}

	// The language menu changes the fence language and the coloring.
	if err := tt.Click("SQL"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	md := a.doc.Doc.Text().Markdown()
	if !strings.Contains(md, "```go") {
		t.Fatalf("language not changed: %q", md)
	}
	if !strings.Contains(md, `title="新标题"`) {
		t.Fatalf("the title was lost with the language: %q", md)
	}
	if !tt.HasText("Go") {
		t.Fatalf("the header does not show Go: %q", tt.Texts())
	}

	// Both survive a save and a reopen.
	a.flushDoc()
	saved, _, err := a.vault.ReadDoc(rel)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(saved, "```go") || !strings.Contains(saved, `title="新标题"`) {
		t.Fatalf("saved Markdown: %q", saved)
	}
	a.doc = nil
	a.openDoc(rel)
	var got richtext.ParagraphStyle
	for _, p := range a.doc.Doc.Text().Paragraphs() {
		if p.Style.CodeBlock {
			got = p.Style
			break
		}
	}
	if got.Language != "go" || got.CodeBlockTitle != "新标题" {
		t.Fatalf("reopened style: %+v", got)
	}
}

// Clicking away from the title field ends editing, as Enter does.
func TestCodeBlockTitleBlur(t *testing.T) {
	a, _, rel := newTestApp(t)
	if _, err := a.vault.WriteDoc(rel, "```sql title=\"旧标题\"\nSELECT 1\n```\n"); err != nil {
		t.Fatal(err)
	}
	a.openDoc(rel)
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	if err := tt.Click("旧标题"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(ui.Super, ui.KeyA)
	tt.Type("新标题")
	if err := tt.Click("SQL"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.codeTitleEditing != 0 {
		t.Fatal("the title field stayed open after clicking away")
	}
	if !tt.HasText("新标题") {
		t.Fatalf("the title is not text again: %q", tt.Texts())
	}
}

// The language menu opens upward when its block of code is near the
// bottom, so it is not lost below the window.
func TestCodeBlockLanguageMenuAtBottom(t *testing.T) {
	a, _, rel := newTestApp(t)
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, fmt.Sprintf("第 %d 段：正文。", i))
	}
	lines = append(lines, "```", "select * from node", "```")
	if _, err := a.vault.WriteDoc(rel, strings.Join(lines, "\n\n")); err != nil {
		t.Fatal(err)
	}
	a.openDoc(rel)
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	tt.Scroll(700, 400, 0, 100000)
	tt.Frame()
	if err := tt.Click("纯文本"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	rect, ok := tt.Find("Go")
	if !ok {
		t.Fatalf("the language menu did not open: %q", tt.Texts())
	}
	if rect.Y < 0 || rect.Y+rect.H > 760 {
		t.Fatalf("the menu is out of the window: %+v", rect)
	}
	if err := tt.Click("Go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	var language string
	for _, p := range a.doc.Doc.Text().Paragraphs() {
		if p.Style.CodeBlock {
			language = p.Style.Language
			break
		}
	}
	if language != "go" {
		t.Fatalf("the language changed to %q", language)
	}
}
