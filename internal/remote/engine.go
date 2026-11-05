package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// stateVersion is the layout of the manifest. A manifest of another
// version is ignored and the vault uploaded again.
const stateVersion = 1

// maxSyncErrors keeps a broken remote from filling the status line with
// one line per file.
const maxSyncErrors = 5

// Entry is what the engine last sent for one file.
type Entry struct {
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime"` // Unix nanoseconds
	Hash  string `json:"hash"`  // SHA-256 of the content
	// Held tells that the file is in the trash: its remote copy stays
	// until the file is deleted from the trash for good.
	Held bool `json:"held,omitempty"`
}

// stateFile is the manifest kept in the vault's .swift folder.
type stateFile struct {
	Version int              `json:"version"`
	Target  string           `json:"target"`
	Entries map[string]Entry `json:"entries"`
}

// Result counts what one sync did.
type Result struct {
	Uploaded int
	Deleted  int
	Scanned  int
}

// Status is what the settings window shows.
type Status struct {
	Target      string
	Description string
	Syncing     bool
	LastSync    time.Time
	LastErr     string
	Uploaded    int
	Deleted     int
	Scanned     int
	Files       int
}

// Options configures an Engine.
type Options struct {
	// Root is the vault folder.
	Root string
	// Store is the remote to mirror to.
	Store Store
	// StatePath is the manifest file, by default
	// <root>/.swift/sync-state.json.
	StatePath string
	// HeldPrefixes returns the paths whose files are in the trash, so
	// their remote copies are kept. A prefix holds the path itself and
	// everything below it. Its error stops a sync before anything is
	// deleted.
	HeldPrefixes func() ([]string, error)
}

// Engine mirrors a vault to a Store. It is safe from any goroutine; one
// sync runs at a time, and a second call waits.
type Engine struct {
	root      string
	store     Store
	statePath string
	held      func() ([]string, error)

	run chan struct{}

	mu       sync.Mutex
	entries  map[string]Entry
	syncing  bool
	lastSync time.Time
	lastErr  string
	uploaded int
	deleted  int
	scanned  int
}

// NewEngine reads the manifest of root and prepares to mirror it.
func NewEngine(opts Options) (*Engine, error) {
	if opts.Store == nil {
		return nil, errors.New("remote: no store")
	}
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("remote: vault root is not a folder")
	}
	statePath := opts.StatePath
	if statePath == "" {
		statePath = filepath.Join(root, ".swift", "sync-state.json")
	}
	e := &Engine{
		root:      root,
		store:     opts.Store,
		statePath: statePath,
		held:      opts.HeldPrefixes,
		run:       make(chan struct{}, 1),
		entries:   map[string]Entry{},
	}
	e.load()
	return e, nil
}

// Status returns a copy of the engine's state for the interface.
func (e *Engine) Status() Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	return Status{
		Target:      e.store.Target(),
		Description: e.store.Describe(),
		Syncing:     e.syncing,
		LastSync:    e.lastSync,
		LastErr:     e.lastErr,
		Uploaded:    e.uploaded,
		Deleted:     e.deleted,
		Scanned:     e.scanned,
		Files:       len(e.entries),
	}
}

// SyncNow compares the vault with the manifest and brings the remote up
// to date. It uploads new and changed files, and deletes the remote files
// whose local file is gone, unless the file is held by the trash.
func (e *Engine) SyncNow(ctx context.Context) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := e.acquire(ctx); err != nil {
		return Result{}, err
	}
	defer e.release()
	e.setSyncing(true)
	res, err := e.sync(ctx)
	e.finish(res, err)
	return res, err
}

// DeletePaths removes the remote copies of prefixes and everything below
// them now, as purging them from the trash does, without waiting for the
// next sync. Files already gone from the remote are not an error.
func (e *Engine) DeletePaths(ctx context.Context, prefixes []string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	prefixes = normalizePrefixes(prefixes)
	if len(prefixes) == 0 {
		return nil
	}
	if err := e.acquire(ctx); err != nil {
		return err
	}
	defer e.release()
	e.setSyncing(true)
	defer e.setSyncing(false)
	if e.hasEntries() {
		if _, err := os.Stat(filepath.Join(e.root, ".swift")); err != nil {
			return fmt.Errorf("文档目录不可用（找不到 .swift）：%w", err)
		}
	}
	var errs []error
	dropped := 0
	addErr := func(err error) {
		if err == nil {
			return
		}
		if len(errs) < maxSyncErrors {
			errs = append(errs, err)
		} else {
			dropped++
		}
	}
	deleted := 0
	for _, rel := range e.rels() {
		if !underPrefixes(rel, prefixes) {
			continue
		}
		// A new document may have taken the path of a trashed one. The
		// live one owns the remote copy now, so it stays.
		if e.localExists(rel) {
			continue
		}
		if err := e.store.Delete(ctx, rel); err != nil {
			addErr(err)
			continue
		}
		e.removeEntry(rel)
		deleted++
	}
	if err := e.save(); err != nil {
		addErr(fmt.Errorf("保存同步状态失败：%w", err))
	}
	err := errors.Join(errs...)
	if dropped > 0 {
		err = errors.Join(err, fmt.Errorf("另有 %d 个错误", dropped))
	}
	e.mu.Lock()
	if err == nil {
		e.lastSync = time.Now()
		e.lastErr = ""
		e.uploaded, e.scanned = 0, 0
		e.deleted = deleted
	} else if !errors.Is(err, context.Canceled) {
		e.lastErr = err.Error()
	}
	e.mu.Unlock()
	return err
}

