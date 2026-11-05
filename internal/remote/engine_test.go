package remote

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// countingStore records what the engine asked the remote to do.
type countingStore struct {
	Store
	mu      sync.Mutex
	puts    []string
	deletes []string
}

func (s *countingStore) PutFile(ctx context.Context, rel, path string) error {
	s.mu.Lock()
	s.puts = append(s.puts, rel)
	s.mu.Unlock()
	return s.Store.PutFile(ctx, rel, path)
}

func (s *countingStore) Delete(ctx context.Context, rel string) error {
	s.mu.Lock()
	s.deletes = append(s.deletes, rel)
	s.mu.Unlock()
	return s.Store.Delete(ctx, rel)
}

func (s *countingStore) counts() (puts, deletes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.puts), len(s.deletes)
}

func writeTestFile(t *testing.T, root, rel, content string, mtime time.Time) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if !mtime.IsZero() {
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
}

func readTestFile(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

func mustNotExist(t *testing.T, root, rel string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); !os.IsNotExist(err) {
		t.Fatalf("%s still exists (%v)", rel, err)
	}
}

func newEngineForTest(t *testing.T, vault, mirror string, held *[]string) (*Engine, *countingStore) {
	t.Helper()
	dir, err := NewDirStore(mirror, ProviderNFS)
	if err != nil {
		t.Fatal(err)
	}
	store := &countingStore{Store: dir}
	opts := Options{
		Root:  vault,
		Store: store,
		HeldPrefixes: func() ([]string, error) {
			if held == nil {
				return nil, nil
			}
			return *held, nil
		},
	}
	engine, err := NewEngine(opts)
	if err != nil {
		t.Fatal(err)
	}
	return engine, store
}

// A file synced once and changed much later is uploaded again, the case a
// one-shot full or incremental copy would miss.
func TestEngineUploadsLaterChange(t *testing.T) {
	vault, mirror := t.TempDir(), t.TempDir()
	old := time.Now().Add(-30 * 24 * time.Hour)
	writeTestFile(t, vault, "知识库/日记.md", "第一版\n", old)
	engine, _ := newEngineForTest(t, vault, mirror, nil)

	res, err := engine.SyncNow(context.Background())
	if err != nil || res.Uploaded != 1 {
		t.Fatalf("first sync: %+v (%v)", res, err)
	}
	if got := readTestFile(t, mirror, "知识库/日记.md"); got != "第一版\n" {
		t.Fatalf("remote holds %q", got)
	}

	// A month later the user edits the document: its modification time
	// moves, and the next sync sends it.
	writeTestFile(t, vault, "知识库/日记.md", "第二版\n", time.Now())
	res, err = engine.SyncNow(context.Background())
	if err != nil || res.Uploaded != 1 {
		t.Fatalf("second sync: %+v (%v)", res, err)
	}
	if got := readTestFile(t, mirror, "知识库/日记.md"); got != "第二版\n" {
		t.Fatalf("remote holds %q after the edit", got)
	}
}

// An unchanged file is not uploaded again.
func TestEngineSkipsUnchangedFiles(t *testing.T) {
	vault, mirror := t.TempDir(), t.TempDir()
	writeTestFile(t, vault, "库/一.md", "内容\n", time.Now())
	engine, store := newEngineForTest(t, vault, mirror, nil)
	if _, err := engine.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	puts, deletes := store.counts()
	if puts != 1 || deletes != 0 {
		t.Fatalf("puts %d deletes %d, want 1 and 0", puts, deletes)
	}
}

// Removing a file takes its remote copy with it.
func TestEngineDeletesMissingFile(t *testing.T) {
	vault, mirror := t.TempDir(), t.TempDir()
	writeTestFile(t, vault, "库/要删.md", "内容\n", time.Now())
	engine, store := newEngineForTest(t, vault, mirror, nil)
	if _, err := engine.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(vault, "库", "要删.md")); err != nil {
		t.Fatal(err)
	}
	res, err := engine.SyncNow(context.Background())
	if err != nil || res.Deleted != 1 {
		t.Fatalf("sync after delete: %+v (%v)", res, err)
	}
	mustNotExist(t, mirror, "库/要删.md")
	if _, deletes := store.counts(); deletes != 1 {
		t.Fatalf("deletes %d, want 1", deletes)
	}
}

