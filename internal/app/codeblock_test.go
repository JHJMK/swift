package app

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"
)

// Enter in a block of code keeps the blank line in the block; Cmd+Enter is
// what leaves it.
func TestCodeBlockBlankLines(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Doc.SetText(richtext.ParseMarkdown("```go\nfmt.Println(\"a\")\n```\n"))
	a.focusBody = true
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()

	pos := utf8.RuneCountInString(`fmt.Println("a")`)
	a.doc.Doc.SetSelection(pos, pos)
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Key(0, ui.KeyEnter)
	if got := a.doc.Doc.Formatting().Paragraph; !got.CodeBlock {
		t.Fatalf("Enter left the code block: %+v", got)
	}
	if !strings.Contains(a.doc.Doc.Text().PlainText(), "fmt.Println(\"a\")\n\n") {
		t.Fatalf("the blank line was not kept in the code: %q", a.doc.Doc.Text().PlainText())
	}

	tt.Key(ui.Super, ui.KeyEnter)
	if a.doc.Doc.Formatting().Paragraph.CodeBlock {
		t.Fatal("Cmd+Enter did not leave the code block")
	}
}

// Backspace and Delete never turn a code line into ordinary text; they join
// code lines and stop at the block's edge.
func TestCodeBlockBackspaceAndDeleteStay(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Doc.SetText(richtext.ParseMarkdown("```go\nline1\nline2\n```\n\nafter\n"))
	a.focusBody = true
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()

	text := a.doc.Doc.Text().PlainText()
	idx := strings.Index(text, "line2")
	pos := utf8.RuneCountInString(text[:idx])
	a.doc.Doc.SetSelection(pos, pos)
	tt.Frame()
	tt.Key(0, ui.KeyBackspace)
	if !a.doc.Doc.Formatting().Paragraph.CodeBlock {
		t.Fatal("Backspace left the code block")
	}
	if got := a.doc.Doc.Text().PlainText(); !strings.Contains(got, "line1line2") {
		t.Fatalf("Backspace did not join the code lines: %q", got)
	}

	a.doc.Doc.SetText(richtext.ParseMarkdown("```go\nline1\nline2\n```\n"))
	a.focusBody = true
	tt.Frame()
	a.doc.Doc.SetSelection(utf8.RuneCountInString("line1"), utf8.RuneCountInString("line1"))
	tt.Frame()
	tt.Key(0, ui.KeyDelete)
	if !a.doc.Doc.Formatting().Paragraph.CodeBlock {
		t.Fatal("Delete left the code block")
	}
	if got := a.doc.Doc.Text().PlainText(); !strings.Contains(got, "line1line2") {
		t.Fatalf("Delete did not join the code lines: %q", got)
	}

	before := a.doc.Doc.Text().PlainText()
	a.doc.Doc.SetSelection(0, 0)
	tt.Frame()
	tt.Key(0, ui.KeyBackspace)
	if !a.doc.Doc.Formatting().Paragraph.CodeBlock {
		t.Fatal("Backspace at the block's edge left the code block")
	}
	if got := a.doc.Doc.Text().PlainText(); got != before {
		t.Fatalf("Backspace at the edge changed the code: %q", got)
	}
}

// Up and Down still leave a block of code at its edges.
func TestCodeBlockArrowsExit(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Doc.SetText(richtext.ParseMarkdown("before\n\n```go\nline1\nline2\n```\n\nafter\n"))
	a.focusBody = true
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()

	text := a.doc.Doc.Text().PlainText()
	first := strings.Index(text, "line1")
	pos := utf8.RuneCountInString(text[:first])
	a.doc.Doc.SetSelection(pos, pos)
	tt.Frame()
	tt.Key(0, ui.KeyUp) // leave the code block for the body above
	if a.doc.Doc.Formatting().Paragraph.CodeBlock {
		t.Fatalf("Up stayed in the code block at %d", pos)
	}

	last := strings.Index(text, "line2") + len("line2")
	pos = utf8.RuneCountInString(text[:last])
	a.doc.Doc.SetSelection(pos, pos)
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	if a.doc.Doc.Formatting().Paragraph.CodeBlock {
		t.Fatalf("Down stayed in the code block at %d", pos)
	}
}

