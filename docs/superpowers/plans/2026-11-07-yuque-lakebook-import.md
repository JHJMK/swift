# Yuque Lakebook Import Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Import a Yuque Lakebook archive into a local 语燕 knowledge base, including document hierarchy, Markdown content, local images, and static board fallbacks.

**Architecture:** Add a dependency-free `internal/lakebook` package for safe tar parsing, TOC reconstruction, and Lake HTML conversion. Add an app-level importer that writes the parsed tree through the existing Vault API and downloads images into `.assets`. Reuse one menu definition in the sidebar and workbench.

**Tech Stack:** Go 1.27, `archive/tar`, `golang.org/x/net/html`, `gopkg.in/yaml.v3`, existing mygo UI/Vault APIs.

---

### Task 1: Safe archive and TOC parser

**Files:**
- Create: `internal/lakebook/lakebook.go`
- Create: `internal/lakebook/lakebook_test.go`
- Modify: `go.mod`

- [ ] **Step 1: Write failing tests for the archive contract**

Add `internal/lakebook/lakebook_test.go` with helpers that build a synthetic `.lakebook` in a temporary file:

```go
func writeLakebook(t *testing.T, files map[string][]byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "示例知识库.lakebook")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	for name, content := range files {
		h := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}
```

Add table cases proving:

```go
func TestParseTreeAndOrder(t *testing.T)
func TestParseRejectsUnsafeArchive(t *testing.T)
func TestParseRejectsUnknownNonBoardType(t *testing.T)
func TestParseRejectsTreeDeeperThanFour(t *testing.T)
func TestParseBoardWithoutDocumentCreatesPlaceholderNode(t *testing.T)
```

The first test must assert root order, nested document children, `DOC`, `TITLE`, `BOARD`, and skipped `META`. The unsafe cases must cover absolute paths, `..`, symlinks, duplicate UUIDs, orphan parents, and cycles. Run:

```bash
go test ./internal/lakebook
```

Expected: FAIL because package `internal/lakebook` does not exist.

- [ ] **Step 2: Implement the public model and safe tar reader**

Create:

```go
type Kind int

const (
	KindDoc Kind = iota
	KindTitle
	KindBoard
)

type Book struct {
	Name       string
	Nodes      []*Node
	BoardCount int
}

type Node struct {
	Kind     Kind
	Title    string
	Markdown string
	Children []*Node
}

func Parse(filename string) (*Book, error)
```

`Parse` validates the archive size before opening it, accepts only regular files and directories, rejects unsafe paths and duplicate entries, caps entries at 2,000 and expanded bytes at 256 MiB, parses `$meta.json`, and derives the book name from the filename.

- [ ] **Step 3: Implement TOC parsing and reconstruction**

Parse `book.tocYml` with `yaml.v3`. Accept case-insensitive `META`, `TITLE`, `DOC`, and `BOARD`; reject every other type. Reconstruct siblings using `child_uuid` first and `prev_uuid` links, then append unlinked nodes in original YAML order. Reject duplicate UUIDs, missing parents, orphan links, and cycles. Reject trees deeper than four visible levels.

- [ ] **Step 4: Implement document loading and board placeholders**

Map each TOC document via `doc_id`, falling back to the final segment of `url`, to the matching `meta.docs[].slug`. A `DOC` missing its document entry or JSON is fatal. A `BOARD` without document data still becomes a node with:

```markdown
[在语雀中打开画板](原链接)

> 此内容来自语雀画板，暂不支持交互编辑。
```

Use `doc.body`; when it is empty use `doc.body_draft`. Do not use `body_asl`.

- [ ] **Step 5: Run parser tests and commit**

```bash
go test ./internal/lakebook
git add go.mod go.sum internal/lakebook
git commit -m "feat: parse Yuque Lakebook archives"
```

Expected: PASS.

### Task 2: Lake HTML to Markdown and board cards

**Files:**
- Create: `internal/lakebook/html.go`
- Create: `internal/lakebook/html_test.go`
- Modify: `internal/lakebook/lakebook.go`
- Modify: `go.mod`

