package vault

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"swift/internal/markdown"
)

const (
	metaDirName       = ".swift"
	noteAssetsDirName = "note-assets"
	externalFileName  = "external.json"
	orderFileName     = "order.json"
	recentFileName    = "recent.json"
	trashDirName      = "trash"
)

// Node is a knowledge base, folder or document in the vault.
type Node struct {
	Name     string // entry name on disk; ".md" included for documents
	Rel      string // slash-separated path from the vault root
	IsDir    bool
	Children []*Node
}

// Title is the name shown for a node: the file name without ".md".
func (n *Node) Title() string {
	if n.IsDir {
		return n.Name
	}
	return strings.TrimSuffix(n.Name, filepath.Ext(n.Name))
}

// ChildrenRel is where the children of a node go: its own path for a
// folder, and the folder beside it for a document, which exists only once
// the document gets a child.
func (n *Node) ChildrenRel() string {
	if n.IsDir {
		return n.Rel
	}
	return strings.TrimSuffix(n.Rel, filepath.Ext(n.Rel))
}

// TrashEntry is a deleted node waiting in the trash.
type TrashEntry struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	OriginalRel string    `json:"originalRel"`
	IsDir       bool      `json:"isDir"`
	DeletedAt   time.Time `json:"deletedAt"`
	// HasChildren tells that a document's children folder is in the entry
	// too, beside its file.
	HasChildren bool `json:"hasChildren,omitempty"`
}

// SearchResult is one document matching a search.
type SearchResult struct {
	Rel     string
	Title   string
	Snippet string
}

// RecentEntry is a document the user opened.
type RecentEntry struct {
	Rel      string    `json:"rel"`
	OpenedAt time.Time `json:"openedAt"`
}

// Vault is a workspace folder. Knowledge bases are its top-level folders,
// folders are nested folders and documents are ".md" files.
type Vault struct {
	Root  string
	Order map[string][]string // directory rel path -> entry names
	// External maps a knowledge base's name to a folder outside the vault
	// that it reads where it is.
	External map[string]string
}

// OpenVault opens root as a vault, creating it and its metadata folder if
// needed.
func OpenVault(root string) (*Vault, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(abs, metaDirName), 0o755); err != nil {
		return nil, err
	}
	v := &Vault{Root: abs, Order: map[string][]string{}, External: map[string]string{}}
	v.loadOrder()
	v.loadExternal()
	return v, nil
}

// Tree returns the knowledge bases and everything below them.
func (v *Vault) Tree() ([]*Node, error) {
	var roots []*Node
	for _, name := range v.orderedRootNames() {
		if root, ok := v.External[name]; ok {
			if info, err := os.Stat(root); err == nil && info.IsDir() {
				roots = append(roots, v.scanNode(name, name, true))
			}
			continue
		}
		abs := filepath.Join(v.Root, name)
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		roots = append(roots, v.scanNode(name, name, true))
	}
	return roots, nil
}

