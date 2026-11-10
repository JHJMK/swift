package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"

	"swift/internal/remote"
)

// syncJob runs the mirror on its own goroutine. Every call into the
// engine comes from this goroutine, so a settings change only has to stop
// the job and start another: two engines never write the manifest at once.
type syncJob struct {
	engine *remote.Engine
	cancel context.CancelFunc
	// wake asks for a sync after a short quiet spell, as an edit does.
	wake chan struct{}
	// now asks for a sync at once, as the settings' button does.
	now chan struct{}
	// purge carries paths whose remote copies go at once, as a purge from
	// the trash does.
	purge  chan []string
	done   chan struct{}
	update func()
	busy   atomic.Bool
	// manual leaves out the timer, the sync on start and the sync after
	// edits: only a press of 立即同步 syncs.
	manual   bool
	onManual func(remote.Result, error)
	onIdle   func()
	// settings are the settings this job runs with, so a save of the same
	// settings does not restart it.
	settings remote.Settings
}

func (j *syncJob) Wake() {
	select {
	case j.wake <- struct{}{}:
	default:
	}
}

func (j *syncJob) SyncNow() {
	select {
	case j.now <- struct{}{}:
	default:
	}
}

func (j *syncJob) Purge(prefixes []string) {
	select {
	case j.purge <- prefixes:
	default:
		// The next full sync finds the file gone from the trash and
		// deletes it, so a full queue loses promptness, not correctness.
	}
}

func (j *syncJob) run(ctx context.Context, interval time.Duration) {
	defer close(j.done)
	var tick <-chan time.Time
	if !j.manual {
		j.syncOnce(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		tick = ticker.C
	}
	var debounce <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			j.syncOnce(ctx)
		case <-j.now:
			res, err := j.syncOnce(ctx)
			if j.onManual != nil {
				j.onManual(res, err)
			}
		case prefixes := <-j.purge:
			j.busy.Store(true)
			j.update()
			_ = j.engine.DeletePaths(ctx, prefixes)
			j.busy.Store(false)
			j.update()
			if ctx.Err() == nil && j.onIdle != nil {
				j.onIdle()
			}
		case <-j.wake:
			// An edit is usually followed by another: wait for the
			// typing to stop before reading the files.
			if !j.manual && debounce == nil {
				debounce = time.After(700 * time.Millisecond)
			}
		case <-debounce:
			debounce = nil
			j.syncOnce(ctx)
		}
	}
}

func (j *syncJob) syncOnce(ctx context.Context) (remote.Result, error) {
	if ctx.Err() != nil {
		return remote.Result{}, ctx.Err()
	}
	j.busy.Store(true)
	j.update()
	res, err := j.engine.SyncNow(ctx)
	j.busy.Store(false)
	j.update()
	if ctx.Err() == nil && j.onIdle != nil {
		j.onIdle()
	}
	return res, err
}

// syncResultState is the popup after 立即同步.
type syncResultState struct {
	Title   string
	Message string
	Detail  string
	Failed  bool
}

// syncNote is the message under the remote sync settings, and the result
// of a manual sync waiting for the window to show it.
type syncNote struct {
	mu     sync.Mutex
	text   string
	isErr  bool
	result *syncResultState
}

func (n *syncNote) set(text string, isErr bool) {
	n.mu.Lock()
	n.text, n.isErr = text, isErr
	n.mu.Unlock()
}

func (n *syncNote) get() (string, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.text, n.isErr
}

func (n *syncNote) setResult(result *syncResultState) {
	n.mu.Lock()
	n.result = result
	n.mu.Unlock()
}

func (n *syncNote) takeResult() *syncResultState {
	n.mu.Lock()
	defer n.mu.Unlock()
	result := n.result
	n.result = nil
	return result
}

