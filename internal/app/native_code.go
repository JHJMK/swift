package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui/richtext"
)

// codeFenceInfo is a fenced code block's Markdown info string: its
// language and, when set, the title shown by the app around it.
type codeFenceInfo struct {
	Language string
	Title    string
}

// annotateCodeFences gives the code-block paragraphs their block IDs and
// keeps the fences' language and title, which the rich text editor does
// not carry, by paragraph ID. The app puts the info string back when it
// saves the Markdown.
func (a *app) annotateCodeFences(text richtext.Text, md string) (richtext.Text, map[string]codeFenceInfo) {
	infos := parseFenceInfos(md)
	fences := map[string]codeFenceInfo{}
	var paras []richtext.Paragraph
	for _, p := range text.Paragraphs() {
		paras = append(paras, p)
	}
	next := 0
	for i := 0; i < len(paras); {
		if !paras[i].Style.CodeBlock {
			i++
			continue
		}
		var info codeFenceInfo
		if next < len(infos) {
			info = infos[next]
		}
		next++
		start := i
		for i < len(paras) && paras[i].Style.CodeBlock {
			i++
		}
		if info.Language == "" && info.Title == "" {
			continue
		}
		id := fmt.Sprintf("code-%d", next)
		for j := start; j < i; j++ {
			paras[j].Style.ID = id
		}
		fences[id] = info
	}
	return richtext.TextOf(paras...), fences
}

// newNativeDocument gives every paragraph an ID, so a block's fence info
// stays with its paragraphs as they are edited.
func (a *app) newNativeDocument(text richtext.Text) *richtext.Document {
	doc := richtext.NewDocument(text)
	next := 0
	doc.IdentifyParagraphs(func() string {
		next++
		return fmt.Sprintf("p-%d", next)
	})
	return doc
}

// nativeMarkdown serializes a rich text document and puts back the
// language and title of its fenced code blocks.
func nativeMarkdown(text richtext.Text, fences map[string]codeFenceInfo) string {
	md := text.Markdown()
	if len(fences) == 0 {
		return md
	}
	var infos []string
	var paras []richtext.Paragraph
	for _, p := range text.Paragraphs() {
		paras = append(paras, p)
	}
	for i := 0; i < len(paras); {
		if !paras[i].Style.CodeBlock {
			i++
			continue
		}
		info := ""
		if f, ok := fences[paras[i].Style.ID]; ok {
			info = fenceInfo(f.Language, f.Title)
		}
		infos = append(infos, info)
		for i < len(paras) && paras[i].Style.CodeBlock {
			i++
		}
	}
	lines := strings.Split(md, "\n")
	next := 0
	inFence := false
	var marker byte
	var size int
	for i, line := range lines {
		prefix, body := fencePrefix(line)
		if !inFence {
			n := 0
			for n < len(body) && (body[n] == '`' || body[n] == '~') {
				n++
			}
			if n < 3 {
				continue
			}
			if strings.TrimSpace(body[n:]) == "" && next < len(infos) && infos[next] != "" {
				lines[i] = prefix + body[:n] + infos[next]
			}
			next++
			inFence = true
			marker, size = body[0], n
			continue
		}
		n := 0
		for n < len(body) && body[n] == marker {
			n++
		}
		if n >= size && strings.TrimSpace(body[n:]) == "" {
			inFence = false
		}
	}
	return strings.Join(lines, "\n")
}

// fencePrefix splits the room before a fence's backticks (indentation and
// blockquote markers) from the fence itself.
func fencePrefix(line string) (prefix, body string) {
	i := 0
	for i < len(line) {
		switch line[i] {
		case ' ', '\t', '>':
			i++
		default:
			return line[:i], line[i:]
		}
	}
	return line, ""
}

// parseFenceInfos reads the language and title of every fenced block of
// code, in document order.
func parseFenceInfos(md string) []codeFenceInfo {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	var out []codeFenceInfo
	for i := 0; i < len(lines); i++ {
		_, body := fencePrefix(lines[i])
		n := 0
		for n < len(body) && (body[n] == '`' || body[n] == '~') {
			n++
		}
		if n < 3 {
			continue
		}
		marker, size := body[0], n
		language, title := parseFenceInfo(body[n:])
		out = append(out, codeFenceInfo{Language: language, Title: title})
		for j := i + 1; j < len(lines); j++ {
			_, closeBody := fencePrefix(lines[j])
			m := 0
			for m < len(closeBody) && closeBody[m] == marker {
				m++
			}
			if m >= size && strings.TrimSpace(closeBody[m:]) == "" {
				i = j
				break
			}
		}
	}
	return out
}

// parseFenceInfo splits a fence's info string into its language and title.
func parseFenceInfo(info string) (language, title string) {
	info = strings.TrimSpace(info)
	if info == "" {
		return "", ""
	}
	if strings.HasPrefix(info, "title=") {
		return "", parseFenceTitle(info[len("title="):])
	}
	language = info
	if i := strings.IndexAny(info, " \t"); i >= 0 {
		language = info[:i]
		rest := strings.TrimSpace(info[i+1:])
		if strings.HasPrefix(rest, "title=") {
			title = parseFenceTitle(rest[len("title="):])
		}
	}
	return strings.TrimSpace(language), title
}

func parseFenceTitle(s string) string {
	s = strings.TrimSpace(s)
	if value, err := strconv.Unquote(s); err == nil {
		return value
	}
	return strings.Trim(s, `"'`)
}

// fenceInfo writes the info string of a block of code: its language and,
// when set, its title.
func fenceInfo(language, title string) string {
	info := strings.TrimSpace(language)
	if title = strings.TrimSpace(title); title != "" {
		if info != "" {
			info += " "
		}
		info += "title=" + strconv.Quote(title)
	}
	return info
}

// markMermaidBlock turns the selected paragraphs into a Mermaid code
// block. The native fallback does not render it, but its language is
// saved so the web editor and other Markdown tools still read it.
func (a *app) markMermaidBlock(doc *docState) {
	if doc == nil || doc.Doc == nil {
		return
	}
	id := fmt.Sprintf("mermaid-%d", time.Now().UnixNano())
	doc.Doc.FormatParagraphs(func(p *richtext.ParagraphStyle) {
		p.Heading, p.List, p.Quote = 0, richtext.NoList, false
		p.CodeBlock = true
		p.ID = id
	})
	if doc.codeFences == nil {
		doc.codeFences = map[string]codeFenceInfo{}
	}
	doc.codeFences[id] = codeFenceInfo{Language: "mermaid"}
}
