package app

import (
	"archive/zip"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui/richtext"
	docx "github.com/mmonterroca/docxgo/v2"
	"github.com/mmonterroca/docxgo/v2/domain"
)

const pdfStyle = `<style>
@page { size: A4; margin: 18mm; }
body { margin: 0; font-family: -apple-system, "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif; font-size: 15px; line-height: 1.8; color: #1a1a1a; }
h1 { font-size: 28px; } h2 { font-size: 23px; } h3 { font-size: 19px; }
h1, h2, h3 { line-height: 1.4; margin: 1.2em 0 0.5em; }
p { margin: 0.5em 0; }
blockquote { margin: 0.8em 0; padding: 0.2em 1em; border-left: 3px solid #c9c9c9; color: #555; }
pre { background: #f4f4f4; padding: 12px; border-radius: 6px; white-space: pre-wrap; }
code { font-family: "SF Mono", Menlo, monospace; font-size: 0.92em; }
li { margin: 0.25em 0; }
img { max-width: 100%; height: auto; }
ul, ol { padding-left: 1.6em; }
</style>`

// exportText returns the document text with its images embedded as data
// URIs, so a hidden web page can print it without file access.
func (a *app) exportText() richtext.Text {
	return a.mapAttachments(a.doc.Doc.Text(), func(at *richtext.Attachment) *richtext.Attachment {
		if at.URL == "" || strings.HasPrefix(at.URL, "data:") {
			return at
		}
		filename, ok := a.vault.ResolveAsset(a.doc.Rel, at.URL)
		if !ok {
			return at
		}
		b, err := os.ReadFile(filename)
		if err != nil {
			return at
		}
		mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename)))
		if mimeType == "" {
			mimeType = "image/png"
		}
		cp := *at
		cp.URL = "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(b)
		return &cp
	})
}

// mapAttachments returns text with fn applied to each attachment. Format
// leaves attachments as they are, so the text is rebuilt from paragraphs.
func (a *app) mapAttachments(text richtext.Text, fn func(*richtext.Attachment) *richtext.Attachment) richtext.Text {
	var paras []richtext.Paragraph
	changed := false
	for _, p := range text.Paragraphs() {
		runs := make([]richtext.Run, 0, len(p.Runs))
		for _, r := range p.Runs {
			if at := r.Attributes.Attachment; at != nil {
				if next := fn(at); next != at {
					r.Attributes.Attachment = next
					changed = true
				}
			}
			runs = append(runs, r)
		}
		paras = append(paras, richtext.Paragraph{Style: p.Style, Runs: runs})
	}
	if !changed {
		return text
	}
	return richtext.TextOf(paras...)
}

func (a *app) exportPDF() {
	p, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:      a.win,
		Title:       "导出 PDF",
		DefaultPath: filepath.Join(defaultExportDir(), a.doc.Title+".pdf"),
		Filters:     []mygo.FileFilter{{Name: "PDF", Extensions: []string{"pdf"}}},
	})
	if err != nil || p == "" {
		return
	}
	if !strings.EqualFold(filepath.Ext(p), ".pdf") {
		p += ".pdf"
	}

	pageHTML := "<!doctype html><html><head><meta charset=\"utf-8\">" +
		"<meta name=\"swift-export\" content=\"1\"><title>" + html.EscapeString(a.doc.Title) + "</title>" +
		pdfStyle + "</head><body>" + a.exportText().HTML() + "</body></html>"

	win := mygo.NewWindow(mygo.WindowOptions{
		Title:  "导出 PDF",
		Width:  900,
		Height: 1200,
		Hidden: true,
	})
	if win == nil || win.Page() == nil {
		a.errMsg = "无法创建导出页面"
		return
	}
	page := win.Page()
	finished := false
	var timeout *time.Timer
	finish := func(err error) {
		if finished {
			return
		}
		finished = true
		if timeout != nil {
			timeout.Stop()
		}
		win.Close()
		if err != nil {
			a.setError(err)
		}
		a.update()
	}
	timeout = time.AfterFunc(20*time.Second, func() {
		mygo.RunOnMain(func() { finish(errors.New("导出 PDF 超时")) })
	})
	page.OnDidFailLoad(func(e *mygo.LoadError) {
		finish(fmt.Errorf("导出 PDF 失败：%s", e.Description))
	})
	page.OnDidFinishLoad(func() {
		// The blank page a window starts on has no marker; ours is the
		// page that carries one.
		v, err := page.Eval(`document.querySelector('meta[name="swift-export"]') !== null`)
		if err != nil {
			return
		}
		if ok, _ := v.(bool); !ok {
			return
		}
		pdf, err := page.PrintToPDF(mygo.PDFOptions{
			PageSize:   mygo.PageA4,
			Margins:    &mygo.Margins{Top: 0.7, Right: 0.7, Bottom: 0.7, Left: 0.7},
			Background: true,
		})
		if err != nil {
			finish(err)
			return
		}
		if err := os.WriteFile(p, pdf, 0o644); err != nil {
			finish(err)
			return
		}
		finish(nil)
	})
	page.LoadHTML(pageHTML, "")
}

