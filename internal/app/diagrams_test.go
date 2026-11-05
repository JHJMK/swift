package app

import (
	"strings"
	"testing"
)

func TestInsertFlowchart(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Doc.SetSelection(0, 0)
	a.insertFlowchart()
	md := a.doc.Doc.Text().Markdown()
	if !strings.Contains(md, "```mermaid\nflowchart TD") {
		t.Fatalf("flowchart block missing: %q", md)
	}
	if !strings.Contains(md, "A[开始]") {
		t.Fatalf("flowchart template missing: %q", md)
	}
	for _, p := range a.doc.Doc.Text().Paragraphs() {
		if p.Style.CodeBlock && p.Style.Language == "mermaid" {
			return
		}
	}
	t.Fatalf("flowchart is not a Mermaid code block: %q", md)
}