// orderedRootNames lists the top-level entries of the vault: the folders
// on disk and the external knowledge bases, in their saved order, so a
// knowledge base can be dragged anywhere among them.
func (v *Vault) orderedRootNames() []string {
	names := v.orderedChildNames("")
	onDisk := map[string]bool{}
	for _, name := range names {
		onDisk[name] = true
	}
	var missing []string
	for name := range v.External {
		if !onDisk[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	names = append(names, missing...)
	rank := map[string]int{}
	for i, name := range v.Order[""] {
		rank[name] = i
	}
	sort.SliceStable(names, func(i, j int) bool {
		ri, iok := rank[names[i]]
		rj, jok := rank[names[j]]
		switch {
		case iok && jok:
			return ri < rj
		case iok:
			return true
		case jok:
			return false
		default:
			return names[i] < names[j]
		}
	})
	return names
}

func (v *Vault) scanNode(rel, name string, isDir bool) *Node {
	n := &Node{Name: name, Rel: rel, IsDir: isDir}
	if !isDir {
		return n
	}
	names := v.orderedChildNames(rel)
	// A document's children live in the folder beside it, which the tree
	// shows as the document's: the folder itself is not a row.
	docs := make(map[string]bool, len(names))
	for _, child := range names {
		if isMarkdown(child) {
			docs[strings.TrimSuffix(child, filepath.Ext(child))] = true
		}
	}
	parentAbs, err := v.abs(rel)
	if err != nil {
		return n
	}
	for _, child := range names {
		abs := filepath.Join(parentAbs, child)
		info, err := os.Stat(abs)
		if err != nil || strings.HasPrefix(child, ".") {
			continue
		}
		childRel := path.Join(rel, child)
		switch {
		case info.IsDir():
			if docs[child] {
				continue
			}
			n.Children = append(n.Children, v.scanNode(childRel, child, true))
		case isMarkdown(child):
			doc := &Node{Name: child, Rel: childRel}
			base := strings.TrimSuffix(child, filepath.Ext(child))
			if info, err := os.Stat(filepath.Join(parentAbs, base)); err == nil && info.IsDir() {
				doc.Children = v.scanNode(path.Join(rel, base), base, true).Children
			}
			n.Children = append(n.Children, doc)
		}
	}
	return n
}

// ReadDoc reads a document and its modification time.
func (v *Vault) ReadDoc(rel string) (string, time.Time, error) {
	abs, err := v.abs(rel)
	if err != nil {
		return "", time.Time{}, err
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return "", time.Time{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", time.Time{}, err
	}
	return string(b), info.ModTime(), nil
}

// WriteDoc writes a document atomically and returns its new modification
// time.
func (v *Vault) WriteDoc(rel, content string) (time.Time, error) {
	abs, err := v.abs(rel)
	if err != nil {
		return time.Time{}, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return time.Time{}, err
	}
	if err := writeFileAtomic(abs, []byte(content)); err != nil {
		return time.Time{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

// CreateKnowledgeBase creates a top-level folder and returns its relative
// path.
func (v *Vault) CreateKnowledgeBase(name string) (string, error) {
	name = v.uniqueName(v.Root, sanitizeName(name), "")
	for v.External[name] != "" {
		name = v.uniqueName(v.Root, name+" 2", "")
	}
	if err := os.Mkdir(filepath.Join(v.Root, name), 0o755); err != nil {
		return "", err
	}
	v.insertOrder("", name, "")
	return name, v.saveOrder()
}

// CreateFolder creates a folder under parentRel.
func (v *Vault) CreateFolder(parentRel, name string) (string, error) {
	parentAbs, err := v.abs(parentRel)
	if err != nil {
		return "", err
	}
	name = v.uniqueName(parentAbs, sanitizeName(name), "")
	if err := os.Mkdir(filepath.Join(parentAbs, name), 0o755); err != nil {
		return "", err
	}
	v.insertOrder(parentRel, name, "")
	rel := path.Join(parentRel, name)
	return rel, v.saveOrder()
}

// CreateDoc creates an empty Markdown document under parentRel and returns
// its relative path.
func (v *Vault) CreateDoc(parentRel, title string) (string, error) {
	parentAbs, err := v.abs(parentRel)
	if err != nil {
		return "", err
	}
	title = strings.TrimSpace(title)
	title = strings.TrimSuffix(strings.TrimSuffix(title, ".md"), ".MD")
	name := v.uniqueName(parentAbs, sanitizeName(title), ".md")
	if err := writeFileAtomic(filepath.Join(parentAbs, name), nil); err != nil {
		return "", err
	}
	v.insertOrder(parentRel, name, "")
	rel := path.Join(parentRel, name)
	return rel, v.saveOrder()
}

// docDir returns the folder beside a document, where its children live,
// when it exists.
func (v *Vault) docDir(rel string) (string, bool) {
	if !isMarkdown(filepath.Base(rel)) {
		return "", false
	}
	dirRel := strings.TrimSuffix(rel, filepath.Ext(rel))
	abs, err := v.abs(dirRel)
	if err != nil {
		return "", false
	}
	if info, err := os.Stat(abs); err == nil && info.IsDir() {
		return dirRel, true
	}
	return "", false
}

// EnsureDocDir creates, when needed, and returns the folder beside a
// document, where its children live.
func (v *Vault) EnsureDocDir(rel string) (string, error) {
	if !isMarkdown(filepath.Base(rel)) {
		return "", errors.New("只能在文档下创建子项")
	}
	dirRel := strings.TrimSuffix(rel, filepath.Ext(rel))
	if dir, ok := v.docDir(rel); ok {
		return dir, nil
	}
	parent, name, err := splitRel(dirRel)
	if err != nil {
		return "", err
	}
	abs, err := v.abs(dirRel)
	if err != nil {
		return "", err
	}
	if err := os.Mkdir(abs, 0o755); err != nil {
		return "", err
	}
	v.insertOrder(parent, name, "")
	return dirRel, v.saveOrder()
}

// Rename renames a node and returns its new relative path.
func (v *Vault) Rename(rel, newName string) (string, error) {
	parent, oldEntry, err := splitRel(rel)
	if err != nil {
		return "", err
	}
	parentAbs, err := v.abs(parent)
	if err != nil {
		return "", err
	}
	oldAbs := filepath.Join(parentAbs, oldEntry)
	info, err := os.Stat(oldAbs)
	if err != nil {
		return "", err
	}
	base := strings.TrimSpace(newName)
	ext := ""
	if !info.IsDir() {
		ext = filepath.Ext(oldEntry)
		base = strings.TrimSuffix(base, ".md")
		base = strings.TrimSuffix(base, ".MD")
	}
	newEntry := sanitizeName(base) + ext
	if newEntry == oldEntry {
		return rel, nil
	}
	newEntry = v.uniqueName(parentAbs, sanitizeName(base), ext)
	// The folder holding a document's children moves with it, under the
	// same name.
	oldDir, hasDir := "", false
	if !info.IsDir() {
		oldDir, hasDir = v.docDir(rel)
	}
	oldBase := strings.TrimSuffix(oldEntry, ext)
	newBase := strings.TrimSuffix(newEntry, ext)
	if hasDir && oldBase != newBase {
		if _, err := os.Stat(filepath.Join(parentAbs, newBase)); err == nil {
			return "", fmt.Errorf("“%s”已存在，无法重命名", newBase)
		}
	}
	newAbs := filepath.Join(parentAbs, newEntry)
	if err := os.Rename(oldAbs, newAbs); err != nil {
		return "", err
	}
	newRel := path.Join(parent, newEntry)
	v.replaceOrder(parent, oldEntry, newEntry)
	if hasDir && oldBase != newBase {
		oldDirAbs, err := v.abs(oldDir)
		if err != nil {
			return "", err
		}
		newDirRel := path.Join(parent, newBase)
		newDirAbs, err := v.abs(newDirRel)
		if err != nil {
			return "", err
		}
		if err := os.Rename(oldDirAbs, newDirAbs); err != nil {
			_ = os.Rename(newAbs, oldAbs)
			v.replaceOrder(parent, newEntry, oldEntry)
			return "", err
		}
		v.replaceOrder(parent, oldBase, newBase)
		v.remapOrder(oldDir, newDirRel)
	}
	if info.IsDir() {
		v.remapOrder(rel, newRel)
	}
	return newRel, v.saveOrder()
}

// Move moves rel under newParentRel, or reorders it inside its own parent
// when newParentRel is that parent: the node then goes before beforeName,
// or at the end when beforeName is empty. A folder target receives the
// node as its last child; a document target inserts the node before it in
// that document's folder. Knowledge bases only move among knowledge bases.
func (v *Vault) Move(rel, newParentRel, beforeName string) (string, error) {
	oldParent, oldName, err := splitRel(rel)
	if err != nil {
		return "", err
	}
	if oldParent == "" && newParentRel != "" {
		return "", errors.New("知识库不能移动到其他目录中")
	}
	if newParentRel != "" && !v.isDir(newParentRel) {
		return "", errors.New("目标不是文件夹")
	}
	if rel == newParentRel || strings.HasPrefix(newParentRel, rel+"/") {
		return "", errors.New("不能移动到自身内部")
	}
	// Dropping a node on the strip just below its previous sibling asks
	// it to go where it already is; moving it to the end would surprise.
	if newParentRel == oldParent && beforeName == oldName {
		return rel, nil
	}
	oldAbs, err := v.abs(rel)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(oldAbs)
	if err != nil {
		return "", err
	}
	newParentAbs, err := v.abs(newParentRel)
	if err != nil {
		return "", err
	}

	// An external knowledge base only changes its place among the others;
	// its folder stays where it is outside the vault.
	if oldParent == "" && newParentRel == "" && v.External[oldName] != "" {
		v.Order[""] = insertAt(without(v.orderList(""), oldName), oldName, beforeName)
		return rel, v.saveOrder()
	}

	newName := oldName
	if newParentRel != oldParent {
		if info.IsDir() {
			newName = v.uniqueName(newParentAbs, oldName, "")
		} else {
			newName = v.uniqueName(newParentAbs, strings.TrimSuffix(oldName, filepath.Ext(oldName)), filepath.Ext(oldName))
		}
	}
	// A document's children folder travels with the document.
	oldDir, hasDir := "", false
	if !info.IsDir() {
		oldDir, hasDir = v.docDir(rel)
	}
	oldBase := strings.TrimSuffix(oldName, filepath.Ext(oldName))
	newBase := strings.TrimSuffix(newName, filepath.Ext(newName))
	newDirRel := path.Join(newParentRel, newBase)
	newDirAbs, err := v.abs(newDirRel)
	if err != nil {
		return "", err
	}
	oldDirAbs := ""
	if hasDir {
		oldDirAbs, err = v.abs(oldDir)
		if err != nil {
			return "", err
		}
	}
	moveDir := hasDir && filepath.Clean(newDirAbs) != filepath.Clean(oldDirAbs)
	if moveDir {
		if _, err := os.Stat(newDirAbs); err == nil {
			return "", fmt.Errorf("“%s”已存在，无法移动", newBase)
		}
	}
	newAbs := filepath.Join(newParentAbs, newName)
	if filepath.Clean(newAbs) != filepath.Clean(oldAbs) {
		if err := movePath(oldAbs, newAbs); err != nil {
			return "", err
		}
	}
	newRel := path.Join(newParentRel, newName)
	if moveDir {
		if err := movePath(oldDirAbs, newDirAbs); err != nil {
			_ = movePath(newAbs, oldAbs)
			return "", err
		}
		v.remapOrder(oldDir, newDirRel)
	}

	oldOrder := v.orderList(oldParent)
	if newParentRel == oldParent {
		order := without(oldOrder, oldName)
		order = insertAt(order, newName, beforeName)
		v.Order[oldParent] = order
	} else {
		targetOrder := v.orderList(newParentRel)
		v.Order[oldParent] = without(oldOrder, oldName)
		targetOrder = without(targetOrder, newName)
		v.Order[newParentRel] = insertAt(targetOrder, newName, beforeName)
	}
	if hasDir {
		if newParentRel != oldParent {
			v.removeOrder(oldParent, oldBase)
			v.insertOrder(newParentRel, newBase, "")
		} else if oldBase != newBase {
			v.replaceOrder(oldParent, oldBase, newBase)
		}
	}
	if info.IsDir() {
		v.remapOrder(rel, newRel)
	}
	return newRel, v.saveOrder()
}

// Trash moves a node into the vault's trash.
func (v *Vault) Trash(rel string) error {
	parent, name, err := splitRel(rel)
	if err != nil {
		return err
	}
	abs, err := v.abs(rel)
	if err != nil {
		return err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}
	oldDir, hasDir := "", false
	if !info.IsDir() {
		oldDir, hasDir = v.docDir(rel)
	}
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	entryDir := filepath.Join(v.Root, metaDirName, trashDirName, id)
	if err := os.MkdirAll(entryDir, 0o755); err != nil {
		return err
	}
	meta := TrashEntry{
		ID:          id,
		Name:        name,
		OriginalRel: rel,
		IsDir:       info.IsDir(),
		DeletedAt:   time.Now().UTC(),
		HasChildren: hasDir,
	}
	b, _ := json.MarshalIndent(meta, "", "  ")
	// Write the entry before the file moves, and write it atomically: a
	// sync reading the trash meanwhile must see the entry, not half of it,
	// or it would take the file for a deletion.
	if err := writeFileAtomic(filepath.Join(entryDir, "meta.json"), b); err != nil {
		os.RemoveAll(entryDir)
		return err
	}
	if err := movePath(abs, filepath.Join(entryDir, "data")); err != nil {
		os.RemoveAll(entryDir)
		return err
	}
	if hasDir {
		dirAbs, err := v.abs(oldDir)
		if err != nil {
			_ = movePath(filepath.Join(entryDir, "data"), abs)
			os.RemoveAll(entryDir)
			return err
		}
		if err := movePath(dirAbs, filepath.Join(entryDir, "children")); err != nil {
			_ = movePath(filepath.Join(entryDir, "data"), abs)
			os.RemoveAll(entryDir)
			return err
		}
	}
	v.removeOrder(parent, name)
	if hasDir {
		v.removeOrder(parent, path.Base(oldDir))
	}
	return v.saveOrder()
}

// ListTrash returns the entries in the trash, newest first.
func (v *Vault) ListTrash() ([]TrashEntry, error) {
	dir := filepath.Join(v.Root, metaDirName, trashDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []TrashEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name(), "meta.json"))
		if err != nil {
			continue
		}
		var t TrashEntry
		if json.Unmarshal(b, &t) == nil {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeletedAt.After(out[j].DeletedAt) })
	return out, nil
}

// RestoreTrash puts a deleted node back where it came from.
func (v *Vault) RestoreTrash(id string) error {
	t, entryDir, err := v.trashEntry(id)
	if err != nil {
		return err
	}
	dest, err := v.abs(t.OriginalRel)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("“%s”已存在，无法还原", t.OriginalRel)
	}
	childrenDest := ""
	if t.HasChildren {
		childrenDest, err = v.abs(strings.TrimSuffix(t.OriginalRel, filepath.Ext(t.OriginalRel)))
		if err != nil {
			return err
		}
		if _, err := os.Stat(childrenDest); err == nil {
			return fmt.Errorf("“%s”已存在，无法还原", strings.TrimSuffix(t.OriginalRel, filepath.Ext(t.OriginalRel)))
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if err := movePath(filepath.Join(entryDir, "data"), dest); err != nil {
		return err
	}
	if t.HasChildren {
		if err := movePath(filepath.Join(entryDir, "children"), childrenDest); err != nil {
			_ = movePath(dest, filepath.Join(entryDir, "data"))
			return err
		}
	}
	parent, name, _ := splitRel(t.OriginalRel)
	v.insertOrder(parent, name, "")
	if t.HasChildren {
		v.insertOrder(parent, path.Base(strings.TrimSuffix(t.OriginalRel, filepath.Ext(t.OriginalRel))), "")
	}
	if err := v.saveOrder(); err != nil {
		return err
	}
	return os.RemoveAll(entryDir)
}

// PurgeTrash deletes a trash entry for good.
func (v *Vault) PurgeTrash(id string) error {
	t, entryDir, err := v.trashEntry(id)
	if err != nil {
		return err
	}
	cleanErr := v.cleanupTrashImages(entryDir, t)
	if err := os.RemoveAll(entryDir); err != nil {
		return err
	}
	return cleanErr
}

// EmptyTrash permanently deletes every entry in the trash, and the images
// only those entries referred to.
func (v *Vault) EmptyTrash() error {
	entries, err := v.ListTrash()
	if err != nil {
		return err
	}
	var first error
	for _, entry := range entries {
		if err := v.PurgeTrash(entry.ID); err != nil && first == nil {
			first = err
		}
	}
	return first
}

var imageExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".bmp": true, ".svg": true, ".heic": true, ".tif": true, ".tiff": true,
}

// assetFile returns the path of a file a document refers to, when it is
// in its knowledge base's .assets.
func (v *Vault) assetFile(docRel, ref string) (string, bool) {
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
	kb := strings.Split(docRel, "/")[0]
	if kb == "" {
		return "", false
	}
	docAbs, err := v.abs(docRel)
	if err != nil {
		return "", false
	}
	var p string
	if filepath.IsAbs(ref) {
		p = ref
	} else {
		p = filepath.Join(filepath.Dir(docAbs), filepath.FromSlash(ref))
	}
	p = filepath.Clean(p)
	kbDir, err := v.abs(kb)
	if err != nil {
		return "", false
	}
	assets := filepath.Join(kbDir, ".assets")
	rel, err := filepath.Rel(assets, p)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", false
	}
	return p, true
}

// assetPath returns the path of the image a document refers to, when it
// is an image in its knowledge base's .assets.
func (v *Vault) assetPath(docRel, ref string) (string, bool) {
	p, ok := v.assetFile(docRel, ref)
	if !ok || !imageExtensions[strings.ToLower(filepath.Ext(p))] {
		return "", false
	}
	return p, true
}

type markdownSource struct {
	rel  string
	path string
}

// trashDocSources lists the documents a trash entry holds, with the paths
// they had before deletion.
func (v *Vault) trashDocSources(entryDir string, t TrashEntry) []markdownSource {
	var out []markdownSource
	add := func(rel, p string) {
		out = append(out, markdownSource{rel: rel, path: p})
	}
	switch {
	case !t.IsDir:
		add(t.OriginalRel, filepath.Join(entryDir, "data"))
		if t.HasChildren {
			base := strings.TrimSuffix(t.OriginalRel, filepath.Ext(t.OriginalRel))
			root := filepath.Join(entryDir, "children")
			_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() || !isMarkdown(d.Name()) {
					return nil
				}
				rel, err := filepath.Rel(root, p)
				if err != nil {
					return nil
				}
				add(path.Join(base, filepath.ToSlash(rel)), p)
				return nil
			})
		}
	case t.IsDir:
		root := filepath.Join(entryDir, "data")
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !isMarkdown(d.Name()) {
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return nil
			}
			add(path.Join(t.OriginalRel, filepath.ToSlash(rel)), p)
			return nil
		})
	}
	return out
}