- [ ] **Step 1: Write failing converter tests**

Cover headings, paragraphs, bold, italic, inline code, line breaks, links, unordered and ordered lists, nested lists, fenced code with language, tables, horizontal rules, images, unknown blocks, empty paragraphs, and LF-only output.

Add board cases:

```go
func TestConvertBoardCard(t *testing.T) {
	got, boards := convertHTML(`<div class="ne-board" data-title="架构图" data-href="https://example.com/board">
		<p>入口 -> 服务</p>
		<img src="https://example.com/preview.png">
	</div>`)
	if boards != 1 {
		t.Fatalf("boards = %d", boards)
	}
	for _, want := range []string{"架构图", "入口", "preview.png", "https://example.com/board", "暂不支持交互编辑"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}
```

Also assert `ne-board` without text, preview, or link emits an explicit placeholder. Run:

```bash
go test ./internal/lakebook -run 'TestConvert'
```

Expected: FAIL because `convertHTML` is missing.

- [ ] **Step 2: Implement block and inline rendering**

Parse with `golang.org/x/net/html`. Render supported blocks and inline nodes directly. Preserve raw code text without Markdown escaping. Escape Markdown control characters only in ordinary text. Compress runs of blank lines and normalize CRLF to LF.

- [ ] **Step 3: Implement conservative board-card fallback**

Only recognize a board when an element has `ne-board`/`lake-board` in `class`, or `data-card-type="board"`/`data-type="board"`. Extract title from `data-title`, `title`, or the first heading; extract visible text; extract preview from nested images or `preview`, `cover`, `thumbnail` attributes; extract the original link from `href` or `data-href`. Always add:

```text
> 此内容来自语雀画板，暂不支持交互编辑。
```

Increment the converter's board count once per recognized card.

- [ ] **Step 4: Connect conversion results to `Parse`**

Set each `DOC.Markdown` to the converted body. Add embedded board counts to `Book.BoardCount`; add one more for every TOC `BOARD` node. A board document body can contribute readable text and preview images but must still count as one board.

- [ ] **Step 5: Run tests and commit**

```bash
go test ./internal/lakebook
go mod tidy
git add go.mod go.sum internal/lakebook
git commit -m "feat: convert Lake HTML and boards"
```

Expected: PASS.

### Task 3: App importer and image downloader

**Files:**
- Create: `internal/app/importlakebook.go`
- Create: `internal/app/importlakebook_test.go`

- [ ] **Step 1: Write a failing end-to-end import test**

Build a synthetic Lakebook and an `httptest.Server` that serves a PNG. Call a testable method:

```go
result, err := a.importLakebook(path)
```

Assert:

```go
result.KB == "导入测试"
result.Docs == 3
result.Boards == 2
result.Images == 1
result.FailedImages == 0
```

Read the imported tree and assert folders, nested document children, local `.assets` image paths, board placeholder text, and source links. Add tests for a failed image retaining its remote URL and for an invalid archive not creating a knowledge base. Run:

```bash
go test ./internal/app -run TestImportLakebook
```

Expected: FAIL because `importLakebook` is missing.

- [ ] **Step 2: Implement preflight parse and Vault writes**

Call `lakebook.Parse` before `CreateKnowledgeBase`. Recursively map `TITLE` to `CreateFolder`, `DOC`/`BOARD` to `CreateDoc`, and child documents to `EnsureDocDir`. Use every Vault-returned relative path rather than rebuilding paths from titles.

- [ ] **Step 3: Implement constrained image download and reuse**

Accept only HTTP(S). Use a client with a 15-second timeout and redirects capped at five. Enforce 20 MiB per image and 200 MiB per import. Validate `2xx` and `image/*` or a known extension when Content-Type is absent. Download each normalized URL once. Save through `ImportAssetBytes`, remember the saved absolute asset path, and compute a relative path for each document that reuses it.

Image errors never fail the document import. Count unique failed URLs. Do not log URLs or body content.

- [ ] **Step 4: Add completion toast and result formatting**

