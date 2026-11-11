package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// allKBsView lists every knowledge base and lets one be pinned to the
// sidebar's 常用 list.
func (a *app) allKBsView(c *ui.Context) {
	t := c.Theme()
	kbs, docs := a.counts()
	ui.Column(c).Fill().Children(func() {
		ui.Box(c).Absolute().Left(0).Top(0).Right(0).Height(c.TitleBar().Height).DragWindow()
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Padding(40, 44).Gap(22).MaxWidth(1040).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Column(c).Gap(4).Grow(1).Children(func() {
						ui.Text(c, "知识库").FontSize(28).Bold()
						ui.Textf(c, "%d 个知识库 · %d 篇文档", kbs, docs).FontSize(14).TextColor(t.TextMuted)
					})
					if ui.PrimaryButton(c, "新建知识库").Clicked() {
						a.askNewKnowledgeBase()
						a.update()
					}
					importBtn := ui.Button(c, "导入知识库")
					if importBtn.Clicked() {
						a.importMenuOpen = !a.importMenuOpen
						a.update()
					}
					dropdownRight(c, importBtn, &a.importMenuOpen, func() {
						a.importMenuItems(c)
					})
				})

				ui.Text(c, "点击知识库打开；固定到常用后会显示在左侧栏。").
					FontSize(13).TextColor(t.TextMuted)

				if len(a.tree) == 0 {
					ui.Column(c).Padding(30).Gap(6).Radius(10).Background(t.Surface).
						Border(1, t.Border).Children(func() {
						ui.Text(c, "还没有知识库").FontSize(15).Bold()
						ui.Text(c, "新建一个知识库，或从文件夹、Lakebook 导入。").
							FontSize(13).TextColor(t.TextMuted)
					})
					return
				}

				ui.Row(c).Wrap().Gap(12).Children(func() {
					for _, n := range a.tree {
						a.kbCard(c, n)
					}
				})
			})
		})
	})
}

// kbCard is one knowledge base on the all-knowledge-bases page.
func (a *app) kbCard(c *ui.Context, n *Node) {
	t := c.Theme()
	pinned := a.isKBPinned(n.Rel)
	label, tip := "固定到常用："+n.Title(), "固定到常用"
	if pinned {
		label, tip = "取消常用："+n.Title(), "取消常用"
	}
	card := ui.Column(c).Width(300).Padding(16).Gap(7).Radius(10).
		Background(t.Surface).Border(1, t.Border).OnClick(func() { a.openKB(n) })
	if card.Hovered() {
		card.Border(1, t.Accent.Alpha(0.45))
	}
	card.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconBook).FontSize(18).TextColor(ui.Hex(bookColor))
			ui.Text(c, n.Title()).FontSize(16).Bold().SingleLine().Grow(1)
			moreLabel := "更多操作：" + n.Title()
			more := ui.ButtonBase(c).Label(moreLabel).Tooltip("更多操作").
				Width(26).Height(26).Radius(6).Justify(ui.Center).AlignItems(ui.Center)
			if more.Hovered() || a.cardMenu == n.Rel {
				more.Background(ui.Hex(hoverBg))
			}
			more.Children(func() {
				ui.Icon(c, iconMore).FontSize(14).TextColor(t.TextMuted)
			})
			if more.Clicked() {
				if a.cardMenu == n.Rel {
					a.cardMenu = ""
				} else {
					a.cardMenu = n.Rel
				}
				a.update()
			}
			open := a.cardMenu == n.Rel
			dropdownRight(c, more, &open, func() {
				a.kbCardMenu(c, n)
			})
			if a.cardMenu == n.Rel && !open {
				a.cardMenu = ""
			}
			pin := ui.ButtonBase(c).Label(label).Tooltip(tip).
				Width(28).Height(28).Radius(6).Justify(ui.Center).AlignItems(ui.Center)
			if pinned {
				pin.Background(t.Accent.Alpha(0.12))
			} else if pin.Hovered() {
				pin.Background(ui.Hex(hoverBg))
			}
			pin.Children(func() {
				color := t.TextMuted
				if pinned {
					color = t.Accent
				}
				ui.Icon(c, iconPin).FontSize(15).TextColor(color)
			})
			if pin.Clicked() {
				if err := a.setKBPinned(n.Rel, !pinned); err != nil {
					a.setError(err)
				}
				a.update()
			}
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Textf(c, "%d 篇文档", nodeDocCount(n)).FontSize(12).TextColor(t.TextMuted)
			if a.vault.IsExternal(n.Rel) {
				ui.Text(c, "外部引用").FontSize(11).TextColor(t.TextMuted).
					Padding(1, 5).Radius(4).Background(t.Accent.Alpha(0.08))
			}
		})
	})
}

// nodeDocCount counts the Markdown documents below a knowledge base.
func nodeDocCount(n *Node) int {
	if n == nil {
		return 0
	}
	if !n.IsDir {
		return 1
	}
	count := 0
	for _, child := range n.Children {
		count += nodeDocCount(child)
	}
	return count
}

// kbCardMenu offers the actions of a knowledge base card, including the
// pin toggle that the card's icon also has.
func (a *app) kbCardMenu(c *ui.Context, n *Node) {
	pinned := a.isKBPinned(n.Rel)
	pinLabel := "固定到常用"
	if pinned {
		pinLabel = "取消常用"
	}
	menuItem(c, iconPin, pinLabel, func() {
		a.cardMenu = ""
		if err := a.setKBPinned(n.Rel, !pinned); err != nil {
			a.setError(err)
		}
		a.update()
	})
	menuSeparator(c)
	if !a.vault.IsExternal(n.Rel) {
		menuItem(c, iconPencil, "重命名", func() {
			a.cardMenu = ""
			a.askRename(n.Rel)
			a.update()
		})
	}
	menuItem(c, iconDownload, "导出", func() {
		a.cardMenu = ""
		a.exportKnowledgeBase(n)
		a.update()
	})
	menuItem(c, iconFolder, "在 Finder 中显示", func() {
		a.cardMenu = ""
		if p, err := a.vault.Abs(n.Rel); err == nil {
			mygo.Shell.ShowItemInFolder(p)
		}
		a.update()
	})
	menuSeparator(c)
	if a.vault.IsExternal(n.Rel) {
		menuItemDanger(c, iconTrash, "移除外部知识库", func() {
			a.cardMenu = ""
			a.askRemoveExternal(n.Rel)
			a.update()
		})
		return
	}
	menuItemDanger(c, iconTrash, "删除", func() {
		a.cardMenu = ""
		a.askDelete(n.Rel)
		a.update()
	})
}
