# 本地诊断日志 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 Go、MyGo 和 Web 编辑器诊断日志保存到本地，仅保留最近三天，并允许用户从设置中导出 ZIP。

**Architecture:** 在 `internal/app` 中增加一个并发安全的按日文件写入器，用 `io.MultiWriter` 接管标准库日志。Web 编辑器通过现有 MyGo bridge 上报警告、错误和未处理异常；设置页直接调用同一个写入器生成最近三天的 ZIP。

**Tech Stack:** Go 标准库 `log`、`archive/zip`、MyGo bridge、TypeScript、Node test runner、MyGo UI tester。

---

### Task 1: 实现按日轮转和 ZIP 导出

**Files:**
- Create: `internal/app/diagnostics.go`
- Create: `internal/app/diagnostics_test.go`

- [x] **Step 1: 写失败测试**

在 `diagnostics_test.go` 中覆盖当天写入、跨天切换、三天清理、ZIP 日期过滤和凭据脱敏：

```go
func TestDiagnosticLoggerWritesByDayAndRotates(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 9, 9, 0, 0, 0, time.Local)
	l, err := newDiagnosticLogger(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })

	if _, err := l.Write([]byte("day one\n")); err != nil {
		t.Fatal(err)
	}
	now = now.Add(24 * time.Hour)
	if _, err := l.Write([]byte("day two\n")); err != nil {
		t.Fatal(err)
	}

	for name, want := range map[string]string{
		"swift-2026-11-09.log": "day one\n",
		"swift-2026-11-10.log": "day two\n",
	} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
}
```

```go
func TestDiagnosticLoggerKeepsThreeCalendarDays(t *testing.T) {
	dir := t.TempDir()
	for _, date := range []string{"2026-11-05", "2026-11-06", "2026-11-07", "2026-11-08"} {
		if err := os.WriteFile(filepath.Join(dir, "swift-"+date+".log"), []byte(date), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.Local)
	l, err := newDiagnosticLogger(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })

	for _, date := range []string{"2026-11-07", "2026-11-08"} {
		if _, err := os.Stat(filepath.Join(dir, "swift-"+date+".log")); err != nil {
			t.Fatalf("keep %s: %v", date, err)
		}
	}
	for _, date := range []string{"2026-11-05", "2026-11-06"} {
		if _, err := os.Stat(filepath.Join(dir, "swift-"+date+".log")); !os.IsNotExist(err) {
			t.Fatalf("remove %s: %v", date, err)
		}
	}
}
```

```go
func TestDiagnosticLoggerExportsOnlyRecentDays(t *testing.T) {
	dir := t.TempDir()
	for _, date := range []string{"2026-11-06", "2026-11-07", "2026-11-08", "2026-11-09"} {
		if err := os.WriteFile(filepath.Join(dir, "swift-"+date+".log"), []byte(date), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.Local)
	l, err := newDiagnosticLogger(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })

	dst := filepath.Join(t.TempDir(), "logs.zip")
	if err := l.Export(dst); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	want := []string{
		"swift-2026-11-07.log",
		"swift-2026-11-08.log",
		"swift-2026-11-09.log",
	}
	if !slices.Equal(names, want) {
		t.Fatalf("zip names = %v, want %v", names, want)
	}
}
```

```go
func TestDiagnosticLoggerRedactsCredentials(t *testing.T) {
	dir := t.TempDir()
	l, err := newDiagnosticLogger(dir, func() time.Time {
		return time.Date(2026, 11, 9, 12, 0, 0, 0, time.Local)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })

	_, err = l.Write([]byte(`{"accessKey":"AKIA","secretKey":"SECRET","sessionToken":"SESSION","Authorization":"Bearer AUTH"}` + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "swift-2026-11-09.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"AKIA", "SECRET", "SESSION", "Bearer AUTH"} {
		if bytes.Contains(got, []byte(secret)) {
			t.Fatalf("log contains %q: %s", secret, got)
		}
	}
}
```

- [x] **Step 2: 运行测试确认失败**

Run: `go test ./internal/app -run 'TestDiagnosticLogger' -v`

Expected: FAIL because `newDiagnosticLogger` is undefined.

- [x] **Step 3: 实现最小日志写入器**

在 `diagnostics.go` 中实现以下接口和行为：

```go
type diagnosticLogger struct {
	dir  string
	now  func() time.Time
	mu   sync.Mutex
	file *os.File
	date string
}

func newDiagnosticLogger(dir string, now func() time.Time) (*diagnosticLogger, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	l := &diagnosticLogger{dir: dir, now: now}
	if err := l.cleanupLocked(l.now()); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *diagnosticLogger) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.ensureFileLocked(); err != nil {
		return 0, err
	}
	return l.file.Write(redactDiagnosticText(p))
}

func (l *diagnosticLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}
```

`ensureFileLocked` 在日期变化时关闭旧文件、清理过期文件、按 `swift-YYYY-MM-DD.log` 创建 `0600` 文件。`Directory()` 返回日志目录。`redactDiagnosticText` 至少隐藏 JSON、查询参数以及 `Authorization` 中的 `accessKey`、`secretKey`、`sessionToken`、`token` 值。

