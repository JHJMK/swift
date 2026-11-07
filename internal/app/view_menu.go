package app

import "github.com/egoist/mygo/ui"

// The dropdown menus copy 语雀's: a white panel of soft corners and a
// quiet gray highlight, its rows 14px text about 34px high.
const (
	menuBg     = "#ffffff"
	menuBorder = "#e7e9e8"
	menuHover  = "#f2f3f3"
	menuText   = "#262626"
	menuMuted  = "#8a8f8d"
	menuLine   = "#eff0f0"
	menuDanger = "#d54941"
)

// dropdown shows what body builds in a menu under anchor while *open is
// true, growing to the left of the anchor, as the menus at the window's
// right edge do. Pressing elsewhere, or Escape, closes it, which sets
// *open to false.
func dropdown(c *ui.Context, anchor ui.Element, open *bool, body func()) {
	dropdownAt(c, anchor, open, ui.AnchorBottomEnd, ui.AnchorTopEnd, body)
}

// dropdownRight shows the menu growing to the right of anchor, as 语雀's
// row menus do.
func dropdownRight(c *ui.Context, anchor ui.Element, open *bool, body func()) {
	dropdownAt(c, anchor, open, ui.AnchorBottomStart, ui.AnchorTopStart, body)
}

func dropdownAt(c *ui.Context, anchor ui.Element, open *bool, at, self ui.Anchor, body func()) {
	if !*open {
		return
	}
	ui.PopoverBase(c, anchor, open, func(panel ui.Element) {
		panel.AttachTo(anchor, at, self).Margin(6, 0, 0, 0)
		panel.Role(ui.RoleMenu).Width(200).Padding(5).Radius(8).Background(ui.Hex(menuBg)).
			Border(1, ui.Hex(menuBorder)).Shadow(0, 8, 28, 0, ui.RGBA(0, 0, 0, 0.12))
		ui.Column(c).FillWidth().Gap(1).Children(body)
	})
}

// menuItem adds an item to the menu being built, with an icon when icon is
// not nil.
func menuItem(c *ui.Context, icon *ui.SVG, label string, onClick func()) {
	menuItemInk(c, icon, label, menuText, onClick)
}

// menuItemDanger adds an item in the color of what cannot be undone.
func menuItemDanger(c *ui.Context, icon *ui.SVG, label string, onClick func()) {
	menuItemInk(c, icon, label, menuDanger, onClick)
}

func menuItemInk(c *ui.Context, icon *ui.SVG, label, ink string, onClick func()) {
	row := ui.Row(c).Role(ui.RoleMenuItem).FillWidth().Padding(9, 11).Gap(10).Radius(6).
		AlignItems(ui.Center).OnClick(onClick)
	if row.Hovered() {
		row.Background(ui.Hex(menuHover))
	}
	row.Children(func() {
		if icon != nil {
			ui.Icon(c, icon).FontSize(16).TextColor(ui.Hex(menuMuted))
		}
		ui.Text(c, label).FontSize(15).TextColor(ui.Hex(ink))
	})
}

// menuSeparator divides groups of items.
func menuSeparator(c *ui.Context) {
	ui.Box(c).FillWidth().Height(1).Margin(4, 9).Background(ui.Hex(menuLine))
}

func (a *app) importMenuItems(c *ui.Context) {
	closeMenu := func() {
		a.kbAddMenuOpen = false
		a.kbImportMenuOpen = false
		a.importMenuOpen = false
		a.update()
	}
	menuItem(c, iconDownload, "复制导入知识库", func() {
		closeMenu()
		a.askImportKnowledgeBaseCopy()
	})
	menuItem(c, iconFolder, "引用外部知识库", func() {
		closeMenu()
		a.askImportExternalKnowledgeBase()
	})
	menuItem(c, iconBook, "导入语雀 Lakebook", func() {
		closeMenu()
		a.askImportLakebook()
	})
}

// menuButton is a button of the theme's look that shows a menu below it
// while open: label and a small arrow, as 语雀's menus have.
func menuButton(c *ui.Context, label string, open *bool) ui.Element {
	t := c.Theme()
	btn := ui.Button(c, "").Label(label).Role(ui.RoleMenuButton).Padding(6, 12).Gap(6)
	btn.Children(func() {
		ui.Text(c, label).FontSize(15)
		ui.Icon(c, iconChevron).FontSize(12).TextColor(t.TextMuted)
	})
	if btn.Clicked() {
		*open = !*open
	}
	return btn
}