func (a *app) exportDOCX() {
	p, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:      a.win,
		Title:       "导出 Word",
		DefaultPath: filepath.Join(defaultExportDir(), a.doc.Title+".docx"),
		Filters:     []mygo.FileFilter{{Name: "Word 文档", Extensions: []string{"docx"}}},
	})
	if err != nil || p == "" {
		return
	}
	if !strings.EqualFold(filepath.Ext(p), ".docx") {
		p += ".docx"
	}
	if err := a.writeDOCX(p); err != nil {
		a.setError(err)
	}
}

func (a *app) writeDOCX(path string) error {
	doc := docx.NewDocument()
	if meta := doc.Metadata(); meta != nil {
		meta.Title = a.doc.Title
		doc.SetMetadata(meta)
	}
	if section, err := doc.DefaultSection(); err == nil {
		section.SetPageSize(domain.PageSizeA4)
		section.SetMargins(domain.Margins{Top: 1440, Right: 1247, Bottom: 1440, Left: 1247, Header: 720, Footer: 720})
	}

	const mono = "Menlo"
	numbers := map[int]int{}
	inNumbers := false
	for _, p := range a.doc.Doc.Text().Paragraphs() {
		para, err := doc.AddParagraph()
		if err != nil {
			return err
		}
		style := p.Style
		switch {
		case style.Heading > 0:
			level := style.Heading
			if level > 6 {
				level = 6
			}
			para.SetStyle([]string{
				domain.StyleIDHeading1, domain.StyleIDHeading2, domain.StyleIDHeading3,
				domain.StyleIDHeading4, domain.StyleIDHeading5, domain.StyleIDHeading6,
			}[level-1])
		case style.CodeBlock:
			para.SetShading(domain.Color{R: 244, G: 244, B: 244})
		case style.Quote:
			para.SetStyle(domain.StyleIDQuote)
		}
		if style.List != richtext.NoList {
			para.SetIndentLeft(style.Indent * 360)
		}

		prefix := ""
		switch style.List {
		case richtext.Bullets:
			prefix = strings.Repeat("  ", style.Indent) + "• "
			inNumbers = false
		case richtext.Tasks:
			if style.Checked {
				prefix = strings.Repeat("  ", style.Indent) + "☑ "
			} else {
				prefix = strings.Repeat("  ", style.Indent) + "☐ "
			}
			inNumbers = false
		case richtext.Numbers:
			if !inNumbers {
				numbers = map[int]int{}
				inNumbers = true
			}
			numbers[style.Indent]++
			for k := range numbers {
				if k > style.Indent {
					delete(numbers, k)
				}
			}
			prefix = strings.Repeat("  ", style.Indent) + fmt.Sprintf("%d. ", numbers[style.Indent])
		default:
			inNumbers = false
		}
		if prefix != "" {
			run, err := para.AddRun()
			if err != nil {
				return err
			}
			run.SetText(prefix)
		}

		for _, r := range p.Runs {
			if at := r.Attributes.Attachment; at != nil {
				if err := a.addDOCXImage(para, at); err != nil {
					continue
				}
				continue
			}
			text := strings.ReplaceAll(r.Text, string(richtext.ObjectReplacement), "")
			if text == "" {
				continue
			}
			if link := r.Attributes.Link; link != "" {
				if _, err := para.AddHyperlink(link, text); err != nil {
					return err
				}
				continue
			}
			run, err := para.AddRun()
			if err != nil {
				return err
			}
			run.SetText(text)
			run.SetBold(r.Attributes.Weight >= 600)
			if r.Attributes.Italic {
				run.SetItalic(true)
			}
			if r.Attributes.Underline {
				run.SetUnderline(domain.UnderlineSingle)
			}
			if r.Attributes.Strikethrough {
				run.SetStrike(true)
			}
			if r.Attributes.Code || style.CodeBlock {
				run.SetFont(domain.Font{Name: mono, EastAsia: mono, CS: mono})
			}
		}
	}
	return doc.SaveAs(path)
}

