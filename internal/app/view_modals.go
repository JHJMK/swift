package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (a *app) promptModal(c *ui.Context) {
	if a.prompt == nil {
		return
	}
	p := a.prompt
	t := c.Theme()
	open := true
	submit := func() {
		if err := p.OnOK(strings.TrimSpace(p.Value)); err != nil {
			p.Err = err.Error()
			return
		}
		a.prompt = nil
		open = false
		a.update()
	}
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(420).Padding(22).Gap(12).Children(func() {
			ui.Text(c, p.Title).FontSize(17).Bold()
			input := ui.TextInput(c, &p.Value).Label(p.Label).Placeholder(p.Label).AutoFocus().Width(372)
			if p.Err != "" {
				ui.Text(c, p.Err).FontSize(12).TextColor(t.Danger)
			}
			submitted := input.Submitted()
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Clicked() {
					a.prompt = nil
					open = false
				}
				if ui.PrimaryButton(c, p.OKLabel).Clicked() || submitted {
					submit()
				}
			})
		})
	})
	if !open {
		a.prompt = nil
	}
}

// firstDocModal names the first knowledge base and document of a vault
// that has no knowledge base yet, so both are named as they are made.
func (a *app) firstDocModal(c *ui.Context) {
	if a.firstDoc == nil {
		return
	}
	f := a.firstDoc
	t := c.Theme()
	open := true
	submitted := false
	submit := func() {
		if err := a.createFirstDoc(); err != nil {
			f.Err = err.Error()
			a.update()
			return
		}
		open = false
		a.update()
	}
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(420).Padding(22).Gap(12).Children(func() {
			ui.Text(c, "创建第一篇文档").FontSize(17).Bold()
			ui.Text(c, "还没有知识库。为它和第一篇文档起个名字，之后可以随时重命名。").
				FontSize(13).TextColor(t.TextMuted)
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, "知识库名称").FontSize(13).TextColor(t.TextMuted)
				kb := ui.TextInput(c, &f.KB).Width(372).AutoFocus()
				submitted = submitted || kb.Submitted()
			})
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, "文档标题").FontSize(13).TextColor(t.TextMuted)
				title := ui.TextInput(c, &f.Title).Width(372)
				submitted = submitted || title.Submitted()
			})
			if f.Err != "" {
				ui.Text(c, f.Err).FontSize(12).TextColor(t.Danger)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Clicked() {
					a.firstDoc = nil
					open = false
				}
				if ui.PrimaryButton(c, "创建").Clicked() || submitted {
					submit()
				}
			})
		})
	})
	if !open {
		a.firstDoc = nil
	}
}

func (a *app) confirmModal(c *ui.Context) {
	if a.confirm == nil {
		return
	}
	p := a.confirm
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(460).Padding(22).Gap(14).Children(func() {
			ui.Text(c, p.Title).FontSize(17).Bold()
			ui.Text(c, p.Message)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Clicked() {
					a.confirm = nil
					open = false
				}
				var ok ui.Element
				if p.Dangerous {
					ok = ui.Button(c, p.OKLabel)
				} else {
					ok = ui.PrimaryButton(c, p.OKLabel)
				}
				if ok.Clicked() {
					fn := p.OnOK
					a.confirm = nil
					open = false
					a.deferred = fn
					a.update()
				}
			})
		})
	})
	if !open {
		a.confirm = nil
	}
}

const (
	settingsWorkspace = "workspace"
	settingsDisplay   = "display"
	settingsSync      = "sync"
	settingsLogs      = "logs"
	settingsAbout     = "about"
)