// cleanupTrashImages deletes the pictures and attachments the entry's
// documents refer to, when no other document still refers to them. It
// only touches files in a knowledge base's .assets.
func (v *Vault) cleanupTrashImages(entryDir string, t TrashEntry) error {
	if t.IsDir && !strings.Contains(t.OriginalRel, "/") {
		// A knowledge base: its .assets are inside the entry itself.
		return nil
	}
	candidates := map[string]bool{}
	collect := func(sources []markdownSource) {
		for _, s := range sources {
			b, err := os.ReadFile(s.path)
			if err != nil {
				continue
			}
			for _, ref := range markdown.Refs(string(b)) {
				if p, ok := v.assetFile(s.rel, ref); ok {
					candidates[p] = true
				}
			}
		}
	}
	collect(v.trashDocSources(entryDir, t))
	if len(candidates) == 0 {
		return nil
	}

	keep := func(rel string, b []byte) {
		for _, ref := range markdown.Refs(string(b)) {
			if p, ok := v.assetFile(rel, ref); ok && candidates[p] {
				delete(candidates, p)
			}
		}
	}
	// Pictures and attachments the vault's documents still show.
	type liveRoot struct {
		abs    string
		prefix string
	}
	roots := []liveRoot{{abs: v.Root}}
	for name, p := range v.External {
		roots = append(roots, liveRoot{abs: p, prefix: name})
	}
	for _, r := range roots {
		_ = filepath.WalkDir(r.abs, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if p != r.abs && d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() || !isMarkdown(d.Name()) {
				return nil
			}
			rel, err := filepath.Rel(r.abs, p)
			if err != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if r.prefix != "" {
				rel = path.Join(r.prefix, rel)
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			keep(rel, b)
			return nil
		})
	}
	// Images another trash entry still shows, so restoring it keeps them.
	if entries, err := v.ListTrash(); err == nil {
		for _, other := range entries {
			if other.ID == t.ID {
				continue
			}
			if other.IsDir && !strings.Contains(other.OriginalRel, "/") {
				continue
			}
			dir := filepath.Join(v.Root, metaDirName, trashDirName, other.ID)
			for _, s := range v.trashDocSources(dir, other) {
				if b, err := os.ReadFile(s.path); err == nil {
					keep(s.rel, b)
				}
			}
		}
	}

	var first error
	for p := range candidates {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) && first == nil {
			first = err
		}
	}
	return first
}

