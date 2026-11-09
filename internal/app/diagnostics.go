package app

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

const diagnosticRetentionDays = 3

var (
	diagnosticLogNamePattern = regexp.MustCompile(`^swift-(\d{4}-\d{2}-\d{2})\.log$`)
	diagnosticSecretPattern  = regexp.MustCompile(`(?i)((?:access[_-]?key|secret[_-]?key|session[_-]?token|token)["']?\s*[:=]\s*["']?)([^"',\s&}]+)`)
	diagnosticAuthPattern    = regexp.MustCompile(`(?i)(authorization["']?\s*[:=]\s*["']?)([^\r\n"'}&]+)`)
)

// diagnosticLogger appends standard log output to one file per day.
type diagnosticLogger struct {
	dir  string
	now  func() time.Time
	mu   sync.Mutex
	file *os.File
	date string
}

func diagnosticLogDirectory() (string, error) {
	if configDirOverride != "" {
		return filepath.Join(configDirOverride, "logs"), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Swift", "logs"), nil
}

func openDiagnosticLogger() (*diagnosticLogger, error) {
	dir, err := diagnosticLogDirectory()
	if err != nil {
		return nil, err
	}
	return newDiagnosticLogger(dir, time.Now)
}

func newDiagnosticLogger(dir string, now func() time.Time) (*diagnosticLogger, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	logger := &diagnosticLogger{dir: dir, now: now}
	if err := logger.cleanupLocked(now()); err != nil {
		return nil, err
	}
	return logger, nil
}

func (l *diagnosticLogger) Directory() string {
	return l.dir
}

func (l *diagnosticLogger) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.ensureFileLocked(); err != nil {
		return 0, err
	}
	redacted := redactDiagnosticText(p)
	n, err := l.file.Write(redacted)
	if err != nil {
		return 0, err
	}
	if n != len(redacted) {
		return 0, io.ErrShortWrite
	}
	return len(p), nil
}

func (l *diagnosticLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closeFileLocked()
}

func (l *diagnosticLogger) ensureFileLocked() error {
	now := l.now()
	date := now.Format("2006-01-02")
	if l.file != nil && l.date == date {
		return nil
	}
	if err := l.closeFileLocked(); err != nil {
		return err
	}
	if err := l.cleanupLocked(now); err != nil {
		return err
	}
	path := filepath.Join(l.dir, "swift-"+date+".log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	l.file = file
	l.date = date
	return nil
}

func (l *diagnosticLogger) closeFileLocked() error {
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	l.date = ""
	return err
}

func (l *diagnosticLogger) cleanupLocked(now time.Time) error {
	recent := diagnosticRecentDates(now)
	keep := make(map[string]bool, len(recent))
	for _, date := range recent {
		keep[date] = true
	}
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := diagnosticLogNamePattern.FindStringSubmatch(entry.Name())
		if match == nil || keep[match[1]] {
			continue
		}
		if err := os.Remove(filepath.Join(l.dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (l *diagnosticLogger) Export(dst string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.closeFileLocked(); err != nil {
		return err
	}
	now := l.now()
	if err := l.cleanupLocked(now); err != nil {
		return err
	}

	file, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			_ = file.Close()
			_ = os.Remove(dst)
		}
	}()

	writer := zip.NewWriter(file)
	for _, date := range diagnosticRecentDates(now) {
		name := "swift-" + date + ".log"
		data, err := os.ReadFile(filepath.Join(l.dir, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		entry, err := writer.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			return err
		}
		if _, err := entry.Write(data); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Chmod(dst, 0o600); err != nil {
		return err
	}
	complete = true
	return nil
}

func diagnosticRecentDates(now time.Time) []string {
	dates := make([]string, 0, diagnosticRetentionDays)
	start := now.AddDate(0, 0, -(diagnosticRetentionDays - 1))
	for offset := 0; offset < diagnosticRetentionDays; offset++ {
		dates = append(dates, start.AddDate(0, 0, offset).Format("2006-01-02"))
	}
	return dates
}

func redactDiagnosticText(text []byte) []byte {
	redacted := diagnosticSecretPattern.ReplaceAllString(string(text), `${1}[REDACTED]`)
	redacted = diagnosticAuthPattern.ReplaceAllString(redacted, `${1}[REDACTED]`)
	return []byte(redacted)
}

func (a *app) reportWebLog(level, message, stack string) {
	switch level {
	case "warn", "error":
	default:
		level = "info"
	}
	message = truncateUTF8(message, 16<<10)
	stack = truncateUTF8(stack, 16<<10)
	entry := fmt.Sprintf("[web][%s] %s", level, message)
	if stack != "" {
		entry += "\n" + stack
	}
	log.Print(entry)
}

func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return strings.ToValidUTF8(value[:limit], "")
}

func (a *app) exportDiagnosticLogs(dst string) error {
	if a.diagnostics == nil {
		return errors.New("诊断日志尚未初始化")
	}
	return a.diagnostics.Export(dst)
}

func (a *app) askExportDiagnosticLogs() {
	name := "语燕日志-" + time.Now().Format("2006-01-02") + ".zip"
	defaultPath := name
	if dir, err := mygo.App.Path(mygo.PathDownloads); err == nil {
		defaultPath = filepath.Join(dir, name)
	}
	dst, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:      a.win,
		Title:       "导出诊断日志",
		DefaultPath: defaultPath,
		ButtonLabel: "导出",
		Filters:     []mygo.FileFilter{{Name: "ZIP", Extensions: []string{"zip"}}},
	})
	if err != nil {
		a.logNote = "导出日志失败：" + err.Error()
		a.logNoteErr = true
		a.update()
		return
	}
	if dst == "" {
		return
	}
	if filepath.Ext(dst) == "" {
		dst += ".zip"
	}
	if err := a.exportDiagnosticLogs(dst); err != nil {
		a.logNote = "导出日志失败：" + err.Error()
		a.logNoteErr = true
	} else {
		a.logNote = "最近 3 天日志已导出。"
		a.logNoteErr = false
	}
	a.update()
}
