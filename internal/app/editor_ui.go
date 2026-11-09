package app

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"
)

// docTopBar is the document page's header: its title and save state on the
// left, and the outline, preview, width and more actions on the right.
func (a *app) docTopBar(c *ui.Context, doc *docState) {
	t := c.Theme()
	ui.Row(c).Padding(topInset+8, 20, 8, 20).Gap(4).AlignItems(ui.Center).
		Background(t.Surface).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).
		DragWindow().Children(func() {
		ui.Column(c).Gap(1).Grow(1).MinWidth(0).Children(func() {
			title := doc.Title
			if title == "" {
				title = "未命名"
			}
			if doc.Note {
				title = "小记"
			}
			ui.Text(c, title).FontSize(15).Bold().SingleLine()
			status := "已保存"
			if doc.Dirty {
				status = "自动保存中…"
			} else if !doc.MTime.IsZero() {
				status += " " + doc.MTime.Format("15:04:05")
			}
			if a.vault.IsExternal(doc.Rel) {
				status += " · 外部引用"
			}
			if doc.Note {
				status = "小记 · " + status
			}
			ui.Text(c, status).FontSize(12).TextColor(t.TextMuted).SingleLine()
		})
		if doc.Note {
			if ui.PrimaryButton(c, "保存到").Padding(5, 11).Clicked() {
				a.askNoteSaveTo()
			}
			return
		}
		outline := a.chromeIconButton(c, iconList, "大纲", a.outlineOpen)
		if outline.Clicked() {
			a.outlineOpen = !a.outlineOpen
			if a.isWebDoc() {
				a.sendWebCommand("outline", a.outlineOpen)
			}
			a.update()
		}
		width := a.chromeIconButton(c, iconWidth, "宽窄模式", a.narrowMode)
		if width.Clicked() {
			a.narrowMode = !a.narrowMode
			if a.isWebDoc() {
				a.sendWebSettings()
			}
			a.update()
		}
		previewIcon := iconEye
		previewTip := "预览"
		if a.previewMode {
			previewIcon = iconPencil
			previewTip = "编辑"
		}
		preview := a.chromeIconButton(c, previewIcon, previewTip, a.previewMode)
		if preview.Clicked() {
			a.previewMode = !a.previewMode
			if a.isWebDoc() {
				a.sendWebCommand("preview", a.previewMode)
			}
			a.update()
		}
		more := a.chromeIconButton(c, iconMore, "更多", a.docMoreOpen)
		if more.Clicked() {
			a.docMoreOpen = !a.docMoreOpen
			a.update()
		}
		dropdown(c, more, &a.docMoreOpen, func() {
			menuItem(c, iconDownload, "导出 Markdown（含图片）", func() {
				a.docMoreOpen = false
				a.exportDoc("md")
				a.update()
			})
			menuItem(c, iconDownload, "导出为 PDF", func() {
				a.docMoreOpen = false
				a.exportDoc("pdf")
				a.update()
			})
			menuItem(c, iconDownload, "Word (.docx)", func() {
				a.docMoreOpen = false
				a.exportDoc("docx")
				a.update()
			})
			menuSeparator(c)
			menuItem(c, iconGear, "页面设置…", func() {
				a.docMoreOpen = false
				a.pageSettingsOpen = true
				a.update()
			})
			menuSeparator(c)
			menuItem(c, iconFolder, "在 Finder 中显示", func() {
				a.docMoreOpen = false
				if p, err := a.vault.Abs(doc.Rel); err == nil {
					mygo.Shell.ShowItemInFolder(p)
				}
				a.update()
			})
			menuSeparator(c)
			menuItemDanger(c, iconTrash, "删除", func() {
				a.docMoreOpen = false
				a.askDelete(doc.Rel)
				a.update()
			})
		})
	})
}

// chromeIconButton is one bare icon of the page chrome.
func (a *app) chromeIconButton(c *ui.Context, icon *ui.SVG, tip string, on bool) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Tooltip(tip).Label(tip).Width(30).Height(30).Radius(6).
		Justify(ui.Center).AlignItems(ui.Center)
	if on {
		b.Background(t.Accent.Alpha(0.12))
	} else if b.Hovered() {
		b.Background(ui.Hex("#f2f3f5"))
	}
	b.Children(func() {
		color := t.Text
		if on {
			color = t.Accent
		}
		ui.Icon(c, icon).FontSize(17).TextColor(color)
	})
	return b
}