- [x] **Step 4: 实现 ZIP 导出**

`Export(dst string) error` 先加锁、关闭当前文件、执行清理，然后只把今天及前两天的现有 `swift-YYYY-MM-DD.log` 写入 ZIP。ZIP 条目按日期升序。写失败时关闭 writer 和文件并删除不完整的目标文件。

- [x] **Step 5: 运行测试确认通过**

Run: `go test ./internal/app -run 'TestDiagnosticLogger' -v`

Expected: PASS。

- [ ] **Step 6: 格式化并提交**

```bash
gofmt -w internal/app/diagnostics.go internal/app/diagnostics_test.go
git add internal/app/diagnostics.go internal/app/diagnostics_test.go
git commit -m "feat: add rotating diagnostic log store"
```

### Task 2: 接入应用启动和前端日志桥

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/main.go`
- Modify: `internal/app/web_editor.go`
- Modify: `internal/app/diagnostics.go`
- Modify: `internal/app/diagnostics_test.go`

- [x] **Step 1: 写目录和前端桥测试**

在 `diagnostics_test.go` 中增加测试，将 `configDirOverride` 指向临时目录，确认 `openDiagnosticLogger()` 使用 `<override>/logs`：

```go
func TestOpenDiagnosticLoggerUsesConfigDirectory(t *testing.T) {
	old := configDirOverride
	configDirOverride = t.TempDir()
	t.Cleanup(func() { configDirOverride = old })

	l, err := openDiagnosticLogger()
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if got, want := l.Directory(), filepath.Join(configDirOverride, "logs"); got != want {
		t.Fatalf("Directory() = %q, want %q", got, want)
	}
}
```

在 `web_editor_test.go` 或新建 `web_editor_diagnostics_test.go` 中测试 `ReportLog` 所调用的 `a.reportWebLog` 写入文件，并验证非法级别被归一化为 `info`。

- [x] **Step 2: 运行测试确认失败**

Run: `go test ./internal/app -run 'TestOpenDiagnosticLogger|TestReportWebLog' -v`

Expected: FAIL because the logger is not connected to the app.

- [x] **Step 3: 添加应用字段和初始化**

在 `app` 中增加：

```go
diagnostics *diagnosticLogger
logNote     string
logNoteErr  bool
```

在 `Run()` 最前面创建日志器，并在成功时接管标准库日志：

```go
diagnostics, err := openDiagnosticLogger()
if err != nil {
	log.Printf("swift: diagnostic logs: %v", err)
} else {
	log.SetOutput(io.MultiWriter(os.Stderr, diagnostics))
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	defer diagnostics.Close()
}
a := newApp()
a.diagnostics = diagnostics
```

如果初始化失败，应用仍正常启动，`a.diagnostics == nil`。

- [x] **Step 4: 增加前端日志方法**

在 `webEditorService` 中增加：

```go
func (s *webEditorService) ReportLog(_ context.Context, level, message, stack string) {
	mygo.RunOnMain(func() {
		s.a.reportWebLog(level, message, stack)
	})
}
```

`reportWebLog` 将级别限制为 `info`、`warn`、`error`，每行写成：

```text
[web][warn] message
stack
```

消息和堆栈各限制为 16 KiB，避免异常页面持续写入超大内容。

- [x] **Step 5: 运行测试确认通过**

Run: `go test ./internal/app -run 'TestOpenDiagnosticLogger|TestReportWebLog' -v`

Expected: PASS。

- [ ] **Step 6: 提交**

```bash
gofmt -w internal/app/app.go internal/app/main.go internal/app/web_editor.go internal/app/diagnostics.go internal/app/diagnostics_test.go internal/app/web_editor_diagnostics_test.go
git add internal/app/app.go internal/app/main.go internal/app/web_editor.go internal/app/diagnostics.go internal/app/diagnostics_test.go internal/app/web_editor_diagnostics_test.go
git commit -m "feat: capture app and editor diagnostics"
```

### Task 3: 捕获 Web 编辑器的警告和异常

**Files:**
- Create: `web/editor/src/diagnostics.ts`
- Create: `web/editor/test/diagnostics.test.ts`
- Modify: `web/editor/src/main.ts`

- [x] **Step 1: 写失败测试**

在 `diagnostics.test.ts` 中用假 bridge、假 target 和假 console 验证：

- `console.warn("warning", { code: 1 })` 调用 `SwiftEditor.ReportLog`，级别为 `warn`。
- `console.error(new Error("broken"))` 调用 `ReportLog`，级别为 `error`。
- `error` 事件的文件名、行列号进入消息。
- `unhandledrejection` 的 reason 进入消息和堆栈。
- 原生 console 方法仍被调用。
- bridge 失败不会再次调用 console，避免递归。

Run: `cd web/editor && node --test test/diagnostics.test.ts`

Expected: FAIL because `diagnostics.ts` does not exist.

- [x] **Step 2: 实现诊断安装器**

在 `diagnostics.ts` 中导出：

```ts
export interface DiagnosticTarget {
  addEventListener(type: string, listener: (event: any) => void): void;
}