// A file in the trash keeps its remote copy, so a restore has something
// to come back to, and loses it once the trash entry is purged.
func TestEngineHoldsTrashedFile(t *testing.T) {
	vault, mirror := t.TempDir(), t.TempDir()
	writeTestFile(t, vault, "库/文档.md", "内容\n", time.Now())
	held := []string{}
	engine, _ := newEngineForTest(t, vault, mirror, &held)
	if _, err := engine.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}

	// The file moves to .swift/trash, as Vault.Trash does, and the trash
	// entry holds it.
	if err := os.Remove(filepath.Join(vault, "库", "文档.md")); err != nil {
		t.Fatal(err)
	}
	held = []string{"库/文档.md"}
	if _, err := engine.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, mirror, "库/文档.md"); got != "内容\n" {
		t.Fatalf("remote lost the trashed file: %q", got)
	}

	// Purging it from the trash releases the hold, and the next sync
	// deletes the remote copy.
	held = nil
	if _, err := engine.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	mustNotExist(t, mirror, "库/文档.md")
}

// A document with children holds the folder beside it too.
func TestEngineHoldsTrashedDocumentChildren(t *testing.T) {
	vault, mirror := t.TempDir(), t.TempDir()
	writeTestFile(t, vault, "库/父.md", "父\n", time.Now())
	writeTestFile(t, vault, "库/父/子.md", "子\n", time.Now())
	held := []string{}
	engine, _ := newEngineForTest(t, vault, mirror, &held)
	if _, err := engine.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(vault, "库", "父.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(vault, "库", "父")); err != nil {
		t.Fatal(err)
	}
	held = []string{"库/父.md", "库/父"}
	if _, err := engine.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"库/父.md", "库/父/子.md"} {
		if readTestFile(t, mirror, rel) == "" {
			t.Fatalf("%s went with the trash", rel)
		}
	}
}

// The app's own metadata, the trash and version control are not mirrored,
// while a knowledge base's .assets are.
func TestEngineSkipsMetadata(t *testing.T) {
	vault, mirror := t.TempDir(), t.TempDir()
	writeTestFile(t, vault, "库/文档.md", "内容\n", time.Now())
	writeTestFile(t, vault, "库/.assets/图.png", "png", time.Now())
	writeTestFile(t, vault, ".swift/note.json", "{}", time.Now())
	writeTestFile(t, vault, ".swift/trash/1/meta.json", "{}", time.Now())
	writeTestFile(t, vault, ".git/config", "git", time.Now())
	writeTestFile(t, vault, ".obsidian/app.json", "{}", time.Now())
	writeTestFile(t, vault, ".DS_Store", "x", time.Now())
	writeTestFile(t, vault, "库/.swift-tmp-1", "x", time.Now())
	engine, _ := newEngineForTest(t, vault, mirror, nil)
	if _, err := engine.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, mirror, "库/.assets/图.png"); got != "png" {
		t.Fatalf("asset not synced: %q", got)
	}
	for _, rel := range []string{
		".swift/note.json", ".swift/trash/1/meta.json", ".git/config",
		".obsidian/app.json", ".DS_Store", "库/.swift-tmp-1",
	} {
		mustNotExist(t, mirror, rel)
	}
}

// A rename is a deletion of the old path and an upload of the new one.
func TestEngineRename(t *testing.T) {
	vault, mirror := t.TempDir(), t.TempDir()
	writeTestFile(t, vault, "库/旧名.md", "内容\n", time.Now())
	engine, _ := newEngineForTest(t, vault, mirror, nil)
	if _, err := engine.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(vault, "库", "旧名.md"), filepath.Join(vault, "库", "新名.md")); err != nil {
		t.Fatal(err)
	}
	res, err := engine.SyncNow(context.Background())
	if err != nil || res.Uploaded != 1 || res.Deleted != 1 {
		t.Fatalf("rename sync: %+v (%v)", res, err)
	}
	mustNotExist(t, mirror, "库/旧名.md")
	if got := readTestFile(t, mirror, "库/新名.md"); got != "内容\n" {
		t.Fatalf("new remote file holds %q", got)
	}
}

// A manifest for another remote is not trusted: the vault is uploaded to
// the new destination.
func TestEngineNewTargetUploadsAgain(t *testing.T) {
	vault, mirror := t.TempDir(), t.TempDir()
	writeTestFile(t, vault, "库/文档.md", "内容\n", time.Now())
	state := filepath.Join(vault, ".swift", "sync-state.json")
	dir, err := NewDirStore(mirror, ProviderNFS)
	if err != nil {
		t.Fatal(err)
	}
	first, err := NewEngine(Options{Root: vault, Store: dir, StatePath: state})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}

	other := t.TempDir()
	otherDir, err := NewDirStore(other, ProviderSMB)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewEngine(Options{Root: vault, Store: otherDir, StatePath: state})
	if err != nil {
		t.Fatal(err)
	}
	res, err := second.SyncNow(context.Background())
	if err != nil || res.Uploaded != 1 {
		t.Fatalf("new target sync: %+v (%v)", res, err)
	}
	if got := readTestFile(t, other, "库/文档.md"); got != "内容\n" {
		t.Fatalf("new remote holds %q", got)
	}
}

