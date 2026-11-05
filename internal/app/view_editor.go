package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"
)

// editorView builds the page's title, toolbar, text and footer. The editor
// reports its caret so the slash menu can sit beside it.
func (a *app) editorView(c *ui.Context) {
	doc := a.doc
	ui.Column(c).Fill().Children(func() {
		a.docTopBar(c, doc)
		if !doc.ReadOnly {
			a.editorToolbar(c, doc)
		}
		if a.findOpen {
			a.findBar(c)
		}
		if !doc.Note && hasMermaidBlocks(doc.Doc.Text()) && mermaidCLI() == "" {
			a.mermaidHint(c)
		}
		empty := strings.TrimSpace(doc.Doc.Text().PlainText()) == ""
		ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
			ui.Row(c).Grow(1).MinWidth(0).Justify(ui.Center).AlignItems(ui.Stretch).Children(func() {
				// The page has no frame: the text sits on the window's white,
				// as 语雀's does, with only the column's margins around it.
				ui.Column(c).WidthPercent(100).MaxWidth(a.pageContentWidth()).Padding(40, a.pagePaddingX()).Children(func() {
					// A document created without a title shows 语雀's untitled
					// page: a large title line above the text.
					if doc.ShowTitle && doc.ReadOnly {
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
						if a.focusTitle {
							a.focusTitle = false
							title = title.Focus()
						} else if untitledTitle(doc.Title) {
							title = title.AutoFocus()
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
					editBox := ui.Box(c).FillWidth().Grow(1).Clip()
					editBox.Children(func() {
						el := richtext.EditorBase(c.Key(key), doc.Doc, richtext.EditorOptions{
							Label:         "文档正文",
							Placeholder:   bodyPlaceholder(doc),
							Font:          ui.Font{Size: fontSize, LineHeight: lineHeight},
							Theme:         theme,
							ReadOnly:      a.findOpen || doc.ReadOnly,
							NoScrollbar:   true,
							BottomPadding: 80,
							OnLinkCommand: a.askLink,
							OnLink:        a.openEditorLink,
							OnCaret:       a.rememberCaret,
							OnTop:         a.focusTitleFromBody,
							OnCopyCode:    a.copyCodeBlock,
							ContextMenu: func(m *ui.Menu) {
								if m.Item("粘贴").Chosen() {
									a.pending = func() { a.pasteFromMenu() }
								}
								if m.Item("粘贴并匹配样式").Chosen() {
									a.pending = func() { a.pasteAndMatchStyle() }
								}
								if doc.Doc.Formatting().Paragraph.CodeBlock {
									m.Separator()
									if m.Item("删除整个代码块").Chosen() {
										a.pending = func() {
											doc.Doc.DeleteCodeBlock()
											a.markDocDirty()
											a.update()
										}
									}
								}
							},
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
						if el.Changed() {
							a.markDocDirty()
							a.maybeSlashMenu()
						}
						a.setEditorFocused(el.Focused())
						a.codeBlockHeaders(c, editBox, el, doc)
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

// focusTitleFromBody moves the focus from the top of the body to the page
// title, so Up and Down work across the two.
func (a *app) focusTitleFromBody() {
	a.focusTitle = true
	a.update()
}

// rememberCaret keeps the caret's window position for the slash menu.
func (a *app) rememberCaret(x, y float32) {
	a.caretX, a.caretY = x, y
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

// mermaidHint tells how to install the renderer the Mermaid blocks need.
func (a *app) mermaidHint(c *ui.Context) {
	ui.Row(c).Padding(8, 20).Gap(8).AlignItems(ui.Center).
		Background(ui.Hex("#fffaf0")).
		BorderWidth(0, 0, 1, 0).BorderColor(ui.Hex("#f0e2c4")).Children(func() {
		ui.Column(c).Gap(4).Grow(1).Children(func() {
			ui.Text(c, "Mermaid 代码块暂以源码显示：未检测到 mermaid-cli。安装后重新打开文档即可预览。").
				FontSize(12).TextColor(ui.Hex("#8a6d3b"))
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "npm i -g @mermaid-js/mermaid-cli").FontSize(12).Bold().TextColor(ui.Hex("#7a5a1f"))
				if ui.Button(c, "复制命令").Padding(2, 8).Clicked() {
					mygo.Clipboard.WriteText("npm i -g @mermaid-js/mermaid-cli")
				}
				if ui.Button(c, "重新检测").Padding(2, 8).Clicked() {
					resetMermaidCLI()
					if mermaidCLI() != "" && a.doc != nil && a.doc.Rel != "" {
						rel := a.doc.Rel
						a.flushTitle()
						a.flushDoc()
						a.doc = nil
						a.openDoc(rel)
					}
					a.update()
				}
			})
		})
	})
}