// outlineEntry is one heading of the page's outline.
type outlineEntry struct {
	Level int
	Index int // rune offset of the heading's first character
	Text  string
}

func outlineEntries(text richtext.Text) []outlineEntry {
	var out []outlineEntry
	idx := 0
	for _, p := range text.Paragraphs() {
		plain := paraText(p)
		if p.Style.Heading > 0 && strings.TrimSpace(plain) != "" {
			out = append(out, outlineEntry{Level: p.Style.Heading, Index: idx, Text: strings.TrimSpace(plain)})
		}
		idx += utf8.RuneCountInString(plain) + 1
	}
	return out
}

// outlineSidebar shows the page's headings in a fixed column on the
// right, as Yuque's outline does.
func (a *app) outlineSidebar(c *ui.Context, doc *docState) {
	t := c.Theme()
	entries := outlineEntries(doc.Doc.Text())
	ui.Column(c).Width(228).Padding(16, 16).Gap(2).Background(t.Surface).
		BorderWidth(0, 0, 0, 1).BorderColor(t.Border).Children(func() {
		ui.Text(c, "大纲").FontSize(13).Bold().TextColor(t.Text).Padding(0, 0, 8, 0)
		if len(entries) == 0 {
			ui.Text(c, "标题会显示在这里").FontSize(12).TextColor(t.TextMuted).Padding(4, 4)
			return
		}
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).FillWidth().Gap(1).Children(func() {
				for _, e := range entries {
					e := e
					row := ui.Row(c).Padding(5, 6).Radius(5).Gap(4).AlignItems(ui.Center).OnClick(func() {
						doc.Doc.SetSelection(e.Index, e.Index)
						a.update()
					})
					if row.Hovered() {
						row.Background(ui.Hex(hoverBg))
					}
					row.Children(func() {
						ui.Box(c).Width(float32(4 + (e.Level-1)*12))
						ui.Text(c, e.Text).FontSize(13).SingleLine().TextColor(t.Text)
					})
				}
			})
		})
	})
}

// pageContentWidth is the page's maximum width in wide or narrow mode.
func (a *app) pageContentWidth() float32 {
	if a.narrowMode {
		return 760
	}
	return 1200
}

// pagePaddingX is the room beside the page's text: wider in wide mode.
func (a *app) pagePaddingX() float32 {
	if a.narrowMode {
		return 70
	}
	return 35
}

// pageFont returns the page's body font size and line height.
func (a *app) pageFont(doc *docState) (size, lineHeight float32) {
	size, lineHeight = doc.PageFontSize, doc.PageLineHeight
	if size <= 0 {
		size = defaultPageFontSize
	}
	if lineHeight <= 0 {
		lineHeight = defaultPageLineHeight
	}
	return
}

// normalizePageSettings fills in the defaults of a partial setting.
func normalizePageSettings(s PageSettings) PageSettings {
	if s.FontSize <= 0 {
		s.FontSize = defaultPageFontSize
	}
	if s.LineHeight <= 0 {
		s.LineHeight = defaultPageLineHeight
	}
	return s
}

// defaultPageSettings returns the global page settings new documents use.
func (a *app) defaultPageSettings() PageSettings {
	s := PageSettings{LineHeight: defaultPageLineHeight, FontSize: defaultPageFontSize}
	if a.cfg != nil {
		s = normalizePageSettings(a.cfg.DefaultPageSettings)
	}
	return s
}

// hasPageSettings reports whether a document overrides the global page
// settings.
func (a *app) hasPageSettings(rel string) bool {
	if a.cfg == nil || a.cfg.PageSettings == nil {
		return false
	}
	_, ok := a.cfg.PageSettings[rel]
	return ok
}

// loadPageSettings gives a document its display settings: the globals,
// then its own settings when it has any.
func (a *app) loadPageSettings(doc *docState) {
	if doc == nil {
		return
	}
	global := a.defaultPageSettings()
	doc.PageFontSize, doc.PageLineHeight = global.FontSize, global.LineHeight
	if a.cfg == nil || a.cfg.PageSettings == nil {
		return
	}
	if s, ok := a.cfg.PageSettings[doc.Rel]; ok {
		if s.FontSize > 0 {
			doc.PageFontSize = s.FontSize
		}
		if s.LineHeight > 0 {
			doc.PageLineHeight = s.LineHeight
		}
	}
}

