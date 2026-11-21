package app

import (
	"context"
	"strings"

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
	// The item is off in either case, but a press may reach it in the
	// frame a modal opens: never paste behind the field being typed in.
	if !a.pasteIntoEditor() {
		return
	}
	if a.isWebDoc() {
		// Reading mode has no editable text, but its find bar is a page
		// field: send the text and let the page fill the field or drop it.
		if a.doc.ReadOnly || a.previewMode {
			a.pasteTextIntoWeb()
			return
		}
		a.pasteFromMenuWeb()
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

// pasteTextIntoWeb sends only the clipboard's text to the page, which
// fills a focused field such as the find bar.
func (a *app) pasteTextIntoWeb() {
	data, err := mygo.Clipboard.Read(transfer.Text)
	if err != nil {
		return
	}
	b, err := data.Read(transfer.Text)
	if err != nil || len(b) == 0 {
		return
	}
	a.sendWebInsertText(string(b), string(b))
}

// pasteFromMenuWeb asks the page to paste. The page reads the clipboard
// through ReadClipboard, so HTML conversion, code blocks and pictures all
// follow one path instead of a second Go conversion with different
// escaping.
func (a *app) pasteFromMenuWeb() {
	if a.doc == nil || a.doc.ReadOnly || a.previewMode {
		return
	}
	a.sendWebCommand("paste", false)
}

// webEditorClipboard is what the page's Paste menu items get: the
// clipboard's rich text, and its plain text. Done says the app inserted
// the clipboard itself, as it does for a picture it saved.
type webEditorClipboard struct {
	HTML     string `json:"html"`
	Markdown string `json:"markdown"`
	Text     string `json:"text"`
	Done     bool   `json:"done"`
}

// ReadClipboard answers the page's Paste menu: the clipboard as the page
// can insert it. A picture is saved as an asset and inserted at once, as
// Edit > Paste does, unless only the text was asked for.
func (s *webEditorService) ReadClipboard(_ context.Context, plain bool) (webEditorClipboard, error) {
	var out webEditorClipboard
	var err error
	mygo.RunOnMain(func() { out, err = s.a.readWebClipboard(plain) })
	return out, err
}

// readWebClipboard reads the clipboard for the page. Main thread only.
func (a *app) readWebClipboard(plain bool) (webEditorClipboard, error) {
	if a.doc == nil || a.doc.ReadOnly || a.previewMode || !a.isWebDoc() {
		return webEditorClipboard{Done: true}, nil
	}
	if !plain {
		if png := mygo.Clipboard.ReadImage(); png != nil {
			result, err := a.webSaveAsset("图片.png", png, true)
			if err != nil {
				return webEditorClipboard{}, err
			}
			a.sendWebInsert(result.Text)
			return webEditorClipboard{Done: true}, nil
		}
	}
	formats := []transfer.Format{transfer.HTML, transfer.Markdown, richtext.RTF, transfer.Text}
	if plain {
		formats = []transfer.Format{transfer.Text}
	}
	data, err := mygo.Clipboard.Read(formats...)
	if err != nil {
		// An empty clipboard leaves nothing to paste.
		return webEditorClipboard{}, nil
	}
	return webClipboardPayload(data), nil
}

// webClipboardPayload picks what the page can insert: HTML to convert as
// a native paste does, Markdown when the clipboard offers it or only RTF
// holds the formatting, and the plain text.
func webClipboardPayload(data transfer.Data) webEditorClipboard {
	var out webEditorClipboard
	if b, err := data.Read(transfer.HTML); err == nil {
		out.HTML = string(b)
	}
	if b, err := data.Read(transfer.Markdown); err == nil {
		out.Markdown = string(b)
	}
	if strings.TrimSpace(out.Markdown) == "" {
		if b, err := data.Read(richtext.RTF); err == nil {
			out.Markdown = strings.TrimSpace(richtext.ParseRTF(b).Markdown())
		}
	}
	if b, err := data.Read(transfer.Text); err == nil {
		out.Text = string(b)
	}
	return out
}

func (a *app) pasteImage(png []byte) {
	img, err := ui.DecodeBitmap(png)
	if err != nil {
		a.setError(err)
		return
	}
	var url string
	if a.doc.Note {
		url, err = a.vault.ImportNoteAssetBytes("图片.png", png)
	} else {
		url, err = a.vault.ImportAssetBytes(a.doc.Rel, "图片.png", png)
	}
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

// setEditorFocused remembers whether the document's text has the focus,
// and keeps the Paste menu item in step with it.
func (a *app) setEditorFocused(focused bool) {
	a.editorFocused = focused
	a.syncPasteItem()
}

// pasteIntoEditor reports whether Edit > Paste should paste into the open
// document. A modal keeps the editor's focus behind it while it holds a
// text field of its own, so the item must be off then: a macOS menu
// handles Cmd+V before the focused field does.
func (a *app) pasteIntoEditor() bool {
	if !a.editorFocused || a.doc == nil {
		return false
	}
	if a.isWebDoc() && (a.doc.ReadOnly || a.previewMode) {
		// Only the page's fields, such as the find bar, can take text.
		return true
	}
	if a.doc.ReadOnly || a.previewMode {
		return false
	}
	return !a.modalOpen()
}

// modalOpen reports whether a modal, or an input over the page such as
// the find bar, is up.
func (a *app) modalOpen() bool {
	return a.settingsOpen || a.pageSettingsOpen || a.trashOpen || a.searchOpen ||
		a.findOpen || a.insertMenuOpen || a.syncResult != nil ||
		a.prompt != nil || a.confirm != nil || a.picker != nil || a.firstDoc != nil ||
		(a.share != nil && a.share.open)
}

// syncPasteItem enables Edit > Paste only while the document's text has
// the focus and nothing over the page takes the keyboard.
func (a *app) syncPasteItem() {
	if a.pasteItem == nil {
		return
	}
	want := a.pasteIntoEditor()
	if a.pasteItem.IsEnabled() != want {
		a.pasteItem.SetEnabled(want)
	}
}

// pasteAndMatchStyle pastes only the clipboard's text, in the typing
// attributes, as Edit > Paste and Match Style does.
func (a *app) pasteAndMatchStyle() {
	if a.doc == nil || a.doc.ReadOnly || a.previewMode {
		return
	}
	if a.isWebDoc() {
		a.sendWebCommand("paste-plain", false)
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