// startSync stops the running job and starts one for the current
// settings, when remote sync is on.
func (a *app) startSync() {
	if a.quitting {
		return
	}
	if a.vault == nil || !a.cfg.Sync.Enabled {
		a.stopSync()
		a.syncNote.set("", false)
		return
	}
	settings := a.cfg.Sync.Normalized()
	if a.syncJob != nil && a.syncJob.settings == settings {
		return
	}
	if err := a.validateSyncSettings(settings); err != nil {
		// Keep a working mirror running when only the new settings are
		// bad.
		a.syncNote.set("同步未启动："+err.Error(), true)
		a.scheduleSyncRetry()
		return
	}
	store, err := settings.Store()
	if err != nil {
		a.syncNote.set("同步未启动："+err.Error(), true)
		a.scheduleSyncRetry()
		return
	}
	// Read the manifest only once the running engine has stopped writing
	// it.
	a.stopSync()
	engine, err := remote.NewEngine(remote.Options{
		Root:         a.vault.Root,
		Store:        store,
		HeldPrefixes: a.heldPrefixes,
	})
	if err != nil {
		a.syncNote.set("同步未启动："+err.Error(), true)
		a.scheduleSyncRetry()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &syncJob{
		engine:   engine,
		cancel:   cancel,
		wake:     make(chan struct{}, 1),
		now:      make(chan struct{}, 1),
		purge:    make(chan []string, 4),
		done:     make(chan struct{}),
		update:   a.update,
		manual:   settings.Manual,
		onManual: a.manualSyncDone,
		onIdle: func() {
			// Let the next frame restart the job; the sync goroutine must
			// not wait for a main-thread start that waits for it.
			a.update()
		},
		settings: settings,
	}
	a.syncJob = job
	a.syncNote.set("", false)
	go job.run(ctx, settings.Interval())
}

// scheduleSyncRetry tries again later when the remote is not available
// yet, as an NFS or SMB share that mounts after the app starts does.
func (a *app) scheduleSyncRetry() {
	if a.syncRetryPending || a.win == nil || a.cfg.Sync.Manual {
		return
	}
	a.syncRetryPending = true
	time.AfterFunc(30*time.Second, func() {
		mygo.RunOnMain(func() {
			a.syncRetryPending = false
			if a.quitting || a.vault == nil || !a.cfg.Sync.Enabled || a.syncJob != nil {
				return
			}
			a.startSync()
			a.update()
		})
	})
}

// stopSync stops the job and waits for it to leave the engine alone.
func (a *app) stopSync() {
	job := a.syncJob
	if job == nil {
		return
	}
	a.syncJob = nil
	job.cancel()
	<-job.done
}

// syncTouch asks for a sync soon after a document or a tree changed.
func (a *app) syncTouch() {
	if job := a.syncJob; job != nil && !job.manual {
		job.Wake()
	}
}

// applySyncSettings checks the settings, saves them and starts a job for
// them. The old job keeps running when the new settings are not valid.
// Settings that did not change never restart a running job, and a change
// made while one is syncing waits for that sync to finish.
func (a *app) applySyncSettings() error {
	settings := a.cfg.Sync.WithDefaults()
	if err := a.validateSyncSettings(settings); err != nil {
		return err
	}
	settings = settings.Normalized()
	a.cfg.Sync = settings
	if err := saveConfig(a.cfg); err != nil {
		return fmt.Errorf("保存设置失败：%w", err)
	}
	if job := a.syncJob; job != nil {
		if job.settings == settings {
			a.syncRestartPending = false
			return nil
		}
		if job.busy.Load() {
			a.syncRestartPending = true
			a.syncNote.set("设置已保存，当前同步结束后会重启同步。", false)
			a.flashToast("设置已保存，当前同步结束后会重启同步")
			return nil
		}
	}
	a.syncRestartPending = false
	a.startSync()
	return nil
}

// restartSyncIfPending restarts the mirror after the sync that was in
// flight when the settings changed has finished.
func (a *app) restartSyncIfPending() {
	if !a.syncRestartPending || a.quitting || a.vault == nil {
		return
	}
	job := a.syncJob
	if job == nil || job.busy.Load() {
		return
	}
	a.syncRestartPending = false
	a.startSync()
	a.update()
}

// syncNow asks the running job to sync at once and reports whether
// there is a job to ask. The result arrives in the window as a popup.
func (a *app) syncNow() bool {
	job := a.syncJob
	if job == nil {
		return false
	}
	job.SyncNow()
	return true
}

// manualSyncDone shows the outcome of 立即同步. It runs on the sync job's
// goroutine, so it only touches the locked note; the window picks the
// popup up in its next frame.
func (a *app) manualSyncDone(res remote.Result, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	if err != nil {
		msg := fmt.Sprintf("上传 %d 个文件，删除 %d 个文件。", res.Uploaded, res.Deleted)
		if res.Uploaded == 0 && res.Deleted == 0 {
			msg = "没有文件成功同步。"
		}
		a.syncNote.set("同步失败："+err.Error(), true)
		a.syncNote.setResult(&syncResultState{
			Title:   "远端同步失败",
			Message: msg,
			Detail:  err.Error(),
			Failed:  true,
		})
		a.update()
		return
	}
	msg := "远端已经是最新的，没有文件需要上传或删除。"
	if res.Uploaded > 0 || res.Deleted > 0 {
		msg = fmt.Sprintf("上传 %d 个文件，删除 %d 个文件。", res.Uploaded, res.Deleted)
	}
	a.syncNote.set(msg, false)
	a.syncNote.setResult(&syncResultState{Title: "远端同步完成", Message: msg})
	a.update()
}

// testSync checks the remote's address and credentials without touching
// the vault's manifest.
func (a *app) testSync() {
	settings := a.cfg.Sync.WithDefaults()
	if err := a.validateSyncSettings(settings); err != nil {
		a.syncNote.set(err.Error(), true)
		return
	}
	store, err := settings.Store()
	if err != nil {
		a.syncNote.set(err.Error(), true)
		return
	}
	a.syncNote.set("正在测试连接…", false)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := store.Ping(ctx); err != nil {
			a.syncNote.set("连接失败："+err.Error(), true)
		} else {
			a.syncNote.set("连接成功。", false)
		}
		a.update()
	}()
}