// savePageSettings writes a document's own display settings, which
// override the global defaults. It leaves the Markdown alone.
func (a *app) savePageSettings(doc *docState) {
	if doc == nil || doc.Note || a.cfg == nil {
		return
	}
	// Choosing the values the document already follows keeps it on the
	// global defaults instead of freezing them into an override.
	global := a.defaultPageSettings()
	if !a.hasPageSettings(doc.Rel) && doc.PageFontSize == global.FontSize && doc.PageLineHeight == global.LineHeight {
		return
	}
	if a.cfg.PageSettings == nil {
		a.cfg.PageSettings = map[string]PageSettings{}
	}
	a.cfg.PageSettings[doc.Rel] = PageSettings{LineHeight: doc.PageLineHeight, FontSize: doc.PageFontSize}
	if err := saveConfig(a.cfg); err != nil {
		a.setError(err)
	}
	if a.isWebDoc() {
		a.sendWebSettings()
	}
}

// resetPageSettings makes a document follow the global page settings
// again, dropping its own.
func (a *app) resetPageSettings(doc *docState) error {
	if doc == nil || doc.Note || a.cfg == nil {
		return nil
	}
	if a.cfg.PageSettings != nil {
		delete(a.cfg.PageSettings, doc.Rel)
	}
	if err := saveConfig(a.cfg); err != nil {
		return err
	}
	a.loadPageSettings(doc)
	if a.isWebDoc() {
		a.sendWebSettings()
	}
	return nil
}

// saveDefaultPageSettings updates the settings new documents start with.
// Documents that already override them keep their own.
func (a *app) saveDefaultPageSettings(s PageSettings) error {
	if a.cfg == nil {
		return nil
	}
	s = normalizePageSettings(s)
	if a.cfg.DefaultPageSettings == s {
		return nil
	}
	a.cfg.DefaultPageSettings = s
	if err := saveConfig(a.cfg); err != nil {
		return err
	}
	if a.doc != nil && !a.hasPageSettings(a.doc.Rel) {
		a.loadPageSettings(a.doc)
		if a.isWebDoc() {
			a.sendWebSettings()
		}
	}
	return nil
}

