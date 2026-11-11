package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"swift/internal/remote"
)

// applyTestSync turns on a one-way mirror into a temporary folder, as an
// NFS or SMB mount would be, and waits for its first pass.
func applyTestSync(t *testing.T, a *app) (string, *remote.Engine) {
	t.Helper()
	mirror := t.TempDir()
	a.cfg.Sync = remote.Settings{
		Enabled:         true,
		Provider:        remote.ProviderNFS,
		MountPath:       mirror,
		IntervalSeconds: 300,
	}
	if err := a.applySyncSettings(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.stopSync)
	job := a.syncJob
	if job == nil {
		t.Fatal("sync job did not start")
	}
	if _, err := job.engine.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	return mirror, job.engine
}

func remoteFile(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("remote %s: %v", rel, err)
	}
	return string(b)
}

func remoteMissing(t *testing.T, root, rel string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); !os.IsNotExist(err) {
		t.Fatalf("remote %s still there (%v)", rel, err)
	}
}

// The settings window has the remote sync section, and 完成 starts the
// mirror with the saved settings.
func TestSettingsRemoteSyncSection(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Sync = remote.Settings{
		Enabled:   true,
		Provider:  remote.ProviderNFS,
		MountPath: t.TempDir(),
	}
	tt := ui.NewTester(a.view, 1180, 760)
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("远端同步"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"远端同步", "启用", "挂载点", "自动同步", "测试连接", "立即同步"} {
		if !tt.HasText(want) {
			t.Fatalf("settings lacks %q: %q", want, tt.Texts())
		}
	}
	if err := tt.Click("完成"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.stopSync)
	if a.settingsOpen {
		t.Fatal("settings stayed open")
	}
	if a.syncJob == nil {
		t.Fatal("sync did not start after 完成")
	}
}

// The saved settings come back with the app.
func TestSyncSettingsPersist(t *testing.T) {
	a, _, _ := newTestApp(t)
	mirror := t.TempDir()
	a.cfg.Sync = remote.Settings{
		Enabled:         true,
		Provider:        remote.ProviderSMB,
		MountPath:       mirror,
		IntervalSeconds: 30,
		Manual:          true,
	}
	if err := a.applySyncSettings(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.stopSync)
	loaded := loadConfig()
	if !loaded.Sync.Enabled || loaded.Sync.Provider != remote.ProviderSMB {
		t.Fatalf("settings not saved: %+v", loaded.Sync)
	}
	if loaded.Sync.MountPath != mirror || loaded.Sync.IntervalSeconds != 30 || !loaded.Sync.Manual {
		t.Fatalf("settings changed: %+v", loaded.Sync)
	}
}

