package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Up at the very start of the body and Down in the title move between the
// two.
func TestTitleBodyArrows(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.focusBody = true
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	a.doc.Doc.SetSelection(0, 0)
	tt.Frame()
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if !a.doc.titleFocused {
		t.Fatal("Up at the start of the body did not focus the title")
	}
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if !a.doc.titleFocused {
		t.Fatal("Up in the title should keep it focused")
	}
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if !a.editorFocused || a.doc.titleFocused {
		t.Fatal("Down in the title did not focus the body")
	}
}
