package app

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"swift/internal/lakebook"
	"swift/internal/markdown"
	"swift/internal/vault"
)

const (
	lakebookImageTimeout   = 15 * time.Second
	lakebookMaxRedirects   = 5
	lakebookMaxImageBytes  = 20 << 20
	lakebookMaxImportBytes = 200 << 20
)

type lakebookImportResult struct {
	KB           string
	Docs         int
	Boards       int
	Images       int
	FailedImages int
}

func (r lakebookImportResult) summary() string {
	text := fmt.Sprintf("导入完成：%d 篇文档", r.Docs)
	if r.Boards > 0 {
		text += fmt.Sprintf("，%d 个画板仅保留静态预览", r.Boards)
	}
	text += fmt.Sprintf("，%d 张图片", r.Images)
	if r.FailedImages > 0 {
		text += fmt.Sprintf("，%d 张未下载", r.FailedImages)
	}
	return text
}

type lakebookImageData struct {
	data []byte
	name string
}

type lakebookImportPlan struct {
	book         *lakebook.Book
	images       map[string]lakebookImageData
	failedImages int
}

type lakebookImportJob struct {
	vault *vault.Vault
	done  chan struct{}
	plan  *lakebookImportPlan
	err   error
}

func (a *app) askImportLakebook() {
	if a.vault == nil {
		return
	}
	a.flushTitle()
	a.flushDoc()
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:      a.win,
		Title:       "导入语雀 Lakebook",
		ButtonLabel: "选择文件",
		Filters: []mygo.FileFilter{{
			Name:       "语雀 Lakebook",
			Extensions: []string{"lakebook"},
		}},
	})
	if err != nil {
		a.setError(err)
		return
	}
	if len(paths) == 0 {
		return
	}

	if !a.startLakebookImport(paths[0]) {
		a.setError(errors.New("已有 Lakebook 正在导入"))
		return
	}
	a.update()
}

func (a *app) startLakebookImport(filename string) bool {
	if a.vault == nil || a.lakebookJob != nil {
		return false
	}
	job := &lakebookImportJob{
		vault: a.vault,
		done:  make(chan struct{}),
	}
	a.lakebookJob = job
	a.toast = ""
	a.toastUntil = time.Time{}
	go func() {
		job.plan, job.err = prepareLakebook(filename)
		close(job.done)
	}()
	return true
}

func (a *app) lakebookImportRunning() bool {
	return a.lakebookJob != nil
}

func (a *app) finishLakebookImport() {
	job := a.lakebookJob
	if job == nil {
		return
	}
	select {
	case <-job.done:
	default:
		return
	}
	a.lakebookJob = nil
	if job.err != nil {
		a.setError(job.err)
		return
	}
	if a.vault != job.vault {
		a.setError(errors.New("工作区已变更，Lakebook 导入已取消"))
		return
	}
	result, err := a.commitLakebookImport(job.plan)
	if err != nil {
		a.setError(err)
		return
	}
	a.refreshTree()
	if n := a.findNode(result.KB); n != nil {
		a.openKB(n)
	}
	a.flashToast(result.summary())
}

func (a *app) importLakebook(filename string) (lakebookImportResult, error) {
	plan, err := prepareLakebook(filename)
	if err != nil {
		return lakebookImportResult{}, err
	}
	return a.commitLakebookImport(plan)
}

func prepareLakebook(filename string) (*lakebookImportPlan, error) {
	book, err := lakebook.Parse(filename)
	if err != nil {
		return nil, err
	}
	preparer := &lakebookImagePreparer{
		client: newLakebookHTTPClient(),
		images: map[string]lakebookImageData{},
		failed: map[string]bool{},
	}
	preparer.collectNodes(book.Nodes)
	return &lakebookImportPlan{
		book:         book,
		images:       preparer.images,
		failedImages: preparer.failedCount,
	}, nil
}

func (a *app) commitLakebookImport(plan *lakebookImportPlan) (lakebookImportResult, error) {
	kb, err := a.vault.CreateKnowledgeBase(plan.book.Name)
	if err != nil {
		return lakebookImportResult{}, err
	}

	result := lakebookImportResult{
		KB:           kb,
		Boards:       plan.book.BoardCount,
		FailedImages: plan.failedImages,
	}
	images := &lakebookImages{
		vault:  a.vault,
		saved:  map[string]string{},
		failed: map[string]bool{},
		result: &result,
		plan:   plan,
	}
	if err := images.writeNodes(kb, plan.book.Nodes); err != nil {
		return result, err
	}
	return result, nil
}

type lakebookImagePreparer struct {
	client      *http.Client
	images      map[string]lakebookImageData
	failed      map[string]bool
	total       int64
	failedCount int
}

func (p *lakebookImagePreparer) collectNodes(nodes []*lakebook.Node) {
	for _, node := range nodes {
		for _, ref := range markdown.ImageRefs(node.Markdown) {
			p.fetch(ref)
		}
		p.collectNodes(node.Children)
	}
}

func (p *lakebookImagePreparer) fetch(raw string) {
	key, target, err := normalizeLakebookImageURL(raw)
	if err != nil {
		return
	}
	if _, ok := p.images[key]; ok || p.failed[key] {
		return
	}
	data, name, err := p.download(target)
	if err != nil {
		p.failed[key] = true
		p.failedCount++
		return
	}
	p.images[key] = lakebookImageData{data: data, name: name}
}

