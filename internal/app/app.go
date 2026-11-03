package app

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"

	"swift/internal/markdown"
)

// docState is the document open in the editor.
type docState struct {
	Rel   string
	Title string
	Doc   *richtext.Document
	Saved string
	Dirty bool
	MTime time.Time

	// TitleDraft is the title as typed on the page, and ShowTitle tells
	// that the page shows the large title field above the text. Every
	// page does, so the name is editable where it is read.
	TitleDraft   string
	ShowTitle    bool
	titleFocused bool

	// Note tells that the page is the quick note, which belongs to no
	// knowledge base until it is saved to one.
	Note bool

	// mermaidPreviews are the rendered diagrams shown above their source;
	// they are left out of the Markdown written.
	mermaidPreviews map[string]bool
}

// promptState asks for one line of text.
type promptState struct {
	Title   string
	Label   string
	Value   string
	OKLabel string
	OnOK    func(string) error
	Err     string
}

// confirmState asks whether to do something.
type confirmState struct {
	Title     string
	Message   string
	OKLabel   string
	Dangerous bool
	OnOK      func()
}

// pickerState chooses a folder of a knowledge base.
type pickerState struct {
	Title    string
	Hint     string
	OKLabel  string
	Selected string
	OnOK     func(rel string) error
}

// clipboardState is a resource copied in the tree, and whether its
// children were copied with it.
type clipboardState struct {
	rel  string
	deep bool
}

type app struct {
	cfg   *Config
	vault *Vault
	tree  []*Node
	win   *mygo.Window

	// pasteItem pastes into the editor, and is enabled only while the
	// editor has the focus.
	pasteItem     *mygo.MenuItem
	editorFocused bool

	selected      string
	selectedIsDir bool
	openDirs      map[string]*bool

	doc    *docState
	saveAt time.Time

	prompt  *promptState
	confirm *confirmState

	settingsOpen bool
	trashOpen    bool
	trash        []TrashEntry

	searchOpen    bool
	searchQuery   string
	searchResults []SearchResult
	searchNote    string

	// The dropdown menus drawn in the window: the knowledge base's plus,
	// the folder's More and the editor's Export.
	kbMenuOpen     bool
	moreMenuOpen   bool
	exportMenuOpen bool
	// rowMenu is the node whose tree row menu shows, by its path, and
	// rowPlus the node whose add menu shows.
	rowMenu string
	rowPlus string

	// clipboard holds the document or folder copied in the tree.
	clipboard *clipboardState

	// picker chooses a knowledge base or folder, for a new document or
	// for saving the quick note.
	picker *pickerState

	// find is the find bar of the editor.
	findOpen    bool
	findFocus   bool
	findQuery   string
	findMatches [][2]int
	findIndex   int
	findRefocus bool

	// searchScope is the knowledge base or resource searched, "" for all.
	searchScope string
	searchFocus bool

	// focusBody puts the caret at the start of the text after a title is
	// entered.
	focusBody bool

	pending   func()
	deferred  func()
	lastTitle string
	errMsg    string
}

func newApp() *app {
	a := &app{cfg: loadConfig(), openDirs: map[string]*bool{}}
	if a.cfg.Workspace != "" {
		if v, err := OpenVault(a.cfg.Workspace); err == nil {
			a.vault = v
			a.refreshTree()
		}
	}
	return a
}

func (a *app) refreshTree() {
	if a.vault == nil {
		return
	}
	if a.openDirs == nil {
		a.openDirs = map[string]*bool{}
	}
	tree, err := a.vault.Tree()
	if err != nil {
		a.setError(err)
		return
	}
	a.tree = tree
	var walk func([]*Node)
	walk = func(nodes []*Node) {
		for _, n := range nodes {
			if n.IsDir && a.openDirs[n.Rel] == nil {
				open := !strings.Contains(n.Rel, "/")
				a.openDirs[n.Rel] = &open
			}
			walk(n.Children)
		}
	}
	walk(tree)
	if a.selected != "" && a.findNode(a.selected) == nil {
		a.selected = ""
		a.doc = nil
	}
	if n := a.findNode(a.selected); n != nil {
		a.selectedIsDir = n.IsDir
	}
}

