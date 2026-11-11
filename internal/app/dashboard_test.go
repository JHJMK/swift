package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestDashboardStatisticsIncludeExternalKnowledgeBases(t *testing.T) {
	a, root, rel := newTestApp(t)
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.Local)
	today := dayOf(now)

	localPath := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.Chtimes(localPath, today.Add(10*time.Hour), today.Add(10*time.Hour)); err != nil {
		t.Fatal(err)
	}

	oldRel, err := a.vault.CreateDoc("我的知识库", "旧文")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.vault.WriteDoc(oldRel, "旧文\n"); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(root, filepath.FromSlash(oldRel))
	oldTime := today.AddDate(0, 0, -400)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	externalRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(externalRoot, ".hidden"), 0o755); err != nil {
		t.Fatal(err)
	}
	externalRel := filepath.Join(externalRoot, "外部文档.md")
	if err := os.WriteFile(externalRel, []byte("外部 内容\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(externalRel, today.Add(9*time.Hour), today.Add(9*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(externalRoot, ".hidden", "忽略.md"), []byte("忽略\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(externalRoot, "图片.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.vault.AddExternal("外部知识库", externalRoot); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()

	data := a.dashboard(now)
	if data.kbs != 2 || data.articles != 3 || data.words != 12 {
		t.Fatalf("dashboard = %+v, want 2 knowledge bases, 3 articles, 12 words", data)
	}
	if got := data.activity[today]; got != 2 {
		t.Fatalf("activity today = %d, want 2", got)
	}

	weeks := dashboardActivityWeeks(now, data.activity)
	if len(weeks) != dashboardWeeks {
		t.Fatalf("activity has %d weeks, want %d", len(weeks), dashboardWeeks)
	}
	todayCell := weeks[dashboardWeeks-1][int(now.Weekday())]
	if !todayCell.date.Equal(today) || todayCell.count != 2 || todayCell.future {
		t.Fatalf("today cell = %+v", todayCell)
	}
	weekStart := today.AddDate(0, 0, -int(today.Weekday()))
	if got := weeks[0][0].date; !got.Equal(weekStart.AddDate(0, 0, -7*(dashboardWeeks-1))) {
		t.Fatalf("activity starts at %v, want %v", got, weekStart.AddDate(0, 0, -7*(dashboardWeeks-1)))
	}
}

func TestDashboardCacheRefreshesAfterTreeChange(t *testing.T) {
	a, _, _ := newTestApp(t)
	now := time.Now()
	if got := a.dashboard(now).articles; got != 1 {
		t.Fatalf("initial articles = %d", got)
	}
	if _, err := a.vault.CreateDoc("我的知识库", "第二篇"); err != nil {
		t.Fatal(err)
	}
	if got := a.dashboard(now.Add(time.Hour)).articles; got != 1 {
		t.Fatalf("cached articles = %d, want 1", got)
	}
	a.refreshTree()
	if got := a.dashboard(now.Add(time.Hour)).articles; got != 2 {
		t.Fatalf("refreshed articles = %d, want 2", got)
	}
}

func TestDashboardUIReplacesRecentDocuments(t *testing.T) {
	a, _, _ := newTestApp(t)
	tt := ui.NewTester(a.view, 860, 760)

	if tt.HasText("最近打开") || tt.HasText("还没有打开过的文档") {
		t.Fatalf("recent documents are still shown: %q", tt.Texts())
	}
	for _, text := range []string{"统计看板", "知识库", "文章", "总字数", "近 52 周活跃度"} {
		if !tt.HasText(text) {
			t.Fatalf("dashboard lacks %q: %q", text, tt.Texts())
		}
	}
	if !tt.HasText("6") {
		t.Fatalf("dashboard does not show the word count: %q", tt.Texts())
	}
}