func (p *lakebookImagePreparer) download(target *url.URL) ([]byte, string, error) {
	request, err := http.NewRequest(http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, "", err
	}
	response, err := p.client.Do(request)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", errors.New("image request failed")
	}

	contentType := strings.ToLower(strings.TrimSpace(response.Header.Get("Content-Type")))
	extension := knownImageExtension(target.Path)
	if mediaType, _, err := mime.ParseMediaType(contentType); err == nil {
		contentType = strings.ToLower(mediaType)
	}
	if contentType != "" {
		if !strings.HasPrefix(contentType, "image/") {
			return nil, "", errors.New("response is not an image")
		}
	} else if extension == "" {
		return nil, "", errors.New("image type is unknown")
	}

	remaining := int64(lakebookMaxImportBytes) - p.total
	if remaining <= 0 {
		return nil, "", errors.New("image import limit reached")
	}
	if response.ContentLength > lakebookMaxImageBytes || response.ContentLength > remaining {
		return nil, "", errors.New("image is too large")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, lakebookMaxImageBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > lakebookMaxImageBytes {
		return nil, "", errors.New("image is too large")
	}
	if int64(len(data)) > remaining {
		return nil, "", errors.New("image import limit reached")
	}
	p.total += int64(len(data))
	return data, lakebookImageName(target, extension, contentType), nil
}

type lakebookImages struct {
	vault  *vault.Vault
	saved  map[string]string
	failed map[string]bool
	result *lakebookImportResult
	plan   *lakebookImportPlan
}

func (i *lakebookImages) writeNodes(parent string, nodes []*lakebook.Node) error {
	for _, node := range nodes {
		switch node.Kind {
		case lakebook.KindTitle:
			rel, err := i.vault.CreateFolder(parent, node.Title)
			if err != nil {
				return err
			}
			if err := i.writeNodes(rel, node.Children); err != nil {
				return err
			}
		case lakebook.KindDoc, lakebook.KindBoard:
			docRel, err := i.vault.CreateDoc(parent, node.Title)
			if err != nil {
				return err
			}
			if node.Kind == lakebook.KindDoc {
				i.result.Docs++
			}
			body := markdown.RewriteImageRefs(node.Markdown, func(ref string) (string, bool) {
				return i.rewrite(docRel, ref)
			})
			if _, err := i.vault.WriteDoc(docRel, body); err != nil {
				return err
			}

			if len(node.Children) == 0 {
				continue
			}
			childParent, err := i.vault.EnsureDocDir(docRel)
			if err != nil {
				return err
			}
			if err := i.writeNodes(childParent, node.Children); err != nil {
				return err
			}
		}
	}
	return nil
}

func (i *lakebookImages) rewrite(docRel, raw string) (string, bool) {
	key, _, err := normalizeLakebookImageURL(raw)
	if err != nil {
		return "", false
	}
	if asset, ok := i.saved[key]; ok {
		ref, ok := i.relative(docRel, asset)
		return ref, ok
	}
	if i.failed[key] {
		return "", false
	}

	image, ok := i.plan.images[key]
	if !ok {
		return "", false
	}
	moved, err := i.vault.ImportAssetBytes(docRel, image.name, image.data)
	if err != nil {
		i.failed[key] = true
		i.result.FailedImages++
		return "", false
	}
	asset, ok := i.vault.ResolveAsset(docRel, moved)
	if !ok {
		i.failed[key] = true
		i.result.FailedImages++
		return "", false
	}
	i.saved[key] = asset
	i.result.Images++
	ref, ok := i.relative(docRel, asset)
	return ref, ok
}

func (i *lakebookImages) relative(docRel, asset string) (string, bool) {
	docAbs := filepath.Join(i.vault.Root, filepath.FromSlash(docRel))
	rel, err := filepath.Rel(filepath.Dir(docAbs), asset)
	if err != nil {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func normalizeLakebookImageURL(raw string) (string, *url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", nil, errors.New("unsupported image URL")
	}
	parsed.Fragment = ""
	return parsed.String(), parsed, nil
}

func lakebookImageName(target *url.URL, extension, contentType string) string {
	name := path.Base(target.Path)
	if unescaped, err := url.PathUnescape(name); err == nil {
		name = unescaped
	}
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "-"))
	if name == "" || name == "." || name == "/" {
		name = ""
	}

	existing := strings.ToLower(path.Ext(name))
	if knownImageExtension(existing) == "" {
		if existing != "" {
			name = strings.TrimSuffix(name, path.Ext(name))
		}
		if name == "" {
			name = "image"
		}
		if extension == "" {
			extension = extensionForContentType(contentType)
		}
		name += extension
	}
	return name
}

func extensionForContentType(contentType string) string {
	switch contentType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/svg+xml":
		return ".svg"
	case "image/bmp":
		return ".bmp"
	default:
		return ".img"
	}
}

func knownImageExtension(value string) string {
	switch strings.ToLower(value) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".bmp":
		return strings.ToLower(value)
	default:
		return ""
	}
}

func newLakebookHTTPClient() *http.Client {
	return &http.Client{
		Timeout: lakebookImageTimeout,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) > lakebookMaxRedirects {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}