func (a *app) findNode(rel string) *Node {
	var find func([]*Node) *Node
	find = func(nodes []*Node) *Node {
		for _, n := range nodes {
			if n.Rel == rel {
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

func (a *app) openState(rel string) *bool {
	if a.openDirs == nil {
		a.openDirs = map[string]*bool{}
	}
	p := a.openDirs[rel]
	if p == nil {
		b := false
		p = &b
		a.openDirs[rel] = p
	}
	return p
}

func (a *app) setError(err error) {
	if err == nil {
		return
	}
	a.errMsg = err.Error()
}

func (a *app) update() {
	if a.win != nil {
		a.win.Update(func() {})
	}
}

// openDoc shows a document in the editor.
func (a *app) openDoc(rel string) {
	a.findOpen = false
	if a.doc != nil && a.doc.Rel == rel {
		a.selected = rel
		a.selectedIsDir = false
		return
	}
	a.flushTitle()
	a.flushDoc()
	md, mtime, err := a.vault.ReadDoc(rel)
	if err != nil {
		a.setError(err)
		return
	}
	text := richtext.ParseMarkdown(md)
	if strings.TrimSpace(md) == "" {
		text = richtext.NewText("")
	}
	text = a.loadImages(text, rel)
	text, previews := a.addMermaidPreviews(text, rel)
	a.doc = &docState{
		Rel:             rel,
		Title:           titleOf(rel),
		Doc:             richtext.NewDocument(text),
		Saved:           text.Markdown(),
		MTime:           mtime,
		mermaidPreviews: previews,
	}
	// Every page shows its title above the text, editable in place.
	a.doc.ShowTitle = true
	a.doc.TitleDraft = a.doc.Title
	if untitledTitle(a.doc.Title) {
		a.doc.TitleDraft = ""
	}
	a.selected = rel
	a.selectedIsDir = false
	// Open the folders above it, so the tree shows where it is.
	for parent := parentRel(rel); parent != ""; parent = parentRel(parent) {
		*a.openState(parent) = true
	}
	a.vault.TouchRecent(rel)
}

func titleOf(rel string) string {
	base := path.Base(rel)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// titleUntitled is the name of a document created without a title, and
// untitledTitle reports whether title is one of its numbered copies.
const titleUntitled = "无标题文档"

func untitledTitle(title string) bool {
	for _, base := range []string{titleUntitled, "未命名文档"} {
		if title == base || strings.HasPrefix(title, base+" ") {
			return true
		}
	}
	return false
}

func parentRel(rel string) string {
	d := path.Dir(rel)
	if d == "." {
		return ""
	}
	return d
}

// flushDoc writes pending edits.
func (a *app) flushDoc() {
	if a.doc == nil || !a.doc.Dirty {
		return
	}
	visible := a.doc.Doc.Text().Markdown()
	if visible == a.doc.Saved && !a.doc.Note {
		a.doc.Dirty = false
		return
	}
	md := visible
	if a.doc.Note {
		title := a.doc.Title
		if title == "" {
			title = "小记"
		}
		if err := a.vault.WriteNote(title, md); err != nil {
			a.setError(err)
			return
		}
		a.doc.Saved = md
		a.doc.Dirty = false
		return
	}
	if len(a.doc.mermaidPreviews) > 0 {
		md = stripMermaidPreviews(md, a.doc.mermaidPreviews)
	}
	mtime, err := a.vault.WriteDoc(a.doc.Rel, md)
	if err != nil {
		a.setError(err)
		return
	}
	a.doc.Saved = visible
	a.doc.MTime = mtime
	a.doc.Dirty = false
}

func (a *app) markDocDirty() {
	if a.doc == nil {
		return
	}
	a.doc.Dirty = true
	a.saveAt = time.Now().Add(600 * time.Millisecond)
	win := a.win
	if win != nil {
		time.AfterFunc(650*time.Millisecond, func() {
			win.Update(func() {})
		})
	}
}

func (a *app) maybeSave() {
	if a.doc != nil && a.doc.Dirty && !a.saveAt.IsZero() && time.Now().After(a.saveAt) {
		a.flushDoc()
	}
}

// loadImages reads the images referenced by a Markdown document so the
// editor can draw them.
func (a *app) loadImages(text richtext.Text, docRel string) richtext.Text {
	return a.mapAttachments(text, func(at *richtext.Attachment) *richtext.Attachment {
		if at.Image != nil || at.URL == "" {
			return at
		}
		filename, ok := a.vault.ResolveAsset(docRel, at.URL)
		if !ok {
			return at
		}
		b, err := os.ReadFile(filename)
		if err != nil {
			return at
		}
		bmp, err := ui.DecodeBitmap(b)
		if err != nil {
			return at
		}
		cp := *at
		cp.Image = bmp
		return &cp
	})
}

// insertImage asks for a picture and puts it into the open document.
func (a *app) insertImage() {
	if a.doc == nil {
		return
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:  a.win,
		Title:   "插入图片",
		Filters: []mygo.FileFilter{{Name: "图片", Extensions: []string{"png", "jpg", "jpeg", "gif", "webp", "bmp"}}},
	})
	if err != nil {
		a.setError(err)
		return
	}
	for _, filename := range paths {
		b, err := os.ReadFile(filename)
		if err != nil {
			a.setError(err)
			continue
		}
		bmp, err := ui.DecodeBitmap(b)
		if err != nil {
			a.setError(err)
			continue
		}
		url, err := a.vault.ImportAsset(a.doc.Rel, filename)
		if err != nil {
			a.setError(err)
			continue
		}
		a.doc.Doc.InsertAttachment(&richtext.Attachment{
			Image: bmp,
			URL:   url,
			Name:  filepath.Base(filename),
		})
		a.markDocDirty()
	}
}

func (a *app) wordCount() int {
	if a.doc == nil {
		return 0
	}
	return countWords(a.doc.Doc.Text().PlainText())
}

func countWords(s string) int {
	count := 0
	inWord := false
	for _, r := range s {
		switch {
		case r == richtext.ObjectReplacement:
			inWord = false
		case unicode.IsSpace(r):
			inWord = false
		case unicode.Is(unicode.Han, r):
			count++
			inWord = false
		case !inWord:
			count++
			inWord = true
		}
	}
	return count
}

// newParent returns the folder new items go into, creating the first
// knowledge base when the vault is empty.
func (a *app) newParent() (string, bool) {
	if a.vault == nil {
		return "", false
	}
	if a.selected != "" {
		if n := a.findNode(a.selected); n != nil {
			if n.IsDir {
				return n.Rel, true
			}
			return parentRel(n.Rel), true
		}
	}
	if len(a.tree) > 0 {
		return a.tree[0].Rel, true
	}
	kb, err := a.vault.CreateKnowledgeBase("我的知识库")
	if err != nil {
		a.setError(err)
		return "", false
	}
	a.refreshTree()
	return kb, true
}

func (a *app) askNewKnowledgeBase() {
	if a.vault == nil {
		return
	}
	a.prompt = &promptState{
		Title:   "新建知识库",
		Label:   "名称",
		Value:   "新知识库",
		OKLabel: "创建",
		OnOK: func(value string) error {
			rel, err := a.vault.CreateKnowledgeBase(value)
			if err != nil {
				return err
			}
			a.selected = rel
			a.selectedIsDir = true
			a.openState(rel)
			*a.openState(rel) = true
			a.refreshTree()
			return nil
		},
	}
}

func (a *app) askNewFolder() {
	parent, ok := a.newParent()
	if !ok {
		return
	}
	a.askNewFolderIn(parent)
}

// askNewFolderIn asks for a folder directly in parent, as the knowledge
// base's own menu does.
func (a *app) askNewFolderIn(parent string) {
	a.askNewFolderTitled(parent, "新建目录", "新目录")
}

// askNewGroupIn asks for a folder in the words the add menu uses.
func (a *app) askNewGroupIn(parent string) {
	a.askNewFolderTitled(parent, "新建分组", "新分组")
}

func (a *app) askNewFolderTitled(parent, title, value string) {
	a.prompt = &promptState{
		Title:   title,
		Label:   "名称",
		Value:   value,
		OKLabel: "创建",
		OnOK: func(value string) error {
			rel, err := a.vault.CreateFolder(parent, value)
			if err != nil {
				return err
			}
			*a.openState(parent) = true
			*a.openState(rel) = true
			a.flushDoc()
			a.doc = nil
			a.selected = rel
			a.selectedIsDir = true
			a.refreshTree()
			return nil
		},
	}
}

func (a *app) askNewDoc() {
	parent, ok := a.newParent()
	if !ok {
		return
	}
	a.askNewDocIn(parent)
}

// askNewDocIn creates an untitled document in parent, as 语雀 does: the
// page shows the large title field where the title is typed.
func (a *app) askNewDocIn(parent string) {
	rel, err := a.vault.CreateDoc(parent, titleUntitled)
	if err != nil {
		a.setError(err)
		return
	}
	*a.openState(parent) = true
	a.refreshTree()
	a.openDoc(rel)
}

// pickerDir turns a picker's choice into the folder a document goes in: a
// folder itself, or the folder beside a document.
func (a *app) pickerDir(rel string) (string, error) {
	if a.vault.IsDir(rel) {
		return rel, nil
	}
	return a.vault.EnsureDocDir(rel)
}

// defaultPickerDir is where the picker starts: the knowledge base of the
// selected resource, or the first knowledge base.
func (a *app) defaultPickerDir() string {
	if kb := a.contextKB(); kb != nil {
		return kb.Rel
	}
	if len(a.tree) > 0 {
		return a.tree[0].Rel
	}
	return ""
}

// askNewDocAt asks where a new document goes before creating it, and
// makes a knowledge base first when there is none.
func (a *app) askNewDocAt() {
	if a.vault == nil {
		return
	}
	a.flushTitle()
	a.flushDoc()
	if len(a.tree) == 0 {
		kb, err := a.vault.CreateKnowledgeBase("我的知识库")
		if err != nil {
			a.setError(err)
			return
		}
		a.refreshTree()
		a.askNewDocIn(kb)
		return
	}
	a.picker = &pickerState{
		Title:    "新建文档",
		Hint:     "选择知识库、目录或文档，文档会放在其中",
		OKLabel:  "创建",
		Selected: a.defaultPickerDir(),
		OnOK: func(rel string) error {
			dir, err := a.pickerDir(rel)
			if err != nil {
				return err
			}
			a.askNewDocIn(dir)
			return nil
		},
	}
	a.update()
}

// openNote opens the quick note, which is outside every knowledge base.
func (a *app) openNote() {
	if a.doc != nil && a.doc.Note {
		return
	}
	a.flushTitle()
	a.flushDoc()
	title, body, err := a.vault.ReadNote()
	if err != nil {
		a.setError(err)
		return
	}
	if title == "" {
		title = "小记"
	}
	text := richtext.ParseMarkdown(body)
	if strings.TrimSpace(body) == "" {
		text = richtext.NewText("")
	}
	a.doc = &docState{
		Note:  true,
		Title: title,
		Doc:   richtext.NewDocument(text),
		Saved: body,
		MTime: time.Now(),
	}
	a.doc.ShowTitle = true
	a.doc.TitleDraft = title
	a.selected = ""
	a.selectedIsDir = false
	a.findOpen = false
	a.update()
}

// askNoteSaveTo asks where the quick note goes in the knowledge bases.
func (a *app) askNoteSaveTo() {
	if a.doc == nil || !a.doc.Note {
		return
	}
	a.flushTitle()
	a.flushDoc()
	if len(a.tree) == 0 {
		if _, err := a.vault.CreateKnowledgeBase("我的知识库"); err != nil {
			a.setError(err)
			return
		}
		a.refreshTree()
	}
	a.picker = &pickerState{
		Title:    "保存到",
		Hint:     "选择知识库、目录或文档，小记会成为其中的一篇文档",
		OKLabel:  "保存",
		Selected: a.defaultPickerDir(),
		OnOK:     a.saveNoteTo,
	}
	a.update()
}

// saveNoteTo makes the quick note a document under rel.
func (a *app) saveNoteTo(rel string) error {
	if a.doc == nil || !a.doc.Note {
		return errors.New("没有可保存的小记")
	}
	a.flushTitle()
	a.flushDoc()
	title := strings.TrimSpace(a.doc.Title)
	if title == "" {
		title = "小记"
	}
	md := a.doc.Doc.Text().Markdown()
	dir, err := a.pickerDir(rel)
	if err != nil {
		return err
	}
	docRel, err := a.vault.CreateDoc(dir, title)
	if err != nil {
		return err
	}
	if _, err := a.vault.WriteDoc(docRel, md); err != nil {
		return err
	}
	a.doc.Dirty = false
	a.doc.Saved = md
	if err := a.vault.ClearNote(); err != nil {
		return err
	}
	a.refreshTree()
	a.openDoc(docRel)
	return nil
}

// commitDocTitle renames the open document to the title typed on its page.
// It runs after the frame that reported the title, so that the view is not
// rebuilt halfway through building it.
func (a *app) commitDocTitle() {
	if a.applyTitleDraft() {
		a.update()
	}
}

// flushTitle applies a title typed on the page before the document is
// left, as a click on another document does not wait for the blur.
func (a *app) flushTitle() {
	a.applyTitleDraft()
}

// applyTitleDraft renames the open document to the title typed on its
// page, and reports whether it did.
func (a *app) applyTitleDraft() bool {
	if a.doc == nil || !a.doc.ShowTitle {
		return false
	}
	title := strings.TrimSpace(a.doc.TitleDraft)
	if a.doc.Note {
		if title == "" {
			title = "小记"
		}
		if title == a.doc.Title {
			return false
		}
		a.doc.Title = title
		a.doc.TitleDraft = title
		a.markDocDirty()
		return true
	}
	if title == "" || title == a.doc.Title {
		return false
	}
	if err := a.renameOpenDoc(title); err != nil {
		a.setError(err)
		return false
	}
	return true
}

// renameOpenDoc renames the open document in place, keeping it open and
// its title field in step with the new name.
func (a *app) renameOpenDoc(title string) error {
	if a.doc == nil {
		return nil
	}
	a.flushDoc()
	oldRel := a.doc.Rel
	newRel, err := a.vault.Rename(oldRel, title)
	if err != nil {
		return err
	}
	a.doc.Rel = newRel
	a.doc.Title = titleOf(newRel)
	a.doc.TitleDraft = a.doc.Title
	a.selected = newRel
	a.selectedIsDir = false
	for parent := parentRel(newRel); parent != ""; parent = parentRel(parent) {
		*a.openState(parent) = true
	}
	a.refreshTree()
	return nil
}

func (a *app) askRename(rel string) {
	// A title typed on the page applies first, so that the prompt renames
	// where the document is now.
	if a.doc != nil && a.doc.Rel == rel {
		a.flushTitle()
		rel = a.doc.Rel
	}
	n := a.findNode(rel)
	if n == nil {
		return
	}
	a.prompt = &promptState{
		Title:   "重命名",
		Label:   "名称",
		Value:   n.Title(),
		OKLabel: "保存",
		OnOK: func(value string) error {
			return a.renameNode(rel, value)
		},
	}
}

// renameNode renames rel and keeps the open document open, with its title
// field, when it is rel or inside it.
func (a *app) renameNode(rel, value string) error {
	under := func(docRel string) bool {
		return docRel == rel || strings.HasPrefix(docRel, rel+"/")
	}
	// Save the open document before the file moves.
	openUnder := a.doc != nil && under(a.doc.Rel)
	if openUnder {
		a.flushDoc()
	}
	show, draft := false, ""
	if openUnder {
		show, draft = a.doc.ShowTitle, a.doc.TitleDraft
	}
	same := a.doc != nil && a.doc.Rel == rel
	newRel, err := a.vault.Rename(rel, value)
	if err != nil {
		return err
	}
	if openUnder {
		moved := newRel + strings.TrimPrefix(a.doc.Rel, rel)
		a.doc = nil
		a.openDoc(moved)
		if a.doc != nil && show {
			a.doc.ShowTitle = true
			if same {
				// The title prompt named the document itself.
				a.doc.TitleDraft = a.doc.Title
			} else {
				a.doc.TitleDraft = draft
			}
		}
	}
	if under(a.selected) {
		a.selected = newRel + strings.TrimPrefix(a.selected, rel)
	}
	a.refreshTree()
	return nil
}

func (a *app) askDelete(rel string) {
	n := a.findNode(rel)
	if n == nil {
		return
	}
	a.confirm = &confirmState{
		Title:     "删除“" + n.Title() + "”",
		Message:   "将移到回收站，可以还原。",
		OKLabel:   "删除",
		Dangerous: true,
		OnOK: func() {
			if a.doc != nil && (a.doc.Rel == rel || strings.HasPrefix(a.doc.Rel, rel+"/")) {
				a.doc = nil
			}
			if a.selected == rel || strings.HasPrefix(a.selected, rel+"/") {
				a.selected = ""
			}
			if err := a.vault.Trash(rel); err != nil {
				a.setError(err)
				return
			}
			a.refreshTree()
		},
	}
}

func (a *app) dropNode(dragged, target *Node) {
	if dragged.Rel == target.Rel {
		return
	}
	newParent := ""
	before := ""
	switch {
	case !strings.Contains(target.Rel, "/") && target.IsDir:
		// Knowledge bases only reorder among themselves.
		newParent = ""
		before = target.Name
	case target.IsDir:
		if !a.canNest(target) {
			return
		}
		newParent = target.Rel
	case !strings.Contains(target.Rel, "/"):
		return
	default:
		// A document holds children too, in the folder beside it.
		if !a.canNest(target) {
			return
		}
		dir, ok := a.childrenDir(target)
		if !ok {
			return
		}
		newParent = dir
	}
	if newParent != "" && treeLevel(newParent)+a.subtreeDepth(dragged) > maxTreeLevel {
		a.setError(fmt.Errorf("目录最多支持 %d 个层级", maxTreeLevel))
		return
	}
	a.moveNode(dragged.Rel, newParent, before)
}

// maxTreeLevel is how many levels the knowledge base's tree shows, below
// the knowledge base itself.
const maxTreeLevel = 4

// treeLevel is how deep rel is below its knowledge base: 1 for the
// knowledge base's own children.
func treeLevel(rel string) int { return strings.Count(rel, "/") }

// canNestRel reports whether rel can hold children within the limit.
func canNestRel(rel string) bool { return treeLevel(rel) < maxTreeLevel }

// canNest reports whether n can hold children within the limit.
func (a *app) canNest(n *Node) bool { return canNestRel(n.Rel) }

// childrenDir returns the folder n's children go in, making the folder
// beside a document when it has none.
func (a *app) childrenDir(n *Node) (string, bool) {
	if !canNestRel(n.Rel) {
		return "", false
	}
	if n.IsDir {
		return n.Rel, true
	}
	dir, err := a.vault.EnsureDocDir(n.Rel)
	if err != nil {
		a.setError(err)
		return "", false
	}
	return dir, true
}

// newChildDoc creates an untitled document under n, which may be a folder
// or a document.
func (a *app) newChildDoc(n *Node) {
	dir, ok := a.childrenDir(n)
	if !ok {
		return
	}
	*a.openState(n.ChildrenRel()) = true
	a.askNewDocIn(dir)
}

// newChildFolder creates a folder under n, which may be a folder or a
// document.
func (a *app) newChildFolder(n *Node) {
	dir, ok := a.childrenDir(n)
	if !ok {
		return
	}
	*a.openState(n.ChildrenRel()) = true
	a.askNewFolderIn(dir)
}

// newChildGroup creates a folder under n, in the words of the add menu.
func (a *app) newChildGroup(n *Node) {
	dir, ok := a.childrenDir(n)
	if !ok {
		return
	}
	*a.openState(n.ChildrenRel()) = true
	a.askNewGroupIn(dir)
}

// subtreeDepth is how many levels a node and its children take.
func (a *app) subtreeDepth(n *Node) int { return subtreeHeight(n) }

// moveNode moves a node and keeps the open document and the selection in
// step with its new path.
func (a *app) moveNode(rel, newParentRel, beforeName string) {
	if a.doc != nil && (a.doc.Rel == rel || strings.HasPrefix(a.doc.Rel, rel+"/")) {
		a.flushDoc()
	}
	newRel, err := a.vault.Move(rel, newParentRel, beforeName)
	if err != nil {
		a.setError(err)
		return
	}
	if a.doc != nil && (a.doc.Rel == rel || strings.HasPrefix(a.doc.Rel, rel+"/")) {
		a.doc.Rel = newRel + strings.TrimPrefix(a.doc.Rel, rel)
	}
	if a.selected == rel || strings.HasPrefix(a.selected, rel+"/") {
		a.selected = newRel + strings.TrimPrefix(a.selected, rel)
	}
	if newParentRel != "" {
		*a.openState(newParentRel) = true
	}
	a.refreshTree()
}

// importMarkdown asks for a Markdown file and imports it under n, which
// may be a folder or a document.
func (a *app) importMarkdown(n *Node) {
	dir, ok := a.childrenDir(n)
	if !ok {
		return
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:  a.win,
		Title:   "导入 Markdown",
		Filters: []mygo.FileFilter{{Name: "Markdown", Extensions: []string{"md", "markdown", "mdown"}}},
	})
	if err != nil {
		a.setError(err)
		return
	}
	if len(paths) == 0 {
		return
	}
	a.importMarkdownFile(dir, paths[0])
}

// importMarkdownFile imports the Markdown file at srcPath into parentRel,
// copying the images it refers to into the knowledge base's assets.
func (a *app) importMarkdownFile(parentRel, srcPath string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		a.setError(err)
		return err
	}
	title := strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath))
	rel, err := a.vault.CreateDoc(parentRel, title)
	if err != nil {
		a.setError(err)
		return err
	}
	md := a.importMarkdownImages(rel, filepath.Dir(srcPath), string(data))
	if _, err := a.vault.WriteDoc(rel, md); err != nil {
		a.setError(err)
		return err
	}
	*a.openState(parentRel) = true
	a.refreshTree()
	a.openDoc(rel)
	return nil
}

