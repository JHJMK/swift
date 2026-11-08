package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui/richtext"
)

// The page's editor is a web view when the system webview is there. Go
// owns the Markdown file; the page owns the text and its caret while it is
// open. These are the messages between them.

type webEditorOpen struct {
	ID                string  `json:"id"`
	Title             string  `json:"title"`
	Content           string  `json:"content"`
	ReadOnly          bool    `json:"readOnly"`
	PreviewMode       bool    `json:"previewMode"`
	Note              bool    `json:"note"`
	FontSize          float32 `json:"fontSize"`
	LineHeight        float32 `json:"lineHeight"`
	ReadableLineWidth bool    `json:"readableLineWidth"`
	OutlineOpen       bool    `json:"outlineOpen"`
	SavedAt           string  `json:"savedAt"`
}

type webEditorStatus struct {
	State string `json:"state"`
	Text  string `json:"text"`
}

type webEditorSettings struct {
	FontSize          float32 `json:"fontSize"`
	LineHeight        float32 `json:"lineHeight"`
	ReadableLineWidth bool    `json:"readableLineWidth"`
}

type webEditorCommand struct {
	Command string `json:"command"`
	Open    bool   `json:"open,omitempty"`
}

type webEditorInsert struct {
	Text string `json:"text"`
}

type webEditorReload struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	SavedAt string `json:"savedAt"`
}

type webEditorTitle struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type webEditorRename struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type webEditorInsertResult struct {
	Text string `json:"text"`
}

var (
	webEditorOpenEvent     = mygo.NewEvent[webEditorOpen]("web-editor:open")
	webEditorStatusEvent   = mygo.NewEvent[webEditorStatus]("web-editor:status")
	webEditorSettingsEvent = mygo.NewEvent[webEditorSettings]("web-editor:settings")
	webEditorCommandEvent  = mygo.NewEvent[webEditorCommand]("web-editor:command")
	webEditorInsertEvent   = mygo.NewEvent[webEditorInsert]("web-editor:insert-text")
	webEditorTitleEvent    = mygo.NewEvent[webEditorTitle]("web-editor:title")
	webEditorReloadEvent   = mygo.NewEvent[webEditorReload]("web-editor:reload")
)

// webEditorService is the Go side the page calls.
type webEditorService struct {
	a *app
}

// Ready is the page asking for the open document, once its DOM is ready.
func (s *webEditorService) Ready(context.Context) {
	mygo.RunOnMain(func() {
		s.a.webReady = true
		if s.a.webFailed {
			return
		}
		if !s.a.editorWeb {
			s.a.editorWeb = true
			if s.a.doc != nil {
				s.a.doc.Web = true
			}
			s.a.update()
		}
		s.a.sendWebOpen()
	})
}

// Changed stores the editor's Markdown as it is typed.
func (s *webEditorService) Changed(_ context.Context, id, markdown string) {
	mygo.RunOnMain(func() { s.a.webChanged(id, markdown) })
}

// Rename renames the open document, or gives the quick note its title.
func (s *webEditorService) Rename(_ context.Context, id, title string) (webEditorRename, error) {
	var result webEditorRename
	var err error
	mygo.RunOnMain(func() { result, err = s.a.webRename(id, title) })
	return result, err
}

// SetFocused keeps Edit > Paste in step with the page's focus.
func (s *webEditorService) SetFocused(_ context.Context, focused bool) {
	mygo.RunOnMain(func() { s.a.setEditorFocused(focused) })
}

// InsertImageData writes an image pasted or dropped on the page.
func (s *webEditorService) InsertImageData(_ context.Context, id, name, data string) (webEditorInsertResult, error) {
	var result webEditorInsertResult
	var err error
	mygo.RunOnMain(func() { result, err = s.a.webInsertImageData(id, name, data) })
	return result, err
}

// InsertAttachmentData writes a file dropped on the page.
func (s *webEditorService) InsertAttachmentData(_ context.Context, id, name, data string) (webEditorInsertResult, error) {
	var result webEditorInsertResult
	var err error
	mygo.RunOnMain(func() { result, err = s.a.webInsertAttachmentData(id, name, data) })
	return result, err
}