// Arrow keys pass the empty lines of a code block to the body around it.
func TestCodeBlockArrowBlankEdge(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Doc.SetText(richtext.ParseMarkdown("before\n\n```go\nline1\n\n```\n\nafter\n"))
	a.focusBody = true
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()

	text := a.doc.Doc.Text().PlainText()
	pos := utf8.RuneCountInString(text[:strings.Index(text, "line1")+len("line1")])
	// Down from the content line reaches the empty code line.
	a.doc.Doc.SetSelection(pos, pos)
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	if !a.doc.Doc.Formatting().Paragraph.CodeBlock {
		t.Fatal("Down left the code block too early")
	}
	// Down again should leave it.
	tt.Key(0, ui.KeyDown)
	if a.doc.Doc.Formatting().Paragraph.CodeBlock {
		t.Fatalf("Down stayed in the code block after the empty line: %q", a.doc.Doc.Text().PlainText())
	}
}

func TestCodeBlockArrowTwoBlanks(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Doc.SetText(richtext.ParseMarkdown("before\n\n```go\nline1\n\n\n```\n\nafter\n"))
	a.focusBody = true
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	text := a.doc.Doc.Text().PlainText()
	pos := utf8.RuneCountInString(text[:strings.Index(text, "line1")+len("line1")])
	a.doc.Doc.SetSelection(pos, pos)
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	if a.doc.Doc.Formatting().Paragraph.CodeBlock {
		t.Fatalf("Down stayed in the code block after two blanks: %q", a.doc.Doc.Text().PlainText())
	}
}