// importMarkdownImages copies the images a Markdown text refers to, local
// files relative to srcDir, into the knowledge base's assets, and points
// the text at them.
func (a *app) importMarkdownImages(docRel, srcDir, md string) string {
	return markdown.RewriteImageRefs(md, func(ref string) (string, bool) {
		return a.importImage(docRel, srcDir, ref)
	})
}

// importImage copies the image a Markdown URL names into the knowledge
// base's assets, and returns the URL the document refers to it by.
func (a *app) importImage(docRel, srcDir, ref string) (string, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.Contains(ref, "://") || strings.HasPrefix(ref, "data:") {
		return "", false
	}
	if i := strings.IndexAny(ref, "?#"); i >= 0 {
		ref = ref[:i]
	}
	if unescaped, err := url.PathUnescape(ref); err == nil {
		ref = unescaped
	}
	p := ref
	if !filepath.IsAbs(p) {
		p = filepath.Join(srcDir, filepath.FromSlash(p))
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	// Asset names have no spaces, as documents' images refer to them by
	// plain paths.
	name := strings.ReplaceAll(filepath.Base(p), " ", "-")
	moved, err := a.vault.ImportAssetBytes(docRel, name, b)
	if err != nil {
		a.setError(err)
		return "", false
	}
	return moved, true
}

// copyNode copies n in the tree, with its children when deep is true.
func (a *app) copyNode(n *Node, deep bool) {
	a.clipboard = &clipboardState{rel: n.Rel, deep: deep}
	a.rowMenu = ""
	a.update()
}

// pasteInto pastes the copied resource under n, which may be a document
// or a folder.
func (a *app) pasteInto(n *Node) {
	if a.clipboard == nil || !a.canNest(n) {
		return
	}
	dir, ok := a.childrenDir(n)
	if !ok {
		return
	}
	a.pasteAt(dir)
}

// pasteAt pastes the copied resource into the folder dir.
func (a *app) pasteAt(dir string) {
	src := a.clipboard
	if src == nil {
		return
	}
	depth := a.clipboardDepth()
	if treeLevel(dir)+depth > maxTreeLevel {
		a.setError(fmt.Errorf("目录最多支持 %d 个层级", maxTreeLevel))
		return
	}
	a.flushTitle()
	a.flushDoc()
	newRel, err := a.vault.Copy(src.rel, dir, src.deep)
	if err != nil {
		a.setError(err)
		return
	}
	a.rewriteCopied(src.rel, newRel, src.deep)
	*a.openState(dir) = true
	a.refreshTree()
	if n := a.findNode(newRel); n != nil && !n.IsDir {
		a.openDoc(newRel)
	} else {
		a.doc = nil
		a.selected = newRel
		a.selectedIsDir = true
		a.refreshTree()
	}
	a.update()
}

// clipboardDepth is how many tree levels the copied resource takes.
func (a *app) clipboardDepth() int {
	if a.clipboard == nil || !a.clipboard.deep {
		return 1
	}
	if n := a.findNode(a.clipboard.rel); n != nil {
		return subtreeHeight(n)
	}
	if tree, err := a.vault.Tree(); err == nil {
		if n := findNodeIn(tree, a.clipboard.rel); n != nil {
			return subtreeHeight(n)
		}
	}
	return 1
}

func findNodeIn(nodes []*Node, rel string) *Node {
	for _, n := range nodes {
		if n.Rel == rel {
			return n
		}
		if got := findNodeIn(n.Children, rel); got != nil {
			return got
		}
	}
	return nil
}

func subtreeHeight(n *Node) int {
	depth := 1
	for _, child := range n.Children {
		if d := subtreeHeight(child) + 1; d > depth {
			depth = d
		}
	}
	return depth
}

// rewriteCopied points the images of a copied document or folder at the
// copies in the destination knowledge base.
func (a *app) rewriteCopied(srcRel, dstRel string, deep bool) {
	if !deep {
		if !a.vault.IsDir(srcRel) {
			a.rewriteCopiedDoc(srcRel, dstRel)
		}
		return
	}
	if a.vault.IsDir(srcRel) {
		a.rewriteCopiedTree(srcRel, dstRel)
		return
	}
	a.rewriteCopiedDoc(srcRel, dstRel)
	if srcDir, ok := a.vault.DocDir(srcRel); ok {
		dstDir := strings.TrimSuffix(dstRel, filepath.Ext(dstRel))
		a.rewriteCopiedTree(srcDir, dstDir)
	}
}

// rewriteCopiedTree rewrites the images of the documents below dstDirRel,
// matching them with the documents below srcDirRel.
func (a *app) rewriteCopiedTree(srcDirRel, dstDirRel string) {
	dstAbs, err := a.vault.Abs(dstDirRel)
	if err != nil {
		return
	}
	_ = filepath.WalkDir(dstAbs, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isMarkdown(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(dstAbs, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		a.rewriteCopiedDoc(path.Join(srcDirRel, rel), path.Join(dstDirRel, rel))
		return nil
	})
}

// rewriteCopiedDoc rewrites the image references of one copied document.
func (a *app) rewriteCopiedDoc(srcRel, dstRel string) {
	dstAbs, err := a.vault.Abs(dstRel)
	if err != nil {
		return
	}
	b, err := os.ReadFile(dstAbs)
	if err != nil {
		return
	}
	srcKB := strings.Split(srcRel, "/")[0]
	dstKB := strings.Split(dstRel, "/")[0]
	dstDir := filepath.Dir(dstAbs)
	md := markdown.RewriteImageRefs(string(b), func(ref string) (string, bool) {
		srcAsset, ok := a.vault.AssetPath(srcRel, ref)
		if !ok {
			return "", false
		}
		if srcKB == dstKB {
			rel, err := filepath.Rel(dstDir, srcAsset)
			if err != nil {
				return "", false
			}
			return filepath.ToSlash(rel), true
		}
		data, err := os.ReadFile(srcAsset)
		if err != nil {
			return "", false
		}
		moved, err := a.vault.ImportAssetBytes(dstRel, filepath.Base(srcAsset), data)
		if err != nil {
			a.setError(err)
			return "", false
		}
		return moved, true
	})
	if md == string(b) {
		return
	}
	if _, err := a.vault.WriteDoc(dstRel, md); err != nil {
		a.setError(err)
	}
}

// openFind shows the editor's find bar.
func (a *app) openFind() {
	if a.doc == nil {
		return
	}
	if !a.findOpen {
		a.findOpen = true
		a.findQuery = ""
		a.findMatches = nil
		a.findIndex = -1
	}
	a.findFocus = true
	a.update()
}

// closeFind hides the find bar and gives the text its focus back.
func (a *app) closeFind() {
	a.findOpen = false
	a.findFocus = false
	a.findRefocus = true
	a.update()
}

// runFind finds the query in the text, and selects the first match.
func (a *app) runFind() {
	a.findMatches = nil
	a.findIndex = -1
	if a.doc == nil || a.findQuery == "" {
		return
	}
	text := a.doc.Doc.Text().PlainText()
	lower := strings.ToLower(text)
	q := strings.ToLower(a.findQuery)
	if q == "" {
		return
	}
	start := 0
	for {
		i := strings.Index(lower[start:], q)
		if i < 0 {
			break
		}
		b := start + i
		e := b + len(q)
		rs := utf8.RuneCountInString(text[:b])
		re := rs + utf8.RuneCountInString(text[b:e])
		a.findMatches = append(a.findMatches, [2]int{rs, re})
		start = e
		if start >= len(text) {
			break
		}
	}
	if len(a.findMatches) > 0 {
		a.findIndex = 0
		a.selectFind()
	}
}

// selectFind highlights the current match; the editor reveals it.
func (a *app) selectFind() {
	if a.doc == nil || a.findIndex < 0 || a.findIndex >= len(a.findMatches) {
		return
	}
	m := a.findMatches[a.findIndex]
	a.doc.Doc.SetSelection(m[0], m[1])
	a.update()
}

// findNext moves to the match after dir, 1 or -1.
func (a *app) findNext(dir int) {
	if a.doc == nil {
		return
	}
	if len(a.findMatches) == 0 {
		a.runFind()
		return
	}
	a.findIndex = (a.findIndex + dir + len(a.findMatches)) % len(a.findMatches)
	a.selectFind()
}

// chooseTree opens a node chosen in the knowledge base's tree.
func (a *app) chooseTree(n *Node) {
	a.flushTitle()
	a.flushDoc()
	a.selected = n.Rel
	a.selectedIsDir = n.IsDir
	if n.IsDir {
		a.doc = nil
		*a.openState(n.Rel) = true
	} else {
		a.openDoc(n.Rel)
	}
	a.update()
}

// canMoveOut reports whether n is nested below a knowledge base's root.
func (a *app) canMoveOut(n *Node) bool {
	return strings.Contains(parentRel(n.Rel), "/")
}

// moveOut moves n up one level.
func (a *app) moveOut(n *Node) {
	parent := parentRel(n.Rel)
	a.moveNode(n.Rel, parentRel(parent), "")
}

// pinDoc moves n to the top of its folder.
func (a *app) pinDoc(n *Node) {
	parent := parentRel(n.Rel)
	before := ""
	if p := a.findNode(parent); p != nil {
		for _, c := range p.Children {
			if c.Rel != n.Rel {
				before = c.Name
				break
			}
		}
	}
	if before == "" {
		return
	}
	a.moveNode(n.Rel, parent, before)
}

// copyNodeLink copies a file link to the node, as 语雀 copies a document's
// link.
func (a *app) copyNodeLink(rel string) {
	p := filepath.Join(a.vault.Root, filepath.FromSlash(rel))
	u := url.URL{Scheme: "file", Path: p}
	mygo.Clipboard.WriteText(u.String())
}

func (a *app) refreshTrash() {
	if a.vault == nil {
		return
	}
	list, err := a.vault.ListTrash()
	if err != nil {
		a.setError(err)
		return
	}
	a.trash = list
}

func (a *app) runSearch() {
	a.searchResults = nil
	a.searchNote = ""
	q := strings.TrimSpace(a.searchQuery)
	if q == "" {
		return
	}
	results, err := a.vault.SearchIn(q, 50, a.searchScope)
	if err != nil {
		a.searchNote = err.Error()
		return
	}
	a.searchResults = results
	if len(results) == 0 {
		a.searchNote = "没有找到匹配的文档"
	}
}

func (a *app) chooseWorkspace() {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:            a.win,
		Title:             "选择语燕的文档目录",
		ButtonLabel:       "选择",
		Message:           "语燕会把知识库和 Markdown 文档保存在这个目录中。",
		Directory:         true,
		CreateDirectories: true,
	})
	if err != nil {
		a.setError(err)
		return
	}
	if len(paths) == 0 {
		return
	}
	a.useWorkspace(paths[0])
}

