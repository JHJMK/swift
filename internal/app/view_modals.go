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

func (a *app) settingsModal(c *ui.Context) {
	if !a.settingsOpen {
		return
	}
	t := c.Theme()
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(660).Padding(24).Gap(14).Children(func() {
			ui.Text(c, "设置").FontSize(20).Bold()

			// Leave room for the header and the footer even in a short
			// window.
			_, windowHeight := c.Size()
			scrollHeight := windowHeight - 240
			if scrollHeight < 220 {
				scrollHeight = 220
			}
			if scrollHeight > 520 {
				scrollHeight = 520
			}
			ui.Scroll(c).MaxHeight(scrollHeight).Scrollbars(false).Children(func() {
				ui.Column(c).Gap(16).Children(func() {
					ui.Column(c).Gap(6).Children(func() {
						ui.Text(c, "文档目录").FontSize(13).Bold()
						ui.Text(c, a.cfg.Workspace).FontSize(13).TextColor(t.TextMuted).Selectable()
						ui.Row(c).Gap(8).Children(func() {
							if ui.Button(c, "更改目录并迁移…").Clicked() {
								a.settingsOpen = false
								open = false
								a.chooseWorkspace()
							}
							if ui.Button(c, "在 Finder 中打开").Clicked() {
								mygo.Shell.OpenPath(a.vault.Root)
							}
						})
						ui.Text(c, "迁移会把当前目录中的全部知识库、文档和回收站移动到新目录；新目录必须为空。").
							FontSize(12).TextColor(t.TextMuted)
					})

					ui.Box(c).Height(1).Background(t.Border)

					a.syncSettingsSection(c)

					ui.Box(c).Height(1).Background(t.Border)

					ui.Column(c).Gap(6).Children(func() {
						ui.Text(c, "诊断日志").FontSize(13).Bold()
						ui.Text(c, "日志只保存在这台电脑，用于排查应用和编辑器问题。").
							FontSize(12).TextColor(t.TextMuted)
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

					ui.Box(c).Height(1).Background(t.Border)

					kbs, docs := a.counts()
					ui.Column(c).Gap(4).Children(func() {
						ui.Text(c, "当前内容").FontSize(13).Bold()
						ui.Textf(c, "%d 个知识库 · %d 篇文档 · Markdown 本地存储", kbs, docs).FontSize(13).TextColor(t.TextMuted)
					})

					ui.Box(c).Height(1).Background(t.Border)

					ui.Column(c).Gap(4).Children(func() {
						ui.Text(c, "关于").FontSize(13).Bold()
						ui.Text(c, "语燕 0.1.0 · 纯 Go 桌面应用（MyGo 原生界面）").FontSize(13).TextColor(t.TextMuted)
					})
				})
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

			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				a.syncFooterButtons(c)
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
	if !open {
		a.settingsOpen = false
		a.showSyncSecret = false
	}
}

func (a *app) trashModal(c *ui.Context) {
	if !a.trashOpen {
		return
	}
	t := c.Theme()
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(680).Padding(24).Gap(14).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
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
func (a *app) pageSettingsModal(c *ui.Context) {
	if !a.pageSettingsOpen || a.doc == nil || a.doc.Note {
		return
	}
	doc := a.doc
	t := c.Theme()
	open := true
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
			ui.Text(c, "只影响页面显示，不改写正文。").FontSize(12).TextColor(t.TextMuted)

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
			ui.Text(c, "已经写好的内容不会改写；字号作为页面的默认显示大小。").FontSize(12).TextColor(t.TextMuted)

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