// Arrows inside a block of code still move between its lines.
func TestCodeBlockArrowsInside(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Doc.SetText(richtext.ParseMarkdown("```go\nline1\nline2\n```\n\nafter\n"))
	a.focusBody = true
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	text := a.doc.Doc.Text().PlainText()
	pos := utf8.RuneCountInString(text[:strings.Index(text, "line2")])
	a.doc.Doc.SetSelection(pos, pos)
	tt.Frame()
	tt.Key(0, ui.KeyUp)
	if !a.doc.Doc.Formatting().Paragraph.CodeBlock {
		t.Fatal("Up left the code block from its middle")
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	if a.doc.Doc.Formatting().Paragraph.CodeBlock {
		t.Fatal("Down did not leave the code block at its end")
	}
}

// At the document's edge, Up and Down make a body paragraph so the code
// block can be left even when nothing surrounds it.
func TestCodeBlockArrowAtDocumentEdge(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Doc.SetText(richtext.ParseMarkdown("```go\nline1\n```\n"))
	a.focusBody = true
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	a.doc.Doc.SetSelection(utf8.RuneCountInString("line1"), utf8.RuneCountInString("line1"))
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	if a.doc.Doc.Formatting().Paragraph.CodeBlock {
		t.Fatalf("Down stayed in a code block that ends the document: %q", a.doc.Doc.Text().PlainText())
	}
	if !hasCodeLine(a.doc.Doc.Text(), "line1") {
		t.Fatalf("Down turned the code line into body text: %q", a.doc.Doc.Text().PlainText())
	}

	a.doc.Doc.SetText(richtext.ParseMarkdown("```go\nline1\n```\n"))
	a.focusBody = true
	tt.Frame()
	a.doc.Doc.SetSelection(0, 0)
	tt.Frame()
	beforeText := a.doc.Doc.Text().PlainText()
	tt.Key(0, ui.KeyUp)
	if a.doc.Doc.CodeBlockFocus() == 0 {
		t.Fatalf("Up did not focus the code block's outer edge: %q", a.doc.Doc.Text().PlainText())
	}
	if got := a.doc.Doc.Text().PlainText(); got != beforeText {
		t.Fatalf("Up inserted a paragraph outside the code: %q", got)
	}
	if !hasCodeLine(a.doc.Doc.Text(), "line1") {
		t.Fatalf("Up turned the code line into body text: %q", a.doc.Doc.Text().PlainText())
	}
	tt.Key(0, ui.KeyDown)
	if a.doc.Doc.CodeBlockFocus() != 0 || !a.doc.Doc.Formatting().Paragraph.CodeBlock {
		t.Fatalf("Down did not return to the code: %q", a.doc.Doc.Text().PlainText())
	}
}

// The quick note ends with a block of code; Down makes a body paragraph
// after it, as the note's quick page has no other paragraph.
func TestNoteCodeBlockArrowExit(t *testing.T) {
	a, _, _ := newTestApp(t)
	body := "```go\nfmt.Println(\"a\")\n\n```\n"
	if err := a.vault.WriteNote("小记", body); err != nil {
		t.Fatal(err)
	}
	a.openNote()
	if a.doc == nil || !a.doc.Note {
		t.Fatal("note did not open")
	}
	a.focusBody = true
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	end := utf8.RuneCountInString(a.doc.Doc.Text().PlainText())
	a.doc.Doc.SetSelection(end, end)
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	if a.doc.Doc.Formatting().Paragraph.CodeBlock {
		t.Fatalf("Down stayed in the note's code block: %q", a.doc.Doc.Text().PlainText())
	}
	if !hasCodeLine(a.doc.Doc.Text(), "fmt.Println") {
		t.Fatalf("Down turned the note's code line into body text: %q", a.doc.Doc.Text().PlainText())
	}
}

func hasCodeLine(t richtext.Text, want string) bool {
	for _, p := range t.Paragraphs() {
		if p.Style.CodeBlock && strings.Contains(paraText(p), want) {
			return true
		}
	}
	return false
}

// Two code blocks that touch stay two fenced blocks in Markdown.
func TestCodeBlocksStaySeparate(t *testing.T) {
	md := "```go\nfmt.Println(\"a\")\n```\n\n```sql\nselect 1;\n```\n"
	text := richtext.ParseMarkdown(md)
	out := text.Markdown()
	if got := strings.Count(out, "```"); got != 4 {
		t.Fatalf("Markdown has %d fences, want 4: %q", got, out)
	}
	if !strings.Contains(out, "```go") || !strings.Contains(out, "```sql") {
		t.Fatalf("Markdown lost a code block language: %q", out)
	}

	// Making a paragraph below a code block into code keeps the two apart.
	text = richtext.ParseMarkdown("```go\nfmt.Println(\"a\")\n```\n\nbody\n")
	plain := text.PlainText()
	pos := utf8.RuneCountInString(plain[:strings.Index(plain, "body")])
	doc := richtext.NewDocument(text)
	doc.SetSelection(pos, pos)
	doc.ToggleCodeBlock()
	out = doc.Text().Markdown()
	if got := strings.Count(out, "```"); got != 4 {
		t.Fatalf("a new code block merged with the one above: %d fences in %q", got, out)
	}
}

// A second code block typed below the first stays its own block.
func TestNewCodeBlockBelowStaysSeparate(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Doc.SetText(richtext.ParseMarkdown("```go\nline1\n```\n"))
	a.focusBody = true
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	a.doc.Doc.SetSelection(utf8.RuneCountInString("line1"), utf8.RuneCountInString("line1"))
	tt.Frame()
	tt.Key(ui.Super, ui.KeyEnter) // leave the first block
	tt.Type("```")
	tt.Key(0, ui.KeyEnter)
	tt.Type("select 1;")
	out := a.doc.Doc.Text().Markdown()
	if got := strings.Count(out, "```"); got != 4 {
		t.Fatalf("the second code block merged with the first: %d fences in %q", got, out)
	}
}

// Enter on the code block's outer edge inserts a paragraph before it, so
// the block and what follows move down.
func TestCodeBlockEdgeEnterPushesDown(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Doc.SetText(richtext.ParseMarkdown("```go\nline1\n```\n\nafter\n"))
	a.focusBody = true
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	a.doc.Doc.SetSelection(0, 0)
	tt.Frame()
	tt.Key(0, ui.KeyUp)
	if a.doc.Doc.CodeBlockFocus() == 0 {
		t.Fatal("Up did not focus the code block edge")
	}
	tt.Key(0, ui.KeyEnter)
	if a.doc.Doc.Formatting().Paragraph.CodeBlock {
		t.Fatalf("Enter left the caret in the code block: %q", a.doc.Doc.Text().PlainText())
	}
	if !hasCodeLine(a.doc.Doc.Text(), "line1") {
		t.Fatalf("Enter turned the code line into body text: %q", a.doc.Doc.Text().PlainText())
	}
	text := a.doc.Doc.Text().PlainText()
	if !strings.HasPrefix(text, "\n") || !strings.Contains(text, "line1\n") || !strings.Contains(text, "after") {
		t.Fatalf("the code block and what follows did not move down: %q", text)
	}
}

// Adjacent code blocks: Left at the lower block's start does nothing, and
// Up gives the lower block the same outer-edge focus.
func TestAdjacentCodeBlockEdges(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Doc.SetText(richtext.ParseMarkdown("```go\na\n```\n\n```sql\nb\n```\n"))
	a.focusBody = true
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	text := a.doc.Doc.Text().PlainText()
	pos := utf8.RuneCountInString(text[:strings.Index(text, "b")])
	a.doc.Doc.SetSelection(pos, pos)
	tt.Frame()
	anchor, caret := a.doc.Doc.Selection()
	tt.Key(0, ui.KeyLeft)
	if aAnchor, aCaret := a.doc.Doc.Selection(); aAnchor != anchor || aCaret != caret {
		t.Fatalf("Left crossed into the code block above: %d,%d -> %d,%d", anchor, caret, aAnchor, aCaret)
	}
	plain := a.doc.Doc.Text().PlainText()
	tt.Key(0, ui.KeyBackspace)
	if got := a.doc.Doc.Text().PlainText(); got != plain {
		t.Fatalf("Backspace merged the adjacent code blocks: %q", got)
	}

	upperEnd := utf8.RuneCountInString(plain[:strings.Index(plain, "a")+1])
	a.doc.Doc.SetSelection(upperEnd, upperEnd)
	tt.Frame()
	plain = a.doc.Doc.Text().PlainText()
	tt.Key(0, ui.KeyDelete)
	if got := a.doc.Doc.Text().PlainText(); got != plain {
		t.Fatalf("Delete merged the adjacent code blocks: %q", got)
	}

	lowerID := 0
	for _, p := range a.doc.Doc.Text().Paragraphs() {
		if p.Style.CodeBlock && strings.Contains(paraText(p), "b") {
			lowerID = p.Style.CodeBlockID
		}
	}
	a.doc.Doc.SetSelection(pos, pos)
	tt.Frame()
	tt.Key(0, ui.KeyUp)
	if got := a.doc.Doc.CodeBlockFocus(); got == 0 || got != lowerID {
		t.Fatalf("Up focused %d, want the lower block %d", got, lowerID)
	}
	tt.Key(0, ui.KeyDown)
	if a.doc.Doc.CodeBlockFocus() != 0 || !a.doc.Doc.Formatting().Paragraph.CodeBlock {
		t.Fatal("Down did not return to the lower code block")
	}
}

// Backspace in the empty paragraph below a code block removes that
// paragraph, not the code block's own last empty line.
func TestBackspaceBelowCodeKeepsCodeBlank(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Doc.SetText(richtext.ParseMarkdown("```sh\nps\n\n```\n"))
	a.focusBody = true
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	text := a.doc.Doc.Text().PlainText()
	end := strings.Index(text, "ps") + len("ps") + 1
	a.doc.Doc.SetSelection(end, end)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyEnter)
	tt.Frame()
	var paras []richtext.Paragraph
	for _, p := range a.doc.Doc.Text().Paragraphs() {
		paras = append(paras, p)
	}
	if len(paras) != 3 || !paras[1].Style.CodeBlock || paras[2].Style.CodeBlock {
		t.Fatalf("unexpected paragraphs after leaving the code block: %+v", paras)
	}
	tt.Key(0, ui.KeyBackspace)
	paras = paras[:0]
	for _, p := range a.doc.Doc.Text().Paragraphs() {
		paras = append(paras, p)
	}
	if len(paras) != 2 || !paras[0].Style.CodeBlock || !paras[1].Style.CodeBlock {
		t.Fatalf("Backspace deleted the code block's last line: %+v", paras)
	}
	if got := paraText(paras[1]); got != "" {
		t.Fatalf("the code block's last line changed: %q", got)
	}
}

