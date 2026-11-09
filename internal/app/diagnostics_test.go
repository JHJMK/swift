package app

import (
	"archive/zip"
	"bytes"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestDiagnosticLoggerWritesByDayAndRotates(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 9, 9, 0, 0, 0, time.Local)
	logger, err := newDiagnosticLogger(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	if _, err := logger.Write([]byte("day one\n")); err != nil {
		t.Fatal(err)
	}
	now = now.Add(24 * time.Hour)
	if _, err := logger.Write([]byte("day two\n")); err != nil {
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

func TestDiagnosticLoggerKeepsThreeCalendarDays(t *testing.T) {
	dir := t.TempDir()
	for _, date := range []string{"2026-11-05", "2026-11-06", "2026-11-07", "2026-11-08"} {
		if err := os.WriteFile(filepath.Join(dir, "swift-"+date+".log"), []byte(date), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.Local)
	logger, err := newDiagnosticLogger(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })

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

func TestDiagnosticLoggerExportsOnlyRecentDays(t *testing.T) {
	dir := t.TempDir()
	for _, date := range []string{"2026-11-06", "2026-11-07", "2026-11-08", "2026-11-09"} {
		if err := os.WriteFile(filepath.Join(dir, "swift-"+date+".log"), []byte(date), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.Local)
	logger, err := newDiagnosticLogger(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	dst := filepath.Join(t.TempDir(), "logs.zip")
	if err := logger.Export(dst); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.OpenReader(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	var names []string
	for _, file := range reader.File {
		names = append(names, file.Name)
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

func TestDiagnosticLoggerRedactsCredentials(t *testing.T) {
	dir := t.TempDir()
	logger, err := newDiagnosticLogger(dir, func() time.Time {
		return time.Date(2026, 11, 9, 12, 0, 0, 0, time.Local)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	_, err = logger.Write([]byte(`{"accessKey":"AKIA","secretKey":"SECRET","sessionToken":"SESSION","Authorization":"Bearer AUTH"}` + "\n"))
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

func TestOpenDiagnosticLoggerUsesConfigDirectory(t *testing.T) {
	old := configDirOverride
	configDirOverride = t.TempDir()
	t.Cleanup(func() { configDirOverride = old })

	logger, err := openDiagnosticLogger()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	want := filepath.Join(configDirOverride, "logs")
	if got := logger.Directory(); got != want {
		t.Fatalf("Directory() = %q, want %q", got, want)
	}
}

func TestReportWebLogWritesThroughDiagnosticLogger(t *testing.T) {
	oldWriter := log.Writer()
	oldFlags := log.Flags()
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
	})

	dir := t.TempDir()
	logger, err := newDiagnosticLogger(dir, func() time.Time {
		return time.Date(2026, 11, 9, 12, 0, 0, 0, time.Local)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	log.SetOutput(io.MultiWriter(io.Discard, logger))
	log.SetFlags(0)

	a := &app{diagnostics: logger}
	a.reportWebLog("verbose", "editor warning", "at editor.ts:12")

	got, err := os.ReadFile(filepath.Join(dir, "swift-2026-11-09.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[web][info] editor warning", "at editor.ts:12"} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("log lacks %q: %s", want, got)
		}
	}
}

func TestExportDiagnosticLogs(t *testing.T) {
	dir := t.TempDir()
	logger, err := newDiagnosticLogger(dir, func() time.Time {
		return time.Date(2026, 11, 9, 12, 0, 0, 0, time.Local)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	if _, err := logger.Write([]byte("diagnostic\n")); err != nil {
		t.Fatal(err)
	}
	a := &app{diagnostics: logger}
	dst := filepath.Join(t.TempDir(), "logs.zip")

	if err := a.exportDiagnosticLogs(dst); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.OpenReader(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if len(reader.File) != 1 || reader.File[0].Name != "swift-2026-11-09.log" {
		t.Fatalf("zip files = %+v", reader.File)
	}
}

func TestSettingsShowsDiagnosticLogs(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.diagnostics, _ = newDiagnosticLogger(filepath.Join(t.TempDir(), "logs"), time.Now)
	if a.diagnostics != nil {
		t.Cleanup(func() { _ = a.diagnostics.Close() })
	}
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

// Starting the app writes a first line, so the log directory has today's
// file even when nothing else warns or fails.
func TestStartDiagnosticsWritesStartupLine(t *testing.T) {
	oldOverride := configDirOverride
	configDirOverride = t.TempDir()
	t.Cleanup(func() { configDirOverride = oldOverride })
	oldWriter, oldFlags := log.Writer(), log.Flags()
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
	})

	date := time.Now().Format("2006-01-02")
	logger := startDiagnostics()
	if logger == nil {
		t.Fatal("diagnostics did not start")
	}
	t.Cleanup(func() { _ = logger.Close() })

	got, err := os.ReadFile(filepath.Join(configDirOverride, "logs", "swift-"+date+".log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "swift: start version=") {
		t.Fatalf("startup line missing: %q", got)
	}
}