func (v *Vault) trashEntry(id string) (TrashEntry, string, error) {
	if strings.ContainsAny(id, `/\`) || id == "" || id == "." || id == ".." {
		return TrashEntry{}, "", errors.New("无效的回收站条目")
	}
	entryDir := filepath.Join(v.Root, metaDirName, trashDirName, id)
	b, err := os.ReadFile(filepath.Join(entryDir, "meta.json"))
	if err != nil {
		return TrashEntry{}, "", err
	}
	var t TrashEntry
	if err := json.Unmarshal(b, &t); err != nil {
		return TrashEntry{}, "", err
	}
	return t, entryDir, nil
}

// Search finds documents whose name or content contains query.
func (v *Vault) Search(query string, limit int) ([]SearchResult, error) {
	return v.SearchIn(query, limit, "")
}

// SearchIn searches within scope: a knowledge base, a folder or a
// document and what is below it. An empty scope searches the whole vault.
func (v *Vault) SearchIn(query string, limit int, scope string) ([]SearchResult, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	lq := strings.ToLower(q)
	var out []SearchResult
	type searchRoot struct {
		abs    string
		prefix string
	}
	roots := []searchRoot{{abs: v.Root}}
	for name, p := range v.External {
		roots = append(roots, searchRoot{abs: p, prefix: name})
	}
	for _, r := range roots {
		root := r.abs
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if p != root && d.IsDir() && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			if d.IsDir() || !isMarkdown(d.Name()) {
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if r.prefix != "" {
				rel = path.Join(r.prefix, rel)
			}
			if scope != "" && !v.inSearchScope(rel, scope) {
				return nil
			}
			title := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
			b, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			content := string(b)
			titleHit := strings.Contains(strings.ToLower(title), lq)
			idx := strings.Index(strings.ToLower(content), lq)
			if !titleHit && idx < 0 {
				return nil
			}
			out = append(out, SearchResult{Rel: rel, Title: title, Snippet: snippet(content, idx, q)})
			if len(out) >= limit {
				return fs.SkipAll
			}
			return nil
		})
		if errors.Is(err, fs.SkipAll) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

// ImportAsset copies a file into the knowledge base's asset folder and
// returns the path to use in Markdown, relative to the document.
func (v *Vault) ImportAsset(docRel, srcPath string) (string, error) {
	b, err := os.ReadFile(srcPath)
	if err != nil {
		return "", err
	}
	return v.ImportAssetBytes(docRel, filepath.Base(srcPath), b)
}

// ImportAssetBytes writes data into the knowledge base's asset folder and
// returns the path to use in Markdown, relative to the document.
func (v *Vault) ImportAssetBytes(docRel, name string, data []byte) (string, error) {
	kb := strings.Split(docRel, "/")[0]
	if kb == "" {
		return "", errors.New("文档必须位于知识库中")
	}
	kbDir, err := v.abs(kb)
	if err != nil {
		return "", err
	}
	assetDir := filepath.Join(kbDir, ".assets")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		return "", err
	}
	name = fmt.Sprintf("%d-%s", time.Now().UnixNano(), sanitizeName(name))
	dst := filepath.Join(assetDir, name)
	if err := writeFileAtomic(dst, data); err != nil {
		return "", err
	}
	docAbs, err := v.abs(docRel)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(filepath.Dir(docAbs), dst)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// ImportNoteAssetBytes copies data into the quick note's own asset folder,
// which lives outside every knowledge base, and returns the link to use in
// the note.
func (v *Vault) ImportNoteAssetBytes(name string, data []byte) (string, error) {
	dir := filepath.Join(v.Root, metaDirName, noteAssetsDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name = strings.ReplaceAll(name, " ", "-")
	name = fmt.Sprintf("%d-%s", time.Now().UnixNano(), sanitizeName(name))
	if err := writeFileAtomic(filepath.Join(dir, name), data); err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Join(metaDirName, noteAssetsDirName, name)), nil
}

// MoveNoteAssets copies the note files still referred to by md into the
// document's knowledge base, and rewrites their links.
func (v *Vault) MoveNoteAssets(docRel, md string) (string, error) {
	dir := filepath.Join(v.Root, metaDirName, noteAssetsDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return md, nil
		}
		return md, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ref := filepath.ToSlash(filepath.Join(metaDirName, noteAssetsDirName, e.Name()))
		if !strings.Contains(md, ref) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return md, err
		}
		moved, err := v.ImportAssetBytes(docRel, e.Name(), data)
		if err != nil {
			return md, err
		}
		md = strings.ReplaceAll(md, ref, moved)
	}
	return md, nil
}

// RemoveNoteAssets forgets the quick note's asset folder.
func (v *Vault) RemoveNoteAssets() {
	_ = os.RemoveAll(filepath.Join(v.Root, metaDirName, noteAssetsDirName))
}

// ResolveAsset turns a Markdown asset URL into a local path.
func (v *Vault) ResolveAsset(docRel, url string) (string, bool) {
	if url == "" || strings.Contains(url, "://") || strings.HasPrefix(url, "data:") {
		return "", false
	}
	var base string
	if docRel == "" {
		base = v.Root
	} else {
		docAbs, err := v.abs(docRel)
		if err != nil {
			return "", false
		}
		base = filepath.Dir(docAbs)
	}
	var p string
	if filepath.IsAbs(url) {
		p = url
	} else {
		p = filepath.Join(base, filepath.FromSlash(url))
	}
	return p, true
}

// Recent returns recently opened documents that still exist.
func (v *Vault) Recent() []RecentEntry {
	var out []RecentEntry
	for _, r := range v.readRecent() {
		abs, err := v.abs(r.Rel)
		if err != nil {
			continue
		}
		if info, err := os.Stat(abs); err == nil && !info.IsDir() {
			out = append(out, r)
		}
	}
	return out
}

// TouchRecent records that a document was opened.
func (v *Vault) TouchRecent(rel string) {
	list := v.readRecent()
	filtered := list[:0]
	for _, r := range list {
		if r.Rel != rel {
			filtered = append(filtered, r)
		}
	}
	list = append([]RecentEntry{{Rel: rel, OpenedAt: time.Now().UTC()}}, filtered...)
	if len(list) > 20 {
		list = list[:20]
	}
	b, _ := json.MarshalIndent(list, "", "  ")
	_ = os.WriteFile(filepath.Join(v.Root, metaDirName, recentFileName), b, 0o644)
}

func (v *Vault) readRecent() []RecentEntry {
	b, err := os.ReadFile(filepath.Join(v.Root, metaDirName, recentFileName))
	if err != nil {
		return nil
	}
	var list []RecentEntry
	_ = json.Unmarshal(b, &list)
	return list
}

func (v *Vault) abs(rel string) (string, error) {
	clean, ok := cleanRel(rel)
	if !ok {
		return "", fmt.Errorf("无效的路径：%s", rel)
	}
	if clean == "" {
		return v.Root, nil
	}
	top, rest, _ := strings.Cut(clean, "/")
	if root, ok := v.External[top]; ok {
		if rest == "" {
			return root, nil
		}
		return filepath.Join(root, filepath.FromSlash(rest)), nil
	}
	return filepath.Join(v.Root, filepath.FromSlash(clean)), nil
}

// IsExternal reports whether rel is in an external knowledge base.
func (v *Vault) IsExternal(rel string) bool {
	top, _, _ := strings.Cut(rel, "/")
	_, ok := v.External[top]
	return ok
}

const externalFile = externalFileName

type externalEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func (v *Vault) loadExternal() {
	b, err := os.ReadFile(filepath.Join(v.Root, metaDirName, externalFile))
	if err != nil {
		return
	}
	var entries []externalEntry
	if json.Unmarshal(b, &entries) != nil {
		return
	}
	for _, e := range entries {
		if e.Name != "" && e.Path != "" {
			v.External[e.Name] = e.Path
		}
	}
}

func (v *Vault) saveExternal() error {
	var entries []externalEntry
	for name, p := range v.External {
		entries = append(entries, externalEntry{Name: name, Path: p})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(v.Root, metaDirName, externalFile), b)
}

// AddExternal adds a knowledge base reading folder where it is, and
// returns its name.
func (v *Vault) AddExternal(name, folder string) (string, error) {
	abs, err := filepath.Abs(folder)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("请选择一个文件夹")
	}
	if abs == v.Root || strings.HasPrefix(abs, v.Root+string(os.PathSeparator)) {
		return "", errors.New("该文件夹已在文档目录内，不需要作为外部知识库")
	}
	base := sanitizeName(name)
	if base == "" {
		base = filepath.Base(abs)
	}
	candidate := v.uniqueName(v.Root, base, "")
	for v.External[candidate] != "" {
		candidate = v.uniqueName(v.Root, candidate+" 2", "")
	}
	v.External[candidate] = abs
	if err := v.saveExternal(); err != nil {
		return "", err
	}
	return candidate, nil
}

// RemoveExternal forgets an external knowledge base, leaving its folder.
func (v *Vault) RemoveExternal(name string) error {
	delete(v.External, name)
	return v.saveExternal()
}

func (v *Vault) isDir(rel string) bool {
	abs, err := v.abs(rel)
	if err != nil {
		return false
	}
	info, err := os.Stat(abs)
	return err == nil && info.IsDir()
}

// IsDir reports whether rel is a folder of the vault.
func (v *Vault) IsDir(rel string) bool { return v.isDir(rel) }

// Abs turns a relative path inside the vault into an absolute path.
func (v *Vault) Abs(rel string) (string, error) { return v.abs(rel) }

// DocDir returns the folder holding a document's children, when it exists.
func (v *Vault) DocDir(rel string) (string, bool) { return v.docDir(rel) }

// AssetPath returns the path of a file a document refers to, when it is in
// its knowledge base's .assets.
func (v *Vault) AssetPath(docRel, ref string) (string, bool) { return v.assetPath(docRel, ref) }

// AssetFile returns the path of any file a document refers to, when it is
// in its knowledge base's .assets.
func (v *Vault) AssetFile(docRel, ref string) (string, bool) { return v.assetFile(docRel, ref) }

// SanitizeName makes s a name a file can have.
func SanitizeName(s string) string { return sanitizeName(s) }

// IsMarkdown reports whether name is a Markdown file.
func IsMarkdown(name string) bool { return isMarkdown(name) }

// MovePath moves a file or folder, copying and removing across volumes.
func MovePath(src, dst string) error { return movePath(src, dst) }

// WriteFileAtomic writes data to filename through a temporary file.
func WriteFileAtomic(filename string, data []byte) error { return writeFileAtomic(filename, data) }

// inSearchScope reports whether rel is scope or below it. A document's
// children live in the folder beside it, so its base counts too.
func (v *Vault) inSearchScope(rel, scope string) bool {
	if rel == scope || strings.HasPrefix(rel, scope+"/") {
		return true
	}
	if !v.IsDir(scope) && isMarkdown(scope) {
		base := strings.TrimSuffix(scope, filepath.Ext(scope))
		return strings.HasPrefix(rel, base+"/")
	}
	return false
}

func (v *Vault) orderedChildNames(parentRel string) []string {
	parentAbs, err := v.abs(parentRel)
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(parentAbs)
	if err != nil {
		return nil
	}
	var all []string
	dirSet := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		switch {
		case e.IsDir():
			all = append(all, name)
			dirSet[name] = true
		case isMarkdown(name):
			all = append(all, name)
		}
	}
	rank := map[string]int{}
	for i, name := range v.Order[parentRel] {
		rank[name] = i
	}
	sort.SliceStable(all, func(i, j int) bool {
		ri, iok := rank[all[i]]
		rj, jok := rank[all[j]]
		switch {
		case iok && jok:
			return ri < rj
		case iok:
			return true
		case jok:
			return false
		case dirSet[all[i]] != dirSet[all[j]]:
			return dirSet[all[i]]
		default:
			return all[i] < all[j]
		}
	})
	return all
}

func (v *Vault) uniqueName(dirAbs, base, ext string) string {
	candidate := base + ext
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(dirAbs, candidate)); os.IsNotExist(err) {
			return candidate
		}
		candidate = fmt.Sprintf("%s %d%s", base, i, ext)
	}
}

// orderList returns the entries of a directory in their current display
// order, with the knowledge bases' own list taking the external knowledge
// bases into account.
func (v *Vault) orderList(parentRel string) []string {
	if parentRel == "" {
		return v.orderedRootNames()
	}
	return v.orderedChildNames(parentRel)
}

func (v *Vault) insertOrder(parentRel, name, beforeName string) {
	order := without(v.orderList(parentRel), name)
	v.Order[parentRel] = insertAt(order, name, beforeName)
}

func insertAt(order []string, name, beforeName string) []string {
	if beforeName == "" || beforeName == name {
		return append(order, name)
	}
	idx := indexOf(order, beforeName)
	if idx < 0 {
		idx = len(order)
	}
	order = append(order, "")
	copy(order[idx+1:], order[idx:])
	order[idx] = name
	return order
}

func (v *Vault) removeOrder(parentRel, name string) {
	order := v.orderList(parentRel)
	v.Order[parentRel] = without(order, name)
}

func (v *Vault) replaceOrder(parentRel, oldName, newName string) {
	order := v.orderList(parentRel)
	for i, name := range order {
		if name == oldName {
			order[i] = newName
		}
	}
	v.Order[parentRel] = order
}

// remapOrder moves the order keys of a renamed or moved folder.
func (v *Vault) remapOrder(oldRel, newRel string) {
	if oldRel == newRel {
		return
	}
	moved := map[string][]string{}
	for key, names := range v.Order {
		if key == oldRel || strings.HasPrefix(key, oldRel+"/") {
			moved[newRel+strings.TrimPrefix(key, oldRel)] = names
			delete(v.Order, key)
		}
	}
	for key, names := range moved {
		v.Order[key] = names
	}
}

func (v *Vault) loadOrder() {
	b, err := os.ReadFile(filepath.Join(v.Root, metaDirName, orderFileName))
	if err != nil {
		return
	}
	var m map[string][]string
	if json.Unmarshal(b, &m) == nil && m != nil {
		v.Order = m
	}
}

func (v *Vault) saveOrder() error {
	b, err := json.MarshalIndent(v.Order, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(v.Root, metaDirName, orderFileName), b)
}

func splitRel(rel string) (parent, name string, err error) {
	clean, ok := cleanRel(rel)
	if !ok || clean == "" {
		return "", "", errors.New("无效的路径")
	}
	parent = path.Dir(clean)
	if parent == "." {
		parent = ""
	}
	return parent, path.Base(clean), nil
}

func cleanRel(rel string) (string, bool) {
	if rel == "" {
		return "", true
	}
	if strings.HasPrefix(rel, "/") || strings.Contains(rel, `\`) {
		return "", false
	}
	c := path.Clean(rel)
	if c == "." || c == ".." || strings.HasPrefix(c, "../") {
		return "", false
	}
	for _, part := range strings.Split(c, "/") {
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") {
			return "", false
		}
	}
	return c, true
}

var invalidNameChars = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]`)

func sanitizeName(s string) string {
	s = strings.TrimSpace(s)
	s = invalidNameChars.ReplaceAllString(s, "-")
	s = strings.Trim(s, " .")
	if s == "" {
		s = "未命名"
	}
	r := []rune(s)
	if len(r) > 100 {
		s = string(r[:100])
	}
	return s
}

func isMarkdown(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".md")
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

func without(list []string, s string) []string {
	out := list[:0]
	for _, v := range list {
		if v != s {
			out = append(out, v)
		}
	}
	return out
}

func snippet(content string, idx int, query string) string {
	if idx < 0 {
		idx = 0
	}
	runes := []rune(content)
	// idx is a byte offset; convert it to a rune offset.
	runeIdx := 0
	for i := range content {
		if i >= idx {
			break
		}
		runeIdx++
	}
	start := runeIdx - 40
	if start < 0 {
		start = 0
	}
	end := runeIdx + utf8.RuneCountInString(query) + 80
	if end > len(runes) {
		end = len(runes)
	}
	s := strings.Join(strings.Fields(string(runes[start:end])), " ")
	if start > 0 {
		s = "…" + s
	}
	if end < len(runes) {
		s += "…"
	}
	return s
}

func writeFileAtomic(filename string, data []byte) error {
	dir := filepath.Dir(filename)
	tmp, err := os.CreateTemp(dir, ".swift-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, filename); err != nil {
		return err
	}
	return nil
}

func movePath(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyPath(src, dst); err != nil {
		os.RemoveAll(dst)
		return err
	}
	if err := os.RemoveAll(src); err != nil {
		return err
	}
	return nil
}

func copyPath(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyPath(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

const noteFileName = "note.json"

type noteFile struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// ReadNote returns the quick note kept outside the knowledge bases.
func (v *Vault) ReadNote() (title, body string, err error) {
	b, err := os.ReadFile(filepath.Join(v.Root, metaDirName, noteFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", nil
		}
		return "", "", err
	}
	var n noteFile
	if err := json.Unmarshal(b, &n); err != nil {
		return "", "", err
	}
	return n.Title, n.Body, nil
}

// WriteNote stores the quick note kept outside the knowledge bases.
func (v *Vault) WriteNote(title, body string) error {
	if err := os.MkdirAll(filepath.Join(v.Root, metaDirName), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(noteFile{Title: title, Body: body}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(v.Root, metaDirName, noteFileName), b)
}

// ClearNote forgets the quick note.
func (v *Vault) ClearNote() error {
	err := os.Remove(filepath.Join(v.Root, metaDirName, noteFileName))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
