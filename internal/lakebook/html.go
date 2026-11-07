package lakebook

import (
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

const boardNotice = "> 此内容来自语雀画板，暂不支持交互编辑。"

type htmlRenderer struct {
	blocks     []string
	boardCount int
}

func convertHTML(source string) (string, int, error) {
	if strings.TrimSpace(source) == "" {
		return "", 0, nil
	}
	root, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return "", 0, err
	}

	renderer := &htmlRenderer{}
	renderer.walkBlocks(root)
	return normalizeMarkdown(strings.Join(renderer.blocks, "\n\n")), renderer.boardCount, nil
}

func (r *htmlRenderer) walkBlocks(node *html.Node) {
	if node.Type == html.TextNode {
		text := strings.TrimSpace(escapeMarkdown(node.Data))
		if text != "" {
			r.blocks = append(r.blocks, text)
		}
		return
	}
	if node.Type != html.ElementNode {
		r.walkChildren(node)
		return
	}
	if isBoardCard(node) {
		r.blocks = append(r.blocks, renderBoardCard(node))
		r.boardCount++
		return
	}

	switch node.Data {
	case "p":
		if text := strings.TrimSpace(r.renderInlineChildren(node)); text != "" {
			r.blocks = append(r.blocks, text)
		}
	case "h1", "h2", "h3", "h4", "h5", "h6":
		if text := strings.TrimSpace(r.renderInlineChildren(node)); text != "" {
			level, _ := strconv.Atoi(node.Data[1:])
			r.blocks = append(r.blocks, strings.Repeat("#", level)+" "+text)
		}
	case "ul", "ol":
		if block := r.renderList(node, 0); block != "" {
			r.blocks = append(r.blocks, block)
		}
	case "pre":
		if block := renderCodeBlock(node); block != "" {
			r.blocks = append(r.blocks, block)
		}
	case "table":
		if block := r.renderTable(node); block != "" {
			r.blocks = append(r.blocks, block)
		}
	case "hr":
		r.blocks = append(r.blocks, "---")
	case "img":
		if image := renderImage(node, ""); image != "" {
			r.blocks = append(r.blocks, image)
		}
	case "blockquote":
		text := strings.TrimSpace(r.renderInlineChildren(node))
		if text != "" {
			lines := strings.Split(text, "\n")
			for index := range lines {
				lines[index] = "> " + lines[index]
			}
			r.blocks = append(r.blocks, strings.Join(lines, "\n"))
		}
	default:
		if hasBlockContent(node) {
			r.walkChildren(node)
			return
		}
		if text := strings.TrimSpace(r.renderInline(node)); text != "" {
			r.blocks = append(r.blocks, text)
		}
	}
}

func (r *htmlRenderer) walkChildren(node *html.Node) {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		r.walkBlocks(child)
	}
}

func (r *htmlRenderer) renderInline(node *html.Node) string {
	if node.Type == html.TextNode {
		return escapeMarkdown(node.Data)
	}
	if node.Type != html.ElementNode {
		return r.renderInlineChildren(node)
	}

	switch node.Data {
	case "strong", "b":
		return wrapInline(r.renderInlineChildren(node), "**", "**")
	case "em", "i":
		return wrapInline(r.renderInlineChildren(node), "*", "*")
	case "code":
		return renderCodeSpan(nodeText(node))
	case "br":
		return "  \n"
	case "a":
		text := strings.TrimSpace(r.renderInlineChildren(node))
		href := attr(node, "href")
		if text == "" {
			text = escapeMarkdown(href)
		}
		if href == "" {
			return text
		}
		return "[" + text + "](" + markdownDestination(href) + ")"
	case "img":
		return renderImage(node, "")
	default:
		return r.renderInlineChildren(node)
	}
}

func (r *htmlRenderer) renderInlineChildren(node *html.Node) string {
	var b strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		b.WriteString(r.renderInline(child))
	}
	return b.String()
}

