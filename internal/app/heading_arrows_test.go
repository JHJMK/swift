package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"
)

// Up at the start of a heading moves to the previous paragraph, even when
// the heading's top margin is larger than a line.
func TestHeadingUpAtStart(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Doc.SetText(richtext.ParseMarkdown("# 111\n\n# 222\n"))
	a.focusBody = true
	tt := ui.NewTester(a.view, 1180, 760)
	tt.Frame()
	text := a.doc.Doc.Text().PlainText()
	pos := strings.Index(text, "222")
	for _, off := range []int{0, 1, len("222")} {
		at := pos + off
		a.doc.Doc.SetSelection(at, at)
		tt.Frame()
		tt.Key(0, ui.KeyUp)
		tt.Frame()
		start, _ := a.doc.Doc.Selection()
		if start >= at {
			t.Fatalf("Up at %d (offset %d) did not move up: %d", at, off, start)
		}
		if got := a.doc.Doc.Text().PlainText(); got != text {
			t.Fatalf("Up at %d changed the text: %q", at, got)
		}
	}
}
