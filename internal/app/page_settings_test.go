package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Page display settings are saved by document and come back with it.
func TestPageSettingsPersist(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.PageFontSize = 18
	a.doc.PageLineHeight = 2
	a.savePageSettings(a.doc)
	a.doc = nil
	a.openDoc(rel)
	if a.doc.PageFontSize != 18 || a.doc.PageLineHeight != 2 {
		t.Fatalf("page settings did not persist: %+v", a.doc)
	}
}

// Documents follow the global display settings until they save their own.
func TestGlobalPageSettingsAndDocumentOverride(t *testing.T) {
	a, _, rel := newTestApp(t)
	if err := a.saveDefaultPageSettings(PageSettings{LineHeight: 2, FontSize: 20}); err != nil {
		t.Fatal(err)
	}
	a.openDoc(rel)
	if a.doc.PageFontSize != 20 || a.doc.PageLineHeight != 2 {
		t.Fatalf("document did not use the global settings: %+v", a.doc)
	}
	if a.hasPageSettings(rel) {
		t.Fatal("a fresh document already overrides the global settings")
	}

	// The page's own settings win over a later change to the globals.
	a.doc.PageFontSize = 18
	a.doc.PageLineHeight = 1.5
	a.savePageSettings(a.doc)
	if err := a.saveDefaultPageSettings(PageSettings{LineHeight: 1.75, FontSize: 14}); err != nil {
		t.Fatal(err)
	}
	if a.doc.PageFontSize != 18 || a.doc.PageLineHeight != 1.5 {
		t.Fatalf("global settings replaced a document override: %+v", a.doc)
	}
	a.doc = nil
	a.openDoc(rel)
	if a.doc.PageFontSize != 18 || a.doc.PageLineHeight != 1.5 {
		t.Fatalf("document override did not persist: %+v", a.doc)
	}

	// Resetting drops the override and follows the globals again.
	if err := a.resetPageSettings(a.doc); err != nil {
		t.Fatal(err)
	}
	if a.hasPageSettings(rel) {
		t.Fatal("override was not removed")
	}
	if a.doc.PageFontSize != 14 || a.doc.PageLineHeight != 1.75 {
		t.Fatalf("document did not follow the globals after reset: %+v", a.doc)
	}
}

// The settings page shows the global display defaults, and the page
// settings window says whether the document follows them.
func TestPageSettingsUI(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.settingsOpen = true
	a.settingsSection = settingsDisplay
	tt := ui.NewTester(a.view, 1180, 900)
	tt.Frame()
	if !tt.HasText("文档显示") || !tt.HasText("新文档默认使用这里的行距和字号；单独设置过的文档会覆盖全局默认。") {
		t.Fatalf("global page settings missing: %q", tt.Texts())
	}
	if err := tt.Click("完成"); err != nil {
		t.Fatal(err)
	}
	a.openDoc(rel)
	a.pageSettingsOpen = true
	tt.Frame()
	if !tt.HasText("此文档跟随全局默认（行距 1.75，字号 16）。") {
		t.Fatalf("page settings did not show the global defaults: %q", tt.Texts())
	}
	if err := tt.Click("18"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.hasPageSettings(rel) || a.doc.PageFontSize != 18 {
		t.Fatalf("choosing a font size did not save an override: %+v", a.doc)
	}
	if !tt.HasText("此文档使用单独设置，覆盖全局默认。") {
		t.Fatalf("page settings did not show the override: %q", tt.Texts())
	}
	if err := tt.Click("恢复为全局默认"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.hasPageSettings(rel) || a.doc.PageFontSize != 16 {
		t.Fatalf("resetting did not follow the globals: %+v", a.doc)
	}
}

// The About section shows the version mygo build took from mygo.json.
func TestAboutVersion(t *testing.T) {
	old := mygo.App.Version()
	t.Cleanup(func() { mygo.App.SetVersion(old) })
	mygo.App.SetVersion("1.2.3")
	if got := appVersion(); got != "1.2.3" {
		t.Fatalf("appVersion = %q", got)
	}
	path := filepath.Join(t.TempDir(), "mygo.json")
	if err := os.WriteFile(path, []byte(`{"version":"9.8.7"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := versionFromFile(path); got != "9.8.7" {
		t.Fatalf("versionFromFile = %q", got)
	}
}
