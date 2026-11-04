package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui/richtext"
)

// insertAttachment asks for a file and inserts a link to it, the file
// copied into the knowledge base's assets.
func (a *app) insertAttachment() {
	if a.doc == nil || a.doc.ReadOnly {
		return
	}
	if a.doc.Note {
		a.setError(errors.New("小记暂不支持附件，请保存到知识库后再插入"))
		return
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:      a.win,
		Title:       "插入附件",
		ButtonLabel: "插入",
	})
	if err != nil {
		a.setError(err)
		return
	}
	if len(paths) == 0 {
		return
	}
	if err := a.attachFile(paths[0]); err != nil {
		a.setError(err)
		return
	}
	a.update()
}

// attachFile puts src in the document's assets and inserts a link to it.
func (a *app) attachFile(src string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	name := filepath.Base(src)
	assetName := strings.ReplaceAll(name, " ", "-")
	url, err := a.vault.ImportAssetBytes(a.doc.Rel, assetName, data)
	if err != nil {
		return err
	}
	a.replaceSelection(richtext.TextOf(richtext.Paragraph{Runs: []richtext.Run{{
		Text: "附件：" + name,
		Attributes: richtext.Attributes{
			Link: url,
		},
	}}}))
	a.update()
	return nil
}

// maybeSlashAttachment turns a typed "/附件" into the attachment picker.
func (a *app) maybeSlashAttachment() bool {
	if a.doc == nil || a.doc.ReadOnly || a.doc.Note {
		return false
	}
	start, end := a.doc.Doc.Selection()
	if start != end {
		return false
	}
	if a.doc.Doc.Formatting().Paragraph.CodeBlock {
		return false
	}
	const cmd = "/附件"
	runes := []rune(a.doc.Doc.Text().PlainText())
	n := len([]rune(cmd))
	if start < n || string(runes[start-n:start]) != cmd {
		return false
	}
	attrs := a.doc.Doc.TypingAttributes()
	a.doc.Doc.Edit(func(e *richtext.Edit) {
		e.ReplaceString(start-n, start, "", attrs)
	})
	a.doc.Doc.SetSelection(start-n, start-n)
	a.markDocDirty()
	a.deferred = a.insertAttachment
	a.update()
	return true
}

// openEditorLink opens a link clicked in the page: a file in the vault
// with the system, a web address in the browser.
func (a *app) openEditorLink(url string) {
	if a.doc == nil {
		return
	}
	if p, ok := a.vault.ResolveAsset(a.doc.Rel, url); ok {
		mygo.Shell.OpenPath(p)
		return
	}
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "mailto:") {
		mygo.Shell.OpenExternal(url)
	}
}
