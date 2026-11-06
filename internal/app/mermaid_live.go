package app

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"
)

func isMermaidStyle(st richtext.ParagraphStyle) bool {
	if !st.CodeBlock {
		return false
	}
	fields := strings.Fields(st.Language)
	return len(fields) > 0 && strings.EqualFold(fields[0], "mermaid")
}

func mermaidPreviewOf(p richtext.Paragraph) (url, name string) {
	for _, r := range p.Runs {
		if at := r.Attributes.Attachment; at != nil {
			return at.URL, at.Name
		}
	}
	return "", ""
}

// syncMermaidVisibility shows the rendered picture in place of a Mermaid
// block while the caret is elsewhere, and the source while it is edited.
func (a *app) syncMermaidVisibility() {
	if a.doc == nil || a.doc.Doc == nil {
		return
	}
	text := a.doc.Doc.Text()
	previewByBlock := map[int]string{}
	blockByPreview := map[string]int{}
	blocks := map[int]bool{}
	preview := ""
	for _, p := range text.Paragraphs() {
		url, name := mermaidPreviewOf(p)
		if url != "" && name == "Mermaid 图表" {
			preview = url
			continue
		}
		if isMermaidStyle(p.Style) {
			id := p.Style.CodeBlockID
			blocks[id] = true
			if preview != "" && a.doc.mermaidPreviews[preview] {
				previewByBlock[id] = preview
				blockByPreview[preview] = id
			}
		}
		preview = ""
	}
	if len(blocks) == 0 {
		a.doc.Doc.ResetHidden()
		for url := range a.doc.mermaidPreviews {
			a.doc.Doc.SetPreviewHidden(url, true)
		}
		return
	}
	active := 0
	if st := a.doc.Doc.Formatting().Paragraph; isMermaidStyle(st) {
		active = st.CodeBlockID
	}
	previous := a.mermaidEditing
	a.mermaidEditing = active
	if previous != 0 && active == 0 {
		// The source left the caret: render it again when it changed, as
		// a live preview does, not before the next opening.
		if src, url, ok := a.mermaidSource(previous); ok && strings.TrimSpace(src) != "" &&
			(url == "" || !strings.Contains(url, mermaidSourceHash(src))) {
			a.mermaidRefresh = true
			a.update()
		}
	}
	for id := range blocks {
		if url, ok := previewByBlock[id]; ok {
			a.doc.Doc.SetCodeBlockHidden(id, id != active)
			a.doc.Doc.SetPreviewHidden(url, id == active)
		} else {
			// No picture for it yet: keep the source in sight.
			a.doc.Doc.SetCodeBlockHidden(id, false)
		}
	}
	for url := range a.doc.mermaidPreviews {
		if _, ok := blockByPreview[url]; !ok {
			a.doc.Doc.SetPreviewHidden(url, true)
		}
	}
}

func mermaidSourceHash(src string) string {
	sum := sha256.Sum256([]byte(src))
	return hex.EncodeToString(sum[:8])
}

// mermaidSource returns a block's Mermaid source and its preview URL, if
// it has one.
func (a *app) mermaidSource(id int) (src, url string, ok bool) {
	if a.doc == nil || a.doc.Doc == nil {
		return "", "", false
	}
	var lines []string
	preview := ""
	for _, p := range a.doc.Doc.Text().Paragraphs() {
		pu, name := mermaidPreviewOf(p)
		if pu != "" && name == "Mermaid 图表" {
			preview = pu
			continue
		}
		if p.Style.CodeBlock && p.Style.CodeBlockID == id && isMermaidStyle(p.Style) {
			if url == "" && preview != "" && a.doc.mermaidPreviews[preview] {
				url = preview
			}
			lines = append(lines, paraText(p))
		}
		preview = ""
	}
	if len(lines) == 0 {
		return "", "", false
	}
	return strings.Join(lines, "\n"), url, true
}

