package app

import (
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
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
	lakebookImageWorkers   = 4
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
	vault   *vault.Vault
	file    string
	started time.Time
	done    chan struct{}
	plan    *lakebookImportPlan
	err     error
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
		vault:   a.vault,
		file:    filename,
		started: time.Now(),
		done:    make(chan struct{}),
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
	elapsed := formatElapsed(job.started)
	if job.err != nil {
		log.Printf("swift: lakebook import: failed file=%q after %s: %v", filepath.Base(job.file), elapsed, job.err)
		a.setError(job.err)
		return
	}
	if a.vault != job.vault {
		log.Printf("swift: lakebook import: cancelled file=%q after %s (workspace changed)", filepath.Base(job.file), elapsed)
		a.setError(errors.New("工作区已变更，Lakebook 导入已取消"))
		return
	}
	result, err := a.commitLakebookImport(job.plan)
	if err != nil {
		log.Printf("swift: lakebook import: failed file=%q after %s: %v", filepath.Base(job.file), elapsed, err)
		a.setError(err)
		return
	}
	log.Printf("swift: lakebook import: finished file=%q docs=%d images=%d failed=%d total=%s",
		filepath.Base(job.file), result.Docs, result.Images, result.FailedImages, elapsed)
	a.refreshTree()
	if n := a.findNode(result.KB); n != nil {
		a.openKB(n)
	}
	a.flashToast(result.summary())
}

func (a *app) importLakebook(filename string) (lakebookImportResult, error) {
	started := time.Now()
	result, err := a.commitAfterPrepare(filename)
	if err != nil {
		return result, err
	}
	log.Printf("swift: lakebook import: finished file=%q docs=%d images=%d failed=%d total=%s",
		filepath.Base(filename), result.Docs, result.Images, result.FailedImages, formatElapsed(started))
	return result, nil
}

// commitAfterPrepare parses and downloads the archive, then writes it to a
// new knowledge base; importLakebook names the whole operation in its log.
func (a *app) commitAfterPrepare(filename string) (lakebookImportResult, error) {
	plan, err := prepareLakebook(filename)
	if err != nil {
		return lakebookImportResult{}, err
	}
	return a.commitLakebookImport(plan)
}

func prepareLakebook(filename string) (*lakebookImportPlan, error) {
	started := time.Now()
	size := int64(0)
	if info, err := os.Stat(filename); err == nil {
		size = info.Size()
	}
	log.Printf("swift: lakebook import: start file=%q size=%d", filepath.Base(filename), size)
	book, err := lakebook.Parse(filename)
	if err != nil {
		log.Printf("swift: lakebook import: parse failed after %s: %v", formatElapsed(started), err)
		return nil, err
	}
	docs := lakebookDocCount(book.Nodes)
	log.Printf("swift: lakebook import: parsed docs=%d boards=%d after %s", docs, book.BoardCount, formatElapsed(started))
	preparer := &lakebookImagePreparer{
		client: newLakebookHTTPClient(),
		images: map[string]lakebookImageData{},
		failed: map[string]bool{},
	}
	preparer.collectNodes(book.Nodes)
	log.Printf("swift: lakebook import: images refs=%d unique=%d downloaded=%d failed=%d bytes=%d total=%s",
		preparer.references, len(preparer.images)+preparer.failedCount, len(preparer.images),
		preparer.failedCount, preparer.total, formatElapsed(started))
	log.Printf("swift: lakebook import: prepared docs=%d boards=%d images=%d failed=%d total=%s",
		docs, book.BoardCount, len(preparer.images), preparer.failedCount, formatElapsed(started))
	return &lakebookImportPlan{
		book:         book,
		images:       preparer.images,
		failedImages: preparer.failedCount,
	}, nil
}

// lakebookDocCount counts the documents of a parsed book, for logging.
func lakebookDocCount(nodes []*lakebook.Node) int {
	count := 0
	var walk func([]*lakebook.Node)
	walk = func(nodes []*lakebook.Node) {
		for _, node := range nodes {
			if node.Kind == lakebook.KindDoc {
				count++
			}
			walk(node.Children)
		}
	}
	walk(nodes)
	return count
}