// editorToolbar is the compact icon toolbar above the page.
func (a *app) editorToolbar(c *ui.Context, doc *docState) {
	t := c.Theme()
	f := doc.Doc.Formatting()
	iconBtn := func(icon *ui.SVG, tip string, on bool, run func()) ui.Element {
		b := a.chromeIconButton(c, icon, tip, on)
		if b.Clicked() {
			run()
			a.markDocDirty()
			a.update()
		}
		return b
	}
	textBtn := func(label, tip string, on bool, st func(ui.Element) ui.Element, run func()) ui.Element {
		b := ui.ButtonBase(c).Tooltip(tip).Label(tip).Width(30).Height(30).Radius(6).
			Justify(ui.Center).AlignItems(ui.Center)
		if on {
			b.Background(t.Accent.Alpha(0.12))
		} else if b.Hovered() {
			b.Background(ui.Hex("#f2f3f5"))
		}
		b.Children(func() {
			color := t.Text
			if on {
				color = t.Accent
			}
			txt := ui.Text(c, label).FontSize(15).TextColor(color)
			if st != nil {
				txt = st(txt)
			}
		})
		if b.Clicked() {
			run()
			a.markDocDirty()
			a.update()
		}
		return b
	}
	gap := func() { ui.Box(c).Width(8) }
	ui.Row(c).Padding(6, 12).Gap(2).AlignItems(ui.Center).
		Background(t.Surface).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		plus := ui.ButtonBase(c).Tooltip("插入块（输入 / 打开）").Width(30).Height(30).Radius(15).
			Background(ui.Hex("#00b96b")).Justify(ui.Center).AlignItems(ui.Center)
		plus.Children(func() {
			ui.Icon(c, iconPlus).FontSize(17).TextColor(ui.Hex("#ffffff"))
		})
		if plus.Clicked() {
			a.insertMenuOpen = !a.insertMenuOpen
			a.insertSlash = false
			a.insertQuery = ""
			a.insertFocus = a.insertMenuOpen
			a.update()
		}
		if !a.insertSlash {
			a.insertPalette(c, plus, doc)
		}
		gap()
		iconBtn(iconUndo, "撤销（⌘Z）", false, doc.Doc.Undo)
		iconBtn(iconRedo, "重做（⇧⌘Z）", false, doc.Doc.Redo)
		gap()
		iconBtn(iconLink, "链接（⌘K）", f.Link != "", a.askLink)
		iconBtn(iconQuote, "引用", f.Paragraph.Quote, doc.Doc.ToggleQuote)
		iconBtn(iconTask, "任务列表", f.Paragraph.List == richtext.Tasks, func() { doc.Doc.ToggleList(richtext.Tasks) })
		gap()
		current := "正文"
		if f.Paragraph.Heading >= 1 && f.Paragraph.Heading <= 3 && !f.ParagraphsMixed {
			current = fmt.Sprintf("标题 %d", f.Paragraph.Heading)
		}
		block := ui.ButtonBase(c).Padding(4, 8).Height(30).Radius(6)
		if a.blockMenuOpen || block.Hovered() {
			block.Background(ui.Hex("#f2f3f5"))
		}
		block.Children(func() {
			ui.Row(c).Gap(3).AlignItems(ui.Center).Children(func() {
				ui.Text(c, current).FontSize(14).TextColor(t.Text)
				ui.Icon(c, iconChevron).FontSize(11).TextColor(t.TextMuted)
			})
		})
		if block.Clicked() {
			a.blockMenuOpen = !a.blockMenuOpen
			a.update()
		}
		dropdown(c, block, &a.blockMenuOpen, func() {
			for i, label := range []string{"正文", "标题 1", "标题 2", "标题 3"} {
				i, label := i, label
				menuItem(c, nil, label, func() {
					a.blockMenuOpen = false
					doc.Doc.SetHeading(i)
					a.markDocDirty()
					a.update()
				})
			}
		})
		if f.Paragraph.CodeBlock {
			gap()
			a.codeBlockLanguageToolbar(c, doc, f.Paragraph.CodeBlockID, f.Paragraph.Language)
		}
		gap()
		textBtn("B", "加粗（⌘B）", f.Bold == richtext.FormatOn, func(e ui.Element) ui.Element { return e.Bold() }, doc.Doc.ToggleBold)
		textBtn("I", "斜体（⌘I）", f.Italic == richtext.FormatOn, func(e ui.Element) ui.Element { return e.Italic() }, doc.Doc.ToggleItalic)
		textBtn("S", "删除线（⇧⌘X）", f.Strikethrough == richtext.FormatOn, func(e ui.Element) ui.Element { return e.Strikethrough() }, doc.Doc.ToggleStrikethrough)
		textBtn("U", "下划线（⌘U）", f.Underline == richtext.FormatOn, func(e ui.Element) ui.Element { return e.Underline() }, doc.Doc.ToggleUnderline)
		gap()
		iconBtn(iconList, "无序列表", f.Paragraph.List == richtext.Bullets, func() { doc.Doc.ToggleList(richtext.Bullets) })
		iconBtn(iconOrderedList, "有序列表", f.Paragraph.List == richtext.Numbers, func() { doc.Doc.ToggleList(richtext.Numbers) })
		iconBtn(iconOutdent, "减少缩进", false, func() { doc.Doc.Indent(-1) })
		iconBtn(iconIndent, "增加缩进", false, func() { doc.Doc.Indent(1) })
	})
}

// insertCommand is one item of the insert palette.
type insertCommand struct {
	Label   string
	Hint    string
	Group   string
	Icon    *ui.SVG
	Enabled bool
	Run     func()
}

