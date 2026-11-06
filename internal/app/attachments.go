package app

import (
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
	if a.isWebDoc() {
		result, err := a.webPickAttachment(a.docKey())
		if err != nil {
			a.setError(err)
			return
		}
		a.sendWebInsert(result.Text)
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
	var url string
	if a.doc.Note {
		url, err = a.vault.ImportNoteAssetBytes(name, data)
	} else {
		url, err = a.vault.ImportAssetBytes(a.doc.Rel, assetName, data)
	}
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