func (a *app) settingsModal(c *ui.Context) {
	if !a.settingsOpen {
		return
	}
	switch a.settingsSection {
	case settingsWorkspace, settingsDisplay, settingsSync, settingsLogs, settingsAbout:
	default:
		a.settingsSection = settingsWorkspace
	}
	t := c.Theme()
	windowWidth, windowHeight := c.Size()
	width := float32(820)
	if windowWidth-60 < width {
		width = windowWidth - 60
	}
	if width < 680 {
		width = 680
	}
	height := float32(600)
	if windowHeight-80 < height {
		height = windowHeight - 80
	}
	if height < 420 {
		height = 420
	}
	open := true
	ui.Modal(c, &open, func() {
		ui.Row(c).Width(width).Height(height).Clip().Radius(8).Children(func() {
			ui.Column(c).Width(180).FillHeight().Padding(10, 8).Gap(2).
				Background(ui.Hex(sidebarBg)).BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Children(func() {
				ui.Text(c, "设置").FontSize(18).Bold().Padding(8, 10, 12, 10)
				a.settingsNavItem(c, settingsWorkspace, iconFolder, "文档目录")
				a.settingsNavItem(c, settingsDisplay, iconWidth, "文档显示")
				a.settingsNavItem(c, settingsSync, iconCloud, "远端同步")
				a.settingsNavItem(c, settingsLogs, iconFile, "诊断日志")
				a.settingsNavItem(c, settingsAbout, iconInfo, "关于")
			})

			ui.Column(c).Grow(1).FillHeight().Padding(22, 24, 16, 20).Gap(12).Children(func() {
				ui.Text(c, settingsTitle(a.settingsSection)).FontSize(20).Bold()
				ui.Text(c, settingsHint(a.settingsSection)).FontSize(12).TextColor(t.TextMuted)
				ui.Scroll(c).Grow(1).Scrollbars(false).Children(func() {
					switch a.settingsSection {
					case settingsWorkspace:
						a.workspaceSettingsSection(c)
					case settingsDisplay:
						a.pageDisplaySettingsSection(c)
					case settingsSync:
						a.syncSettingsSection(c)
					case settingsLogs:
						a.diagnosticsSettingsSection(c)
					case settingsAbout:
						a.aboutSettingsSection(c)
					}
				})

				msg, isErr := a.syncNote.get()
				if !isErr && a.syncJob != nil {
					if st := a.syncJob.engine.Status(); st.LastErr != "" {
						msg, isErr = "同步失败："+st.LastErr, true
					}
				}
				if !a.cfg.Sync.Enabled {
					msg = ""
				}
				if msg != "" {
					color := t.TextMuted
					if isErr {
						color = t.Danger
					}
					ui.Text(c, msg).FontSize(12).TextColor(color)
				}

				ui.Box(c).Height(1).Background(t.Border)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					if a.settingsSection == settingsSync {
						a.syncFooterButtons(c)
					}
					ui.Box(c).Grow(1)
					if ui.PrimaryButton(c, "完成").Clicked() {
						if err := a.applySyncSettings(); err != nil {
							a.syncNote.set(err.Error(), true)
							a.update()
						} else {
							a.settingsOpen = false
							open = false
						}
					}
				})
			})
		})
	})
	if !open {
		a.settingsOpen = false
		a.showSyncSecret = false
	}
}

// settingsNavItem is one row of the settings window's left menu.
func (a *app) settingsNavItem(c *ui.Context, section string, icon *ui.SVG, label string) {
	t := c.Theme()
	selected := a.settingsSection == section
	item := ui.Row(c).Height(34).Padding(7, 10).Gap(9).Radius(6).AlignItems(ui.Center).
		OnClick(func() {
			a.settingsSection = section
			a.update()
		})
	switch {
	case selected:
		item.Background(t.Accent.Alpha(0.12))
	case item.Hovered():
		item.Background(ui.Hex(hoverBg))
	}
	item.Children(func() {
		color := t.TextMuted
		if selected {
			color = t.Accent
		}
		ui.Icon(c, icon).FontSize(15).TextColor(color)
		ui.Text(c, label).FontSize(13).TextColor(color)
	})
}

func settingsTitle(section string) string {
	switch section {
	case settingsDisplay:
		return "文档显示"
	case settingsSync:
		return "远端同步"
	case settingsLogs:
		return "诊断日志"
	case settingsAbout:
		return "关于"
	default:
		return "文档目录"
	}
}

