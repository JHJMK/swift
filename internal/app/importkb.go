package app

import (
	"errors"
	"io/fs"
	"log"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"swift/internal/markdown"
)

// askImportKnowledgeBase imports a folder of Markdown as a new knowledge
// base: copied into the vault, or referenced where it is.
// chooseImportFolder asks for the folder to import and returns it.
func (a *app) chooseImportFolder(title string) (string, bool) {
	if a.vault == nil {
		return "", false
	}
	a.flushTitle()
	a.flushDoc()
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:      a.win,
		Title:       title,
		ButtonLabel: "选择文件夹",
		Directory:   true,
	})
	if err != nil {
		a.setError(err)
		return "", false
	}
	if len(paths) == 0 {
		return "", false
	}
	return paths[0], true
}

// askImportKnowledgeBaseCopy imports a folder as a new knowledge base of
// the vault.
func (a *app) askImportKnowledgeBaseCopy() {
	src, ok := a.chooseImportFolder("复制导入知识库")
	if !ok {
		return
	}
	kb, err := a.importKnowledgeBaseCopy(src)
	if err != nil {
		a.setError(err)
		return
	}
	a.refreshTree()
	if n := a.findNode(kb); n != nil {
		a.openKB(n)
	}
	a.update()
}

// askImportExternalKnowledgeBase adds a folder as an external knowledge
// base, read where it is.
func (a *app) askImportExternalKnowledgeBase() {
	src, ok := a.chooseImportFolder("引用外部知识库")
	if !ok {
		return
	}
	started := time.Now()
	log.Printf("swift: import kb external: start src=%q", src)
	name, err := a.vault.AddExternal(filepath.Base(filepath.Clean(src)), src)
	if err != nil {
		log.Printf("swift: import kb external: failed after %s: %v", formatElapsed(started), err)
		a.setError(err)
		return
	}
	log.Printf("swift: import kb external: done name=%q after %s", name, formatElapsed(started))
	a.refreshTree()
	if n := a.findNode(name); n != nil {
		a.openKB(n)
	}
	a.update()
}

// importKnowledgeBaseCopy copies a folder's Markdown hierarchy into a new
// knowledge base, with the pictures and files its documents refer to.
func (a *app) importKnowledgeBaseCopy(src string) (string, error) {
	started := time.Now()
	log.Printf("swift: import kb copy: start src=%q", src)
	info, err := os.Stat(src)
	if err != nil {
		log.Printf("swift: import kb copy: failed after %s: %v", formatElapsed(started), err)
		return "", err
	}
	if !info.IsDir() {
		err := errors.New("请选择一个文件夹")
		log.Printf("swift: import kb copy: failed after %s: %v", formatElapsed(started), err)
		return "", err
	}
	kb, err := a.createKnowledgeBase(filepath.Base(filepath.Clean(src)))
	if err != nil {
		log.Printf("swift: import kb copy: failed after %s: %v", formatElapsed(started), err)
		return "", err
	}
	docs, assets := 0, 0
	dirs := map[string]string{".": kb}
	var ensureDir func(rel string) (string, error)
	ensureDir = func(rel string) (string, error) {
		if v, ok := dirs[rel]; ok {
			return v, nil
		}
		parent, err := ensureDir(path.Dir(rel))
		if err != nil {
			return "", err
		}
		child, err := a.vault.CreateFolder(parent, path.Base(rel))
		if err != nil {
			return "", err
		}
		dirs[rel] = child
		return child, nil
	}
	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || !importMarkdown(d.Name()) {
			return nil
		}
		parent, err := ensureDir(path.Dir(rel))
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		title := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
		docRel, err := a.vault.CreateDoc(parent, title)
		if err != nil {
			return err
		}
		md, copied := a.rewriteImportedMarkdown(string(data), p, docRel)
		if _, err := a.vault.WriteDoc(docRel, md); err != nil {
			return err
		}
		docs++
		assets += copied
		return nil
	})
	if err != nil {
		log.Printf("swift: import kb copy: failed kb=%q after %s docs=%d assets=%d: %v",
			kb, formatElapsed(started), docs, assets, err)
		return kb, err
	}
	log.Printf("swift: import kb copy: done kb=%q docs=%d assets=%d after %s",
		kb, docs, assets, formatElapsed(started))
	return kb, nil
}

func importMarkdown(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".md" || ext == ".markdown"
}

// rewriteImportedMarkdown copies the local pictures and attached files a
// document refers to into its assets, and points the document at them.
func (a *app) rewriteImportedMarkdown(md, srcPath, docRel string) (string, int) {
	srcDir := filepath.Dir(srcPath)
	copied := 0
	copyRef := func(target string) (string, bool) {
		ref := strings.TrimSpace(target)
		if ref == "" || strings.Contains(ref, "://") || strings.HasPrefix(ref, "data:") || strings.HasPrefix(ref, "#") {
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
			p = filepath.Join(srcDir, filepath.FromSlash(ref))
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return "", false
		}
		name := strings.ReplaceAll(filepath.Base(p), " ", "-")
		moved, err := a.vault.ImportAssetBytes(docRel, name, data)
		if err != nil {
			return "", false
		}
		copied++
		return moved, true
	}
	md = markdown.RewriteImageRefs(md, copyRef)
	md = markdown.RewriteLocalRefs(md, func(ref string, image bool) (string, bool) {
		if image {
			return "", false
		}
		ext := strings.ToLower(filepath.Ext(ref))
		if ext == ".md" || ext == ".markdown" {
			// The imported hierarchy keeps the document beside it.
			return "", false
		}
		return copyRef(ref)
	})
	return md, copied
}

// askRemoveExternal forgets an external knowledge base from the list.
func (a *app) askRemoveExternal(name string) {
	a.confirm = &confirmState{
		Title:     "移除外部知识库“" + name + "”",
		Message:   "只会从语燕的列表中移除，不会删除原来的文件夹。",
		OKLabel:   "移除",
		Dangerous: true,
		OnOK: func() {
			if err := a.vault.RemoveExternal(name); err != nil {
				a.setError(err)
				return
			}
			if err := a.forgetKBPin(name); err != nil {
				a.setError(err)
			}
			if a.doc != nil && strings.Split(a.doc.Rel, "/")[0] == name {
				a.doc = nil
			}
			if strings.Split(a.selected, "/")[0] == name {
				a.selected = ""
			}
			a.refreshTree()
			a.update()
		},
	}
}