After import, refresh the tree, open the new knowledge base, and call `flashToast` with:

```text
导入完成：3 篇文档，2 个画板仅保留静态预览，1 张图片
```

Append `，1 张未下载` only when failures exist. Omit the board clause when `Boards == 0`.

- [ ] **Step 5: Run tests and commit**

```bash
go test ./internal/app -run TestImportLakebook
go test ./internal/lakebook ./internal/app
git add internal/app/importlakebook.go internal/app/importlakebook_test.go
git commit -m "feat: import Lakebooks into knowledge bases"
```

Expected: PASS.

### Task 4: Unified import menus

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/view.go`
- Modify: `internal/app/view_menu.go`
- Modify: `internal/app/importlakebook.go`
- Modify: `internal/app/ui_test.go`

- [ ] **Step 1: Write failing UI tests**

Add `TestUnifiedImportMenus`. In the sidebar click `新建或导入知识库`, click `导入`, and assert exactly one each of `复制导入知识库`, `引用外部知识库`, and `导入语雀 Lakebook`. On the workbench click `导入知识库` and assert the same three choices. Assert the sidebar top-level no longer shows both copy and external beside `新建知识库`.

Run:

```bash
go test ./internal/app -run TestUnifiedImportMenus
```

Expected: FAIL because the nested menu and Lakebook action are missing.

- [ ] **Step 2: Add shared import menu content**

Add one helper:

```go
func importMenuItems(c *ui.Context, copyKB, externalKB, lakebook func()) {
	menuItem(c, iconDownload, "复制导入知识库", copyKB)
	menuItem(c, iconFolder, "引用外部知识库", externalKB)
	menuItem(c, iconBook, "导入语雀 Lakebook", lakebook)
}
```

Use it in both sidebar and workbench menus.

- [ ] **Step 3: Add the sidebar two-level menu state**

Add `kbImportMenuOpen bool` to `app`. The sidebar top level shows `新建知识库`, a separator, and `导入`. Clicking `导入` closes `kbAddMenuOpen`, opens `kbImportMenuOpen` at the same plus-button anchor, and shows the shared three items.

- [ ] **Step 4: Add the file picker action**

Add `askImportLakebook` using:

```go
mygo.Dialog.Open(mygo.OpenDialogOptions{
	Parent:      a.win,
	Title:       "导入语雀 Lakebook",
	ButtonLabel: "选择文件",
	Filters: []mygo.FileFilter{{
		Name:       "语雀 Lakebook",
		Extensions: []string{"lakebook"},
	}},
})
```

Return quietly when cancelled. Keep the native File menu unchanged.

- [ ] **Step 5: Run UI tests and commit**

```bash
go test ./internal/app -run 'TestUnifiedImportMenus|TestImportLakebook'
git add internal/app/app.go internal/app/view.go internal/app/view_menu.go internal/app/importlakebook.go internal/app/importlakebook_test.go internal/app/ui_test.go
git commit -m "feat: unify knowledge base import menus"
```

Expected: PASS.

### Task 5: Full verification and visual QA

**Files:**
- Verify only; fix only files required by failing checks.

- [ ] **Step 1: Run all automated checks**

```bash
gofmt -w internal/lakebook/*.go internal/app/importlakebook.go internal/app/importlakebook_test.go internal/app/app.go internal/app/view.go internal/app/view_menu.go internal/app/ui_test.go
go test ./...
go vet ./...
git diff --check
```

Expected: all commands exit 0.

- [ ] **Step 2: Start the app**

```bash
go run ./cmd/swift
```

Expected: the window opens with the existing workspace. Do not open or display the real Ucloud document.

- [ ] **Step 3: Inspect screenshots**

Capture the sidebar first-level menu, sidebar second-level import menu, and workbench import menu at desktop size. Capture the narrow window layout. Check clipping, overlap, alignment, and button hit areas.

- [ ] **Step 4: Final status**

```bash
git status --short
git log --oneline -6
```

Expected: only the user's pre-existing `mygo.json` remains uncommitted. Report the commits, test result, and screenshot checks.