func (e *Engine) sync(ctx context.Context) (Result, error) {
	var res Result
	var errs []error
	dropped := 0
	addErr := func(err error) {
		if err == nil {
			return
		}
		if len(errs) < maxSyncErrors {
			errs = append(errs, err)
		} else {
			dropped++
		}
	}

	// An unmounted share can leave an empty folder where the vault was.
	// With a manifest in hand, a vault without its .swift folder is not
	// the vault: uploads and, above all, deletions wait for it to come
	// back.
	if e.hasEntries() {
		if _, err := os.Stat(filepath.Join(e.root, ".swift")); err != nil {
			return res, fmt.Errorf("文档目录不可用（找不到 .swift）：%w", err)
		}
	}

	var held []string
	if e.held != nil {
		prefixes, err := e.held()
		if err != nil {
			return res, fmt.Errorf("读取回收站失败：%w", err)
		}
		held = normalizePrefixes(prefixes)
	}

	seen := make(map[string]bool)
	// An unreadable folder hides files that are still there. A sync that
	// could not see the whole vault must not take them for deletions.
	incomplete := false
	walkErr := filepath.WalkDir(e.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			incomplete = true
			if p == e.root {
				return err
			}
			// A file another program removed between the listing and
			// the read is not a failure of the mirror.
			if !os.IsNotExist(err) {
				addErr(fmt.Errorf("读取 %s 失败：%w", p, err))
			}
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		rel, relErr := filepath.Rel(e.root, p)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if e.skip(rel, d.Name(), true) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || e.skip(rel, d.Name(), false) {
			return nil
		}
		seen[rel] = true
		res.Scanned++
		info, infoErr := d.Info()
		if infoErr != nil {
			addErr(fmt.Errorf("读取 %s 失败：%w", rel, infoErr))
			return nil
		}
		entry, known := e.entry(rel)
		if known && !entry.Held && entry.Size == info.Size() &&
			entry.MTime == info.ModTime().UnixNano() {
			return nil
		}
		sum, hashErr := hashFile(p)
		if hashErr != nil {
			addErr(fmt.Errorf("读取 %s 失败：%w", rel, hashErr))
			return nil
		}
		if known && !entry.Held && entry.Hash == sum {
			// A touch, or a copy that kept the content: keep the remote
			// file and only remember the new time. A file released by the
			// trash is uploaded even when its content is unchanged, which
			// puts back a remote copy that went missing meanwhile.
			e.setEntry(rel, Entry{Size: info.Size(), MTime: info.ModTime().UnixNano(), Hash: sum})
			return nil
		}
		if err := e.store.PutFile(ctx, rel, p); err != nil {
			addErr(err)
			return nil
		}
		e.setEntry(rel, Entry{Size: info.Size(), MTime: info.ModTime().UnixNano(), Hash: sum})
		res.Uploaded++
		return nil
	})
	if walkErr != nil {
		if errors.Is(walkErr, context.Canceled) {
			return res, walkErr
		}
		incomplete = true
		addErr(walkErr)
	}

	for _, rel := range e.rels() {
		if seen[rel] {
			continue
		}
		if underPrefixes(rel, held) {
			// The file is in the trash. Its remote copy is what a restore
			// comes back from, so it stays until the trash entry is
			// deleted for good.
			e.setHeld(rel, true)
			continue
		}
		if incomplete {
			continue
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return res, ctxErr
		}
		if err := e.store.Delete(ctx, rel); err != nil {
			addErr(err)
			continue
		}
		e.removeEntry(rel)
		res.Deleted++
	}
	if incomplete {
		addErr(errors.New("有目录无法读取，本次没有删除远端文件"))
	}

	if err := e.save(); err != nil {
		addErr(fmt.Errorf("保存同步状态失败：%w", err))
	}
	err := errors.Join(errs...)
	if dropped > 0 {
		err = errors.Join(err, fmt.Errorf("另有 %d 个错误", dropped))
	}
	return res, err
}

