package lakebook

import (
	"strings"
	"testing"
)

func TestConvertHTMLBlocksAndInline(t *testing.T) {
	source := `<h1>一级标题</h1>
<p>这是 <strong>粗体</strong> 和 <em>斜体</em> 以及 <code>code</code>。<br>下一行 <a class="ne-link" href="https://example.com/a?q=1">链接</a></p>
<ul><li>第一项<ul><li>子项</li></ul></li><li>第二项</li></ul>
<ol><li>甲</li><li>乙</li></ol>
<hr class="ne-hr">`

	got, boards, err := convertHTML(source)
	if err != nil {
		t.Fatal(err)
	}
	if boards != 0 {
		t.Fatalf("boards = %d", boards)
	}
	for _, want := range []string{
		"# 一级标题",
		"**粗体**",
		"*斜体*",
		"`code`",
		"  \n下一行",
		"[链接](https://example.com/a?q=1)",
		"- 第一项",
		"  - 子项",
		"- 第二项",
		"1. 甲",
		"2. 乙",
		"---",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestConvertHTMLKeepsUnderscoresInPlainText(t *testing.T) {
	got, _, err := convertHTML(`<p>chatglm_device_reported</p>`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "chatglm_device_reported\n" {
		t.Fatalf("markdown = %q", got)
	}
}

func TestConvertHTMLCodeTableAndImage(t *testing.T) {
	source := `<pre><code data-language="go">fmt.Println("x")
if a &lt; b {
	println("y")
}</code></pre>
<table>
  <thead><tr><th>名称</th><th>说明</th></tr></thead>
  <tbody><tr><td>A</td><td>第一项</td></tr><tr><td>B</td><td>第二项</td></tr></tbody>
</table>
<p><img class="ne-image" src="https://example.com/image.png" alt="示意图"></p>`

	got, _, err := convertHTML(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"```go\nfmt.Println(\"x\")\nif a < b {\n\tprintln(\"y\")\n}\n```",
		"| 名称 | 说明 |",
		"| --- | --- |",
		"| A | 第一项 |",
		"| B | 第二项 |",
		"![示意图](https://example.com/image.png)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestConvertHTMLUnknownBlockAndWhitespace(t *testing.T) {
	source := `<html><body>
<p>第一段</p><p> </p>
<section><unknown><span class="ne-text">未知块里的文字</span></unknown></section>
<div><br></div>
<p>第二段</p>
</body></html>`

	got, _, err := convertHTML(source)
	if err != nil {
		t.Fatal(err)
	}
	if got != "第一段\n\n未知块里的文字\n\n第二段\n" {
		t.Fatalf("markdown = %q", got)
	}
	if strings.Contains(got, "\r") || strings.Contains(got, "\n\n\n") {
		t.Fatalf("markdown was not normalized: %q", got)
	}
}

func TestConvertBoardCard(t *testing.T) {
	got, boards, err := convertHTML(`<div class="ne-board" data-title="架构图" data-href="https://example.com/board">
		<p>入口 -&gt; 服务</p>
		<img src="https://example.com/preview.png">
	</div>`)
	if err != nil {
		t.Fatal(err)
	}
	if boards != 1 {
		t.Fatalf("boards = %d", boards)
	}
	for _, want := range []string{
		"架构图",
		"入口 -> 服务",
		"https://example.com/preview.png",
		"https://example.com/board",
		"暂不支持交互编辑",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestConvertEmptyBoardCardHasPlaceholder(t *testing.T) {
	got, boards, err := convertHTML(`<div data-card-type="board"></div>`)
	if err != nil {
		t.Fatal(err)
	}
	if boards != 1 {
		t.Fatalf("boards = %d", boards)
	}
	for _, want := range []string{"暂无可见内容", "暂不支持交互编辑"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestConvertDoesNotTreatOrdinaryBoardLinkAsCard(t *testing.T) {
	got, boards, err := convertHTML(`<p><a href="https://example.com/board">board 说明</a></p>`)
	if err != nil {
		t.Fatal(err)
	}
	if boards != 0 {
		t.Fatalf("boards = %d", boards)
	}
	if !strings.Contains(got, "[board 说明](https://example.com/board)") {
		t.Fatalf("markdown = %q", got)
	}
}