// PickImage asks for a picture and returns the Markdown to insert.
func (s *webEditorService) PickImage(_ context.Context, id string) (webEditorInsertResult, error) {
	var result webEditorInsertResult
	var err error
	mygo.RunOnMain(func() { result, err = s.a.webPickImage(id) })
	return result, err
}

// PickAttachment asks for a file and returns the Markdown to insert.
func (s *webEditorService) PickAttachment(_ context.Context, id string) (webEditorInsertResult, error) {
	var result webEditorInsertResult
	var err error
	mygo.RunOnMain(func() { result, err = s.a.webPickAttachment(id) })
	return result, err
}

// CopyText copies from the page through the app's clipboard.
func (s *webEditorService) CopyText(_ context.Context, text string) {
	mygo.Clipboard.WriteText(text)
}

// OpenLink opens a link clicked with Cmd or Ctrl in the page.
func (s *webEditorService) OpenLink(_ context.Context, url string) {
	mygo.RunOnMain(func() { s.a.openEditorLink(url) })
}

// setupEditorWebView creates the page's web view. It runs once the window
// exists; the page stays alive for the life of the window.
func (a *app) setupEditorWebView() {
	if a.win == nil {
		return
	}
	view, err := a.win.NewWebView(mygo.WebViewOptions{
		URL:             "/",
		BackgroundColor: "#ffffff",
		Page:            mygo.PageOptions{DevTools: mygo.DevToolsAuto},
	})
	if err != nil {
		a.webFailed = true
		log.Printf("swift: editor web view: %v", err)
		return
	}
	a.webView = view
	a.editorWeb = true
	// A page that never calls Ready (a missing WKWebView, say) should not
	// leave the document area blank.
	time.AfterFunc(12*time.Second, func() {
		mygo.RunOnMain(func() {
			if a.webReady || a.webView != view || a.webFailed {
				return
			}
			log.Printf("swift: editor page did not become ready; using the native editor")
			a.webFailed = true
			a.editorWeb = false
			if a.doc != nil {
				a.doc.Web = false
			}
			a.update()
		})
	})
}

// isWebDoc reports whether the open document is drawn by the page.
func (a *app) isWebDoc() bool {
	return a.doc != nil && a.doc.Web && a.editorWeb && a.webView != nil
}

// docKey is the document's id in the page.
func (a *app) docKey() string {
	if a.doc == nil {
		return ""
	}
	if a.doc.Note {
		return "note"
	}
	return a.doc.Rel
}

func (a *app) sendWebOpen() {
	if a.webView == nil || a.doc == nil {
		return
	}
	doc := a.doc
	fontSize, lineHeight := a.pageFont(doc)
	savedAt := ""
	if !doc.Dirty && !doc.MTime.IsZero() {
		savedAt = doc.MTime.Format("15:04:05")
	}
	payload := webEditorOpen{
		ID:                a.docKey(),
		Title:             doc.Title,
		Content:           doc.Markdown,
		ReadOnly:          doc.ReadOnly,
		PreviewMode:       a.previewMode,
		Note:              doc.Note,
		FontSize:          fontSize,
		LineHeight:        lineHeight,
		ReadableLineWidth: a.narrowMode,
		OutlineOpen:       a.outlineOpen,
		SavedAt:           savedAt,
	}
	a.webSentID = payload.ID
	if err := webEditorOpenEvent.EmitPage(a.webView.Page(), payload); err != nil {
		log.Printf("swift: editor open: %v", err)
	}
	a.sendWebSettings()
}

func (a *app) sendWebStatus(state, text string) {
	if a.webView == nil {
		return
	}
	_ = webEditorStatusEvent.EmitPage(a.webView.Page(), webEditorStatus{State: state, Text: text})
}

func (a *app) sendWebSettings() {
	if a.webView == nil || a.doc == nil {
		return
	}
	fontSize, lineHeight := a.pageFont(a.doc)
	_ = webEditorSettingsEvent.EmitPage(a.webView.Page(), webEditorSettings{
		FontSize:          fontSize,
		LineHeight:        lineHeight,
		ReadableLineWidth: a.narrowMode,
	})
}

