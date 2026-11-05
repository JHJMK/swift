package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"
)

// codeLanguages are the languages the code block header offers, the ones
// the editor colors. 纯文本 is no language at all.
var codeLanguages = []struct{ Label, Code string }{
	{"纯文本", ""},
	{"SQL", "sql"},
	{"Go", "go"},
	{"JavaScript", "js"},
	{"TypeScript", "ts"},
	{"Python", "py"},
	{"Shell", "sh"},
	{"JSON", "json"},
	{"YAML", "yaml"},
	{"Rust", "rust"},
	{"Java", "java"},
	{"C / C++", "c"},
}

// codeHeaderKey names one code block's header across frames.
type codeHeaderKey struct {
	id, index int
}

// codeBlockHeaders puts a title field, a language choice and the copy
// control above each block of code in view, in the room the editor leaves
// for its header.
func (a *app) codeBlockHeaders(c *ui.Context, editBox, editor ui.Element, doc *docState) {
	if doc == nil || doc.Doc == nil || doc.ReadOnly || a.findOpen {
		return
	}
	boxes := doc.Doc.VisibleParagraphs()
	if len(boxes) == 0 {
		return
	}
	var paras []richtext.Paragraph
	for _, p := range doc.Doc.Text().Paragraphs() {
		paras = append(paras, p)
	}
	if len(paras) == 0 {
		return
	}
	box := editBox.Bounds()
	ed := editor.Bounds()
	if ed.W <= 0 || ed.H <= 0 {
		return
	}
	for _, p := range boxes {
		if p.Index < 0 || p.Index >= len(paras) {
			continue
		}
		st := paras[p.Index].Style
		if !st.CodeBlock {
			continue
		}
		if p.Index > 0 {
			prev := paras[p.Index-1].Style
			if prev.CodeBlock && prev.CodeBlockID == st.CodeBlockID {
				continue
			}
		}
		top := ed.Y - box.Y + p.Top - richtext.CodeBlockHeaderHeight
		if top+richtext.CodeBlockHeaderHeight <= 0 || top >= box.H {
			continue
		}
		left := ed.X - box.X + 8
		width := ed.W - 16 - 34 // leave the copy control on the right
		if width < 160 {
			width = ed.W - 16
		}
		ui.Row(c).Key(codeHeaderKey{id: st.CodeBlockID, index: p.Index}).Absolute().Left(left).Top(top).
			Width(width).Height(richtext.CodeBlockHeaderHeight).
			Padding(0, 4).Gap(8).AlignItems(ui.Center).Children(func() {
			a.codeBlockHeaderControls(c, doc, st, width)
		})
	}
}

// codeBlockHeaderControls are the title field and the language menu of
// one block of code.
func (a *app) codeBlockHeaderControls(c *ui.Context, doc *docState, st richtext.ParagraphStyle, width float32) {
	if a.codeTitleEditing == st.CodeBlockID {
		a.codeTitleInput(c, doc, st, width)
		ui.Box(c).Grow(1)
	} else {
		a.codeTitleText(c, doc, st)
	}

	a.codeLanguageSelect(c, doc, st)
}

// codeTitleText is the title as it reads when it is not being edited: no
// field, no border, the whole title across the header.
func (a *app) codeTitleText(c *ui.Context, doc *docState, st richtext.ParagraphStyle) {
	t := c.Theme()
	title := strings.TrimSpace(st.CodeBlockTitle)
	row := ui.Row(c).Grow(1).Height(28).Padding(2, 6).Radius(5).OnClick(func() {
		a.codeTitleEditing = st.CodeBlockID
		a.codeTitleFocus = true
		a.codeTitleFocused = false
		a.update()
	})
	if row.Hovered() {
		row.Background(ui.Hex(hoverBg))
	}
	row.Children(func() {
		if title == "" {
			ui.Text(c, "代码块标题").FontSize(13).TextColor(t.TextMuted).SingleLine().Grow(1)
			return
		}
		ui.Text(c, title).FontSize(13).SingleLine().Grow(1)
	})
	if title != "" {
		row.Tooltip(title)
	}
}

