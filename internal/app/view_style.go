package app

import "github.com/egoist/mygo/ui"

const (
	sidebarBg  = "#f7f8fa"
	sidebarSel = "#e9ebee"
	hoverBg    = "#f0f1f2"
	bookColor  = "#4a7cf3"

	// topInset is the room above the knowledge base's and the document's
	// headers, under the window's top edge.
	topInset = 12
)

// applyTheme gives the window 语雀's look: white paper, a green accent and
// quiet gray text.
func (a *app) applyTheme(c *ui.Context) {
	t := *c.Theme()
	t.Dark = false
	t.Background = ui.Hex("#ffffff")
	t.Surface = ui.Hex("#ffffff")
	t.SurfaceHover = ui.Hex("#f5f6f7")
	t.SurfacePressed = ui.Hex("#eef0f2")
	t.Border = ui.Hex("#e7e9e8")
	t.Text = ui.Hex("#262626")
	t.TextMuted = ui.Hex("#8c8c8c")
	t.Accent = ui.Hex("#00b96b")
	t.AccentHover = ui.Hex("#00a65f")
	t.AccentPressed = ui.Hex("#009554")
	t.AccentText = ui.Hex("#ffffff")
	t.Selection = ui.Hex("#00b96b").Alpha(0.2)
	t.Focus = ui.Hex("#00b96b")
	t.Inverse = ui.Hex("#262626")
	t.InverseText = ui.Hex("#ffffff")
	t.Scrollbar = ui.Hex("#d9dbdd")
	t.Radius = 6
	t.Spacing = 4
	// A step up from 语雀's 14px interface, and the doc's body is 16px.
	t.FontSize = 15
	c.SetTheme(&t)
}

// navRow is a sidebar entry with an icon, highlighted while selected or
// under the pointer.
func navRow(c *ui.Context, icon *ui.SVG, label string, selected bool, onClick func()) {
	t := c.Theme()
	row := ui.Row(c).Padding(7, 10).Gap(10).Radius(6).AlignItems(ui.Center).MinHeight(32).OnClick(onClick)
	switch {
	case selected:
		row.Background(ui.Hex(sidebarSel))
	case row.Hovered():
		row.Background(ui.Hex(hoverBg))
	}
	row.Children(func() {
		ui.Icon(c, icon).FontSize(16).TextColor(t.TextMuted)
		ui.Text(c, label).FontSize(14).TextColor(t.Text)
	})
}

// actionCard is a clickable card of the home page.
func actionCard(c *ui.Context, icon *ui.SVG, title, subtitle string, onClick func()) ui.Element {
	t := c.Theme()
	card := ui.Column(c).Grow(1).Padding(16).Gap(4).Radius(10).
		Background(t.Surface).Border(1, t.Border).OnClick(onClick)
	if card.Hovered() {
		card.Border(1, t.Accent.Alpha(0.45))
	}
	card.Children(func() {
		ui.Row(c).Width(34).Height(34).Radius(8).Justify(ui.Center).AlignItems(ui.Center).
			Background(t.Accent.Alpha(0.10)).Children(func() {
			ui.Icon(c, icon).FontSize(17).TextColor(t.Accent)
		})
		ui.Text(c, title).FontSize(15).Bold().Padding(6, 0, 0, 0)
		ui.Text(c, subtitle).FontSize(13).TextColor(t.TextMuted).SingleLine()
	})
	return card
}