// Delete at the start of an empty code block removes the whole block,
// leaving the surrounding paragraphs.
func TestDeleteEmptyCodeBlock(t *testing.T) {
	cases := []struct {
		name string
		md   string
	}{
		{"only", "```go\n```\n"},
		{"between", "before\n\n```go\n```\n\nafter\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, _, rel := newTestApp(t)
			a.openDoc(rel)
			a.doc.Doc.SetText(richtext.ParseMarkdown(tc.md))
			a.focusBody = true
			tt := ui.NewTester(a.view, 1180, 760)
			tt.Frame()
			off := 0
			start := -1
			for _, p := range a.doc.Doc.Text().Paragraphs() {
				if p.Style.CodeBlock && start < 0 {
					start = off
				}
				off += utf8.RuneCountInString(paraText(p)) + 1
			}
			if start < 0 {
				t.Fatalf("no empty code block parsed from %q", tc.md)
			}
			a.doc.Doc.SetSelection(start, start)
			tt.Frame()
			tt.Key(0, ui.KeyDelete)
			if a.doc.Doc.Formatting().Paragraph.CodeBlock {
				t.Fatalf("Delete left the empty code block: %q", a.doc.Doc.Text().PlainText())
			}
			if strings.Contains(a.doc.Doc.Text().Markdown(), "```go") {
				t.Fatalf("code fence survived Delete: %q", a.doc.Doc.Text().Markdown())
			}
		})
	}
}

