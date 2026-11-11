package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"
)

// editorView builds the page's title, toolbar, text and footer. The editor
// reports its caret so the slash menu can sit beside it.
func (a *app) editorView(c *ui.Context) {
	if a.isWebDoc() {
		a.webEditorView(c)
		return
	}
	doc := a.doc
	ui.Column(c).Fill().Children(func() {
		a.docTopBar(c, doc)
		if !doc.ReadOnly && !a.previewMode {
			a.editorToolbar(c, doc)
		}
		if a.findOpen {
			a.findBar(c)
		}
		empty := strings.TrimSpace(doc.Doc.Text().PlainText()) == ""
		ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
			ui.Row(c).Grow(1).MinWidth(0).Justify(ui.Center).AlignItems(ui.Stretch).Children(func() {
				// The page has no frame: the text sits on the window's white,
				// as 语雀's does, with only the column's margins around it.
				ui.Column(c).WidthPercent(100).MaxWidth(a.pageContentWidth()).Padding(40, a.pagePaddingX()).Children(func() {
					// A document created without a title shows 语雀's untitled
					// page: a large title line above the text.
					if doc.ShowTitle && (doc.ReadOnly || a.previewMode) {
						ui.Text(c, doc.Title).FontSize(36).Bold().FillWidth()
						ui.Box(c).Height(20)
					} else if doc.ShowTitle {
						title := ui.TextInputBase(c, &doc.TitleDraft).
							Placeholder("请输入标题").
							PlaceholderColor(ui.Hex("#c3c7c6")).
							FontSize(36).Bold().FillWidth()
						title = title.OnTextKey(func(k ui.TextKey) bool {
							if k.Mods != 0 {
								return false
							}
							switch k.Key {
							case ui.KeyDown:
								a.pending = func() {
									a.focusBody = true
									a.update()
								}
								return true
							case ui.KeyUp:
								return true
							}
							return false
						})
						if untitledTitle(doc.Title) {
							title = title.AutoFocus()
							a.refocusEditor = false
						}
						if title.Submitted() {
							// The rename may rebuild the editor under a new
							// key, so put the caret in the text next frame.
							a.pending = func() {
								a.focusBody = true
								a.update()
							}
							a.deferred = a.commitDocTitle
							a.update()
						} else if doc.titleFocused && !title.Focused() {
							a.deferred = a.commitDocTitle
							a.update()
						}
						doc.titleFocused = title.Focused()
						ui.Box(c).Height(20)
					}
					theme := &richtext.Theme{ParagraphSpacing: 0.75}
					for i := range theme.Headings {
						theme.Headings[i].Top = 1
						theme.Headings[i].Bottom = 0.6
					}
					if empty {
						theme.Muted = ui.Hex("#c3c7c6")
					}
					key := doc.Rel
					if doc.Note {
						key = "note"
					}
					fontSize, lineHeight := a.pageFont(doc)
					// The editor and the code blocks' headers, which sit
					// over the room the editor leaves above each block.
					// The eight DIPs at the left leave the block's outer
					// caret outside its background.
					editBox := ui.Box(c).FillWidth().Grow(1).Padding(0, 0, 0, 8).Clip()
					editBox.Children(func() {
						el := richtext.EditorBase(c.Key(key), doc.Doc, richtext.EditorOptions{
							Label:         "文档正文",
							Placeholder:   bodyPlaceholder(doc),
							Font:          ui.Font{Size: fontSize, LineHeight: lineHeight},
							Theme:         theme,
							ReadOnly:      a.findOpen || doc.ReadOnly || a.previewMode,
							OnLinkCommand: a.askLink,
							OnLink:        a.openEditorLink,
						}).FillWidth().FillHeight()

						if a.focusBody {
							a.focusBody = false
							doc.Doc.SetSelection(0, 0)
							el.Focus()
						}
						if a.findRefocus {
							a.findRefocus = false
							el.Focus()
						}
						if a.refocusEditor {
							a.refocusEditor = false
							el.Focus()
						}
						if el.Changed() {
							a.markDocDirty()
							a.rememberNativeCaret(editBox, el, doc)
							a.maybeSlashMenu()
						}
						a.setEditorFocused(el.Focused())
					})
				})
			})
			if a.outlineOpen {
				a.outlineSidebar(c, doc)
			}
		})
		a.editorFooter(c)
	})
}

// webEditorView is the document page drawn by the web view. The page's
// own header holds its title and actions; the web view fills the rest.
func (a *app) webEditorView(c *ui.Context) {
	doc := a.doc
	ui.Column(c).Fill().Children(func() {
		a.docTopBar(c, doc)
		ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
			ui.WebView(c, a.webView).Grow(1)
		})
	})
}

// rememberNativeCaret estimates the caret's window position for the
// slash menu. The rich text editor no longer reports it.
func (a *app) rememberNativeCaret(_ ui.Element, editor ui.Element, doc *docState) {
	if doc == nil || doc.Doc == nil {
		return
	}
	boxes := doc.Doc.VisibleParagraphs()
	if len(boxes) == 0 {
		return
	}
	_, caret := doc.Doc.Selection()
	target := boxes[0]
	for _, box := range boxes {
		if box.Start <= caret {
			target = box
		} else {
			break
		}
	}
	ed := editor.Bounds()
	if ed.W <= 0 || ed.H <= 0 {
		return
	}
	a.caretX = ed.X + 12
	a.caretY = ed.Y + target.Top + max(target.Height, 18)
}

// bodyPlaceholder is the prompt of an empty page, hidden once the page
// has text or a block such as a task, a list or a quotation.
func bodyPlaceholder(doc *docState) string {
	if strings.TrimSpace(doc.Doc.Text().PlainText()) != "" {
		return ""
	}
	p := doc.Doc.Formatting().Paragraph
	if p.List != 0 || p.Quote || p.CodeBlock || p.Heading > 0 {
		return ""
	}
	return "直接输入正文，也可选择一个模板："
}

func (a *app) askLink() {
	if a.doc == nil {
		return
	}
	a.prompt = &promptState{
		Title:   "链接",
		Label:   "网址",
		Value:   a.doc.Doc.Formatting().Link,
		OKLabel: "应用",
		OnOK: func(value string) error {
			a.doc.Doc.SetLink(value)
			a.markDocDirty()
			return nil
		},
	}
}

// findBar is the editor's find strip, below the toolbar.
func (a *app) findBar(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Padding(6, 20).Gap(8).AlignItems(ui.Center).
		Background(ui.Hex("#f7f8fa")).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		ui.Icon(c, iconSearch).FontSize(14).TextColor(t.TextMuted)
		input := ui.TextInput(c, &a.findQuery).Label("查找").
			Placeholder("在文档中查找").Width(240)
		if a.findFocus {
			input = input.AutoFocus()
			a.findFocus = false
		}
		if input.Changed() {
			a.runFind()
		}
		if input.Submitted() {
			a.findNext(1)
		}
		count := "无结果"
		if len(a.findMatches) > 0 {
			count = fmt.Sprintf("%d/%d", a.findIndex+1, len(a.findMatches))
		}
		ui.Text(c, count).FontSize(12).TextColor(t.TextMuted).Width(44)
		if ui.Button(c, "上一个").Padding(3, 8).Clicked() {
			a.findNext(-1)
		}
		if ui.Button(c, "下一个").Padding(3, 8).Clicked() {
			a.findNext(1)
		}
		if ui.Button(c, "关闭").Padding(3, 8).Clicked() {
			a.closeFind()
		}
	})
}