// mermaidPreviewControls puts the edit button on a rendered diagram when
// the pointer is over it.
func (a *app) mermaidPreviewControls(c *ui.Context, editBox, editor ui.Element, doc *docState) {
	if doc == nil || doc.ReadOnly || len(doc.mermaidPreviews) == 0 {
		return
	}
	boxes := doc.Doc.VisibleParagraphs()
	box, ed := editBox.Bounds(), editor.Bounds()
	if ed.W <= 0 || ed.H <= 0 {
		return
	}
	t := c.Theme()
	for _, p := range boxes {
		url := p.AttachmentURL
		if url == "" || !doc.mermaidPreviews[url] {
			continue
		}
		// The image's visible part; its top-right control stays in view
		// when a tall diagram is scrolled.
		left, width := ed.X-box.X, ed.W
		top, height := ed.Y-box.Y+max(p.Top, 0), p.Height+min(p.Top, 0)
		if p.ImageWidth > 0 && p.ImageHeight > 0 {
			left = ed.X - box.X + p.ImageLeft
			width = p.ImageWidth
			top = ed.Y - box.Y + max(p.ImageTop, 0)
			height = p.ImageHeight + min(p.ImageTop, 0)
		}
		if height < 24 {
			height = 24
		}
		area := ui.Row(c).Absolute().Left(left).Top(top).
			Width(width).Height(height).
			Padding(8).Justify(ui.End).AlignItems(ui.Start)
		if !area.Hovered() {
			continue
		}
		area.Children(func() {
			ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
				refresh := ui.ButtonBase(c).Label("重新渲染图表").Tooltip("重新渲染 Mermaid 图表").
					Width(28).Height(28).Radius(6).Justify(ui.Center).AlignItems(ui.Center).
					Background(t.Surface).Border(1, t.Border).
					Shadow(0, 2, 8, 0, ui.RGBA(0, 0, 0, 0.12))
				if refresh.Hovered() {
					refresh.Background(ui.Hex(hoverBg))
				}
				refresh.Children(func() {
					ui.Icon(c, iconRefresh).FontSize(14).TextColor(t.TextMuted)
				})
				if refresh.Clicked() {
					a.mermaidRefresh = true
					a.update()
				}
				btn := ui.ButtonBase(c).Label("编辑 Mermaid").Tooltip("编辑 Mermaid").
					Width(28).Height(28).Radius(6).Justify(ui.Center).AlignItems(ui.Center).
					Background(t.Surface).Border(1, t.Border).
					Shadow(0, 2, 8, 0, ui.RGBA(0, 0, 0, 0.12))
				if btn.Hovered() {
					btn.Background(ui.Hex(hoverBg))
				}
				btn.Children(func() {
					ui.Icon(c, iconCode).FontSize(14).TextColor(t.TextMuted)
				})
				if btn.Clicked() {
					a.editMermaidPreview(url)
				}
			})
		})
	}
}

// editMermaidPreview shows a diagram's source and puts the caret in it.
func (a *app) editMermaidPreview(url string) {
	if a.doc == nil {
		return
	}
	id, start, ok := a.mermaidBlockForPreview(url)
	if !ok {
		return
	}
	a.doc.Doc.SetCodeBlockHidden(id, false)
	a.doc.Doc.SetPreviewHidden(url, true)
	a.doc.Doc.SetSelection(start, start)
	a.refocusEditor = true
	a.update()
}

// mermaidBlockForPreview finds the block of code the preview url shows.
func (a *app) mermaidBlockForPreview(url string) (id, start int, ok bool) {
	if a.doc == nil || a.doc.Doc == nil {
		return 0, 0, false
	}
	text := a.doc.Doc.Text()
	preview := ""
	for i := 0; i < text.ParagraphCount(); i++ {
		at := text.ParagraphStart(i)
		st := text.ParagraphStyleAt(at)
		if a := text.AttributesAt(at).Attachment; a != nil && a.Name == "Mermaid 图表" {
			preview = a.URL
			continue
		}
		if isMermaidStyle(st) {
			if preview == url {
				return st.CodeBlockID, at, true
			}
		}
		preview = ""
	}
	return 0, 0, false
}