// finish records the outcome for the status line.
func (e *Engine) finish(res Result, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.syncing = false
	e.uploaded, e.deleted, e.scanned = res.Uploaded, res.Deleted, res.Scanned
	if err == nil {
		e.lastSync = time.Now()
		e.lastErr = ""
	} else if !errors.Is(err, context.Canceled) {
		e.lastErr = err.Error()
	}
}

func (e *Engine) setSyncing(syncing bool) {
	e.mu.Lock()
	e.syncing = syncing
	e.mu.Unlock()
}

func (e *Engine) acquire(ctx context.Context) error {
	select {
	case e.run <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Engine) release() { <-e.run }

func (e *Engine) load() {
	b, err := os.ReadFile(e.statePath)
	if err != nil {
		return
	}
	var state stateFile
	if json.Unmarshal(b, &state) != nil || state.Version != stateVersion {
		return
	}
	if state.Target != e.store.Target() {
		// A manifest of another remote says nothing about this one.
		return
	}
	for rel, entry := range state.Entries {
		if entry.Hash != "" {
			e.entries[rel] = entry
		}
	}
}

func (e *Engine) save() error {
	e.mu.Lock()
	entries := make(map[string]Entry, len(e.entries))
	for rel, entry := range e.entries {
		entries[rel] = entry
	}
	e.mu.Unlock()
	b, err := json.MarshalIndent(stateFile{
		Version: stateVersion,
		Target:  e.store.Target(),
		Entries: entries,
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(e.statePath), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(e.statePath, b)
}

func (e *Engine) entry(rel string) (Entry, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	entry, ok := e.entries[rel]
	return entry, ok
}

func (e *Engine) setEntry(rel string, entry Entry) {
	e.mu.Lock()
	e.entries[rel] = entry
	e.mu.Unlock()
}

func (e *Engine) setHeld(rel string, held bool) {
	e.mu.Lock()
	if entry, ok := e.entries[rel]; ok && entry.Held != held {
		entry.Held = held
		e.entries[rel] = entry
	}
	e.mu.Unlock()
}

func (e *Engine) removeEntry(rel string) {
	e.mu.Lock()
	delete(e.entries, rel)
	e.mu.Unlock()
}

func (e *Engine) hasEntries() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.entries) > 0
}

// localExists reports whether rel is still a file of the vault.
func (e *Engine) localExists(rel string) bool {
	if _, err := os.Stat(filepath.Join(e.root, filepath.FromSlash(rel))); err == nil {
		return true
	}
	return false
}

// rels returns the paths the manifest knows, sorted so deletes happen in
// a stable order.
func (e *Engine) rels() []string {
	e.mu.Lock()
	out := make([]string, 0, len(e.entries))
	for rel := range e.entries {
		out = append(out, rel)
	}
	e.mu.Unlock()
	sort.Strings(out)
	return out
}

// skip leaves out the app's own metadata, version control and the
// temporary files an atomic write leaves for a moment.
func (e *Engine) skip(rel, name string, isDir bool) bool {
	if rel == ".swift" || rel == ".git" {
		return true
	}
	if strings.HasPrefix(rel, ".swift/") || strings.HasPrefix(rel, ".git/") {
		return true
	}
	// A folder at the vault's own level that begins with a dot is another
	// app's or version control's, not a note.
	if !strings.Contains(rel, "/") && strings.HasPrefix(name, ".") {
		return true
	}
	if isDir {
		return false
	}
	if name == ".DS_Store" || name == "Thumbs.db" || strings.HasPrefix(name, ".swift-tmp-") {
		return true
	}
	return false
}

func normalizePrefixes(prefixes []string) []string {
	var out []string
	for _, prefix := range prefixes {
		prefix = strings.Trim(strings.TrimSpace(prefix), "/")
		if prefix == "" || prefix == "." {
			continue
		}
		bad := false
		for _, part := range strings.Split(prefix, "/") {
			if part == "" || part == "." || part == ".." {
				bad = true
				break
			}
		}
		if !bad {
			out = append(out, prefix)
		}
	}
	return out
}

func underPrefixes(rel string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
			return true
		}
	}
	return false
}

func hashFile(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// writeFileAtomic writes data through a temporary file beside filename.
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
	return os.Rename(tmpName, filename)
}