func settingsHint(section string) string {
	switch section {
	case settingsDisplay:
		return "新文档默认使用的行距和字号。"
	case settingsSync:
		return "选择远端类型并填写连接信息。"
	case settingsLogs:
		return "只保存在这台电脑，用于排查应用和编辑器问题。"
	case settingsAbout:
		return "版本与 GitHub 仓库。"
	default:
		return "所有知识库的根文件夹，以及迁移和打开它的操作。"
	}
}

// workspaceSettingsSection is the settings' 文档目录 page.
func (a *app) workspaceSettingsSection(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Gap(12).Children(func() {
		ui.Text(c, "当前目录").FontSize(13).Bold()
		ui.Text(c, a.cfg.Workspace).FontSize(13).TextColor(t.TextMuted).Selectable()
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c, "更改目录并迁移…").Clicked() {
				a.settingsOpen = false
				a.chooseWorkspace()
			}
			if ui.Button(c, "在 Finder 中打开").Clicked() {
				mygo.Shell.OpenPath(a.vault.Root)
			}
		})
		ui.Text(c, "迁移会把当前目录中的全部知识库、文档和回收站移动到新目录；新目录必须为空。").
			FontSize(12).TextColor(t.TextMuted)
	})
}

// diagnosticsSettingsSection is the settings' 诊断日志 page.
func (a *app) diagnosticsSettingsSection(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Gap(8).Children(func() {
		if a.diagnostics != nil {
			ui.Text(c, a.diagnostics.Directory()).FontSize(12).TextColor(t.TextMuted).Selectable().SingleLine()
		} else {
			ui.Text(c, "日志目录不可用。").FontSize(12).TextColor(t.Danger)
		}
		ui.Text(c, "仅保留最近 3 天，供开发者排查问题。").FontSize(12).TextColor(t.TextMuted)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if ui.Button(c, "导出最近 3 天日志…").Clicked() {
				a.logNote = ""
				a.deferred = a.askExportDiagnosticLogs
				a.update()
			}
			if a.logNote != "" {
				color := t.TextMuted
				if a.logNoteErr {
					color = t.Danger
				}
				ui.Text(c, a.logNote).FontSize(12).TextColor(color)
			}
		})
	})
}

// aboutSettingsSection is the settings' 关于 page.
func (a *app) aboutSettingsSection(c *ui.Context) {
	t := c.Theme()
	const repository = "https://github.com/JHJMK/swift"
	ui.Column(c).Gap(12).Children(func() {
		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "版本").Width(56).FontSize(12).TextColor(t.TextMuted)
			ui.Text(c, appVersion()).FontSize(13)
		})
		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "GitHub").Width(56).FontSize(12).TextColor(t.TextMuted)
			ui.Text(c, repository).FontSize(13).TextColor(t.Accent).Underline().
				Cursor(ui.CursorPointer).Selectable().Role(ui.RoleLink).
				OnClick(func() { mygo.Shell.OpenExternal(repository) })
		})
	})
}

// pageDisplaySettingsSection sets the global page display defaults that
// documents follow unless they override them.
func (a *app) pageDisplaySettingsSection(c *ui.Context) {
	t := c.Theme()
	global := a.defaultPageSettings()
	choice := func(label string, selected bool, run func()) {
		b := ui.Button(c, label).Padding(4, 12)
		if selected {
			b = b.Background(t.Accent.Alpha(0.12)).TextColor(t.Accent)
		}
		if b.Clicked() {
			run()
			a.update()
		}
	}
	ui.Column(c).Gap(6).Children(func() {
		ui.Text(c, "新文档默认使用这里的行距和字号；单独设置过的文档会覆盖全局默认。").
			FontSize(12).TextColor(t.TextMuted)

		ui.Text(c, "行间距").FontSize(12).TextColor(t.TextMuted)
		ui.Row(c).Gap(6).Children(func() {
			choice("紧凑", global.LineHeight == 1.5, func() {
				if err := a.saveDefaultPageSettings(PageSettings{LineHeight: 1.5, FontSize: global.FontSize}); err != nil {
					a.setError(err)
				}
			})
			choice("标准", global.LineHeight == 1.75, func() {
				if err := a.saveDefaultPageSettings(PageSettings{LineHeight: 1.75, FontSize: global.FontSize}); err != nil {
					a.setError(err)
				}
			})
			choice("宽松", global.LineHeight == 2, func() {
				if err := a.saveDefaultPageSettings(PageSettings{LineHeight: 2, FontSize: global.FontSize}); err != nil {
					a.setError(err)
				}
			})
		})

		ui.Text(c, "默认字体大小").FontSize(12).TextColor(t.TextMuted)
		ui.Row(c).Gap(6).Children(func() {
			for _, size := range []float32{14, 15, 16, 18, 20} {
				size := size
				choice(fmt.Sprintf("%.0f", size), global.FontSize == size, func() {
					if err := a.saveDefaultPageSettings(PageSettings{LineHeight: global.LineHeight, FontSize: size}); err != nil {
						a.setError(err)
					}
				})
			}
		})
	})
}