func (r *htmlRenderer) renderList(list *html.Node, depth int) string {
	ordered := list.Data == "ol"
	indent := strings.Repeat("  ", depth)
	var lines []string
	itemNumber := 1
	for child := list.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode || child.Data != "li" {
			continue
		}
		var inline strings.Builder
		var nested []*html.Node
		for part := child.FirstChild; part != nil; part = part.NextSibling {
			if part.Type == html.ElementNode && (part.Data == "ul" || part.Data == "ol") {
				nested = append(nested, part)
				continue
			}
			inline.WriteString(r.renderInline(part))
		}
		text := strings.TrimSpace(inline.String())
		if text != "" {
			marker := "- "
			if ordered {
				marker = strconv.Itoa(itemNumber) + ". "
				itemNumber++
			}
			lines = append(lines, indent+marker+text)
		}
		for _, nestedList := range nested {
			if block := r.renderList(nestedList, depth+1); block != "" {
				lines = append(lines, block)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func renderCodeBlock(pre *html.Node) string {
	code := pre
	for child := pre.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == "code" {
			code = child
			break
		}
	}
	language := attr(code, "data-language")
	if language == "" {
		for _, class := range strings.Fields(attr(code, "class")) {
			if strings.HasPrefix(class, "language-") {
				language = strings.TrimPrefix(class, "language-")
				break
			}
		}
	}
	content := strings.TrimRight(normalizeNewlines(nodeText(code)), "\n")
	if strings.TrimSpace(content) == "" {
		return ""
	}
	return "```" + language + "\n" + content + "\n```"
}

func (r *htmlRenderer) renderTable(table *html.Node) string {
	var rows [][]string
	var collectRows func(*html.Node)
	collectRows = func(node *html.Node) {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.ElementNode {
				continue
			}
			switch child.Data {
			case "tr":
				var cells []string
				for cell := child.FirstChild; cell != nil; cell = cell.NextSibling {
					if cell.Type != html.ElementNode || (cell.Data != "th" && cell.Data != "td") {
						continue
					}
					text := strings.TrimSpace(r.renderInlineChildren(cell))
					text = strings.ReplaceAll(text, "\n", "<br>")
					text = strings.ReplaceAll(text, "|", `\|`)
					cells = append(cells, text)
				}
				if len(cells) > 0 {
					rows = append(rows, cells)
				}
			case "thead", "tbody", "tfoot":
				collectRows(child)
			}
		}
	}
	collectRows(table)
	if len(rows) == 0 {
		return ""
	}

	columns := 0
	for _, row := range rows {
		if len(row) > columns {
			columns = len(row)
		}
	}
	if columns == 0 {
		return ""
	}
	var b strings.Builder
	writeRow := func(row []string) {
		b.WriteString("|")
		for index := 0; index < columns; index++ {
			cell := ""
			if index < len(row) {
				cell = row[index]
			}
			b.WriteString(" ")
			b.WriteString(cell)
			b.WriteString(" |")
		}
	}
	writeRow(rows[0])
	b.WriteByte('\n')
	b.WriteString("|")
	for index := 0; index < columns; index++ {
		b.WriteString(" --- |")
	}
	for _, row := range rows[1:] {
		b.WriteByte('\n')
		writeRow(row)
	}
	return b.String()
}

func isBoardCard(node *html.Node) bool {
	for _, class := range strings.Fields(attr(node, "class")) {
		if class == "ne-board" || class == "lake-board" {
			return true
		}
	}
	return strings.EqualFold(attr(node, "data-card-type"), "board") ||
		strings.EqualFold(attr(node, "data-type"), "board")
}

func renderBoardCard(node *html.Node) string {
	title := strings.TrimSpace(attr(node, "data-title"))
	if title == "" {
		title = strings.TrimSpace(attr(node, "title"))
	}
	if title == "" {
		title = findHeadingText(node)
	}

	link := findAttribute(node, []string{"data-href", "href"})
	preview := findAttribute(node, []string{"preview", "cover", "thumbnail", "data-preview"})
	if preview == "" {
		preview = findImageSource(node)
	}
	text := visibleText(node, title)

	var parts []string
	if title != "" {
		parts = append(parts, "## "+escapeMarkdown(title))
	}
	if text != "" {
		parts = append(parts, escapeMarkdown(text))
	}
	if preview != "" {
		parts = append(parts, "![画板预览]("+markdownDestination(preview)+")")
	}
	if link != "" {
		parts = append(parts, "[在语雀中打开画板]("+markdownDestination(link)+")")
	}
	if text == "" && preview == "" && link == "" && title == "" {
		parts = append(parts, "暂无可见内容。")
	}
	parts = append(parts, boardNotice)
	return strings.Join(parts, "\n\n")
}

func findHeadingText(root *html.Node) string {
	var found string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if found != "" {
			return
		}
		if node.Type == html.ElementNode && len(node.Data) == 2 && node.Data[0] == 'h' &&
			node.Data[1] >= '1' && node.Data[1] <= '6' {
			found = strings.TrimSpace(nodeText(node))
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		walk(child)
	}
	return found
}

func findAttribute(root *html.Node, names []string) string {
	var found string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if found != "" {
			return
		}
		if node.Type == html.ElementNode {
			for _, name := range names {
				if value := strings.TrimSpace(attr(node, name)); value != "" {
					found = value
					return
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

func findImageSource(root *html.Node) string {
	var found string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if found != "" {
			return
		}
		if node.Type == html.ElementNode && node.Data == "img" {
			found = firstNonEmpty(attr(node, "src"), attr(node, "data-src"))
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

func visibleText(root *html.Node, title string) string {
	text := normalizeWhitespace(nodeText(root))
	if title != "" {
		text = strings.TrimSpace(strings.TrimPrefix(text, title))
	}
	return text
}

func hasBlockContent(node *html.Node) bool {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode {
			continue
		}
		switch child.Data {
		case "p", "h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "pre",
			"table", "hr", "blockquote", "div", "section", "article", "main",
			"body", "html", "thead", "tbody", "tr":
			return true
		}
		if isBoardCard(child) {
			return true
		}
	}
	return false
}

func renderImage(node *html.Node, fallbackAlt string) string {
	src := firstNonEmpty(attr(node, "src"), attr(node, "data-src"))
	if src == "" {
		return ""
	}
	alt := strings.TrimSpace(attr(node, "alt"))
	if alt == "" {
		alt = fallbackAlt
	}
	alt = strings.ReplaceAll(alt, "]", `\]`)
	return "![" + alt + "](" + markdownDestination(src) + ")"
}

func wrapInline(value, before, after string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return before + value + after
}

func renderCodeSpan(value string) string {
	if value == "" {
		return ""
	}
	maxRun := 0
	current := 0
	for _, char := range value {
		if char == '`' {
			current++
			if current > maxRun {
				maxRun = current
			}
		} else {
			current = 0
		}
	}
	delimiter := strings.Repeat("`", maxRun+1)
	if strings.HasPrefix(value, "`") || strings.HasSuffix(value, "`") {
		return delimiter + " " + value + " " + delimiter
	}
	return delimiter + value + delimiter
}

func escapeMarkdown(value string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		"*", `\*`,
		"[", `\[`,
		"]", `\]`,
		"`", "\\`",
	)
	return replacer.Replace(normalizeNewlines(value))
}

func markdownDestination(value string) string {
	value = strings.TrimSpace(value)
	if strings.ContainsAny(value, " \t\n()") {
		return "<" + strings.ReplaceAll(value, ">", "%3E") + ">"
	}
	return value
}

func attr(node *html.Node, name string) string {
	for _, attribute := range node.Attr {
		if strings.EqualFold(attribute.Key, name) {
			return attribute.Val
		}
	}
	return ""
}

func nodeText(node *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			b.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return b.String()
}

func normalizeWhitespace(value string) string {
	return strings.Join(strings.Fields(normalizeNewlines(value)), " ")
}

func normalizeMarkdown(value string) string {
	value = normalizeNewlines(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	lines := strings.Split(value, "\n")
	normalized := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if blank {
				continue
			}
			blank = true
			normalized = append(normalized, "")
			continue
		}
		blank = false
		normalized = append(normalized, line)
	}
	return strings.Join(normalized, "\n") + "\n"
}

func normalizeNewlines(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.ReplaceAll(value, "\r", "\n")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
