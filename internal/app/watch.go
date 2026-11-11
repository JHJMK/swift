package app

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui/richtext"
)

// kbWatch follows the folder of the open knowledge base, so edits made in
// another editor, files dropped into the folder and files removed there
// show up without reopening the knowledge base.
type kbWatch struct {
	rel    string
	root   string
	cancel context.CancelFunc
}

// syncKBWatch starts or stops the watch of the knowledge base around the
// selected node. It runs on the main thread, once a frame.
func (a *app) syncKBWatch() {
	if a.vault == nil || a.win == nil {
		return
	}
	kb := a.contextKB()
	if kb == nil {
		a.stopKBWatch()
		return
	}
	if a.kbWatch != nil && a.kbWatch.rel == kb.Rel {
		return
	}
	a.stopKBWatch()
	root, err := a.vault.Abs(kb.Rel)
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &kbWatch{rel: kb.Rel, root: root, cancel: cancel}
	a.kbWatch = w
	go a.watchKnowledgeBase(ctx, w)
}

// stopKBWatch stops following the current knowledge base.
func (a *app) stopKBWatch() {
	if a.kbWatch == nil {
		return
	}
	a.kbWatch.cancel()
	a.kbWatch = nil
}

// watchKnowledgeBase reports the changes in a knowledge base to the app.
// A backend without a native watch, or a watch that fails, falls back to
// polling the folder a few times a second, so network folders still work.
func (a *app) watchKnowledgeBase(ctx context.Context, w *kbWatch) {
	watcher, err := mygo.WatchFiles(ctx, w.root, mygo.FileWatchOptions{
		Recursive: true,
		Debounce:  120 * time.Millisecond,
		MaxDelay:  600 * time.Millisecond,
		Ignore:    watchIgnore,
	})
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("swift: watch %s: %v; polling instead", w.rel, err)
			a.pollKnowledgeBase(ctx, w)
		}
		return
	}
	defer watcher.Close()
	// Catch a change made between reading the tree and enrolling the
	// watch.
	mygo.RunOnMain(func() {
		if a.kbWatch == w {
			a.applyKBSnapshot(w)
		}
	})
	for {
		events, err := watcher.Next(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if errors.Is(err, io.EOF) {
				mygo.RunOnMain(func() {
					if a.kbWatch == w {
						a.kbWatch = nil
						a.applyKBSnapshot(w)
					}
				})
				return
			}
			log.Printf("swift: watch %s: %v; polling instead", w.rel, err)
			a.pollKnowledgeBase(ctx, w)
			return
		}
		if len(events) > 0 {
			mygo.RunOnMain(func() {
				if a.kbWatch == w {
					a.applyKBEvents(w, events)
				}
			})
		}
	}
}

// watchIgnore skips the folders the tree doesn't show and files that are
// not documents: pictures, attachments and metadata.
func watchIgnore(rel string, isDir bool) bool {
	if rel == "" || rel == "." {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	if isDir {
		return false
	}
	return !isMarkdown(path.Base(rel))
}

// pollKnowledgeBase is the fallback watch: it compares a light snapshot
// of the folder and refreshes the tree when it changed.
func (a *app) pollKnowledgeBase(ctx context.Context, w *kbWatch) {
	var previous map[string]fileStamp
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snapshot, err := snapshotKB(w.root)
			if err != nil {
				continue
			}
			if previous == nil {
				previous = snapshot
				mygo.RunOnMain(func() {
					if a.kbWatch == w {
						a.applyKBSnapshot(w)
					}
				})
				continue
			}
			if sameSnapshot(previous, snapshot) {
				continue
			}
			previous = snapshot
			mygo.RunOnMain(func() {
				if a.kbWatch == w {
					a.applyKBSnapshot(w)
				}
			})
		}
	}
}

type fileStamp struct {
	isDir bool
	size  int64
	mtime int64
}

// snapshotKB lists the directories and Markdown files of a knowledge base,
// with the size and modification time that tell a write.
func snapshotKB(root string) (map[string]fileStamp, error) {
	out := map[string]fileStamp{}
	err := filepath.WalkDir(root, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if rel == "." {
			out["."] = fileStamp{isDir: true}
			return nil
		}
		for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
			if strings.HasPrefix(part, ".") {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if entry.IsDir() {
			out[filepath.ToSlash(rel)] = fileStamp{isDir: true}
			return nil
		}
		if !isMarkdown(entry.Name()) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		out[filepath.ToSlash(rel)] = fileStamp{size: info.Size(), mtime: info.ModTime().UnixNano()}
		return nil
	})
	if os.IsNotExist(err) {
		return map[string]fileStamp{}, nil
	}
	return out, err
}

func sameSnapshot(a, b map[string]fileStamp) bool {
	if len(a) != len(b) {
		return false
	}
	for key, stamp := range a {
		if b[key] != stamp {
			return false
		}
	}
	return true
}