func (a *app) trashModal(c *ui.Context) {
	if !a.trashOpen {
		return
	}
	t := c.Theme()
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(680).Padding(24).Gap(14).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "回收站").FontSize(20).Bold().Grow(1)
				if len(a.trash) > 0 {
					if ui.Button(c, "清空回收站").TextColor(t.Danger).Clicked() {
						a.deferred = func() { a.confirmEmptyTrash() }
						a.update()
					}
				}
				if ui.Button(c, "关闭").Clicked() {
					a.trashOpen = false
					open = false
				}
			})
			if len(a.trash) == 0 {
				ui.Text(c, "回收站是空的。").TextColor(t.TextMuted)
				return
			}
			ui.Scroll(c).MaxHeight(420).Children(func() {
				ui.Column(c).Gap(4).Children(func() {
					for _, entry := range a.trash {
						entry := entry
						ui.Row(c).Key(entry.ID).Padding(10, 10).Radius(6).Gap(10).AlignItems(ui.Center).Children(func() {
							ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
								ui.Text(c, entry.Name).SingleLine()
								ui.Textf(c, "%s · %s", entry.OriginalRel, entry.DeletedAt.Local().Format("2006-01-02 15:04")).
									FontSize(12).TextColor(t.TextMuted).SingleLine()
							})
							if ui.Button(c, "还原").Clicked() {
								if err := a.vault.RestoreTrash(entry.ID); err != nil {
									a.setError(err)
								} else {
									a.refreshTrash()
									a.refreshTree()
								}
								a.update()
							}
							if ui.Button(c, "彻底删除").Clicked() {
								a.deferred = func() {
									res, err := mygo.Dialog.Message(mygo.MessageOptions{
										Parent:             a.win,
										Type:               mygo.MessageWarning,
										Title:              "彻底删除",
										Message:            "“" + entry.Name + "”将被永久删除，文档引用的图片也会一并清理，无法恢复。",
										Buttons:            []string{"删除", "取消"},
										DestructiveButtons: []int{0},
									})
									if err == nil && res.Button == 0 {
										if err := a.vault.PurgeTrash(entry.ID); err != nil {
											a.setError(err)
										} else {
											a.syncPurged([]TrashEntry{entry})
											a.refreshTrash()
										}
										a.update()
									}
								}
								a.update()
							}
						})
					}
				})
			})
		})
	})
	if !open {
		a.trashOpen = false
	}
}

