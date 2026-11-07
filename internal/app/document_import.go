package app

import (
	"encoding/xml"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui/richtext"
	docx "github.com/mmonterroca/docxgo/v2"
	"github.com/mmonterroca/docxgo/v2/domain"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// importDocumentFile imports a supported document under parentRel.
func (a *app) importDocumentFile(parentRel, srcPath string) error {
	switch strings.ToLower(filepath.Ext(srcPath)) {
	case ".md", ".markdown", ".mdown":
		return a.importMarkdownFile(parentRel, srcPath)
	case ".txt":
		return a.importTextDocument(parentRel, srcPath)
	case ".docx":
		return a.importWordDocument(parentRel, srcPath)
	case ".doc":
		err := fmt.Errorf("不支持旧版 Word 文档，请另存为 .docx 后再导入")
		a.setError(err)
		return err
	default:
		err := fmt.Errorf("不支持的文档格式：%s", filepath.Ext(srcPath))
		a.setError(err)
		return err
	}
}

// writeImportedDocument creates a Markdown document and lets prepare fill it
// before it is written and opened.
func (a *app) writeImportedDocument(parentRel, srcPath string, prepare func(rel string) (string, error)) error {
	title := strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath))
	rel, err := a.vault.CreateDoc(parentRel, title)
	if err != nil {
		a.setError(err)
		return err
	}
	md, err := prepare(rel)
	if err != nil {
		a.setError(err)
		return err
	}
	if _, err := a.vault.WriteDoc(rel, md); err != nil {
		a.setError(err)
		return err
	}
	*a.openState(parentRel) = true
	a.refreshTree()
	a.openDoc(rel)
	return nil
}

func (a *app) importTextDocument(parentRel, srcPath string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		a.setError(err)
		return err
	}
	text, err := decodeTextDocument(data)
	if err != nil {
		a.setError(err)
		return err
	}
	return a.writeImportedDocument(parentRel, srcPath, func(string) (string, error) {
		return plainTextDocumentMarkdown(text), nil
	})
}

