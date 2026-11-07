package lakebook

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testDoc struct {
	Slug string `json:"slug"`
	HTML string `json:"html"`
}

func writeTestLakebook(t *testing.T, name, toc string, docs ...testDoc) string {
	t.Helper()

	metas := make([]map[string]any, 0, len(docs))
	files := map[string][]byte{}
	for i, doc := range docs {
		slug := doc.Slug
		if slug == "" {
			slug = "doc"
		}
		metas = append(metas, map[string]any{
			"id":    i + 1,
			"slug":  slug,
			"title": slug,
		})
		docFile, err := json.Marshal(map[string]any{
			"doc": map[string]any{
				"body":       doc.HTML,
				"body_draft": "",
				"body_asl":   "<p>must not be used</p>",
				"title":      slug,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		files["book/"+slug+".json"] = docFile
	}

	inner, err := json.Marshal(map[string]any{
		"book":    map[string]any{"tocYml": toc},
		"config":  map[string]any{"isYuque": true},
		"docs":    metas,
		"version": "1.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	outer, err := json.Marshal(map[string]any{"meta": string(inner)})
	if err != nil {
		t.Fatal(err)
	}
	files["book/$meta.json"] = outer

	return writeLakebookEntries(t, name, files)
}

func writeLakebookEntries(t *testing.T, name string, files map[string][]byte) string {
	t.Helper()

	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	for path, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: path,
			Mode: 0o600,
			Size: int64(len(content)),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), name+".lakebook")
	if err := os.WriteFile(path, archive.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeLakebookFile(t *testing.T, name string, write func(*testing.T, *tar.Writer)) string {
	t.Helper()

	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	write(t, tw)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), name+".lakebook")
	if err := os.WriteFile(path, archive.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeLakebookEntry(t *testing.T, tw *tar.Writer, name string, body []byte) {
	t.Helper()
	if err := tw.WriteHeader(&tar.Header{
		Name: name,
		Mode: 0o600,
		Size: int64(len(body)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
}

func TestParseTreeAndBoards(t *testing.T) {
	toc := `
- uuid: doc-a
  parent_uuid: ""
  title: 文档 A
  type: DOC
  doc_id: 1
- uuid: title
  parent_uuid: ""
  prev_uuid: doc-a
  title: 章节
  type: TITLE
  child_uuid: doc-b
- uuid: doc-b
  parent_uuid: title
  title: 文档 B
  type: DOC
  doc_id: 2
- uuid: board
  parent_uuid: title
  prev_uuid: doc-b
  title: 架构画板
  type: BOARD
  url: https://example.com/board/architecture
- uuid: meta
  title: 元数据
  type: META
`
	path := writeTestLakebook(t, "导入测试", toc,
		testDoc{Slug: "a", HTML: `<p>正文 A</p>`},
		testDoc{Slug: "b", HTML: `<p>正文 B</p>`},
	)

	book, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if book.Name != "导入测试" {
		t.Fatalf("name = %q", book.Name)
	}
	if len(book.Nodes) != 2 {
		t.Fatalf("root nodes = %d", len(book.Nodes))
	}
	if book.Nodes[0].Kind != KindDoc || book.Nodes[0].Title != "文档 A" {
		t.Fatalf("first root = %+v", book.Nodes[0])
	}
	if book.Nodes[0].Markdown != "正文 A\n" {
		t.Fatalf("first markdown = %q", book.Nodes[0].Markdown)
	}
	if book.Nodes[1].Kind != KindTitle || len(book.Nodes[1].Children) != 2 {
		t.Fatalf("second root = %+v", book.Nodes[1])
	}
	if book.Nodes[1].Children[0].Kind != KindDoc {
		t.Fatalf("nested document = %+v", book.Nodes[1].Children[0])
	}
	board := book.Nodes[1].Children[1]
	if board.Kind != KindBoard || board.Title != "架构画板" {
		t.Fatalf("board = %+v", board)
	}
	if !strings.Contains(board.Markdown, "https://example.com/board/architecture") ||
		!strings.Contains(board.Markdown, "暂不支持交互编辑") {
		t.Fatalf("board markdown = %q", board.Markdown)
	}
	if book.BoardCount != 1 {
		t.Fatalf("board count = %d", book.BoardCount)
	}
}

func TestParseRejectsUnsafeArchive(t *testing.T) {
	validMeta := []byte(`{"meta":"{\"book\":{\"tocYml\":\"- uuid: a\\n  type: DOC\\n  doc_id: 1\\n\"},\"docs\":[{\"id\":1,\"slug\":\"a\"}]}"}`)
	tests := []struct {
		name string
		add  func(*testing.T, *tar.Writer)
	}{
		{
			name: "absolute path",
			add: func(t *testing.T, tw *tar.Writer) {
				writeLakebookEntry(t, tw, "/book/$meta.json", validMeta)
			},
		},
		{
			name: "parent traversal",
			add: func(t *testing.T, tw *tar.Writer) {
				writeLakebookEntry(t, tw, "book/../$meta.json", validMeta)
			},
		},
		{
			name: "symlink",
			add: func(t *testing.T, tw *tar.Writer) {
				if err := tw.WriteHeader(&tar.Header{
					Name:     "book/$meta.json",
					Typeflag: tar.TypeSymlink,
					Linkname: "/tmp/meta.json",
				}); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeLakebookFile(t, "不安全", tt.add)
			if _, err := Parse(path); err == nil {
				t.Fatal("Parse() accepted unsafe archive")
			}
		})
	}
}

func TestParseRejectsBrokenTree(t *testing.T) {
	tests := []struct {
		name string
		toc  string
	}{
		{
			name: "duplicate uuid",
			toc: `
- uuid: same
  type: TITLE
- uuid: same
  type: TITLE
`,
		},
		{
			name: "orphan parent",
			toc: `
- uuid: a
  parent_uuid: missing
  type: TITLE
`,
		},
		{
			name: "parent cycle",
			toc: `
- uuid: a
  parent_uuid: b
  type: TITLE
- uuid: b
  parent_uuid: a
  type: TITLE
`,
		},
		{
			name: "sibling cycle",
			toc: `
- uuid: a
  prev_uuid: b
  type: TITLE
- uuid: b
  prev_uuid: a
  type: TITLE
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTestLakebook(t, "损坏", tt.toc)
			if _, err := Parse(path); err == nil {
				t.Fatal("Parse() accepted broken tree")
			}
		})
	}
}

func TestParseRejectsUnknownType(t *testing.T) {
	toc := `
- uuid: unknown
  title: 未知
  type: SPREADSHEET
`
	path := writeTestLakebook(t, "未知", toc)
	if _, err := Parse(path); err == nil {
		t.Fatal("Parse() accepted unknown TOC type")
	}
}

func TestParseRejectsTreeDeeperThanFour(t *testing.T) {
	toc := `
- uuid: one
  type: TITLE
  child_uuid: two
- uuid: two
  parent_uuid: one
  type: TITLE
  child_uuid: three
- uuid: three
  parent_uuid: two
  type: TITLE
  child_uuid: four
- uuid: four
  parent_uuid: three
  type: TITLE
  child_uuid: five
- uuid: five
  parent_uuid: four
  type: TITLE
`
	path := writeTestLakebook(t, "超深", toc)
	_, err := Parse(path)
	if err == nil {
		t.Fatal("Parse() accepted tree deeper than four")
	}
	if !strings.Contains(err.Error(), "4") {
		t.Fatalf("error = %q, want depth limit", err)
	}
}

func TestParseAcceptsTreeWithFourLevels(t *testing.T) {
	toc := `
- uuid: one
  type: TITLE
  child_uuid: two
- uuid: two
  parent_uuid: one
  type: TITLE
  child_uuid: three
- uuid: three
  parent_uuid: two
  type: TITLE
  child_uuid: four
- uuid: four
  parent_uuid: three
  type: TITLE
`
	path := writeTestLakebook(t, "四层", toc)
	book, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(book.Nodes) != 1 || len(book.Nodes[0].Children[0].Children[0].Children) != 1 {
		t.Fatalf("tree = %+v", book.Nodes)
	}
}

func TestParseBoardWithoutDocumentCreatesPlaceholderNode(t *testing.T) {
	toc := `
- uuid: board
  title: 无预览画板
  type: BOARD
  url: https://example.com/board/empty
`
	path := writeTestLakebook(t, "画板", toc)
	book, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(book.Nodes) != 1 || book.Nodes[0].Kind != KindBoard {
		t.Fatalf("nodes = %+v", book.Nodes)
	}
	md := book.Nodes[0].Markdown
	if !strings.Contains(md, "无预览画板") ||
		!strings.Contains(md, "https://example.com/board/empty") ||
		!strings.Contains(md, "暂不支持交互编辑") {
		t.Fatalf("markdown = %q", md)
	}
	if book.BoardCount != 1 {
		t.Fatalf("board count = %d", book.BoardCount)
	}
}

func TestBoardMarkdownEscapesTitleAndDestination(t *testing.T) {
	got := boardMarkdown("架构 [图]", "https://example.com/a b")
	if !strings.Contains(got, `## 架构 \[图\]`) {
		t.Fatalf("title was not escaped: %q", got)
	}
	if !strings.Contains(got, `[在语雀中打开画板](<https://example.com/a b>)`) {
		t.Fatalf("destination was not escaped: %q", got)
	}
}