// Touching a file without changing its content keeps the remote copy.
func TestEngineTouchDoesNotUpload(t *testing.T) {
	vault, mirror := t.TempDir(), t.TempDir()
	writeTestFile(t, vault, "库/文档.md", "内容\n", time.Now().Add(-time.Hour))
	engine, store := newEngineForTest(t, vault, mirror, nil)
	if _, err := engine.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(filepath.Join(vault, "库", "文档.md"), now, now); err != nil {
		t.Fatal(err)
	}
	res, err := engine.SyncNow(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 0 {
		t.Fatalf("touch uploaded %d files", res.Uploaded)
	}
	if puts, _ := store.counts(); puts != 1 {
		t.Fatalf("puts %d, want 1", puts)
	}
}

// DeletePaths removes the remote copy immediately, as a purge does.
func TestEngineDeletePaths(t *testing.T) {
	vault, mirror := t.TempDir(), t.TempDir()
	writeTestFile(t, vault, "库/文档.md", "内容\n", time.Now())
	writeTestFile(t, vault, "库/文档/子.md", "子\n", time.Now())
	engine, _ := newEngineForTest(t, vault, mirror, nil)
	if _, err := engine.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Purging removes the files from the vault first.
	if err := os.RemoveAll(filepath.Join(vault, "库", "文档.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(vault, "库", "文档")); err != nil {
		t.Fatal(err)
	}
	if err := engine.DeletePaths(context.Background(), []string{"库/文档.md", "库/文档"}); err != nil {
		t.Fatal(err)
	}
	mustNotExist(t, mirror, "库/文档.md")
	mustNotExist(t, mirror, "库/文档/子.md")
}

// The manifest lives under .swift, which is not itself mirrored.
func TestEngineManifestIsNotMirrored(t *testing.T) {
	vault, mirror := t.TempDir(), t.TempDir()
	writeTestFile(t, vault, "库/文档.md", "内容\n", time.Now())
	engine, _ := newEngineForTest(t, vault, mirror, nil)
	if _, err := engine.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(mirror, ".swift")); !os.IsNotExist(err) {
		t.Fatalf("the manifest was mirrored: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(vault, ".swift", "sync-state.json"))
	if err != nil || !strings.Contains(string(b), "库/文档.md") {
		t.Fatalf("manifest missing: %q (%v)", b, err)
	}
}

// DeletePaths leaves a remote file that still exists locally: a new
// document can take the path of a trashed one before the trash is purged.
func TestEngineDeletePathsKeepsLiveFile(t *testing.T) {
	vault, mirror := t.TempDir(), t.TempDir()
	writeTestFile(t, vault, "库/文档.md", "内容\n", time.Now())
	engine, _ := newEngineForTest(t, vault, mirror, nil)
	if _, err := engine.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := engine.DeletePaths(context.Background(), []string{"库/文档.md"}); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, mirror, "库/文档.md"); got != "内容\n" {
		t.Fatalf("a live file's remote copy went: %q", got)
	}
	if err := os.Remove(filepath.Join(vault, "库", "文档.md")); err != nil {
		t.Fatal(err)
	}
	if err := engine.DeletePaths(context.Background(), []string{"库/文档.md"}); err != nil {
		t.Fatal(err)
	}
	mustNotExist(t, mirror, "库/文档.md")
}

// A vault whose .swift folder went away, as an unmounted share leaves,
// does not lose its remote files.
func TestEngineStopsWhenVaultMetadataIsGone(t *testing.T) {
	vault, mirror := t.TempDir(), t.TempDir()
	writeTestFile(t, vault, "库/文档.md", "内容\n", time.Now())
	engine, _ := newEngineForTest(t, vault, mirror, nil)
	if _, err := engine.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(vault, ".swift")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(vault, "库", "文档.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.SyncNow(context.Background()); err == nil {
		t.Fatal("sync ran without the vault's metadata")
	}
	if got := readTestFile(t, mirror, "库/文档.md"); got != "内容\n" {
		t.Fatalf("remote file went with the metadata: %q", got)
	}
}