// applyKBSnapshot refreshes the tree and the open document after a coarse
// change, as the polling fallback reports.
func (a *app) applyKBSnapshot(w *kbWatch) {
	if a.vault == nil || a.doc == nil && a.contextKB() == nil {
		return
	}
	doc := a.doc
	selected := a.selected
	if doc != nil && !relWithin(doc.Rel, w.rel) {
		doc = nil
	}
	a.refreshTreeSlowly()
	if doc != nil {
		if a.findNode(doc.Rel) == nil {
			if a.doc == doc {
				a.handleOpenDocDeleted(w, doc)
			}
		} else if md, mtime, err := a.vault.ReadDoc(doc.Rel); err == nil {
			a.reloadIfChanged(doc, md, mtime)
		}
	}
	if a.selected == "" && selected != "" {
		a.selectAfterRemoval(selected, w.rel)
	}
	a.update()
}

// applyKBEvents acts on one batch of file events from the watch.
func (a *app) applyKBEvents(w *kbWatch, events []mygo.FileEvent) {
	if a.vault == nil {
		return
	}
	doc := a.doc
	if doc != nil && !relWithin(doc.Rel, w.rel) {
		doc = nil
	}
	selected := a.selected

	structural := false
	touched := false
	deleted := false

	// Moves first: one may carry the open document, and its new path must
	// be known before the tree is read again.
	for _, event := range events {
		switch event.Op {
		case mygo.FileRename:
			if doc != nil && event.OldPath != "" && event.Path != "" {
				oldRel := kbPath(w.rel, event.OldPath)
				newRel := kbPath(w.rel, event.Path)
				if relWithin(doc.Rel, oldRel) {
					newDocRel := newRel + strings.TrimPrefix(doc.Rel, oldRel)
					a.adoptExternalMove(doc, newDocRel, oldRel, event.IsDir)
					doc = a.doc
					touched = true
				}
			}
			if selected != "" && event.OldPath != "" && event.Path != "" {
				oldRel := kbPath(w.rel, event.OldPath)
				if relWithin(selected, oldRel) {
					selected = kbPath(w.rel, event.Path) + strings.TrimPrefix(selected, oldRel)
					a.selected = selected
				}
			}
			if event.IsDir || isMarkdown(path.Base(event.Path)) {
				structural = true
			}
		case mygo.FileCreate:
			if doc != nil && kbPath(w.rel, event.Path) == doc.Rel {
				touched = true
			}
			if event.IsDir || isMarkdown(path.Base(event.Path)) {
				structural = true
			}
		case mygo.FileRemove:
			rel := kbPath(w.rel, event.Path)
			if doc != nil && relWithin(doc.Rel, rel) {
				deleted = true
			}
			if event.IsDir || isMarkdown(path.Base(event.Path)) {
				structural = true
			}
		case mygo.FileWrite:
			if doc != nil && kbPath(w.rel, event.Path) == doc.Rel {
				touched = true
			}
		case mygo.FileHint:
			structural, touched = true, true
		}
	}

	if deleted && doc != nil {
		// Some editors remove a file and write a new one at the same
		// path in the same batch; it is a replacement, not a deletion.
		if _, err := os.Stat(filepath.Join(w.root, filepath.FromSlash(strings.TrimPrefix(doc.Rel, w.rel+"/")))); err == nil {
			deleted = false
			touched = true
		}
	}
	if deleted && doc != nil {
		a.handleOpenDocDeleted(w, doc)
		return
	}

	if structural {
		a.refreshTreeSlowly()
		// A dirty document that went missing is kept: the author may
		// still want to save it. A clean one closes, as the file did.
		if a.doc == nil && doc != nil && doc.Dirty {
			a.doc = doc
			a.selected = doc.Rel
		}
		if doc != nil && a.doc == doc && a.findNode(doc.Rel) == nil {
			a.handleOpenDocDeleted(w, doc)
			return
		}
		if a.selected == "" && selected != "" {
			a.selectAfterRemoval(selected, w.rel)
		}
	}

	if touched {
		a.dashboardCache = nil
		if target := a.doc; target != nil {
			if md, mtime, err := a.vault.ReadDoc(target.Rel); err == nil {
				a.reloadIfChanged(target, md, mtime)
			} else if os.IsNotExist(err) {
				a.handleOpenDocDeleted(w, target)
				return
			}
		}
	}
	a.update()
}

// adoptExternalMove keeps the open document open when another program
// moves it or a folder above it.
func (a *app) adoptExternalMove(doc *docState, newRel, oldRel string, isDir bool) {
	a.moveNavigation(oldRel, newRel)
	doc.Rel = newRel
	doc.Title = titleOf(newRel)
	doc.TitleDraft = doc.Title
	if relWithin(a.selected, oldRel) {
		a.selected = newRel + strings.TrimPrefix(a.selected, oldRel)
	}
	a.movePageSettings(oldRel, newRel)
	if isDir {
		a.moveOpenDirs(oldRel, newRel)
	}
	for parent := parentRel(newRel); parent != ""; parent = parentRel(parent) {
		*a.openState(parent) = true
	}
	a.sendWebTitle()
}

