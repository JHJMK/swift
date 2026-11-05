package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"
)

// The editor's scroll bar is hidden, but the wheel still scrolls it.
func TestEditorScrollsWithoutScrollbar(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	var lines []string
	for i := 0; i < 80; i++ {
		lines = append(lines, fmt.Sprintf("段落 %d", i))
	}
	a.doc.Doc.SetText(richtext.NewText(strings.Join(lines, "\n")))
	a.doc.Doc.SetSelection(0, 0)
	a.focusBody = true
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	before := a.doc.Doc.VisibleParagraphs()
	if len(before) == 0 || before[0].Index != 0 {
		t.Fatalf("the editor did not start at the top: %+v", before)
	}
	tt.Scroll(700, 400, 0, 500)
	tt.Frame()
	after := a.doc.Doc.VisibleParagraphs()
	if len(after) == 0 || after[0].Index <= before[0].Index {
		t.Fatalf("the editor did not scroll: %+v -> %+v", before, after)
	}
}