// A code block with content can be deleted as a whole, from the document
// API or with ⌘Delete, keeping the surrounding text.
func TestDeleteNonEmptyCodeBlock(t *testing.T) {
	md := "before\n\n```go\nline1\nline2\n```\n\nafter\n"
	for _, cmd := range []bool{false, true} {
		a, _, rel := newTestApp(t)
		a.openDoc(rel)
		a.doc.Doc.SetText(richtext.ParseMarkdown(md))
		a.focusBody = true
		tt := ui.NewTester(a.view, 1180, 760)
		tt.Frame()
		text := a.doc.Doc.Text().PlainText()
		pos := utf8.RuneCountInString(text[:strings.Index(text, "line1")])
		a.doc.Doc.SetSelection(pos, pos)
		tt.Frame()
		if cmd {
			tt.Key(ui.Cmd, ui.KeyDelete)
		} else {
			a.doc.Doc.DeleteCodeBlock()
		}
		if a.doc.Doc.Formatting().Paragraph.CodeBlock {
			t.Fatalf("code block survived deletion (cmd=%v): %q", cmd, a.doc.Doc.Text().PlainText())
		}
		md := a.doc.Doc.Text().Markdown()
		if strings.Contains(md, "```go") || strings.Contains(md, "line1") {
			t.Fatalf("code block content survived deletion (cmd=%v): %q", cmd, md)
		}
		if !strings.Contains(md, "before") || !strings.Contains(md, "after") {
			t.Fatalf("surrounding text lost (cmd=%v): %q", cmd, md)
		}
	}
}