func (a *app) searchModal(c *ui.Context) {
	if !a.searchOpen {
		return
	}
	t := c.Theme()
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(680).Padding(24).Gap(14).Children(func() {
			ui.Text(c, "搜索").FontSize(20).Bold()
			submitted := false
			ui.Row(c).FillWidth().Padding(8, 10).Gap(8).Radius(8).AlignItems(ui.Center).
				Background(t.Surface).Border(1, t.Border).Children(func() {
				input := ui.TextInputBase(c, &a.searchQuery).Label("搜索文档").
					Placeholder("搜索标题和正文…").Grow(1)
				if a.searchFocus {
					input = input.AutoFocus()
					a.searchFocus = false
				}
				submitted = input.Submitted()
				ui.Text(c, "回车").FontSize(12).TextColor(t.TextMuted)
			})
			labels := []string{"全部知识库"}
			rels := []string{""}
			for _, kb := range a.tree {
				labels = append(labels, kb.Title())
				rels = append(rels, kb.Rel)
			}
			if n := a.findNode(a.selected); n != nil {
				labels = append(labels, "当前资源："+n.Title())
				rels = append(rels, n.Rel)
			}
			current := labels[0]
			for i, rel := range rels {
				if rel == a.searchScope {
					current = labels[i]
					break
				}
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "范围").FontSize(13).TextColor(t.TextMuted)
				if ui.Select(c, &current, labels).Label("搜索范围").Width(260).Changed() {
					for i, label := range labels {
						if label == current {
							a.searchScope = rels[i]
							break
						}
					}
					a.runSearch()
				}
			})
			if submitted {
				a.runSearch()
			}
			if a.searchNote != "" {
				ui.Text(c, a.searchNote).FontSize(13).TextColor(t.TextMuted)
			}
			if len(a.searchResults) > 0 {
				ui.Scroll(c).MaxHeight(420).Children(func() {
					ui.Column(c).Gap(2).Children(func() {
						for _, result := range a.searchResults {
							result := result
							ui.Row(c).Key(result.Rel).Padding(9, 10).Radius(6).Gap(12).OnClick(func() {
								a.openEditorTarget(result.Rel)
							}).Children(func() {
								ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
									ui.Text(c, result.Title).SingleLine()
									ui.Text(c, result.Rel).FontSize(11).TextColor(t.TextMuted).SingleLine()
									if result.Snippet != "" {
										ui.Text(c, result.Snippet).FontSize(12).TextColor(t.TextMuted).SingleLine()
									}
								})
							})
						}
					})
				})
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "关闭").Clicked() {
					a.searchOpen = false
					open = false
				}
			})
		})
	})
	if !open {
		a.searchOpen = false
	}
}

// confirmEmptyTrash asks once more before the whole trash is deleted.
func (a *app) confirmEmptyTrash() {
	entries := append([]TrashEntry(nil), a.trash...)
	count := len(entries)
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:             a.win,
		Type:               mygo.MessageWarning,
		Title:              "清空回收站",
		Message:            fmt.Sprintf("回收站中的 %d 项将被永久删除，文档引用的图片也会一并清理，无法恢复。", count),
		Buttons:            []string{"清空", "取消"},
		DestructiveButtons: []int{0},
	})
	if err != nil || res.Button != 0 {
		return
	}
	if err := a.vault.EmptyTrash(); err != nil {
		a.setError(err)
	}
	a.syncPurged(entries)
	a.refreshTrash()
	a.refreshTree()
	a.update()
}

// pickerRow is a knowledge base or folder the picker offers.
type pickerRow struct {
	node  *Node
	depth int
}

func pickerDirs(nodes []*Node, depth int, out *[]pickerRow) {
	for _, n := range nodes {
		*out = append(*out, pickerRow{node: n, depth: depth})
		pickerDirs(n.Children, depth+1, out)
	}
}

