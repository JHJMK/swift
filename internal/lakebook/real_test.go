package lakebook

import (
	"os"
	"strings"
	"testing"
)

func TestParseRealLakebook(t *testing.T) {
	filename := os.Getenv("LAKEBOOK_REAL")
	if filename == "" {
		t.Skip("set LAKEBOOK_REAL to inspect a Yuque export")
	}

	book, err := Parse(filename)
	if err != nil {
		t.Fatal(err)
	}
	var docs, titles, boards, images int
	var walk func([]*Node, int)
	walk = func(nodes []*Node, depth int) {
		for _, node := range nodes {
			switch node.Kind {
			case KindDoc:
				docs++
			case KindTitle:
				titles++
			case KindBoard:
				boards++
			}
			images += strings.Count(node.Markdown, "![")
			t.Logf("%s%s [%d] markdown=%d", strings.Repeat("  ", depth), node.Title, node.Kind, len(node.Markdown))
			walk(node.Children, depth+1)
		}
	}
	walk(book.Nodes, 0)
	t.Logf("name=%q docs=%d titles=%d boards=%d images=%d", book.Name, docs, titles, boards, images)
}