// movePageSettings moves the display settings of a renamed document or
// of the documents in a renamed folder.
func (a *app) movePageSettings(oldRel, newRel string) {
	if a.cfg == nil || a.cfg.PageSettings == nil {
		return
	}
	moved := false
	for rel, settings := range a.cfg.PageSettings {
		if rel != oldRel && !strings.HasPrefix(rel, oldRel+"/") {
			continue
		}
		delete(a.cfg.PageSettings, rel)
		a.cfg.PageSettings[newRel+strings.TrimPrefix(rel, oldRel)] = settings
		moved = true
	}
	if moved {
		if err := saveConfig(a.cfg); err != nil {
			a.setError(err)
		}
	}
}

// moveOpenDirs keeps the open folders of a renamed directory open.
func (a *app) moveOpenDirs(oldRel, newRel string) {
	for rel, open := range a.openDirs {
		if rel != oldRel && !strings.HasPrefix(rel, oldRel+"/") {
			continue
		}
		delete(a.openDirs, rel)
		a.openDirs[newRel+strings.TrimPrefix(rel, oldRel)] = open
	}
}

// refreshTreeSlowly rebuilds the tree and keeps a dirty document open
// when its file went away; the tree skips missing selections.
func (a *app) refreshTreeSlowly() {
	doc, selected := a.doc, a.selected
	a.refreshTree()
	if a.doc == nil && doc != nil && doc.Dirty {
		a.doc = doc
		a.selected = selected
	}
}

// reloadIfChanged replaces the open document when the file on disk has
// other text than the editor. It never discards local unsaved changes.
func (a *app) reloadIfChanged(doc *docState, md string, mtime time.Time) {
	if a.doc != doc || doc.Markdown == md {
		return
	}
	// The file holds exactly what this app last wrote: the echo of its own
	// save, reported by the watch while the author kept typing. Only text
	// from another program counts as a change.
	if md == doc.Saved {
		return
	}
	// A watch may also report an older save after a newer one. It carries
	// an older modification time and is one of the texts written here, so
	// it is not another program's edit either.
	if !mtime.After(doc.MTime) && doc.wrote(md) {
		return
	}
	if doc.Dirty {
		if !doc.externalConflict {
			doc.externalConflict = true
			a.flashToast("磁盘文件已更新，当前有未保存修改，已保留本地内容")
		}
		return
	}
	a.reloadDocFromDisk(doc, md, mtime)
}

// reloadDocFromDisk shows the text that another program wrote.
func (a *app) reloadDocFromDisk(doc *docState, md string, mtime time.Time) {
	doc.Markdown = md
	doc.Saved = md
	doc.MTime = mtime
	doc.Dirty = false
	doc.externalConflict = false
	doc.externalDeleted = false
	if doc.Web {
		a.sendWebReload(doc, md, mtime)
	} else {
		text := richtext.ParseMarkdown(md)
		if strings.TrimSpace(md) == "" {
			text = richtext.NewText("")
		}
		text = a.loadImages(text, doc.Rel)
		text, fences := a.annotateCodeFences(text, md)
		doc.Doc = a.newNativeDocument(text)
		doc.codeFences = fences
		doc.Saved = nativeMarkdown(doc.Doc.Text(), fences)
	}
	a.flashToast("文档已从磁盘更新")
	a.update()
}

// handleOpenDocDeleted closes a document another program removed, unless
// it has unsaved changes: those stay in the editor so they can still be
// saved.
func (a *app) handleOpenDocDeleted(w *kbWatch, doc *docState) {
	if a.doc != doc {
		return
	}
	if doc.Dirty {
		if !doc.externalDeleted {
			doc.externalDeleted = true
			a.flashToast("文件已在外部删除，未保存的修改仍保留")
		}
		a.refreshTreeSlowly()
		a.update()
		return
	}
	deletedRel := doc.Rel
	a.doc = nil
	a.refreshTree()
	if !a.selectAfterRemoval(deletedRel, w.rel) {
		a.selected = ""
		a.selectedIsDir = false
	}
	a.flashToast("文档已在外部删除")
	a.update()
}

// flashToast shows a short message over the page.
func (a *app) flashToast(text string) {
	if a.win == nil {
		a.toast = text
		return
	}
	a.toast = text
	deadline := time.Now().Add(2200 * time.Millisecond)
	a.toastUntil = deadline
	a.update()
	time.AfterFunc(2200*time.Millisecond, func() {
		mygo.RunOnMain(func() {
			if a.toastUntil.Equal(deadline) {
				a.toast = ""
				a.update()
			}
		})
	})
}

func kbPath(kbRel, rel string) string {
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "./")
	if rel == "" || rel == "." {
		return kbRel
	}
	return path.Join(kbRel, rel)
}

func relWithin(rel, parent string) bool {
	return rel == parent || strings.HasPrefix(rel, parent+"/")
}