// A TXT file is plain text, so its physical lines and blank lines stay as
// they are. Markdown hard breaks keep the native editor from joining them
// into a paragraph, while remaining invisible in the web editor.
func plainTextDocumentMarkdown(text string) string {
	lines := strings.Split(strings.TrimSuffix(normalizeNewlines(text), "\n"), "\n")
	for i, line := range lines {
		line = escapePlainTextLine(line)
		if i+1 < len(lines) && strings.TrimSpace(line) != "" {
			line += "  "
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

func escapePlainTextLine(line string) string {
	var b strings.Builder
	for i, r := range line {
		switch r {
		case '\\', '*', '_', '`', '[', ']', '<', '~':
			b.WriteByte('\\')
		case '&':
			if i+1 < len(line) && (line[i+1] == '#' || (line[i+1] >= 'A' && line[i+1] <= 'Z') || (line[i+1] >= 'a' && line[i+1] <= 'z')) {
				b.WriteByte('\\')
			}
		}
		b.WriteRune(r)
	}
	text := b.String()
	switch {
	case strings.HasPrefix(text, "#"), strings.HasPrefix(text, ">"), strings.HasPrefix(text, "-"), strings.HasPrefix(text, "+"), strings.HasPrefix(text, "="):
		return `\` + text
	}
	i := 0
	for i < len(text) && text[i] >= '0' && text[i] <= '9' {
		i++
	}
	if i > 0 && i+1 < len(text) && (text[i] == '.' || text[i] == ')') && (text[i+1] == ' ' || text[i+1] == '\t') {
		return text[:i] + `\` + text[i:]
	}
	return text
}

func decodeTextDocument(data []byte) (string, error) {
	data = bytesTrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	text := ""
	if utf8.Valid(data) {
		text = string(data)
	} else {
		decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(data)
		if err != nil {
			return "", fmt.Errorf("无法识别文本编码：%w", err)
		}
		text = string(decoded)
	}
	return normalizeNewlines(text), nil
}

func bytesTrimPrefix(data, prefix []byte) []byte {
	if len(data) >= len(prefix) && string(data[:len(prefix)]) == string(prefix) {
		return data[len(prefix):]
	}
	return data
}

func normalizeNewlines(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}

func (a *app) importWordDocument(parentRel, srcPath string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		a.setError(err)
		return err
	}
	doc, err := docx.OpenDocumentFromBytes(data)
	if err != nil {
		a.setError(err)
		return err
	}
	numbering := wordNumberingFormats(doc)
	return a.writeImportedDocument(parentRel, srcPath, func(rel string) (string, error) {
		return a.wordDocumentMarkdown(doc, numbering, rel)
	})
}

func (a *app) wordDocumentMarkdown(doc domain.Document, numbering map[wordNumberingKey]string, docRel string) (string, error) {
	var blocks []string
	for _, block := range doc.Blocks() {
		switch {
		case block.Paragraph != nil:
			md, err := a.wordParagraphMarkdown(block.Paragraph, numbering, docRel)
			if err != nil {
				return "", err
			}
			if md != "" {
				blocks = append(blocks, md)
			}
		case block.Table != nil:
			md, err := a.wordTableMarkdown(block.Table, docRel)
			if err != nil {
				return "", err
			}
			if md != "" {
				blocks = append(blocks, md)
			}
		}
	}
	if len(blocks) == 0 {
		return "", nil
	}
	return strings.Join(blocks, "\n\n") + "\n", nil
}

func (a *app) wordParagraphMarkdown(p domain.Paragraph, numbering map[wordNumberingKey]string, docRel string) (string, error) {
	text, err := a.wordParagraphInline(p, docRel)
	if err != nil {
		return "", err
	}
	if text == "" {
		return "", nil
	}
	if level := wordHeadingLevel(p); level > 0 {
		level = min(level, 6)
		return strings.Repeat("#", level) + " " + text, nil
	}
	if ref, ok := p.Numbering(); ok {
		prefix := "1. "
		if numbering[wordNumberingKey{ref.ID, ref.Level}] == "bullet" {
			prefix = "- "
		}
		return strings.Repeat("  ", ref.Level) + prefix + text, nil
	}
	return text, nil
}

func (a *app) wordParagraphInline(p domain.Paragraph, docRel string) (string, error) {
	var parts []string
	var runs []richtext.Run
	flushText := func() {
		if len(runs) == 0 {
			return
		}
		parts = append(parts, richtext.TextOf(richtext.Paragraph{Runs: runs}).Markdown())
		runs = nil
	}
	seenImages := map[string]bool{}
	for _, run := range p.Runs() {
		text := cleanWordText(run.Text())
		if text != "" {
			attrs := richtext.Attributes{}
			if run.Italic() {
				attrs.Italic = true
			}
			if run.Bold() {
				attrs.Weight = 700
			}
			if run.Strike() {
				attrs.Strikethrough = true
			}
			if run.Underline() != domain.UnderlineNone {
				attrs.Underline = true
			}
			runs = append(runs, richtext.Run{Text: text, Attributes: attrs})
		}
		if len(run.Breaks()) > 0 {
			flushText()
			parts = append(parts, strings.Repeat("<br>", len(run.Breaks())))
		}
		if img := run.Image(); img != nil {
			flushText()
			md, err := a.importWordImage(docRel, img)
			if err != nil {
				return "", err
			}
			parts = append(parts, md)
			seenImages[img.ID()] = true
		}
	}
	flushText()
	for _, img := range p.Images() {
		if img == nil || seenImages[img.ID()] {
			continue
		}
		md, err := a.importWordImage(docRel, img)
		if err != nil {
			return "", err
		}
		parts = append(parts, md)
	}
	return strings.Join(parts, ""), nil
}

func (a *app) importWordImage(docRel string, img domain.Image) (string, error) {
	name := path.Base(img.Target())
	if name == "." || name == "" {
		name = "image." + string(img.Format())
	}
	ref, err := a.vault.ImportAssetBytes(docRel, name, img.Data())
	if err != nil {
		return "", err
	}
	alt := strings.TrimSpace(img.Description())
	if alt == "" {
		alt = "图片"
	}
	alt = strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`).Replace(alt)
	return "![" + alt + "](" + ref + ")", nil
}

func (a *app) wordTableMarkdown(table domain.Table, docRel string) (string, error) {
	rows := table.Rows()
	if len(rows) == 0 {
		return "", nil
	}
	columns := table.ColumnCount()
	if columns == 0 {
		for _, row := range rows {
			if n := len(row.Cells()); n > columns {
				columns = n
			}
		}
	}
	if columns == 0 {
		return "", nil
	}
	var lines []string
	for i, row := range rows {
		cells := make([]string, columns)
		for col, cell := range row.Cells() {
			if col >= columns {
				break
			}
			text, err := a.wordTableCellMarkdown(cell, docRel)
			if err != nil {
				return "", err
			}
			cells[col] = text
		}
		lines = append(lines, "| "+strings.Join(cells, " | ")+" |")
		if i == 0 {
			separator := make([]string, columns)
			for col := range separator {
				separator[col] = "---"
			}
			lines = append(lines, "| "+strings.Join(separator, " | ")+" |")
		}
	}
	return strings.Join(lines, "\n"), nil
}

func (a *app) wordTableCellMarkdown(cell domain.TableCell, docRel string) (string, error) {
	var paragraphs []string
	for _, p := range cell.Paragraphs() {
		text, err := a.wordParagraphInline(p, docRel)
		if err != nil {
			return "", err
		}
		if text != "" {
			paragraphs = append(paragraphs, text)
		}
	}
	text := strings.Join(paragraphs, "<br>")
	text = strings.ReplaceAll(text, "\n", "<br>")
	return strings.ReplaceAll(text, "|", `\|`), nil
}

func wordHeadingLevel(p domain.Paragraph) int {
	styleID := ""
	if style := p.Style(); style != nil {
		styleID = style.ID()
	} else if named, ok := p.(interface{ StyleName() string }); ok {
		styleID = named.StyleName()
	}
	styleID = strings.ToLower(strings.TrimSpace(styleID))
	styleID = strings.NewReplacer(" ", "", "-", "", "_", "").Replace(styleID)
	if !strings.HasPrefix(styleID, "heading") {
		return 0
	}
	level, err := strconv.Atoi(strings.TrimPrefix(styleID, "heading"))
	if err != nil || level < 1 || level > 9 {
		return 0
	}
	return level
}

func cleanWordText(text string) string {
	text = normalizeNewlines(text)
	text = strings.ReplaceAll(text, "\t", " ")
	return strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\n' {
			return -1
		}
		return r
	}, text)
}

