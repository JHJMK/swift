package app

import (
	"strings"

	"github.com/egoist/mygo/ui/richtext"
)

// insertDiagram inserts a Mermaid block. The editor's Mermaid preview
// renders it when the page reopens, as other Mermaid blocks do.
func (a *app) insertDiagram(source string) {
	if a.doc == nil || a.doc.ReadOnly {
		return
	}
	md := "```mermaid\n" + strings.TrimSpace(source) + "\n```\n"
	a.replaceSelection(richtext.ParseMarkdown(md))
	a.markDocDirty()
	a.update()
}

func (a *app) insertBoard() {
	a.insertDiagram(`block-beta
  columns 3
  a["想法"] b["方案"] c["上线"]
  a --> b
  b --> c`)
}

func (a *app) insertFlowchart() {
	a.insertDiagram(`flowchart TD
  A[开始] --> B[处理]
  B --> C[结束]`)
}

func (a *app) insertMindmap() {
	a.insertDiagram(`mindmap
  root((中心主题))
    分支一
    分支二`)
}