func (a *app) useWorkspace(root string) {
	abs, err := filepath.Abs(root)
	if err != nil {
		a.setError(err)
		return
	}
	if a.vault != nil {
		if filepath.Clean(a.vault.Root) == filepath.Clean(abs) {
			return
		}
		a.confirm = &confirmState{
			Title:     "迁移文档目录",
			Message:   fmt.Sprintf("把当前目录中的知识库和文档移动到：\n%s\n\n目标目录必须为空。", abs),
			OKLabel:   "迁移",
			Dangerous: true,
			OnOK:      func() { a.migrateTo(abs) },
		}
		return
	}
	a.adoptWorkspace(abs)
}

func (a *app) adoptWorkspace(abs string) {
	v, err := OpenVault(abs)
	if err != nil {
		a.setError(err)
		return
	}
	a.vault = v
	a.cfg.Workspace = v.Root
	if err := saveConfig(a.cfg); err != nil {
		a.setError(err)
	}
	a.selected = ""
	a.doc = nil
	a.refreshTree()
}

func (a *app) migrateTo(newRoot string) {
	oldRoot := a.vault.Root
	if filepath.Clean(newRoot) == filepath.Clean(oldRoot) {
		return
	}
	if strings.HasPrefix(filepath.Clean(newRoot)+string(os.PathSeparator), filepath.Clean(oldRoot)+string(os.PathSeparator)) {
		a.setError(errors.New("新目录不能位于当前目录内部"))
		return
	}
	if err := os.MkdirAll(newRoot, 0o755); err != nil {
		a.setError(err)
		return
	}
	entries, err := os.ReadDir(newRoot)
	if err != nil {
		a.setError(err)
		return
	}
	if len(entries) > 0 {
		a.setError(errors.New("目标目录不是空的，请选择一个空目录"))
		return
	}
	a.flushDoc()
	old, err := os.ReadDir(oldRoot)
	if err != nil {
		a.setError(err)
		return
	}
	for _, e := range old {
		if err := movePath(filepath.Join(oldRoot, e.Name()), filepath.Join(newRoot, e.Name())); err != nil {
			a.setError(err)
			return
		}
	}
	a.adoptWorkspace(newRoot)
}