type wordNumberingKey struct {
	ID    int
	Level int
}

type wordNumberingXML struct {
	AbstractNums []struct {
		ID     string `xml:"abstractNumId,attr"`
		Levels []struct {
			Level  int `xml:"ilvl,attr"`
			Format struct {
				Val string `xml:"val,attr"`
			} `xml:"numFmt"`
		} `xml:"lvl"`
	} `xml:"abstractNum"`
	Nums []struct {
		ID         int `xml:"numId,attr"`
		AbstractID struct {
			Val string `xml:"val,attr"`
		} `xml:"abstractNumId"`
	} `xml:"num"`
}

func wordNumberingFormats(doc domain.Document) map[wordNumberingKey]string {
	provider, ok := doc.(interface{ NumberingPartInfo() ([]byte, string) })
	if !ok {
		return nil
	}
	data, _ := provider.NumberingPartInfo()
	if len(data) == 0 {
		return nil
	}
	var numbering wordNumberingXML
	if err := xml.Unmarshal(data, &numbering); err != nil {
		return nil
	}
	abstract := make(map[string]map[int]string)
	for _, def := range numbering.AbstractNums {
		levels := make(map[int]string)
		for _, level := range def.Levels {
			levels[level.Level] = level.Format.Val
		}
		abstract[def.ID] = levels
	}
	formats := make(map[wordNumberingKey]string)
	for _, num := range numbering.Nums {
		for level, format := range abstract[num.AbstractID.Val] {
			formats[wordNumberingKey{num.ID, level}] = format
		}
	}
	return formats
}