func (a *app) insertPalette(c *ui.Context, anchor ui.Element, doc *docState) {
	if !a.insertMenuOpen {
		return
	}
	t := c.Theme()
	ui.PopoverBase(c, anchor, &a.insertMenuOpen, func(panel ui.Element) {
		panel.AttachTo(anchor, ui.AnchorBottomStart, ui.AnchorTopStart).Margin(6, 0, 0, 0)
		panel.Width(300).MaxHeight(430).Padding(8).Radius(10).Background(t.Surface).
			Border(1, t.Border).Shadow(0, 8, 28, 0, ui.RGBA(0, 0, 0, 0.12))
		ui.Column(c).FillWidth().Gap(2).Children(func() {
			search := ui.TextInputBase(c, &a.insertQuery).Placeholder("搜索块类型，回车选择").FillWidth()
			if a.insertFocus {
				a.insertFocus = false
				search = search.AutoFocus()
			}
			close := func() {
				a.insertMenuOpen = false
				a.insertSlash = false
				a.insertQuery = ""
				a.update()
			}
			commands := a.insertCommands(doc, close)
			if a.insertSlash {
				commands = a.slashCommands(doc, close)
			}
			q := strings.ToLower(strings.TrimSpace(a.insertQuery))
			var shown []insertCommand
			for _, cmd := range commands {
				if q == "" || strings.Contains(strings.ToLower(cmd.Label+" "+cmd.Hint), q) {
					shown = append(shown, cmd)
				}
			}
			if search.Submitted() && len(shown) > 0 {
				cmd := shown[0]
				if cmd.Enabled && cmd.Run != nil {
					close()
					cmd.Run()
					return
				}
			}
			if len(shown) == 0 {
				ui.Text(c, "没有匹配的命令").FontSize(13).TextColor(t.TextMuted).Padding(8, 8)
				return
			}
			menuHeight := float32(330)
			if a.insertSlash {
				menuHeight = 255
			}
			ui.Scroll(c).Height(menuHeight).Children(func() {
				ui.Column(c).FillWidth().Gap(1).Children(func() {
					lastGroup := ""
					for _, cmd := range shown {
						cmd := cmd
						if a.insertSlash && cmd.Group != "" && cmd.Group != lastGroup {
							lastGroup = cmd.Group
							ui.Text(c, cmd.Group).FontSize(11).TextColor(t.TextMuted).Padding(8, 8, 2, 8)
						}
						row := ui.Row(c).Padding(6, 7).Radius(6).Gap(9).AlignItems(ui.Center)
						if cmd.Enabled && cmd.Run != nil {
							row.OnClick(func() {
								close()
								cmd.Run()
							})
						}
						if row.Hovered() && cmd.Enabled {
							row.Background(ui.Hex(hoverBg))
						}
						row.Children(func() {
							if cmd.Icon != nil {
								ui.Icon(c, cmd.Icon).FontSize(16).TextColor(t.TextMuted)
							} else {
								ui.Box(c).Width(16)
							}
							ui.Column(c).Gap(0).Grow(1).MinWidth(0).Children(func() {
								ui.Text(c, cmd.Label).FontSize(13).TextColor(t.Text)
								if cmd.Hint != "" {
									ui.Text(c, cmd.Hint).FontSize(11).TextColor(t.TextMuted).SingleLine()
								}
							})
							if !cmd.Enabled {
								ui.Text(c, "下一版").FontSize(10).TextColor(t.TextMuted)
							}
						})
					}
				})
			})
		})
	})
}

func (a *app) insertCommands(doc *docState, close func()) []insertCommand {
	apply := func(fn func()) {
		fn()
		a.markDocDirty()
		a.update()
	}
	return []insertCommand{
		{Label: "正文", Hint: "普通段落", Enabled: true, Run: func() { apply(func() { doc.Doc.SetHeading(0) }) }},
		{Label: "标题 1", Hint: "大标题", Enabled: true, Run: func() { apply(func() { doc.Doc.SetHeading(1) }) }},
		{Label: "标题 2", Hint: "小节标题", Enabled: true, Run: func() { apply(func() { doc.Doc.SetHeading(2) }) }},
		{Label: "标题 3", Hint: "小标题", Enabled: true, Run: func() { apply(func() { doc.Doc.SetHeading(3) }) }},
		{Label: "无序列表", Hint: "圆点列表", Enabled: true, Run: func() { apply(func() { doc.Doc.ToggleList(richtext.Bullets) }) }},
		{Label: "有序列表", Hint: "编号列表", Enabled: true, Run: func() { apply(func() { doc.Doc.ToggleList(richtext.Numbers) }) }},
		{Label: "任务列表", Hint: "可勾选的事项", Enabled: true, Run: func() { apply(func() { doc.Doc.ToggleList(richtext.Tasks) }) }},
		{Label: "引用", Hint: "引用一段话", Enabled: true, Run: func() { apply(func() { doc.Doc.ToggleQuote() }) }},
		{Label: "代码块", Hint: "Enter 换行，⌘Enter 退出", Enabled: true, Run: func() { apply(func() { doc.Doc.ToggleCodeBlock() }) }},
		{Label: "Mermaid 图表", Hint: "兼容导入的旧文档", Enabled: true, Run: func() {
			apply(func() {
				doc.Doc.FormatParagraphs(func(p *richtext.ParagraphStyle) {
					p.CodeBlock, p.Language = true, "mermaid"
				})
			})
		}},
		{Label: "图片", Hint: "从文件插入图片", Icon: iconDownload, Enabled: true, Run: func() { a.insertImage() }},
		{Label: "附件", Hint: "从文件插入附件", Group: "基础", Icon: iconFile, Enabled: true, Run: func() { a.insertAttachment() }},
		{Label: "链接", Hint: "给选中的文字加链接", Icon: iconLink, Enabled: true, Run: func() { a.askLink() }},
		{Label: "行内代码", Hint: "等宽显示一小段代码", Enabled: true, Run: func() { apply(func() { doc.Doc.ToggleCode() }) }},
		{Label: "表格", Hint: "需要扩展表格块，下一阶段支持", Enabled: false},
		{Label: "高亮块", Hint: "需要扩展高亮块，下一阶段支持", Enabled: false},
		{Label: "图片说明", Hint: "需要扩展图片说明，下一阶段支持", Enabled: false},
	}
}

