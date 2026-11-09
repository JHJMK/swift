package app

import (
	"strings"
	"testing"
)

// The rich text editor carries no Markdown info string, so the app keeps
// a fence's language and title by paragraph ID and writes them back.
func TestNativeCodeFenceInfoSurvivesSave(t *testing.T) {
	a, _, rel := newTestApp(t)
	original := "# 标题\n\n```bash title=\"示例\"\necho hi\n```\n\n正文\n"
	if _, err := a.vault.WriteDoc(rel, original); err != nil {
		t.Fatal(err)
	}
	a.openDoc(rel)
	if a.doc.Web {
		t.Fatal("the test should use the native editor")
	}
	at := a.doc.Doc.Text().Len()
	a.doc.Doc.SetSelection(at, at)
	a.doc.Doc.InsertText("。")
	a.markDocDirty()
	a.flushDoc()

	got, _, err := a.vault.ReadDoc(rel)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"```bash title=\"示例\"", "echo hi", "正文。"} {
		if !strings.Contains(got, want) {
			t.Fatalf("saved Markdown lacks %q:\n%s", want, got)
		}
	}
}

// parseFenceInfos understands language and title, and ignores fences in
// code content.
func TestParseFenceInfos(t *testing.T) {
	md := "```go\n```\n\n```bash title=\"部署\"\necho hi\n```\n\n```\nplain\n```\n"
	got := parseFenceInfos(md)
	want := []codeFenceInfo{
		{Language: "go"},
		{Language: "bash", Title: "部署"},
		{},
	}
	if len(got) != len(want) {
		t.Fatalf("infos = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("info %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	if got := fenceInfo("bash", "部署"); got != `bash title="部署"` {
		t.Fatalf("fenceInfo = %q", got)
	}
}

// Inserting a Mermaid block in the native fallback keeps its language, so
// the web editor can render it later.
func TestNativeMermaidBlockKeepsLanguage(t *testing.T) {
	a, _, rel := newTestApp(t)
	a.openDoc(rel)
	a.doc.Doc.SetSelection(0, 0)
	a.markMermaidBlock(a.doc)
	a.markDocDirty()
	a.flushDoc()
	got, _, err := a.vault.ReadDoc(rel)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "```mermaid") {
		t.Fatalf("saved Markdown lacks the Mermaid fence:\n%s", got)
	}
}
