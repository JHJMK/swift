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
	{"Bash", "bash"},
	{"Mermaid", "mermaid"},
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
	box := editBox.Bounds()
	ed := editor.Bounds()
	if ed.W <= 0 || ed.H <= 0 {
		return
	}
	for _, p := range boxes {
		st := p.Style
		if !st.CodeBlock || !p.FirstOfCode || doc.Doc.CodeBlockHidden(st.CodeBlockID) {
			continue
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
		if doc.Doc.CodeBlockFocus() == st.CodeBlockID {
			// The block's outside edge has the caret; the editor paints
			// it at the block's left, where the wrapper clips it, so show
			// the mark here too.
			ui.Box(c).Absolute().Left(left - 6).Top(top + 6).Width(2).Height(18).
				Radius(1).Background(c.Theme().Accent)
		}
		header := ui.Row(c).Key(codeHeaderKey{id: st.CodeBlockID, index: p.Index}).Absolute().
			Left(left).Top(top).Width(width).Height(richtext.CodeBlockHeaderHeight).
			Padding(0, 4).Gap(8).AlignItems(ui.Center)
		// The wheel over a header scrolls the page, as it does over the
		// code itself.
		header.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputScroll || doc.Doc == nil {
				return false
			}
			doc.Doc.ScrollBy(ev.DY)
			return true
		})
		header.Children(func() {
			a.codeBlockHeaderControls(c, doc, st, width, p.Index)
		})
	}
}

// codeBlockHeaderControls are the title field and the language menu of
// one block of code.
func (a *app) codeBlockHeaderControls(c *ui.Context, doc *docState, st richtext.ParagraphStyle, width float32, index int) {
	t := c.Theme()
	if a.codeTitleEditing == st.CodeBlockID {
		a.codeTitleInput(c, doc, st, width)
	} else {
		a.codeTitleText(c, doc, st, width)
	}
	ui.Box(c).Grow(1)

	a.codeLanguageSelect(c, doc, st)
	if strings.EqualFold(strings.TrimSpace(st.Language), "mermaid") {
		refresh := ui.ButtonBase(c).Label("重新渲染图表").Tooltip("重新渲染 Mermaid 图表").
			Width(24).Height(24).Radius(5).Justify(ui.Center).AlignItems(ui.Center)
		if refresh.Hovered() {
			refresh.Background(ui.Hex(hoverBg))
		}
		refresh.Children(func() {
			ui.Icon(c, iconRefresh).FontSize(14).TextColor(t.TextMuted)
		})
		if refresh.Clicked() {
			a.mermaidRefresh = true
			a.update()
		}
	}
	del := ui.ButtonBase(c).Label("删除代码块").Tooltip("删除代码块").
		Width(24).Height(24).Radius(5).Justify(ui.Center).AlignItems(ui.Center)
	if del.Hovered() {
		del.Background(ui.Hex("#fdecec"))
	}
	del.Children(func() {
		color := t.TextMuted
		if del.Hovered() {
			color = t.Danger
		}
		ui.Icon(c, iconTrash).FontSize(14).TextColor(color)
	})
	if del.Clicked() {
		// Delete the paragraph's block itself: an empty block starts where
		// the block above ends, and a rune lookup there takes the block
		// above.
		doc.Doc.DeleteCodeBlockAtIndex(index)
		a.codeTitleEditing = 0
		a.codeTitleFocused = false
		a.refocusEditor = true
		a.markDocDirty()
		a.update()
	}
}

// codeTitleText is the title as it reads when it is not being edited: no
// field, no border, the whole title across the header.
func (a *app) codeTitleText(c *ui.Context, doc *docState, st richtext.ParagraphStyle, width float32) {
	edit := func() {
		a.codeTitleEditing = st.CodeBlockID
		a.codeTitleFocus = true
		a.codeTitleFocused = false
		a.update()
	}
	title := strings.TrimSpace(st.CodeBlockTitle)
	if title == "" {
		// An empty title shows nothing: the fixed field's room is the
		// target, and the pointer and the tooltip say what it is.
		area := ui.Box(c).Width(codeTitleFieldWidth(width)).Height(28).Radius(5).
			Padding(2, 6).Cursor(ui.CursorPointer).
			Tooltip("请输入代码块标题").Label("请输入代码块标题").OnClick(edit)
		if area.Hovered() {
			area.Background(ui.Hex(hoverBg))
		}
		return
	}
	// A written title is the target, not the empty room to its right.
	row := ui.Row(c).Height(28).Padding(2, 6).Radius(5).
		Cursor(ui.CursorPointer).Tooltip(title).OnClick(edit)
	row.MaxWidth(max(width-150, 80))
	if row.Hovered() {
		row.Background(ui.Hex(hoverBg))
	}
	row.Children(func() {
		ui.Text(c, title).FontSize(13).SingleLine().Grow(1)
	})
}

// codeTitleInput is the title while it is edited: a field of a fixed
// width, where a long title scrolls with the caret. Leaving it shows the
// title as plain text again.
func (a *app) codeTitleInput(c *ui.Context, doc *docState, st richtext.ParagraphStyle, width float32) {
	t := c.Theme()
	title := st.CodeBlockTitle
	fieldWidth := codeTitleFieldWidth(width)
	input := ui.TextInputBase(c, &title).Label("代码块标题").Placeholder("请输入代码块标题").
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

func codeTitleFieldWidth(width float32) float32 {
	return min(float32(200), max(width-120, 120))
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
			if code == "mermaid" {
				a.mermaidRefresh = true
			}
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
			if code == "mermaid" {
				a.mermaidRefresh = true
			}
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
	case "bash":
		return "Bash"
	case "sh", "shell", "zsh", "console":
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