func (a *app) sendWebCommand(command string, open bool) {
	if a.webView == nil {
		return
	}
	_ = webEditorCommandEvent.EmitPage(a.webView.Page(), webEditorCommand{Command: command, Open: open})
}

func (a *app) sendWebReload(doc *docState, markdown string, mtime time.Time) {
	if a.webView == nil || doc == nil || doc != a.doc {
		return
	}
	_ = webEditorReloadEvent.EmitPage(a.webView.Page(), webEditorReload{
		ID:      a.docKey(),
		Content: markdown,
		SavedAt: mtime.Format("15:04:05"),
	})
}

func (a *app) sendWebInsert(text string) {
	if a.webView == nil || text == "" {
		return
	}
	_ = webEditorInsertEvent.EmitPage(a.webView.Page(), webEditorInsert{Text: text})
}

func (a *app) sendWebTitle() {
	if a.webView == nil || a.doc == nil {
		return
	}
	a.webSentID = a.docKey()
	_ = webEditorTitleEvent.EmitPage(a.webView.Page(), webEditorTitle{ID: a.docKey(), Title: a.doc.Title})
}

// syncWebIdentity follows renames and moves that happen outside the page.
func (a *app) syncWebIdentity() {
	if !a.isWebDoc() || a.webSentID == "" {
		return
	}
	id := a.docKey()
	if id != a.webSentID {
		a.sendWebTitle()
	}
}

// webChanged takes the page's Markdown. The native rich text model is left
// alone until an export needs it; saves use the Markdown the page has.
func (a *app) webChanged(id, markdown string) {
	doc := a.doc
	if doc == nil || !doc.Web || doc.ReadOnly || a.docKey() != id {
		return
	}
	if doc.Markdown == markdown {
		return
	}
	doc.Markdown = markdown
	a.markDocDirty()
}

// webRename renames the document to the page's inline title.
func (a *app) webRename(id, title string) (webEditorRename, error) {
	doc := a.doc
	if doc == nil || !doc.Web || a.docKey() != id {
		return webEditorRename{}, errors.New("文档已切换")
	}
	title = strings.TrimSpace(strings.ReplaceAll(title, "\n", " "))
	if title == "" {
		return webEditorRename{}, errors.New("标题不能为空")
	}
	if doc.ReadOnly {
		return webEditorRename{}, errors.New("只读文档不能重命名")
	}
	if doc.Note {
		if title != doc.Title {
			doc.Title = title
			doc.TitleDraft = title
			a.markDocDirty()
			a.update()
		}
		return webEditorRename{ID: a.docKey(), Title: doc.Title}, nil
	}
	if title == doc.Title {
		return webEditorRename{ID: a.docKey(), Title: doc.Title}, nil
	}
	if err := a.renameOpenDoc(title); err != nil {
		return webEditorRename{}, err
	}
	a.sendWebTitle()
	a.update()
	return webEditorRename{ID: a.docKey(), Title: a.doc.Title}, nil
}

func (a *app) webInsertImageData(id, name, data string) (webEditorInsertResult, error) {
	doc := a.doc
	if doc == nil || !doc.Web || doc.ReadOnly || a.docKey() != id {
		return webEditorInsertResult{}, errors.New("文档已切换")
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(data)
		if err != nil {
			return webEditorInsertResult{}, err
		}
	}
	return a.webSaveAsset(name, raw, true)
}

func (a *app) webInsertAttachmentData(id, name, data string) (webEditorInsertResult, error) {
	doc := a.doc
	if doc == nil || !doc.Web || doc.ReadOnly || a.docKey() != id {
		return webEditorInsertResult{}, errors.New("文档已切换")
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(data)
		if err != nil {
			return webEditorInsertResult{}, err
		}
	}
	return a.webSaveAsset(name, raw, false)
}