func (a *app) commitLakebookImport(plan *lakebookImportPlan) (lakebookImportResult, error) {
	started := time.Now()
	log.Printf("swift: lakebook import: write start name=%q docs=%d boards=%d images=%d failed=%d",
		plan.book.Name, lakebookDocCount(plan.book.Nodes), plan.book.BoardCount, len(plan.images), plan.failedImages)
	kb, err := a.createKnowledgeBase(plan.book.Name)
	if err != nil {
		log.Printf("swift: lakebook import: write failed after %s: %v", formatElapsed(started), err)
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
		log.Printf("swift: lakebook import: write failed kb=%q after %s: %v", kb, formatElapsed(started), err)
		return result, err
	}
	log.Printf("swift: lakebook import: write done kb=%q docs=%d images=%d failed=%d after %s",
		kb, result.Docs, result.Images, result.FailedImages, formatElapsed(started))
	return result, nil
}

type lakebookImagePreparer struct {
	client      *http.Client
	images      map[string]lakebookImageData
	failed      map[string]bool
	mu          sync.Mutex
	total       int64
	references  int
	failedCount int
}

type lakebookImageJob struct {
	key    string
	target *url.URL
}

// collectNodes gathers the unique pictures of the book and downloads them
// with a few workers: a book with many pictures was otherwise as slow as
// their times added up, while a bigger text-only book stayed quick.
func (p *lakebookImagePreparer) collectNodes(nodes []*lakebook.Node) {
	var jobs []lakebookImageJob
	seen := map[string]bool{}
	var walk func([]*lakebook.Node)
	walk = func(nodes []*lakebook.Node) {
		for _, node := range nodes {
			for _, ref := range markdown.ImageRefs(node.Markdown) {
				p.references++
				key, target, err := normalizeLakebookImageURL(ref)
				if err != nil || seen[key] {
					continue
				}
				seen[key] = true
				jobs = append(jobs, lakebookImageJob{key: key, target: target})
			}
			walk(node.Children)
		}
	}
	walk(nodes)
	if len(jobs) == 0 {
		return
	}

	workers := lakebookImageWorkers
	if len(jobs) < workers {
		workers = len(jobs)
	}
	queue := make(chan lakebookImageJob)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range queue {
				p.fetch(job)
			}
		}()
	}
	for _, job := range jobs {
		queue <- job
	}
	close(queue)
	wg.Wait()
}

func (p *lakebookImagePreparer) fetch(job lakebookImageJob) {
	started := time.Now()
	data, name, err := p.download(job.target)
	elapsed := time.Since(started).Round(time.Millisecond)
	if err != nil {
		p.mu.Lock()
		p.failed[job.key] = true
		p.failedCount++
		p.mu.Unlock()
		log.Printf("swift: lakebook import: image failed url=%q after %s: %s",
			lakebookLogURL(job.target), elapsed, lakebookLogError(job.target, err))
		return
	}
	p.mu.Lock()
	p.images[job.key] = lakebookImageData{data: data, name: name}
	p.mu.Unlock()
	if elapsed >= 2*time.Second {
		log.Printf("swift: lakebook import: image slow url=%q bytes=%d after %s",
			lakebookLogURL(job.target), len(data), elapsed)
	}
}

// formatElapsed keeps log durations short and readable.
func formatElapsed(start time.Time) string {
	return time.Since(start).Round(time.Millisecond).String()
}

// lakebookLogURL is the image URL without the query and fragment, which
// can carry signed tokens.
func lakebookLogURL(target *url.URL) string {
	clone := *target
	clone.User = nil
	clone.RawQuery = ""
	clone.Fragment = ""
	return clone.String()
}

// lakebookLogError removes the signed query of the image URL when a
// network error quotes it.
func lakebookLogError(target *url.URL, err error) string {
	text := err.Error()
	if full := target.String(); full != "" {
		text = strings.ReplaceAll(text, full, lakebookLogURL(target))
	}
	return text
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
		return nil, "", fmt.Errorf("image request failed: %s", response.Status)
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

	p.mu.Lock()
	remaining := int64(lakebookMaxImportBytes) - p.total
	p.mu.Unlock()
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
	p.mu.Lock()
	if p.total+int64(len(data)) > lakebookMaxImportBytes {
		p.mu.Unlock()
		return nil, "", errors.New("image import limit reached")
	}
	p.total += int64(len(data))
	p.mu.Unlock()
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