// validateSyncSettings checks the settings and, for a mounted share,
// that the mirror does not sit inside the vault or hold it: either would
// let the mirror copy itself or mix with the notes.
func (a *app) validateSyncSettings(settings remote.Settings) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	if a.vault == nil {
		return nil
	}
	if settings.Provider != remote.ProviderNFS && settings.Provider != remote.ProviderSMB {
		return nil
	}
	mount, err := filepath.Abs(settings.MountPath)
	if err != nil {
		return err
	}
	root := filepath.Clean(a.vault.Root)
	mount = filepath.Clean(mount)
	// A symlink must not hide that the mirror is inside the vault.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if resolved, err := filepath.EvalSymlinks(mount); err == nil {
		mount = resolved
	}
	sep := string(os.PathSeparator)
	if mount == root || strings.HasPrefix(mount, root+sep) {
		return errors.New("同步目录不能位于文档目录内部")
	}
	if strings.HasPrefix(root, mount+sep) {
		return errors.New("同步目录不能包含文档目录")
	}
	return nil
}

// heldPrefixes lists the paths whose files are in the trash. The mirror
// keeps their remote copies until the trash entry is purged.
func (a *app) heldPrefixes() ([]string, error) {
	if a.vault == nil {
		return nil, nil
	}
	entries, err := a.vault.ListTrash()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		out = append(out, entry.OriginalRel)
		if !entry.IsDir && entry.HasChildren {
			// A document's children live in the folder beside it, which
			// leaves with the same trash entry.
			out = append(out, strings.TrimSuffix(entry.OriginalRel, path.Ext(entry.OriginalRel)))
		}
	}
	return out, nil
}

// syncPurged tells the job that trash entries are gone for good, so their
// remote copies go now rather than at the next scan.
func (a *app) syncPurged(entries []TrashEntry) {
	job := a.syncJob
	if job == nil || len(entries) == 0 {
		return
	}
	var prefixes []string
	for _, entry := range entries {
		prefixes = append(prefixes, entry.OriginalRel)
		if !entry.IsDir && entry.HasChildren {
			prefixes = append(prefixes, strings.TrimSuffix(entry.OriginalRel, path.Ext(entry.OriginalRel)))
		}
	}
	job.Purge(prefixes)
}

// purgeRemoteNow deletes the remote copies of entries in the foreground.
// The trash window uses syncPurged; tests use this one.
func (a *app) purgeRemoteNow(entries []TrashEntry) error {
	job := a.syncJob
	if job == nil || len(entries) == 0 {
		return nil
	}
	var prefixes []string
	for _, entry := range entries {
		prefixes = append(prefixes, entry.OriginalRel)
		if !entry.IsDir && entry.HasChildren {
			prefixes = append(prefixes, strings.TrimSuffix(entry.OriginalRel, path.Ext(entry.OriginalRel)))
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return job.engine.DeletePaths(ctx, prefixes)
}

// flushSync sends what the last edits left before the app quits. It
// stops the job first, so no other sync is in the engine's way.
func (a *app) flushSync() {
	job := a.syncJob
	if job == nil {
		return
	}
	if job.manual {
		// In manual mode closing the app is not a sync trigger.
		a.stopSync()
		return
	}
	a.syncJob = nil
	job.cancel()
	<-job.done
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = job.engine.SyncNow(ctx)
}

func providerLabel(provider remote.Provider) string {
	if provider == "" {
		return ""
	}
	return provider.Label()
}

func providerFromLabel(label string) remote.Provider {
	switch label {
	case "S3":
		return remote.ProviderS3
	case "NFS":
		return remote.ProviderNFS
	case "SMB":
		return remote.ProviderSMB
	}
	return ""
}

func syncIntervalLabel(settings remote.Settings) string {
	if settings.Manual {
		return "关闭（仅手动）"
	}
	return intervalLabel(settings.IntervalSeconds)
}

func intervalLabel(seconds int) string {
	switch seconds {
	case 30:
		return "30 秒"
	case 60:
		return "1 分钟"
	case 300:
		return "5 分钟"
	}
	return "15 秒"
}

func intervalSeconds(label string) int {
	switch label {
	case "30 秒":
		return 30
	case "1 分钟":
		return 60
	case "5 分钟":
		return 300
	}
	return 15
}