// slashCommands are the block types the slash menu offers. Four drawing
// and attachment types are enough for now.
func (a *app) slashCommands(doc *docState, close func()) []insertCommand {
	return []insertCommand{
		{Label: "附件", Hint: "从文件插入附件", Group: "基础", Icon: iconFile, Enabled: true, Run: func() { a.insertAttachment() }},
		{Label: "Mermaid 图表", Hint: "把当前段落变成 Mermaid 代码块", Group: "绘图", Enabled: true, Run: func() {
			doc.Doc.FormatParagraphs(func(p *richtext.ParagraphStyle) {
				p.CodeBlock, p.Language = true, "mermaid"
			})
			a.markDocDirty()
			a.update()
		}},
	}
}

// openInsertMenu shows the block menu from the menu bar or a shortcut.
func (a *app) openInsertMenu() {
	if a.doc == nil || a.doc.ReadOnly || a.previewMode {
		return
	}
	if a.isWebDoc() {
		a.sendWebCommand("slash", false)
		return
	}
	a.insertMenuOpen = true
	a.insertSlash = false
	a.insertQuery = ""
	a.insertFocus = true
	a.update()
}

// maybeSlashMenu opens the insert palette when a slash begins a paragraph.
func (a *app) maybeSlashMenu() bool {
	if a.doc == nil || a.doc.ReadOnly || a.previewMode {
		return false
	}
	if a.doc.Doc.Formatting().Paragraph.CodeBlock {
		return false
	}
	start, end := a.doc.Doc.Selection()
	if start != end || start < 1 {
		return false
	}
	runes := []rune(a.doc.Doc.Text().PlainText())
	if start > len(runes) || runes[start-1] != '/' {
		return false
	}
	i := start - 1
	for i > 0 && runes[i-1] != '\n' {
		i--
	}
	// The menu opens at the start of a paragraph, and after text since
	// the last line break too: a slash after a URL's colon or another
	// slash is just a slash.
	if start-i > 1 {
		switch runes[start-2] {
		case ':', '/', '\\':
			return false
		}
		if strings.Contains(string(runes[i:start-1]), "://") {
			return false
		}
	}
	attrs := a.doc.Doc.TypingAttributes()
	a.doc.Doc.Edit(func(e *richtext.Edit) {
		e.ReplaceString(start-1, start, "", attrs)
	})
	a.doc.Doc.SetSelection(start-1, start-1)
	a.markDocDirty()
	a.insertMenuOpen = true
	a.insertSlash = true
	a.insertQuery = ""
	a.insertFocus = true
	a.update()
	return true
}

// editorFooter is the page's word count and save state.
func (a *app) editorFooter(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Height(28).Padding(0, 20).Gap(8).AlignItems(ui.Center).
		Background(t.Surface).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		ui.Text(c, fmt.Sprintf("%d 字", a.wordCount())).FontSize(12).TextColor(t.TextMuted)
		if a.doc != nil && a.doc.Doc.Formatting().Paragraph.CodeBlock {
			ui.Box(c).Grow(1)
			ui.Text(c, "Enter 换行 · ⌘Enter 退出代码块").FontSize(12).TextColor(t.TextMuted)
		}
	})
}

// copyCodeBlock puts a block of code on the clipboard.
func (a *app) copyCodeBlock(code string) {
	mygo.Clipboard.WriteText(code)
	a.toast = "复制成功"
	a.toastUntil = time.Now().Add(time.Second)
	a.update()
	deadline := a.toastUntil
	time.AfterFunc(time.Second, func() {
		if a.toastUntil.Equal(deadline) {
			a.toast = ""
			a.update()
		}
	})
}
