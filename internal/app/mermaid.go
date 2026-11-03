package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"
)

var (
	mermaidCLIMu    sync.Mutex
	mermaidCLIPath  string
	mermaidCLIFound bool
)

// mermaidCLI finds mmdc: on the PATH, or in the npx cache where
// mermaid-cli installs itself.
func mermaidCLI() string {
	mermaidCLIMu.Lock()
	defer mermaidCLIMu.Unlock()
	if !mermaidCLIFound {
		mermaidCLIPath = findMermaidCLI()
		mermaidCLIFound = true
	}
	return mermaidCLIPath
}

// resetMermaidCLI looks for mmdc again, as after the user installs it.
func resetMermaidCLI() {
	mermaidCLIMu.Lock()
	mermaidCLIFound = false
	mermaidCLIMu.Unlock()
}

func findMermaidCLI() string {
	if p, err := exec.LookPath("mmdc"); err == nil {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(home, ".npm", "_npx", "*", "node_modules", ".bin", "mmdc"))
	if len(matches) == 0 {
		return ""
	}
	sort.Slice(matches, func(i, j int) bool {
		ai, erri := os.Stat(matches[i])
		aj, errj := os.Stat(matches[j])
		if erri != nil || errj != nil {
			return matches[i] < matches[j]
		}
		return ai.ModTime().After(aj.ModTime())
	})
	return matches[0]
}

// hasMermaidBlocks reports whether the text has a Mermaid block of code.
func hasMermaidBlocks(text richtext.Text) bool {
	for _, p := range text.Paragraphs() {
		if isMermaidPara(p) {
			return true
		}
	}
	return false
}

// isMermaidPara tells a paragraph of a Mermaid block of code.
func isMermaidPara(p richtext.Paragraph) bool {
	if !p.Style.CodeBlock {
		return false
	}
	fields := strings.Fields(p.Style.Language)
	return len(fields) > 0 && strings.EqualFold(fields[0], "mermaid")
}

func paraText(p richtext.Paragraph) string {
	var b strings.Builder
	for _, r := range p.Runs {
		b.WriteString(r.Text)
	}
	return b.String()
}

// addMermaidPreviews puts a rendered picture above each Mermaid block, so
// the page shows the diagram and keeps its source below it.
func (a *app) addMermaidPreviews(text richtext.Text, docRel string) (richtext.Text, map[string]bool) {
	previews := map[string]bool{}
	if !strings.Contains(docRel, "/") {
		return text, previews
	}
	kb := strings.Split(docRel, "/")[0]
	assets := filepath.Join(a.vault.Root, kb, ".assets")
	docDir := filepath.Dir(filepath.Join(a.vault.Root, filepath.FromSlash(docRel)))
	cli := mermaidCLI()
	var paras []richtext.Paragraph
	for _, p := range text.Paragraphs() {
		paras = append(paras, p)
	}
	var out []richtext.Paragraph
	for i := 0; i < len(paras); i++ {
		if !isMermaidPara(paras[i]) {
			out = append(out, paras[i])
			continue
		}
		j := i
		var lines []string
		for j < len(paras) && isMermaidPara(paras[j]) {
			lines = append(lines, paraText(paras[j]))
			j++
		}
		if img, url, ok := renderMermaid(cli, assets, docDir, strings.Join(lines, "\n")); ok {
			previews[url] = true
			out = append(out, richtext.Paragraph{Style: richtext.ParagraphStyle{Align: ui.Center}, Runs: []richtext.Run{{
				Text: string(richtext.ObjectReplacement),
				Attributes: richtext.Attributes{Attachment: &richtext.Attachment{
					Image: img,
					URL:   url,
					Name:  "Mermaid 图表",
				}},
			}}})
		}
		out = append(out, paras[i:j]...)
		i = j - 1
	}
	return richtext.TextOf(out...), previews
}

// renderMermaid renders src to a PNG cached in assets, and returns it with
// the URL a document refers to it by.
func renderMermaid(cli, assets, docDir, src string) (*ui.Bitmap, string, bool) {
	sum := sha256.Sum256([]byte(src))
	name := "mermaid-" + hex.EncodeToString(sum[:8]) + ".png"
	path := filepath.Join(assets, name)
	if _, err := os.Stat(path); err != nil {
		if cli == "" {
			return nil, "", false
		}
		if err := os.MkdirAll(assets, 0o755); err != nil {
			return nil, "", false
		}
		dir, err := os.MkdirTemp("", "swift-mermaid-")
		if err != nil {
			return nil, "", false
		}
		defer os.RemoveAll(dir)
		in := filepath.Join(dir, "figure.mmd")
		out := filepath.Join(dir, "figure.png")
		if err := os.WriteFile(in, []byte(src), 0o644); err != nil {
			return nil, "", false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, cli, "-i", in, "-o", out, "-b", "white", "--size", "1600", "-q")
		if err := cmd.Run(); err != nil {
			return nil, "", false
		}
		rendered, err := os.ReadFile(out)
		if err != nil {
			return nil, "", false
		}
		rendered = scalePNG(rendered, 900, 420)
		if err := os.WriteFile(path, rendered, 0o644); err != nil {
			return nil, "", false
		}
	}
	png, err := os.ReadFile(path)
	if err != nil {
		return nil, "", false
	}
	img, err := ui.DecodeBitmap(png)
	if err != nil {
		return nil, "", false
	}
	rel, err := filepath.Rel(docDir, path)
	if err != nil {
		return nil, "", false
	}
	return img, filepath.ToSlash(rel), true
}

var mermaidPreviewPattern = regexp.MustCompile(`!\[[^\]]*\]\(([^)]*)\)`)

// stripMermaidPreviews removes the rendered pictures from the Markdown
// that is written, keeping the Mermaid blocks below them.
func stripMermaidPreviews(md string, previews map[string]bool) string {
	md = mermaidPreviewPattern.ReplaceAllStringFunc(md, func(m string) string {
		sub := mermaidPreviewPattern.FindStringSubmatch(m)
		if sub == nil {
			return m
		}
		if previews[sub[1]] {
			return ""
		}
		return m
	})
	md = regexp.MustCompile(`\n{3,}`).ReplaceAllString(md, "\n\n")
	return strings.TrimSpace(md)
}

// scalePNG keeps a picture within maxW by maxH, so a tall diagram does not
// fill the page.
func scalePNG(b []byte, maxW, maxH int) []byte {
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return b
	}
	bb := img.Bounds()
	w, h := bb.Dx(), bb.Dy()
	if w <= 0 || h <= 0 {
		return b
	}
	scale := math.Min(float64(maxW)/float64(w), float64(maxH)/float64(h))
	if scale >= 1 {
		return b
	}
	nw, nh := int(float64(w)*scale), int(float64(h)*scale)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy := bb.Min.Y + y*h/nh
		for x := 0; x < nw; x++ {
			sx := bb.Min.X + x*w/nw
			dst.Set(x, y, img.At(sx, sy))
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return b
	}
	return out.Bytes()
}