// codeTitleInput is the title while it is edited: a field of a fixed
// width, where a long title scrolls with the caret. Leaving it shows the
// title as plain text again.
func (a *app) codeTitleInput(c *ui.Context, doc *docState, st richtext.ParagraphStyle, width float32) {
	t := c.Theme()
	title := st.CodeBlockTitle
	fieldWidth := min(float32(200), max(width-120, 120))
	input := ui.TextInputBase(c, &title).Label("代码块标题").Placeholder("代码块标题").
		FontSize(13).Height(28).Width(fieldWidth).Padding(2, 8).Radius(5).
		Background(t.Surface).Border(1, t.Accent)
	if a.codeTitleFocus {
		input = input.Focus()
		a.codeTitleFocus = false
	}
	if input.Focused() {
		a.codeTitleFocused = true
	}
	if input.Changed() {
		doc.Doc.SetCodeBlockTitle(st.CodeBlockID, title)
		a.markDocDirty()
	}
	if input.Submitted() || (a.codeTitleFocused && !input.Focused()) {
		a.codeTitleEditing = 0
		a.codeTitleFocused = false
		a.update()
	}
}

// codeLanguageSelect is the language dropdown of a block of code. It is
// the framework's select, which handles its popup where the pointer goes
// and where the window has room.
func (a *app) codeLanguageSelect(c *ui.Context, doc *docState, st richtext.ParagraphStyle) {
	labels, byLabel := codeLanguageChoices(st.Language)
	label := codeLanguageLabel(st.Language)
	sel := ui.Select(c, &label, labels).Label("代码块语言").Width(130).Height(28)
	if sel.Changed() {
		if code, ok := byLabel[label]; ok && code != st.Language {
			doc.Doc.SetCodeBlockLanguage(st.CodeBlockID, code)
			a.markDocDirty()
			a.update()
		}
	}
}

// codeLanguageChoices are the select's options for a block whose language
// is current. A language the list does not offer is added, so the select
// shows it instead of an empty choice.
func codeLanguageChoices(current string) ([]string, map[string]string) {
	labels := make([]string, 0, len(codeLanguages)+1)
	byLabel := make(map[string]string, len(codeLanguages)+1)
	for _, language := range codeLanguages {
		labels = append(labels, language.Label)
		byLabel[language.Label] = language.Code
	}
	label := codeLanguageLabel(current)
	if _, ok := byLabel[label]; !ok {
		labels = append(labels, label)
		byLabel[label] = current
	}
	return labels, byLabel
}

// codeBlockLanguageToolbar is a second way to choose the language, in the
// editing toolbar, for when the header is out of reach.
func (a *app) codeBlockLanguageToolbar(c *ui.Context, doc *docState, id int, language string) {
	labels, byLabel := codeLanguageChoices(language)
	label := codeLanguageLabel(language)
	sel := ui.Select(c, &label, labels).Label("代码块语言").Width(120).Height(30)
	if sel.Changed() {
		if code, ok := byLabel[label]; ok && code != language {
			doc.Doc.SetCodeBlockLanguage(id, code)
			a.markDocDirty()
			a.update()
		}
	}
}

// codeLanguageLabel names a fence language as the header shows it.
func codeLanguageLabel(code string) string {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "", "text", "plain", "plaintext":
		return "纯文本"
	case "sql", "mysql", "postgres", "postgresql", "sqlite":
		return "SQL"
	case "go", "golang":
		return "Go"
	case "js", "javascript", "jsx", "node":
		return "JavaScript"
	case "ts", "typescript", "tsx":
		return "TypeScript"
	case "py", "python", "python3":
		return "Python"
	case "sh", "bash", "shell", "zsh", "console":
		return "Shell"
	case "json":
		return "JSON"
	case "yaml", "yml":
		return "YAML"
	case "rust", "rs":
		return "Rust"
	case "java":
		return "Java"
	case "c", "cpp", "c++", "cs", "csharp":
		return "C / C++"
	case "mermaid":
		return "Mermaid"
	}
	return strings.TrimSpace(code)
}
