package app

import (
	"github.com/egoist/mygo/ui"
)

// sidebarRail is the sidebar collapsed to icons: the same places, with
// their names on hover and the knowledge bases in a flyout.
func (a *app) sidebarRail(c *ui.Context) {
	bar := c.TitleBar()
	ui.Column(c).Width(56).Shrink(0).Background(ui.Hex(sidebarBg)).
		BorderWidth(0, 1, 0, 0).BorderColor(ui.Hex("#eff0f0")).Children(func() {
		ui.Row(c).Height(bar.Height).DragWindow()
		ui.Row(c).Padding(2, 0, 8, 0).Justify(ui.Center).DragWindow().Children(func() {
			brandLogo(c, 28, 8)
		})
		ui.Box(c).Height(4)
		expand := a.railButton(c, iconChevronRight, "展开侧栏")
		expand.Background(ui.Hex(sidebarSel))
		if expand.Clicked() {
			a.sidebarCollapsed = false
			a.kbRailOpen = false
			a.update()
		}
		ui.Box(c).Height(8)
		if a.railButton(c, iconSearch, "搜索").Clicked() {
			a.openSearch()
		}
		if a.railButton(c, iconHome, "工作台").Clicked() {
			a.goHome()
		}
		if a.railButton(c, iconPencil, "小记").Clicked() {
			a.openNote()
		}
		if a.railButton(c, iconTrash, "回收站").Clicked() {
			a.refreshTrash()
			a.trashOpen = true
			a.update()
		}
		ui.Box(c).Height(6)
		kb := a.railButton(c, iconBook, "知识库")
		if kb.Hovered() {
			a.kbRailOpen = true
		}
		if kb.Clicked() {
			a.kbRailOpen = true
			a.update()
		}
		a.kbRailFlyout(c, kb)
		ui.Box(c).Grow(1)
		if a.railButton(c, iconGear, "设置").Clicked() {
			a.settingsOpen = true
			a.update()
		}
		ui.Box(c).Height(10)
	})
}

// railButton is one icon of the collapsed sidebar, its name a tooltip.
func (a *app) railButton(c *ui.Context, icon *ui.SVG, tip string) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Label(tip).Tooltip(tip).Width(38).Height(38).Radius(7).
		Justify(ui.Center).AlignItems(ui.Center).AlignSelf(ui.Center)
	if b.Hovered() {
		b.Background(ui.Hex(hoverBg))
	}
	b.Children(func() {
		ui.Icon(c, icon).FontSize(18).TextColor(t.TextMuted)
	})
	return b
}

// kbRailFlyout lists the knowledge bases beside the collapsed sidebar,
// opened by hovering its book icon.
func (a *app) kbRailFlyout(c *ui.Context, anchor ui.Element) {
	if !a.kbRailOpen {
		return
	}
	t := c.Theme()
	ui.PopoverBase(c, anchor, &a.kbRailOpen, func(panel ui.Element) {
		panel.AttachTo(anchor, ui.AnchorTopEnd, ui.AnchorTopStart).Margin(0, 6, 0, 0)
		panel.Width(220).Padding(8).Radius(10).Background(t.Surface).
			Border(1, t.Border).Shadow(0, 8, 28, 0, ui.RGBA(0, 0, 0, 0.12))
		ui.Column(c).FillWidth().Gap(1).Children(func() {
			ui.Text(c, "常用").FontSize(12).Bold().TextColor(t.TextMuted).Padding(4, 8)
			if len(a.tree) == 0 {
				ui.Text(c, "还没有知识库").FontSize(13).TextColor(t.TextMuted).Padding(6, 8)
			}
			pinned := a.pinnedKBNodes()
			if len(a.tree) > 0 && len(pinned) == 0 {
				ui.Text(c, "还没有常用知识库").FontSize(13).TextColor(t.TextMuted).Padding(6, 8)
			}
			for _, n := range pinned {
				n := n
				row := ui.Row(c).Padding(7, 9).Gap(8).Radius(6).AlignItems(ui.Center).
					OnClick(func() {
						a.kbRailOpen = false
						a.openKB(n)
						a.update()
					})
				if row.Hovered() {
					row.Background(ui.Hex(hoverBg))
				}
				row.Children(func() {
					ui.Icon(c, iconBook).FontSize(17).TextColor(ui.Hex(bookColor))
					ui.Text(c, n.Title()).FontSize(14).SingleLine().Grow(1)
				})
			}
			menuSeparator(c)
			menuItem(c, iconBook, "所有知识库", func() {
				a.kbRailOpen = false
				a.openAllKBs()
				a.update()
			})
			menuItem(c, iconPlus, "新建知识库", func() {
				a.kbRailOpen = false
				a.askNewKnowledgeBase()
				a.update()
			})
			menuItem(c, iconHome, "工作台", func() {
				a.kbRailOpen = false
				a.goHome()
				a.update()
			})
		})
	})
}

// treeExpandButton opens the tree when both left columns are folded away.
func (a *app) treeExpandButton(c *ui.Context) {
	t := c.Theme()
	b := ui.ButtonBase(c).Label("展开目录").Tooltip("展开目录").Absolute().
		Left(0).Top(150).Width(16).Height(48).Radius(8).
		Justify(ui.Center).AlignItems(ui.Center).
		Background(ui.Hex(sidebarBg)).Border(1, t.Border).
		Shadow(0, 2, 8, 0, ui.RGBA(0, 0, 0, 0.08))
	if b.Hovered() {
		b.Background(ui.Hex(hoverBg))
	}
	b.Children(func() {
		ui.Icon(c, iconChevronRight).FontSize(11).TextColor(t.TextMuted)
	})
	if b.Clicked() {
		a.treeCollapsed = false
		a.update()
	}
}