// pickerModal chooses the folder a new document or the quick note goes in.
func (a *app) pickerModal(c *ui.Context) {
	if a.picker == nil {
		return
	}
	pk := a.picker
	t := c.Theme()
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(520).Padding(24).Gap(14).Children(func() {
			ui.Column(c).Gap(3).Children(func() {
				ui.Text(c, pk.Title).FontSize(20).Bold()
				ui.Text(c, pk.Hint).FontSize(13).TextColor(t.TextMuted)
			})
			tree, err := a.vault.Tree()
			if err != nil {
				a.setError(err)
				return
			}
			var rows []pickerRow
			pickerDirs(tree, 0, &rows)
			if len(rows) == 0 {
				ui.Text(c, "还没有知识库。").TextColor(t.TextMuted)
				return
			}
			ui.Scroll(c).MaxHeight(380).Children(func() {
				ui.Column(c).Gap(2).Children(func() {
					for _, r := range rows {
						r := r
						row := ui.Row(c).Key(r.node.Rel).Padding(7, 10).Gap(8).Radius(6).
							AlignItems(ui.Center).OnClick(func() { pk.Selected = r.node.Rel })
						switch {
						case pk.Selected == r.node.Rel:
							row.Background(t.Accent.Alpha(0.12))
						case row.Hovered():
							row.Background(ui.Hex(hoverBg))
						}
						row.Children(func() {
							ui.Box(c).Width(float32(r.depth) * 16)
							icon := iconFile
							if r.node.IsDir {
								icon = iconFolder
								if r.depth == 0 {
									icon = iconBook
								}
							}
							ui.Icon(c, icon).FontSize(15).TextColor(t.TextMuted)
							ui.Text(c, r.node.Title()).FontSize(14).Grow(1)
						})
					}
				})
			})
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Clicked() {
					a.picker = nil
					open = false
				}
				if pk.Selected != "" && ui.PrimaryButton(c, pk.OKLabel).Clicked() {
					if err := pk.OnOK(pk.Selected); err != nil {
						a.setError(err)
					} else {
						a.picker = nil
						open = false
					}
				}
			})
		})
	})
	if !open {
		a.picker = nil
	}
}

// pageSettingsModal sets the document page's line spacing and default
// font size. These are display settings; the Markdown is not rewritten.
// A document starts with the global settings and saves its own once they
// are changed here.
func (a *app) pageSettingsModal(c *ui.Context) {
	if !a.pageSettingsOpen || a.doc == nil || a.doc.Note {
		return
	}
	doc := a.doc
	t := c.Theme()
	open := true
	global := a.defaultPageSettings()
	overridden := a.hasPageSettings(doc.Rel)
	choice := func(label string, selected bool, run func()) {
		b := ui.Button(c, label).Padding(4, 12)
		if selected {
			b = b.Background(t.Accent.Alpha(0.12)).TextColor(t.Accent)
		}
		if b.Clicked() {
			run()
			a.savePageSettings(doc)
			a.update()
		}
	}
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(440).Padding(22).Gap(14).Children(func() {
			ui.Text(c, "页面设置").FontSize(18).Bold()
			ui.Text(c, "只影响页面显示，不改写正文。").
				FontSize(12).TextColor(t.TextMuted)

			ui.Column(c).Gap(3).Children(func() {
				if overridden {
					ui.Text(c, "此文档使用单独设置，覆盖全局默认。").
						FontSize(12).TextColor(t.TextMuted)
				} else {
					ui.Textf(c, "此文档跟随全局默认（行距 %.2f，字号 %.0f）。",
						global.LineHeight, global.FontSize).
						FontSize(12).TextColor(t.TextMuted)
				}
				if overridden {
					ui.Row(c).Children(func() {
						if ui.Button(c, "恢复为全局默认").Width(150).Clicked() {
							if err := a.resetPageSettings(doc); err != nil {
								a.setError(err)
							}
							a.update()
						}
					})
				}
			})

			ui.Text(c, "行间距").FontSize(13).Bold()
			ui.Row(c).Gap(6).Children(func() {
				choice("紧凑", doc.PageLineHeight == 1.5, func() { doc.PageLineHeight = 1.5 })
				choice("标准", doc.PageLineHeight == 1.75, func() { doc.PageLineHeight = 1.75 })
				choice("宽松", doc.PageLineHeight == 2, func() { doc.PageLineHeight = 2 })
			})

			ui.Text(c, "默认字体大小").FontSize(13).Bold()
			ui.Row(c).Gap(6).Children(func() {
				for _, size := range []float32{14, 15, 16, 18, 20} {
					size := size
					choice(fmt.Sprintf("%.0f", size), doc.PageFontSize == size, func() { doc.PageFontSize = size })
				}
			})
			ui.Text(c, "选择上面的值会为此文档保存单独设置；全局默认在「设置 > 文档显示」中修改。").
				FontSize(12).TextColor(t.TextMuted)

			ui.Row(c).Justify(ui.End).Children(func() {
				if ui.PrimaryButton(c, "完成").Clicked() {
					open = false
				}
			})
		})
	})
	if !open {
		a.pageSettingsOpen = false
	}
}
