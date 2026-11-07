package app

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

type lakebookTestDoc struct {
	Slug string
	HTML string
}

func writeAppLakebook(t *testing.T, toc string, docs ...lakebookTestDoc) string {
	t.Helper()

	files := map[string][]byte{}
	metas := make([]map[string]any, 0, len(docs))
	for index, doc := range docs {
		metas = append(metas, map[string]any{
			"id":   index + 1,
			"slug": doc.Slug,
		})
		body, err := json.Marshal(map[string]any{
			"doc": map[string]any{"body": doc.HTML},
		})
		if err != nil {
			t.Fatal(err)
		}
		files["book/"+doc.Slug+".json"] = body
	}
	inner, err := json.Marshal(map[string]any{
		"book": map[string]any{"tocYml": toc},
		"docs": metas,
	})
	if err != nil {
		t.Fatal(err)
	}
	files["book/$meta.json"], err = json.Marshal(map[string]any{"meta": string(inner)})
	if err != nil {
		t.Fatal(err)
	}

	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	filename := filepath.Join(t.TempDir(), "导入测试.lakebook")
	if err := os.WriteFile(filename, archive.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return filename
}

func TestImportLakebook(t *testing.T) {
	var requests atomic.Int32
	pngData := testPNG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngData)
	}))
	defer server.Close()

	toc := `
- uuid: group
  title: 分组
  type: TITLE
  child_uuid: first
- uuid: first
  parent_uuid: group
  title: 文档一
  type: DOC
  doc_id: 1
- uuid: parent
  parent_uuid: group
  prev_uuid: first
  title: 父文档
  type: DOC
  doc_id: 2
  child_uuid: child
- uuid: child
  parent_uuid: parent
  title: 子文档
  type: DOC
  doc_id: 3
- uuid: board
  parent_uuid: group
  prev_uuid: parent
  title: 独立画板
  type: BOARD
  url: https://example.com/boards/one
`
	imageURL := server.URL + "/image.png"
	path := writeAppLakebook(t, toc,
		lakebookTestDoc{
			Slug: "first",
			HTML: `<p>第一篇</p><img src="` + imageURL + `" alt="示意图">` +
				`<div class="ne-board" data-title="正文画板" data-href="https://example.com/boards/inline"><p>入口到服务</p></div>`,
		},
		lakebookTestDoc{Slug: "parent", HTML: `<p>父文档正文</p>`},
		lakebookTestDoc{
			Slug: "child",
			HTML: `<p>子文档正文</p><img src="` + imageURL + `" alt="复用图片">`,
		},
	)

	a, _, _ := newTestApp(t)
	result, err := a.importLakebook(path)
	if err != nil {
		t.Fatal(err)
	}
	if result.KB != "导入测试" || result.Docs != 3 || result.Boards != 2 || result.Images != 1 || result.FailedImages != 0 {
		t.Fatalf("result = %+v", result)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("image requests = %d, want 1", got)
	}

	first := "导入测试/分组/文档一.md"
	firstMD, _, err := a.vault.ReadDoc(first)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(firstMD, "第一篇") ||
		!strings.Contains(firstMD, "正文画板") ||
		!strings.Contains(firstMD, "暂不支持交互编辑") ||
		!strings.Contains(firstMD, "![示意图](") {
		t.Fatalf("first markdown = %q", firstMD)
	}
	if strings.Contains(firstMD, imageURL) {
		t.Fatalf("first markdown kept remote image: %q", firstMD)
	}

	child := "导入测试/分组/父文档/子文档.md"
	childMD, _, err := a.vault.ReadDoc(child)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(childMD, "子文档正文") || strings.Contains(childMD, imageURL) {
		t.Fatalf("child markdown = %q", childMD)
	}
	firstAsset := firstImageRef(t, firstMD)
	childAsset := firstImageRef(t, childMD)
	firstPath, ok := a.vault.ResolveAsset(first, firstAsset)
	if !ok {
		t.Fatalf("first image does not resolve: %q", firstAsset)
	}
	childPath, ok := a.vault.ResolveAsset(child, childAsset)
	if !ok || childPath != firstPath {
		t.Fatalf("child image = %q, first = %q", childPath, firstPath)
	}
	if !strings.Contains(firstPath, string(filepath.Separator)+".assets"+string(filepath.Separator)) {
		t.Fatalf("image was not stored in .assets: %q", firstPath)
	}

	board := "导入测试/分组/独立画板.md"
	boardMD, _, err := a.vault.ReadDoc(board)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(boardMD, "https://example.com/boards/one") ||
		!strings.Contains(boardMD, "暂不支持交互编辑") {
		t.Fatalf("board markdown = %q", boardMD)
	}
}

func TestImportLakebookKeepsFailedImageURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer server.Close()

	imageURL := server.URL + "/missing.png"
	path := writeAppLakebook(t, `
- uuid: doc
  title: 图片失败
  type: DOC
  doc_id: 1
`, lakebookTestDoc{Slug: "doc", HTML: `<p>正文</p><img src="` + imageURL + `">`})

	a, _, _ := newTestApp(t)
	result, err := a.importLakebook(path)
	if err != nil {
		t.Fatal(err)
	}
	if result.Docs != 1 || result.Images != 0 || result.FailedImages != 1 {
		t.Fatalf("result = %+v", result)
	}
	md, _, err := a.vault.ReadDoc("导入测试/图片失败.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, imageURL) {
		t.Fatalf("failed image URL was not retained: %q", md)
	}
}

func TestImportLakebookRejectsInvalidArchiveBeforeCreatingKB(t *testing.T) {
	a, _, _ := newTestApp(t)
	beforeTree, err := a.vault.Tree()
	if err != nil {
		t.Fatal(err)
	}
	before := len(beforeTree)

	path := filepath.Join(t.TempDir(), "损坏.lakebook")
	if err := os.WriteFile(path, []byte("not a tar"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.importLakebook(path); err == nil {
		t.Fatal("invalid archive was imported")
	}
	tree, err := a.vault.Tree()
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != before {
		t.Fatalf("knowledge bases = %d, want original only", len(tree))
	}
	for _, node := range tree {
		if node.Name == "损坏" {
			t.Fatal("invalid archive created a knowledge base")
		}
	}
}

func TestStartLakebookImportPreparesBeforeWritingVault(t *testing.T) {
	requested := make(chan struct{})
	release := make(chan struct{})
	var requestOnce sync.Once
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
	})

	pngData := testPNG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestOnce.Do(func() { close(requested) })
		<-release
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngData)
	}))
	defer server.Close()

	path := writeAppLakebook(t, `
- uuid: doc
  title: 异步导入
  type: DOC
  doc_id: 1
`, lakebookTestDoc{
		Slug: "doc",
		HTML: `<p>异步正文</p><img src="` + server.URL + `/image.png">`,
	})

	a, _, _ := newTestApp(t)
	tt := ui.NewTester(a.view, 1180, 760)
	started := make(chan bool, 1)
	go func() {
		started <- a.startLakebookImport(path)
	}()

	select {
	case ok := <-started:
		if !ok {
			t.Fatal("import did not start")
		}
	case <-time.After(time.Second):
		t.Fatal("startLakebookImport blocked")
	}
	select {
	case <-requested:
	case <-time.After(time.Second):
		t.Fatal("image download did not start")
	}

	if !a.lakebookImportRunning() {
		t.Fatal("import is not marked as running")
	}
	tt.Frame()
	if !tt.HasText("正在导入语雀 Lakebook…") {
		t.Fatalf("import progress not shown: %q", tt.Texts())
	}
	importRect, ok := tt.Find("正在导入语雀 Lakebook…")
	if !ok || importRect.X < 0 || importRect.X+importRect.W > 200 {
		t.Fatalf("import progress is not in the knowledge base sidebar: %+v", importRect)
	}
	kbRect, ok := tt.Find("我的知识库")
	if !ok || importRect.Y >= kbRect.Y {
		t.Fatalf("import progress should sit above knowledge bases: import=%+v kb=%+v", importRect, kbRect)
	}
	tree, err := a.vault.Tree()
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range tree {
		if node.Name == "导入测试" {
			t.Fatal("vault changed before preparation finished")
		}
	}

	releaseOnce.Do(func() { close(release) })
	deadline := time.Now().Add(3 * time.Second)
	for a.lakebookImportRunning() {
		tt.Frame()
		if time.Now().After(deadline) {
			t.Fatal("background import did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	tt.Frame()

	if a.findNode("导入测试/异步导入.md") == nil {
		t.Fatal("imported document missing")
	}
	if tt.HasText("正在导入语雀 Lakebook…") {
		t.Fatal("import progress stayed after the import finished")
	}
	if !strings.Contains(a.toast, "导入完成：1 篇文档") {
		t.Fatalf("toast = %q", a.toast)
	}
}

func firstImageRef(t *testing.T, md string) string {
	t.Helper()
	if !strings.Contains(md, "![") {
		t.Fatalf("markdown has no image: %q", md)
	}
	target := strings.Index(md, "](")
	if target < 0 {
		t.Fatalf("markdown image has no target: %q", md)
	}
	start := target + 2
	end := strings.IndexByte(md[start:], ')')
	if end < 0 {
		t.Fatalf("markdown image target is unterminated: %q", md)
	}
	return md[start : start+end]
}