func (a *app) exportDoc(format string) {
	if a.doc == nil {
		return
	}
	a.flushDoc()
	title := a.doc.Title
	if title == "" {
		title = "未命名"
	}
	switch format {
	case "md":
		a.exportMarkdownZip()
	case "pdf":
		a.exportPDF()
	case "docx":
		a.exportDOCX()
	}
}

func defaultExportDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Desktop")
}

// exportMarkdownZip writes the document as a zip holding its Markdown and
// the pictures it shows, so that the two travel together.
func (a *app) exportMarkdownZip() {
	if a.doc == nil {
		return
	}
	a.flushDoc()
	title := strings.ReplaceAll(sanitizeName(a.doc.Title), " ", "-")
	if title == "" {
		title = "未命名"
	}
	p, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:      a.win,
		Title:       "导出 Markdown",
		DefaultPath: filepath.Join(defaultExportDir(), title+".zip"),
		Filters:     []mygo.FileFilter{{Name: "ZIP", Extensions: []string{"zip"}}},
	})
	if err != nil || p == "" {
		return
	}
	if !strings.EqualFold(filepath.Ext(p), ".zip") {
		p += ".zip"
	}
	if err := a.writeMarkdownZip(p, title); err != nil {
		a.setError(err)
	}
}

// writeMarkdownZip writes dst with <title>/<title>.md and the pictures it
// refers to in <title>/<title>.assets.
func (a *app) writeMarkdownZip(dst, title string) error {
	if a.doc == nil {
		return errors.New("没有打开的文档")
	}
	a.flushDoc()
	md := a.doc.Doc.Text().Markdown()
	if len(a.doc.mermaidPreviews) > 0 {
		md = stripMermaidPreviews(md, a.doc.mermaidPreviews)
	}
	tmp, err := os.MkdirTemp("", "swift-markdown-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	top := filepath.Join(tmp, title)
	if err := os.MkdirAll(top, 0o755); err != nil {
		return err
	}
	assetsName := title + ".assets"
	assetsDir := filepath.Join(top, assetsName)
	used := map[string]bool{}
	md = markdown.RewriteImageRefs(md, func(ref string) (string, bool) {
		if ref == "" || strings.Contains(ref, "://") || strings.HasPrefix(ref, "data:") {
			return "", false
		}
		src, ok := a.vault.ResolveAsset(a.doc.Rel, ref)
		if !ok {
			return "", false
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return "", false
		}
		name := strings.ReplaceAll(filepath.Base(src), " ", "-")
		if used[name] {
			base, ext := strings.TrimSuffix(name, filepath.Ext(name)), filepath.Ext(name)
			for i := 2; ; i++ {
				candidate := fmt.Sprintf("%s-%d%s", base, i, ext)
				if !used[candidate] {
					name = candidate
					break
				}
			}
		}
		used[name] = true
		if err := os.MkdirAll(assetsDir, 0o755); err != nil {
			return "", false
		}
		if err := os.WriteFile(filepath.Join(assetsDir, name), data, 0o644); err != nil {
			return "", false
		}
		return assetsName + "/" + name, true
	})
	if err := os.WriteFile(filepath.Join(top, title+".md"), []byte(md), 0o644); err != nil {
		return err
	}
	return zipTree(top, title, dst)
}
