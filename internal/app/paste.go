package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"
)

// pasteFromMenu pastes into the editor: an image on the clipboard becomes
// an attachment, and other content goes in as rich text. The menu's Paste
// item is enabled only while the editor has the focus, so text fields keep
// the usual Cmd+V.
func (a *app) pasteFromMenu() {
	if a.doc == nil {
		return
	}
	defer a.update()
	if png := mygo.Clipboard.ReadImage(); png != nil {
		a.pasteImage(png)
		return
	}

	data, err := mygo.Clipboard.Read(transfer.HTML, richtext.RTF, transfer.Markdown, transfer.Text)
	if err != nil {
		return
	}
	if b, err := data.Read(transfer.HTML); err == nil && len(b) > 0 {
		a.replaceSelection(richtext.ParseHTML(string(b)))
		return
	}
	if b, err := data.Read(richtext.RTF); err == nil && len(b) > 0 {
		a.replaceSelection(richtext.ParseRTF(b))
		return
	}
	if b, err := data.Read(transfer.Markdown); err == nil && len(b) > 0 {
		a.replaceSelection(richtext.ParseMarkdown(string(b)))
		return
	}
	if b, err := data.Read(transfer.Text); err == nil && len(b) > 0 {
		a.insertPlainText(string(b))
	}
}

func (a *app) pasteImage(png []byte) {
	img, err := ui.DecodeBitmap(png)
	if err != nil {
		a.setError(err)
		return
	}
	url, err := a.vault.ImportAssetBytes(a.doc.Rel, "图片.png", png)
	if err != nil {
		a.setError(err)
		return
	}
	a.replaceSelection(richtext.TextOf(richtext.Paragraph{Runs: []richtext.Run{{
		Text: string(richtext.ObjectReplacement),
		Attributes: richtext.Attributes{Attachment: &richtext.Attachment{
			Image: img,
			URL:   url,
			Name:  "图片",
		}},
	}}}))
}

// insertPlainText pastes plain text in the typing attributes, as the editor
// does itself.
func (a *app) insertPlainText(s string) {
	doc := a.doc.Doc
	start, end := selection(doc)
	attrs := doc.TypingAttributes()
	doc.Edit(func(e *richtext.Edit) { e.ReplaceString(start, end, s, attrs) })
	caret := start + len([]rune(s))
	doc.SetSelection(caret, caret)
	a.markDocDirty()
}

func (a *app) replaceSelection(text richtext.Text) {
	doc := a.doc.Doc
	start, end := selection(doc)
	doc.Edit(func(e *richtext.Edit) { e.Replace(start, end, text) })
	caret := start + text.Len()
	doc.SetSelection(caret, caret)
	a.markDocDirty()
}

func selection(doc *richtext.Document) (int, int) {
	start, end := doc.Selection()
	if start > end {
		start, end = end, start
	}
	return start, end
}

// setEditorFocused keeps the Paste menu item in step with the editor: a
// disabled item leaves Cmd+V to the focus, as a text field needs.
func (a *app) setEditorFocused(focused bool) {
	if a.editorFocused == focused {
		return
	}
	a.editorFocused = focused
	if a.pasteItem != nil {
		a.pasteItem.SetEnabled(focused)
	}
}

// pasteAndMatchStyle pastes only the clipboard's text, in the typing
// attributes, as Edit > Paste and Match Style does.
func (a *app) pasteAndMatchStyle() {
	if a.doc == nil {
		return
	}
	defer a.update()
	data, err := mygo.Clipboard.Read(transfer.Text)
	if err != nil {
		return
	}
	b, err := data.Read(transfer.Text)
	if err != nil || len(b) == 0 {
		return
	}
	a.insertPlainText(string(b))
}