export interface DiagnosticConsole {
  warn(...data: unknown[]): void;
  error(...data: unknown[]): void;
}

export function installDiagnostics(
  bridge: Pick<MyGoBridge, "call">,
  service: string,
  target: DiagnosticTarget = window,
  consoleTarget: DiagnosticConsole = console,
): void
```

实现时保存原始 `warn`/`error`，先上报再调用原始方法。所有 bridge Promise 都用不写 console 的 `.catch(() => undefined)` 吞掉，避免日志桥自身失败造成递归。

- [x] **Step 3: 接入编辑器入口**

在 `main.ts` 创建 `bridge` 后立即调用：

```ts
installDiagnostics(bridge, SERVICE);
```

确保监听器只安装一次，且保留原 console 输出。

- [x] **Step 4: 运行前端测试和类型检查**

Run:

```bash
cd web/editor
node --test test/diagnostics.test.ts
npm run check
```

Expected: 测试 PASS，TypeScript 无错误。

- [ ] **Step 5: 提交**

```bash
git add web/editor/src/diagnostics.ts web/editor/test/diagnostics.test.ts web/editor/src/main.ts
git commit -m "feat: report editor diagnostics to the app"
```

### Task 4: 设置页展示并导出日志

**Files:**
- Modify: `internal/app/view_modals.go`
- Modify: `internal/app/diagnostics.go`
- Modify: `internal/app/ui_test.go`
- Modify: `internal/app/diagnostics_test.go`

- [x] **Step 1: 写设置页和导出测试**

在 `ui_test.go` 增加：

```go
func TestSettingsShowsDiagnosticLogs(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.diagnostics, _ = newDiagnosticLogger(filepath.Join(t.TempDir(), "logs"), time.Now)
	tt := ui.NewTester(a.view, 1180, 760)
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"诊断日志", "仅保留最近 3 天", "导出最近 3 天日志…"} {
		if !tt.HasText(want) {
			t.Fatalf("settings lack %q: %q", want, tt.Texts())
		}
	}
}
```

把 ZIP 导出逻辑拆成不弹窗的 `exportDiagnosticLogs(dst string) error`，并在 `diagnostics_test.go` 中直接测试它。

- [x] **Step 2: 运行测试确认失败**

Run: `go test ./internal/app -run 'TestSettingsShowsDiagnosticLogs|TestExportDiagnosticLogs' -v`

Expected: FAIL because the settings section and export method do not exist.

- [x] **Step 3: 实现无对话框导出**

在 `diagnostics.go` 中增加：

```go
func (a *app) exportDiagnosticLogs(dst string) error {
	if a.diagnostics == nil {
		return errors.New("诊断日志尚未初始化")
	}
	return a.diagnostics.Export(dst)
}
```

- [x] **Step 4: 实现设置页按钮**

在“远端同步”和“当前内容”之间插入分隔线和“诊断日志”区域。显示日志目录和“仅保留最近 3 天，供开发者排查问题。”，按钮文案为“导出最近 3 天日志…”。

按钮点击时调用 `mygo.Dialog.Save`，默认文件名使用：

```go
"语燕日志-" + time.Now().Format("2006-01-02") + ".zip"
```

用户取消时不报错。导出成功设置 `a.logNote = "最近 3 天日志已导出。"`；失败设置 `a.logNote` 并在区域内用危险色显示。

- [x] **Step 5: 运行测试确认通过**

Run: `go test ./internal/app -run 'TestSettingsShowsDiagnosticLogs|TestExportDiagnosticLogs' -v`

Expected: PASS。

- [ ] **Step 6: 提交**

```bash
gofmt -w internal/app/view_modals.go internal/app/diagnostics.go internal/app/ui_test.go internal/app/diagnostics_test.go
git add internal/app/view_modals.go internal/app/diagnostics.go internal/app/ui_test.go internal/app/diagnostics_test.go
git commit -m "feat: export diagnostic logs from settings"
```

### Task 5: 全量验证和界面检查

**Files:**
- Modify if needed: `internal/app/webassets/**`
- Verify: 设置页实际布局

- [x] **Step 1: 构建 Web 编辑器**

Run:

```bash
cd web/editor
npm test
npm run check
npm run build
```

Expected: 所有测试和类型检查通过，构建更新 `internal/app/webassets/`。

- [x] **Step 2: 运行 Go 验证**

Run:

```bash
go test ./...
go vet ./...
go tool mygo vet .
git diff --check
```

Expected: 全部通过，没有格式或 vet 错误。

- [x] **Step 3: 启动应用并截图**

Run: `make dev`

打开“设置”，滚动到“诊断日志”，确认：

- 区域与现有设置间距一致。
- 日志路径和说明文字不溢出。
- 按钮完整可见。
- 页面仍可滚动，底部“完成”按钮不被遮挡。

保存截图并检查实际界面。

- [ ] **Step 4: 最终提交**

只在构建产物确实属于本次前端改动时提交：

```bash
git add internal/app/webassets
git commit -m "build: refresh editor assets for diagnostics"
```
