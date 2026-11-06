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

// An empty code block title shows nothing; its fixed field's room is
// clickable, with a hint, and opens the title field.
func TestCodeBlockTitleEmpty(t *testing.T) {
	a, _, rel := newTestApp(t)
	if _, err := a.vault.WriteDoc(rel, "```\nselect 1\n```\n"); err != nil {
		t.Fatal(err)
	}
	a.openDoc(rel)
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	if _, ok := tt.Find("代码块标题"); ok {
		t.Fatalf("the empty title still shows a placeholder: %q", tt.Texts())
	}
	if !tt.HasText("请输入代码块标题") {
		t.Fatalf("the empty title has no hint: %q", tt.Texts())
	}
	if err := tt.Click("请输入代码块标题"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.codeTitleEditing == 0 {
		t.Fatal("clicking the empty title did not open its field")
	}
}

// The wheel over a code block's title scrolls the editor, as over the
// code itself.
func TestCodeBlockTitleScrollsEditor(t *testing.T) {
	a, _, rel := newTestApp(t)
	var lines []string
	lines = append(lines, "```sql title=\"查询\"", "select 1", "```")
	for i := 0; i < 70; i++ {
		lines = append(lines, "", fmt.Sprintf("第 %d 段：正文。", i))
	}
	if _, err := a.vault.WriteDoc(rel, strings.Join(lines, "\n")); err != nil {
		t.Fatal(err)
	}
	a.openDoc(rel)
	a.doc.Doc.SetSelection(0, 0)
	a.focusBody = true
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	before := a.doc.Doc.VisibleParagraphs()
	rect, ok := tt.Find("查询")
	if !ok {
		t.Fatalf("the code title is not in view: %q", tt.Texts())
	}
	tt.Scroll(rect.X+rect.W/2, rect.Y+rect.H/2, 0, 300)
	tt.Frame()
	after := a.doc.Doc.VisibleParagraphs()
	if len(before) == 0 || len(after) == 0 {
		t.Fatalf("no paragraphs: %+v -> %+v", before, after)
	}
	if after[0].Top >= before[0].Top-1 {
		t.Fatalf("the wheel over the title did not scroll: %+v -> %+v", before[0], after[0])
	}
}

// The header's delete control removes the block at once, and the edit
// undoes.
func TestCodeBlockDeleteButton(t *testing.T) {
	a, _, rel := newTestApp(t)
	if _, err := a.vault.WriteDoc(rel, "```sql\nSELECT 1;\n```\n"); err != nil {
		t.Fatal(err)
	}
	a.openDoc(rel)
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	if err := tt.Click("删除代码块"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, p := range a.doc.Doc.Text().Paragraphs() {
		if p.Style.CodeBlock {
			t.Fatal("the block of code stayed")
		}
	}
	tt.Key(ui.Super, ui.KeyZ)
	tt.Frame()
	found := false
	for _, p := range a.doc.Doc.Text().Paragraphs() {
		if p.Style.CodeBlock {
			found = true
		}
	}
	if !found {
		t.Fatalf("undo did not bring the block back: %q", a.doc.Doc.Text().Markdown())
	}
}

// Cmd+A in a block of code selects that block, not the whole page.
func TestCodeBlockSelectAll(t *testing.T) {
	a, _, rel := newTestApp(t)
	if _, err := a.vault.WriteDoc(rel, "开头\n\n```\nline1\nline2\n```\n\n结尾\n"); err != nil {
		t.Fatal(err)
	}
	a.openDoc(rel)
	a.focusBody = true
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	plain := []rune(a.doc.Doc.Text().PlainText())
	at := 0
	for i := 0; i+5 <= len(plain); i++ {
		if string(plain[i:i+5]) == "line1" {
			at = i
			break
		}
	}
	a.doc.Doc.SetSelection(at+2, at+2)
	tt.Frame()
	tt.Key(ui.Super, ui.KeyA)
	s, e := a.doc.Doc.Selection()
	selected := string(plain[s:e])
	if !strings.Contains(selected, "line1") || !strings.Contains(selected, "line2") {
		t.Fatalf("the code was not selected: %q", selected)
	}
	if strings.Contains(selected, "开头") || strings.Contains(selected, "结尾") {
		t.Fatalf("the selection left the block: %q", selected)
	}

	a.doc.Doc.SetSelection(0, 0)
	tt.Frame()
	tt.Key(ui.Super, ui.KeyA)
	s, e = a.doc.Doc.Selection()
	if s != 0 || e != len(plain) {
		t.Fatalf("outside a block Cmd+A selected %d..%d of %d", s, e, len(plain))
	}
}

// A bash fence is laballed Bash and keeps its source.
func TestCodeBlockBashLanguage(t *testing.T) {
	a, _, rel := newTestApp(t)
	if _, err := a.vault.WriteDoc(rel, "```bash\necho hi\n```\n"); err != nil {
		t.Fatal(err)
	}
	a.openDoc(rel)
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	if !tt.HasText("Bash") {
		t.Fatalf("the bash block is not labelled Bash: %q", tt.Texts())
	}
	if !strings.Contains(a.doc.Doc.Text().Markdown(), "```bash") {
		t.Fatalf("the fence language is gone: %q", a.doc.Doc.Text().Markdown())
	}
}

// Deleting the lower of two blocks leaves the one above it, even when
// they are next to each other.
func TestCodeBlockDeleteSecond(t *testing.T) {
	a, _, rel := newTestApp(t)
	md := "```text\nfirst\n```\n\n```text\n\n```\n\n正文\n"
	if _, err := a.vault.WriteDoc(rel, md); err != nil {
		t.Fatal(err)
	}
	a.openDoc(rel)
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	r, ok := tt.Find("删除代码块")
	if !ok {
		t.Fatal("no delete button")
	}
	boxes := a.doc.Doc.VisibleParagraphs()
	if len(boxes) < 2 {
		t.Fatalf("no second block: %+v", boxes)
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2+(boxes[1].Top-boxes[0].Top))
	tt.Frame()
	md = a.doc.Doc.Text().Markdown()
	if !strings.Contains(md, "first") {
		t.Fatalf("the upper block was deleted too: %q", md)
	}
	if strings.Contains(md, "```text\n\n```") {
		t.Fatalf("the lower block stayed: %q", md)
	}
}

// Mermaid is one of the languages the header offers, and choosing it asks
// for a render.
func TestMermaidLanguageChoice(t *testing.T) {
	for _, l := range codeLanguages {
		if l.Code == "mermaid" {
			if l.Label != "Mermaid" {
				t.Fatalf("mermaid is labelled %q", l.Label)
			}
			goto found
		}
	}
	t.Fatal("Mermaid is not in the language list")
found:
	a, _, rel := newTestApp(t)
	if _, err := a.vault.WriteDoc(rel, "```text\nselect 1\n```\n"); err != nil {
		t.Fatal(err)
	}
	a.openDoc(rel)
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	if err := tt.Click("纯文本"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Mermaid"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	var lang string
	for _, p := range a.doc.Doc.Text().Paragraphs() {
		if p.Style.CodeBlock {
			lang = p.Style.Language
		}
	}
	if lang != "mermaid" {
		t.Fatalf("the language is %q", lang)
	}
}
