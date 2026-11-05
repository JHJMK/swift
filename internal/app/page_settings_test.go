package app

import "testing"

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