// webSaveAsset writes an asset of the open document and returns the
// Markdown that inserts it.
func (a *app) webSaveAsset(name string, data []byte, image bool) (webEditorInsertResult, error) {
	doc := a.doc
	if doc == nil || doc.ReadOnly {
		return webEditorInsertResult{}, errors.New("没有可编辑的文档")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		if image {
			name = "图片.png"
		} else {
			name = "附件"
		}
	}
	var rel string
	var err error
	if doc.Note {
		rel, err = a.vault.ImportNoteAssetBytes(filepath.Base(name), data)
	} else {
		rel, err = a.vault.ImportAssetBytes(doc.Rel, filepath.Base(name), data)
	}
	if err != nil {
		return webEditorInsertResult{}, err
	}
	if image {
		return webEditorInsertResult{Text: fmt.Sprintf("![%s](%s)", filepath.Base(name), rel)}, nil
	}
	return webEditorInsertResult{Text: fmt.Sprintf("[%s](%s)", filepath.Base(name), rel)}, nil
}

func (a *app) webPickImage(id string) (webEditorInsertResult, error) {
	if a.doc == nil || a.doc.ReadOnly || a.docKey() != id {
		return webEditorInsertResult{}, errors.New("文档已切换")
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:  a.win,
		Title:   "插入图片",
		Filters: []mygo.FileFilter{{Name: "图片", Extensions: []string{"png", "jpg", "jpeg", "gif", "webp", "bmp"}}},
	})
	if err != nil {
		return webEditorInsertResult{}, err
	}
	var texts []string
	for _, filename := range paths {
		data, err := os.ReadFile(filename)
		if err != nil {
			a.setError(err)
			continue
		}
		result, err := a.webSaveAsset(filepath.Base(filename), data, true)
		if err != nil {
			a.setError(err)
			continue
		}
		texts = append(texts, result.Text)
	}
	return webEditorInsertResult{Text: strings.Join(texts, "\n")}, nil
}

func (a *app) webPickAttachment(id string) (webEditorInsertResult, error) {
	if a.doc == nil || a.doc.ReadOnly || a.docKey() != id {
		return webEditorInsertResult{}, errors.New("文档已切换")
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:      a.win,
		Title:       "插入附件",
		ButtonLabel: "插入",
	})
	if err != nil {
		return webEditorInsertResult{}, err
	}
	var texts []string
	for _, filename := range paths {
		data, err := os.ReadFile(filename)
		if err != nil {
			a.setError(err)
			continue
		}
		result, err := a.webSaveAsset(filepath.Base(filename), data, false)
		if err != nil {
			a.setError(err)
			continue
		}
		texts = append(texts, result.Text)
	}
	return webEditorInsertResult{Text: strings.Join(texts, "\n")}, nil
}

// refreshDocFromMarkdown parses the page's Markdown for the native
// operations that still use richtext, chiefly export.
func (a *app) refreshDocFromMarkdown() {
	if a.doc == nil || !a.doc.Web {
		return
	}
	a.doc.Doc = richtext.NewDocument(richtext.ParseMarkdown(a.doc.Markdown))
}

// findDocByTitle finds a document by the title a wiki link names.
func (a *app) findDocByTitle(title string) *Node {
	title = strings.TrimSpace(title)
	if i := strings.Index(title, "#"); i >= 0 {
		title = strings.TrimSpace(title[:i])
	}
	var find func([]*Node) *Node
	find = func(nodes []*Node) *Node {
		for _, n := range nodes {
			if !n.IsDir && (n.Title() == title || strings.TrimSuffix(n.Rel, ".md") == title) {
				return n
			}
			if got := find(n.Children); got != nil {
				return got
			}
		}
		return nil
	}
	return find(a.tree)
}

// webOpenWikiLink opens a [[link]] of the page, or opens a file or URL.
func (a *app) webOpenWikiLink(url string) bool {
	if n := a.findDocByTitle(url); n != nil {
		a.flushTitle()
		a.flushDoc()
		a.selected = n.Rel
		a.selectedIsDir = false
		a.openDoc(n.Rel)
		a.update()
		return true
	}
	return false
}
