package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"
)

func (a *app) editorView(c *ui.Context) {
	t := c.Theme()
	doc := a.doc
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Padding(topInset+10, 24).Gap(6).AlignItems(ui.Center).DragWindow().Children(func() {
			ui.Column(c).Gap(1).Grow(1).MinWidth(0).Children(func() {
				status := "已保存"
				if doc.Dirty {
					status = "未保存"
				}
				meta := fmt.Sprintf("%d 字 · %s", a.wordCount(), status)
				if doc.Note {
					meta = "小记 · " + meta
				} else if where := strings.ReplaceAll(parentRel(doc.Rel), "/", " / "); where != "" {
					meta += " · " + where
				}
				ui.Text(c, meta).FontSize(13).TextColor(t.TextMuted).SingleLine()
			})
			if doc.Note {
				if ui.PrimaryButton(c, "保存到").Padding(5, 11).Clicked() {
					a.askNoteSaveTo()
				}
			} else {
				if ui.Button(c, "重命名").Padding(5, 11).Clicked() {
					a.askRename(doc.Rel)
				}
				export := menuButton(c, "导出", &a.exportMenuOpen)
				dropdown(c, export, &a.exportMenuOpen, func() {
					menuItem(c, nil, "Markdown（含图片）", func() {
						a.exportMenuOpen = false
						a.exportDoc("md")
						a.update()
					})
					menuItem(c, nil, "导出为 PDF", func() {
						a.exportMenuOpen = false
						a.exportDoc("pdf")
						a.update()
					})
					menuItem(c, nil, "Word (.docx)", func() {
						a.exportMenuOpen = false
						a.exportDoc("docx")
						a.update()
					})
				})
			}
		})
		ui.Box(c).Height(1).Background(t.Border)
		a.toolbar(c, doc)
		if a.findOpen {
			a.findBar(c)
		}
		if !doc.Note && hasMermaidBlocks(doc.Doc.Text()) && mermaidCLI() == "" {
			a.mermaidHint(c)
		}
		ui.Box(c).Height(1).Background(t.Border)
		empty := strings.TrimSpace(doc.Doc.Text().PlainText()) == ""
		ui.Row(c).Grow(1).Justify(ui.Center).AlignItems(ui.Stretch).Children(func() {
			// The page has no frame: the text sits on the window's white,
			// as 语雀's does, with only the column's margins around it.
			ui.Column(c).WidthPercent(100).MaxWidth(840).Padding(40, 48).Children(func() {
				// A document created without a title shows 语雀's untitled
				// page: a large title line above the text.
				if doc.ShowTitle {
					title := ui.TextInputBase(c, &doc.TitleDraft).
						Placeholder("请输入标题").
						PlaceholderColor(ui.Hex("#c3c7c6")).
						FontSize(38).Bold().FillWidth()
					if untitledTitle(doc.Title) {
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
				el := richtext.EditorBase(c.Key(key), doc.Doc, richtext.EditorOptions{
					Label:         "文档正文",
					Placeholder:   bodyPlaceholder(doc),
					Font:          ui.Font{Size: 16, LineHeight: 1.75},
					Theme:         theme,
					ReadOnly:      a.findOpen,
					OnLinkCommand: a.askLink,
					ContextMenu: func(m *ui.Menu) {
						if m.Item("粘贴").Chosen() {
							a.pending = func() { a.pasteFromMenu() }
						}
						if m.Item("粘贴并匹配样式").Chosen() {
							a.pending = func() { a.pasteAndMatchStyle() }
						}
					},
				}).FillWidth().Grow(1)
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
				}
				a.setEditorFocused(el.Focused())
			})
		})
	})
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

func (a *app) toolbar(c *ui.Context, doc *docState) {
	t := c.Theme()
	f := doc.Doc.Formatting()
	button := func(label, tip string, on bool, run func()) ui.Element {
		b := ui.Button(c, label).KeepFocus().Padding(5, 8).MinWidth(28).Tooltip(tip)
		if on {
			b.Background(t.Accent.Alpha(0.16)).TextColor(t.Accent)
		}
		if b.Clicked() {
			run()
			a.markDocDirty()
		}
		return b
	}
	divider := func() { ui.Box(c).Width(1).Height(18).Background(t.Border) }
	ui.Row(c).Padding(6, 20).Gap(3).Wrap().AlignItems(ui.Center).
		Background(ui.Hex("#fbfcfc")).Children(func() {
		headings := []string{"正文", "标题 1", "标题 2", "标题 3"}
		current := "正文"
		if f.Paragraph.Heading >= 1 && f.Paragraph.Heading <= 3 && !f.ParagraphsMixed {
			current = headings[f.Paragraph.Heading]
		}
		if ui.Select(c, &current, headings).KeepFocus().Width(112).Label("段落样式").Changed() {
			for i, name := range headings {
				if name == current {
					doc.Doc.SetHeading(i)
					a.markDocDirty()
					break
				}
			}
		}
		divider()
		button("B", "加粗（⌘B）", f.Bold == richtext.FormatOn, doc.Doc.ToggleBold).Bold()
		button("I", "斜体（⌘I）", f.Italic == richtext.FormatOn, doc.Doc.ToggleItalic).Italic()
		button("U", "下划线（⌘U）", f.Underline == richtext.FormatOn, doc.Doc.ToggleUnderline).Underline()
		button("S", "删除线（⇧⌘X）", f.Strikethrough == richtext.FormatOn, doc.Doc.ToggleStrikethrough).Strikethrough()
		button("</>", "行内代码（⌘E）", f.Code == richtext.FormatOn, doc.Doc.ToggleCode)
		button("链接", "链接（⌘K）", f.Link != "", a.askLink)
		divider()
		button("•", "无序列表", f.Paragraph.List == richtext.Bullets, func() { doc.Doc.ToggleList(richtext.Bullets) })
		button("1.", "有序列表", f.Paragraph.List == richtext.Numbers, func() { doc.Doc.ToggleList(richtext.Numbers) })
		button("☐", "待办列表", f.Paragraph.List == richtext.Tasks, func() { doc.Doc.ToggleList(richtext.Tasks) })
		button("❝", "引用", f.Paragraph.Quote, doc.Doc.ToggleQuote)
		button("{}", "代码块", f.Paragraph.CodeBlock, doc.Doc.ToggleCodeBlock)
		divider()
		if ui.Button(c, "图片").KeepFocus().Tooltip("插入图片（也可直接 ⌘V 粘贴）").Clicked() {
			a.insertImage()
		}
	})
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