// A document uploaded once is uploaded again when it is edited later, and
// its remote copy follows the edit.
func TestSyncUploadsLaterEdit(t *testing.T) {
	a, _, rel := newTestApp(t)
	mirror, engine := applyTestSync(t, a)
	if got := remoteFile(t, mirror, rel); !strings.Contains(got, "正文内容") {
		t.Fatalf("first upload holds %q", got)
	}

	// A month later, as the user describes, the document changes.
	if _, err := a.vault.WriteDoc(rel, "# 标题\n\n一个月后的修改\n"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(filepath.Join(a.vault.Root, filepath.FromSlash(rel)), now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := remoteFile(t, mirror, rel); !strings.Contains(got, "一个月后的修改") {
		t.Fatalf("remote copy is stale: %q", got)
	}
}

// A document in the trash keeps its remote copy, so a restore works, and
// a purge deletes it from the remote too.
func TestPurgeTrashDeletesRemoteCopy(t *testing.T) {
	a, _, rel := newTestApp(t)
	mirror, engine := applyTestSync(t, a)
	remoteFile(t, mirror, rel)

	if err := a.vault.Trash(rel); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	remoteFile(t, mirror, rel) // held by the trash

	list, err := a.vault.ListTrash()
	if err != nil || len(list) != 1 {
		t.Fatalf("trash: %+v (%v)", list, err)
	}
	entry := list[0]
	if err := a.vault.PurgeTrash(entry.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.purgeRemoteNow([]TrashEntry{entry}); err != nil {
		t.Fatal(err)
	}
	remoteMissing(t, mirror, rel)
}

// Restoring a document from the trash leaves its remote copy in place.
func TestRestoreTrashKeepsRemoteCopy(t *testing.T) {
	a, _, rel := newTestApp(t)
	mirror, engine := applyTestSync(t, a)
	if err := a.vault.Trash(rel); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	list, err := a.vault.ListTrash()
	if err != nil || len(list) != 1 {
		t.Fatalf("trash: %+v (%v)", list, err)
	}
	if err := a.vault.RestoreTrash(list[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	remoteFile(t, mirror, rel)
}

// Emptying the trash deletes every remote copy that belonged to it.
func TestEmptyTrashDeletesRemoteCopies(t *testing.T) {
	a, _, rel := newTestApp(t)
	second, err := a.vault.CreateDoc("我的知识库", "第二篇")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.vault.WriteDoc(second, "第二篇内容\n"); err != nil {
		t.Fatal(err)
	}
	mirror, engine := applyTestSync(t, a)
	remoteFile(t, mirror, rel)
	remoteFile(t, mirror, second)

	for _, docRel := range []string{rel, second} {
		if err := a.vault.Trash(docRel); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := engine.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	entries, err := a.vault.ListTrash()
	if err != nil || len(entries) != 2 {
		t.Fatalf("trash: %+v (%v)", entries, err)
	}
	if err := a.vault.EmptyTrash(); err != nil {
		t.Fatal(err)
	}
	if err := a.purgeRemoteNow(entries); err != nil {
		t.Fatal(err)
	}
	remoteMissing(t, mirror, rel)
	remoteMissing(t, mirror, second)
}

// Deleting a file outside the app also deletes it from the remote.
func TestSyncDeletesFileRemovedOutsideApp(t *testing.T) {
	a, _, rel := newTestApp(t)
	mirror, engine := applyTestSync(t, a)
	if err := os.Remove(filepath.Join(a.vault.Root, filepath.FromSlash(rel))); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	remoteMissing(t, mirror, rel)
}

// Bad settings keep the old mirror running and say why in the window.
func TestSyncSettingsRejectBadMount(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Sync = remote.Settings{
		Enabled:   true,
		Provider:  remote.ProviderNFS,
		MountPath: filepath.Join(t.TempDir(), "还没有这个目录"),
	}
	if err := a.applySyncSettings(); err == nil {
		t.Fatal("a missing mount was accepted")
	}
	if a.syncJob != nil {
		t.Fatal("a job started for bad settings")
	}
}

// Purging a document also removes the pictures only it showed from the
// remote, once the local cleanup has removed them.
func TestPurgeTrashDeletesRemoteAsset(t *testing.T) {
	a, _, rel := newTestApp(t)
	asset, err := a.vault.ImportAssetBytes(rel, "图.png", testPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.vault.WriteDoc(rel, "# 图\n\n![图]("+asset+")\n"); err != nil {
		t.Fatal(err)
	}
	localAsset, ok := a.vault.ResolveAsset(rel, asset)
	if !ok {
		t.Fatal("asset does not resolve")
	}
	relAsset, err := filepath.Rel(a.vault.Root, localAsset)
	if err != nil {
		t.Fatal(err)
	}
	relAsset = filepath.ToSlash(relAsset)

	mirror, engine := applyTestSync(t, a)
	remoteFile(t, mirror, relAsset)

	if err := a.vault.Trash(rel); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Trashing keeps both the document and its picture on the remote.
	remoteFile(t, mirror, rel)
	remoteFile(t, mirror, relAsset)

	list, err := a.vault.ListTrash()
	if err != nil || len(list) != 1 {
		t.Fatalf("trash: %+v (%v)", list, err)
	}
	entry := list[0]
	if err := a.vault.PurgeTrash(entry.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.purgeRemoteNow([]TrashEntry{entry}); err != nil {
		t.Fatal(err)
	}
	remoteMissing(t, mirror, rel)
	remoteMissing(t, mirror, relAsset)
}

// A mirror folder inside the vault would copy itself, so it is refused.
func TestSyncRejectsMountInsideVault(t *testing.T) {
	a, root, _ := newTestApp(t)
	mirror := filepath.Join(root, "远端")
	if err := os.MkdirAll(mirror, 0o755); err != nil {
		t.Fatal(err)
	}
	a.cfg.Sync = remote.Settings{
		Enabled:   true,
		Provider:  remote.ProviderNFS,
		MountPath: mirror,
	}
	err := a.applySyncSettings()
	if err == nil || !strings.Contains(err.Error(), "文档目录内部") {
		t.Fatalf("error is %v", err)
	}
}

// The trash window's purge enqueues the remote deletion on the sync job.
func TestSyncPurgedQueuesRemoteDeletion(t *testing.T) {
	a, _, rel := newTestApp(t)
	mirror, engine := applyTestSync(t, a)
	remoteFile(t, mirror, rel)
	if err := a.vault.Trash(rel); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	list, err := a.vault.ListTrash()
	if err != nil || len(list) != 1 {
		t.Fatalf("trash: %+v (%v)", list, err)
	}
	entry := list[0]
	if err := a.vault.PurgeTrash(entry.ID); err != nil {
		t.Fatal(err)
	}
	a.syncPurged([]TrashEntry{entry})
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(mirror, filepath.FromSlash(rel))); os.IsNotExist(err) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the queued remote deletion did not run")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Choosing S3 shows the bucket and key fields, not the mount point.
func TestSettingsRemoteSyncS3Fields(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Sync = remote.Settings{
		Enabled:   true,
		Provider:  remote.ProviderS3,
		Bucket:    "notes",
		AccessKey: "key",
		SecretKey: "secret",
	}
	tt := ui.NewTester(a.view, 1180, 760)
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("远端同步"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"服务地址", "区域", "存储桶", "前缀", "Access Key", "Secret Key", "路径式地址"} {
		if !tt.HasText(want) {
			t.Fatalf("S3 settings lack %q: %q", want, tt.Texts())
		}
	}
	if tt.HasText("挂载点") {
		t.Fatal("S3 settings show the NFS mount point")
	}
	a.settingsOpen = false
	tt.Frame()
}

// With automatic syncing off, the first sync waits for 立即同步, and the
// result arrives as a popup.
func TestManualSyncOnlyWhenAsked(t *testing.T) {
	a, _, rel := newTestApp(t)
	mirror := t.TempDir()
	a.cfg.Sync = remote.Settings{
		Enabled:   true,
		Provider:  remote.ProviderNFS,
		MountPath: mirror,
		Manual:    true,
	}
	if err := a.applySyncSettings(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.stopSync)
	job := a.syncJob
	if job == nil || !job.manual {
		t.Fatalf("manual job is %+v", job)
	}
	if _, err := os.Stat(filepath.Join(mirror, filepath.FromSlash(rel))); !os.IsNotExist(err) {
		t.Fatalf("manual mode synced on start: %v", err)
	}
	// An edit asks for no sync either.
	a.syncTouch()
	if len(job.wake) != 0 {
		t.Fatal("an edit queued a sync in manual mode")
	}
	if !a.syncNow() {
		t.Fatal("manual sync did not reach a job")
	}

	deadline := time.Now().Add(3 * time.Second)
	tt := ui.NewTester(a.view, 1180, 760)
	for a.syncResult == nil {
		tt.Frame()
		if time.Now().After(deadline) {
			t.Fatal("the manual sync produced no popup")
		}
		time.Sleep(10 * time.Millisecond)
	}
	remoteFile(t, mirror, rel)
	for _, want := range []string{"远端同步完成", "上传 1 个文件"} {
		if !tt.HasText(want) {
			t.Fatalf("popup lacks %q: %q", want, tt.Texts())
		}
	}
	if err := tt.Click("好"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.syncResult != nil {
		t.Fatal("the popup stayed open")
	}
}

// A manual sync that fails says why in the popup.
func TestManualSyncFailurePopup(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.manualSyncDone(remote.Result{Uploaded: 1}, errors.New("网络断开"))
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	for _, want := range []string{"远端同步失败", "上传 1 个文件", "网络断开"} {
		if !tt.HasText(want) {
			t.Fatalf("popup lacks %q: %q", want, tt.Texts())
		}
	}
	if err := tt.Click("好"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.syncResult != nil {
		t.Fatal("the popup stayed open")
	}
}

// Manual mode is offered in the settings, and closing the app does not
// sync by itself.
func TestManualSyncSettingsAndQuit(t *testing.T) {
	a, _, _ := newTestApp(t)
	mirror := t.TempDir()
	a.cfg.Sync = remote.Settings{
		Enabled:   true,
		Provider:  remote.ProviderNFS,
		MountPath: mirror,
		Manual:    true,
	}
	tt := ui.NewTester(a.view, 1180, 760)
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("远端同步"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"关闭（仅手动）", "自动同步已关闭"} {
		if !tt.HasText(want) {
			t.Fatalf("settings lack %q: %q", want, tt.Texts())
		}
	}
	if err := tt.Click("完成"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.stopSync)
	if a.syncJob == nil || !a.syncJob.manual {
		t.Fatalf("the job is %+v", a.syncJob)
	}
	a.flushSync()
	if a.syncJob != nil {
		t.Fatal("flushSync kept the manual job")
	}
	if _, err := os.Stat(filepath.Join(mirror, "我的知识库", "开始.md")); !os.IsNotExist(err) {
		t.Fatalf("closing the app synced in manual mode: %v", err)
	}
}

// The settings' scrolling still works with its scroll bar indicator
// turned off.
func TestSettingsScrollsWithoutScrollbar(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Sync = remote.Settings{
		Enabled:   true,
		Provider:  remote.ProviderS3,
		Bucket:    "notes",
		AccessKey: "key",
		SecretKey: "secret",
		Manual:    true,
	}
	tt := ui.NewTester(a.view, 1180, 520)
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("远端同步"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	before, ok := tt.Find("Session Token（可选）")
	if !ok {
		t.Fatal("the secret token field is not in the settings")
	}
	tt.Scroll(600, 300, 0, 200)
	tt.Frame()
	after, ok := tt.Find("Session Token（可选）")
	if !ok {
		t.Fatal("the secret token field went away")
	}
	if after.Y >= before.Y {
		t.Fatalf("the settings did not scroll: %v -> %v", before, after)
	}
	a.settingsOpen = false
	tt.Frame()
}

// Saving the same settings while a sync runs must not restart its job;
// changed settings wait for the running sync and then restart.
func TestSyncSettingsDoNotInterruptRunningSync(t *testing.T) {
	a, _, _ := newTestApp(t)
	mirror := t.TempDir()
	a.cfg.Sync = remote.Settings{
		Enabled:   true,
		Provider:  remote.ProviderNFS,
		MountPath: mirror,
		Manual:    true,
	}
	if err := a.applySyncSettings(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.stopSync)
	job := a.syncJob
	if job == nil {
		t.Fatal("sync job did not start")
	}
	job.busy.Store(true)

	// Unchanged settings: 完成 only closes the window.
	if err := a.applySyncSettings(); err != nil {
		t.Fatal(err)
	}
	if a.syncJob != job || a.syncRestartPending {
		t.Fatal("unchanged settings restarted the running sync")
	}
	select {
	case <-job.done:
		t.Fatal("unchanged settings cancelled the running sync")
	default:
	}

	// Changed settings: save now, restart after the sync in flight.
	a.cfg.Sync.IntervalSeconds = 30
	if err := a.applySyncSettings(); err != nil {
		t.Fatal(err)
	}
	if a.syncJob != job || !a.syncRestartPending {
		t.Fatalf("changed settings did not wait: job=%p want=%p pending=%v", a.syncJob, job, a.syncRestartPending)
	}
	job.busy.Store(false)
	a.restartSyncIfPending()
	if a.syncJob == job {
		t.Fatal("the pending sync restart did not start a new job")
	}
	if a.syncJob == nil || a.syncJob.settings.IntervalSeconds != 30 {
		t.Fatalf("new job settings = %+v", a.syncJob)
	}
}