func (a *app) addDOCXImage(para domain.Paragraph, at *richtext.Attachment) error {
	filename, ok := a.vault.ResolveAsset(a.doc.Rel, at.URL)
	if !ok {
		return errors.New("图片不存在")
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	format := docxImageFormat(filename)
	if format == "" {
		return errors.New("不支持的图片格式")
	}
	if at.Width > 0 && at.Height > 0 {
		_, err = para.AddImageFromBytesWithSize(data, format, domain.NewImageSize(int(at.Width), int(at.Height)))
	} else {
		_, err = para.AddImageFromBytes(data, format)
	}
	return err
}

func docxImageFormat(name string) domain.ImageFormat {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return domain.ImageFormatPNG
	case ".jpg", ".jpeg":
		return domain.ImageFormatJPEG
	case ".gif":
		return domain.ImageFormatGIF
	case ".bmp":
		return domain.ImageFormatBMP
	case ".tif", ".tiff":
		return domain.ImageFormatTIFF
	case ".webp":
		return domain.ImageFormatWEBP
	case ".svg":
		return domain.ImageFormatSVG
	}
	return ""
}

// exportKnowledgeBase writes a knowledge base, its documents and assets,
// as a zip archive where the chosen path says.
func (a *app) exportKnowledgeBase(n *Node) {
	a.flushTitle()
	a.flushDoc()
	title := n.Title()
	p, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:      a.win,
		Title:       "导出知识库",
		DefaultPath: filepath.Join(defaultExportDir(), title+".zip"),
		Filters:     []mygo.FileFilter{{Name: "ZIP", Extensions: []string{"zip"}}},
	})
	if err != nil || p == "" {
		return
	}
	if !strings.EqualFold(filepath.Ext(p), ".zip") {
		p += ".zip"
	}
	src, err := a.vault.Abs(n.Rel)
	if err != nil {
		a.setError(err)
		return
	}
	if strings.HasPrefix(filepath.Clean(p), filepath.Clean(src)+string(os.PathSeparator)) {
		a.setError(errors.New("不能把知识库导出到它自己的目录中"))
		return
	}
	if err := zipTree(src, title, p); err != nil {
		a.setError(err)
	}
}

// zipTree writes dir and what is below it into a zip archive at dst,
// under top as their first path element. It keeps the assets of a
// knowledge base, and skips the Finder's .DS_Store files.
func zipTree(dir, top, dst string) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	if _, err := zw.CreateHeader(&zip.FileHeader{Name: top + "/", Method: zip.Store, Modified: time.Now()}); err != nil {
		zw.Close()
		f.Close()
		os.Remove(dst)
		return err
	}
	walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		name := path.Join(top, filepath.ToSlash(rel))
		if d.IsDir() {
			_, err := zw.CreateHeader(&zip.FileHeader{Name: name + "/", Method: zip.Store, Modified: time.Now()})
			return err
		}
		if d.Name() == ".DS_Store" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: info.ModTime()})
		if err != nil {
			return err
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(w, in)
		return err
	})
	if walkErr != nil {
		zw.Close()
		f.Close()
		os.Remove(dst)
		return walkErr
	}
	if err := zw.Close(); err != nil {
		f.Close()
		os.Remove(dst)
		return err
	}
	return f.Close()
}
