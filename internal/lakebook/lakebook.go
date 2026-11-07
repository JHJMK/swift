package lakebook

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	maxArchiveSize  = 32 << 20
	maxExpandedSize = 256 << 20
	maxEntries      = 2000
	maxTOCNodes     = 2000
	maxTreeDepth    = 4
)

type Kind int

const (
	KindDoc Kind = iota
	KindTitle
	KindBoard
)

type Book struct {
	Name       string
	Nodes      []*Node
	BoardCount int
}

type Node struct {
	Kind     Kind
	Title    string
	Markdown string
	Children []*Node
}

type archiveFiles struct {
	root  string
	files map[string][]byte
}

type metaDocument struct {
	ID    json.RawMessage `json:"id"`
	DocID json.RawMessage `json:"doc_id"`
	Slug  string          `json:"slug"`
	Title string          `json:"title"`
	URL   string          `json:"url"`
}

type metaPayload struct {
	Book struct {
		TOCYML string `json:"tocYml"`
	} `json:"book"`
	Docs []metaDocument `json:"docs"`
}

type yamlString string

func (s *yamlString) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("expected scalar")
	}
	*s = yamlString(node.Value)
	return nil
}

type tocNode struct {
	UUID       yamlString `yaml:"uuid"`
	ParentUUID yamlString `yaml:"parent_uuid"`
	PrevUUID   yamlString `yaml:"prev_uuid"`
	ChildUUID  yamlString `yaml:"child_uuid"`
	Title      yamlString `yaml:"title"`
	URL        yamlString `yaml:"url"`
	Type       yamlString `yaml:"type"`
	DocID      yamlString `yaml:"doc_id"`
}

type treeNode struct {
	value    *Node
	uuid     string
	parent   string
	prev     string
	child    string
	docID    string
	url      string
	index    int
	children []*treeNode
}

func Parse(filename string) (*Book, error) {
	archive, err := readArchive(filename)
	if err != nil {
		return nil, err
	}

	meta, err := readMeta(archive)
	if err != nil {
		return nil, err
	}
	roots, nodes, err := buildTree(meta.Book.TOCYML)
	if err != nil {
		return nil, err
	}
	documents := indexDocuments(meta.Docs)

	book := &Book{Name: bookName(filename), Nodes: roots}
	for _, item := range nodes {
		switch item.value.Kind {
		case KindDoc:
			doc, err := findDocument(item, documents)
			if err != nil {
				return nil, err
			}
			markdown, boards, err := readDocumentBody(archive, doc)
			if err != nil {
				return nil, err
			}
			item.value.Markdown = markdown
			book.BoardCount += boards
		case KindBoard:
			book.BoardCount++
			item.value.Markdown = boardMarkdown(item.value.Title, item.url)
		}
	}
	return book, nil
}

func readArchive(filename string) (*archiveFiles, error) {
	info, err := os.Stat(filename)
	if err != nil {
		return nil, fmt.Errorf("读取 Lakebook 失败: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("读取 Lakebook 失败: 文件不是普通文件")
	}
	if info.Size() > maxArchiveSize {
		return nil, fmt.Errorf("读取 Lakebook 失败: 文件超过 %d MiB", maxArchiveSize>>20)
	}

	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("读取 Lakebook 失败: %w", err)
	}
	defer f.Close()

	files := make(map[string][]byte)
	tr := tar.NewReader(f)
	var expanded int64
	count := 0
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("解析 Lakebook 失败: %w", err)
		}
		count++
		if count > maxEntries {
			return nil, fmt.Errorf("解析 Lakebook 失败: 条目超过 %d 个", maxEntries)
		}
		name, err := safeArchivePath(header.Name)
		if err != nil {
			return nil, err
		}
		if _, exists := files[name]; exists {
			return nil, fmt.Errorf("解析 Lakebook 失败: 重复条目 %q", name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			files[name] = nil
			continue
		case tar.TypeReg, tar.TypeRegA:
		default:
			return nil, fmt.Errorf("解析 Lakebook 失败: 不支持的条目类型 %q", name)
		}
		if header.Size < 0 || header.Size > maxExpandedSize-expanded {
			return nil, fmt.Errorf("解析 Lakebook 失败: 展开后超过 %d MiB", maxExpandedSize>>20)
		}
		expanded += header.Size

		data, err := io.ReadAll(io.LimitReader(tr, header.Size+1))
		if err != nil {
			return nil, fmt.Errorf("解析 Lakebook 失败: %w", err)
		}
		if int64(len(data)) != header.Size {
			return nil, errors.New("解析 Lakebook 失败: 条目的声明大小与内容不一致")
		}
		files[name] = data
	}
	return &archiveFiles{files: files}, nil
}

func safeArchivePath(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, '\x00') || path.IsAbs(name) {
		return "", fmt.Errorf("解析 Lakebook 失败: 不安全路径 %q", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", fmt.Errorf("解析 Lakebook 失败: 不安全路径 %q", name)
		}
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("解析 Lakebook 失败: 不安全路径 %q", name)
	}
	return clean, nil
}

func readMeta(archive *archiveFiles) (*metaPayload, error) {
	var metaPath string
	for name := range archive.files {
		if path.Base(name) != "$meta.json" {
			continue
		}
		if metaPath != "" {
			return nil, errors.New("解析 Lakebook 失败: 存在多个 $meta.json")
		}
		metaPath = name
	}
	if metaPath == "" {
		return nil, errors.New("解析 Lakebook 失败: 缺少 $meta.json")
	}
	archive.root = path.Dir(metaPath)

	var outer struct {
		Meta json.RawMessage `json:"meta"`
	}
	if err := json.Unmarshal(archive.files[metaPath], &outer); err != nil {
		return nil, fmt.Errorf("解析 $meta.json 失败: %w", err)
	}
	raw := bytes.TrimSpace(outer.Meta)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, errors.New("解析 $meta.json 失败: 缺少 meta")
	}
	if raw[0] == '"' {
		var encoded string
		if err := json.Unmarshal(raw, &encoded); err != nil {
			return nil, fmt.Errorf("解析 $meta.json 失败: %w", err)
		}
		raw = []byte(encoded)
	}

	var meta metaPayload
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, fmt.Errorf("解析 meta 失败: %w", err)
	}
	if strings.TrimSpace(meta.Book.TOCYML) == "" {
		return nil, errors.New("解析 meta 失败: 缺少 tocYml")
	}
	return &meta, nil
}

func buildTree(rawTOC string) ([]*Node, []*treeNode, error) {
	var entries []tocNode
	if err := yaml.Unmarshal([]byte(rawTOC), &entries); err != nil {
		return nil, nil, fmt.Errorf("解析 tocYml 失败: %w", err)
	}
	if len(entries) > maxTOCNodes {
		return nil, nil, fmt.Errorf("解析 tocYml 失败: 节点超过 %d 个", maxTOCNodes)
	}

	allIDs := make(map[string]struct{})
	items := make([]*treeNode, 0, len(entries))
	byID := make(map[string]*treeNode)
	for index, entry := range entries {
		kind, include, err := parseKind(string(entry.Type))
		if err != nil {
			return nil, nil, err
		}
		uuid := strings.TrimSpace(string(entry.UUID))
		if uuid != "" {
			if _, exists := allIDs[uuid]; exists {
				return nil, nil, fmt.Errorf("解析 tocYml 失败: UUID %q 重复", uuid)
			}
			allIDs[uuid] = struct{}{}
		}
		if !include {
			continue
		}
		if uuid == "" {
			return nil, nil, errors.New("解析 tocYml 失败: 节点缺少 uuid")
		}
		title := strings.TrimSpace(string(entry.Title))
		if title == "" {
			title = "未命名"
		}
		item := &treeNode{
			value: &Node{
				Kind:  kind,
				Title: title,
			},
			uuid:   uuid,
			parent: strings.TrimSpace(string(entry.ParentUUID)),
			prev:   strings.TrimSpace(string(entry.PrevUUID)),
			child:  strings.TrimSpace(string(entry.ChildUUID)),
			docID:  normalizeScalar([]byte(entry.DocID)),
			url:    strings.TrimSpace(string(entry.URL)),
			index:  index,
		}
		items = append(items, item)
		byID[uuid] = item
	}

	for _, item := range items {
		if item.parent != "" {
			if _, exists := byID[item.parent]; !exists {
				return nil, nil, fmt.Errorf("解析 tocYml 失败: 节点 %q 的父节点不存在", item.uuid)
			}
		}
		if item.prev != "" {
			if _, exists := byID[item.prev]; !exists {
				return nil, nil, fmt.Errorf("解析 tocYml 失败: 节点 %q 的 prev_uuid 无效", item.uuid)
			}
		}
		if item.child != "" {
			child, exists := byID[item.child]
			if !exists || child.parent != item.uuid {
				return nil, nil, fmt.Errorf("解析 tocYml 失败: 节点 %q 的 child_uuid 无效", item.uuid)
			}
		}
	}

	groups := map[string][]*treeNode{"": {}}
	for _, item := range items {
		groups[item.parent] = append(groups[item.parent], item)
	}
	for parent, group := range groups {
		firstChild := ""
		if parent != "" {
			firstChild = byID[parent].child
		}
		ordered, err := orderSiblings(group, firstChild)
		if err != nil {
			return nil, nil, err
		}
		for _, item := range ordered {
			if parent == "" {
				continue
			}
			parentItem := byID[parent]
			parentItem.children = append(parentItem.children, item)
		}
	}

	roots, err := orderSiblings(groups[""], "")
	if err != nil {
		return nil, nil, err
	}
	visited := make(map[string]bool)
	var build func([]*treeNode, int) error
	build = func(group []*treeNode, depth int) error {
		if len(group) == 0 {
			return nil
		}
		if depth > maxTreeDepth {
			return fmt.Errorf("导入 Lakebook 失败: 知识库层级超过 %d 层", maxTreeDepth)
		}
		for _, item := range group {
			if visited[item.uuid] {
				return fmt.Errorf("解析 tocYml 失败: 节点 %q 形成循环", item.uuid)
			}
			visited[item.uuid] = true
			if err := build(item.children, depth+1); err != nil {
				return err
			}
			for _, child := range item.children {
				item.value.Children = append(item.value.Children, child.value)
			}
		}
		return nil
	}
	if err := build(roots, 1); err != nil {
		return nil, nil, err
	}
	if len(visited) != len(items) {
		return nil, nil, errors.New("解析 tocYml 失败: 节点树包含父子循环")
	}

	bookNodes := make([]*Node, 0, len(roots))
	for _, item := range roots {
		bookNodes = append(bookNodes, item.value)
	}
	return bookNodes, items, nil
}

func parseKind(value string) (Kind, bool, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "META":
		return KindDoc, false, nil
	case "DOC":
		return KindDoc, true, nil
	case "TITLE":
		return KindTitle, true, nil
	case "BOARD":
		return KindBoard, true, nil
	default:
		return KindDoc, false, fmt.Errorf("解析 tocYml 失败: 不支持的节点类型 %q", value)
	}
}

func orderSiblings(group []*treeNode, firstUUID string) ([]*treeNode, error) {
	if len(group) < 2 {
		return group, nil
	}
	members := make(map[*treeNode]bool, len(group))
	for _, item := range group {
		members[item] = true
	}
	next := make(map[*treeNode]*treeNode, len(group))
	indegree := make(map[*treeNode]int, len(group))
	addEdge := func(from, to *treeNode) error {
		if from == to {
			return errors.New("解析 tocYml 失败: 同级节点链接形成循环")
		}
		if existing, ok := next[from]; ok {
			if existing != to {
				return errors.New("解析 tocYml 失败: 同级节点存在冲突的排序链接")
			}
			return nil
		}
		next[from] = to
		indegree[to]++
		return nil
	}
	byID := make(map[string]*treeNode, len(group))
	for _, item := range group {
		byID[item.uuid] = item
	}
	if firstUUID != "" {
		first, ok := byID[firstUUID]
		if !ok {
			return nil, fmt.Errorf("解析 tocYml 失败: 首个子节点 %q 不在同一层级", firstUUID)
		}
		indegree[first]++
	}
	for _, item := range group {
		if item.prev != "" {
			prev, ok := byID[item.prev]
			if ok && members[prev] {
				if err := addEdge(prev, item); err != nil {
					return nil, err
				}
			}
		}
	}

	ordered := make([]*treeNode, 0, len(group))
	visited := make(map[*treeNode]bool, len(group))
	var start *treeNode
	if firstUUID != "" {
		start = byID[firstUUID]
	}
	if start == nil {
		for _, item := range group {
			if indegree[item] == 0 {
				start = item
				break
			}
		}
	}
	if start != nil {
		for current := start; current != nil; current = next[current] {
			if visited[current] {
				return nil, errors.New("解析 tocYml 失败: 同级节点链接形成循环")
			}
			visited[current] = true
			ordered = append(ordered, current)
		}
	}
	for _, item := range group {
		if visited[item] {
			continue
		}
		if indegree[item] != 0 {
			continue
		}
		for current := item; current != nil; current = next[current] {
			if visited[current] {
				return nil, errors.New("解析 tocYml 失败: 同级节点链接形成循环")
			}
			visited[current] = true
			ordered = append(ordered, current)
		}
	}
	if len(visited) != len(group) {
		return nil, errors.New("解析 tocYml 失败: 同级节点链接形成循环")
	}
	return ordered, nil
}

func indexDocuments(docs []metaDocument) map[string]metaDocument {
	indexed := make(map[string]metaDocument, len(docs))
	for _, doc := range docs {
		id := documentID(doc)
		if id != "" {
			indexed[id] = doc
		}
		if doc.Slug != "" {
			if _, exists := indexed[doc.Slug]; !exists {
				indexed[doc.Slug] = doc
			}
		}
	}
	return indexed
}

func findDocument(item *treeNode, documents map[string]metaDocument) (metaDocument, error) {
	if item.docID != "" {
		if doc, ok := documents[item.docID]; ok {
			return doc, nil
		}
	}
	slug := urlSlug(item.url)
	if slug != "" {
		if doc, ok := documents[slug]; ok {
			return doc, nil
		}
	}
	return metaDocument{}, fmt.Errorf("解析 Lakebook 失败: 文档节点 %q 找不到对应内容", item.uuid)
}

func readDocumentBody(archive *archiveFiles, doc metaDocument) (string, int, error) {
	slug := strings.TrimSpace(doc.Slug)
	if slug == "" || path.Base(slug) != slug || strings.ContainsAny(slug, `/\`) {
		return "", 0, errors.New("解析 Lakebook 失败: 文档 slug 无效")
	}
	name := path.Join(archive.root, slug+".json")
	data, ok := archive.files[name]
	if !ok {
		return "", 0, fmt.Errorf("解析 Lakebook 失败: 文档文件 %q 不存在", slug+".json")
	}
	var outer struct {
		Doc json.RawMessage `json:"doc"`
	}
	if err := json.Unmarshal(data, &outer); err != nil {
		return "", 0, fmt.Errorf("解析文档 %q 失败: %w", slug, err)
	}
	if len(outer.Doc) == 0 || bytes.Equal(bytes.TrimSpace(outer.Doc), []byte("null")) {
		return "", 0, fmt.Errorf("解析文档 %q 失败: 缺少 doc", slug)
	}
	var body struct {
		Body      string `json:"body"`
		BodyDraft string `json:"body_draft"`
	}
	if err := json.Unmarshal(outer.Doc, &body); err != nil {
		return "", 0, fmt.Errorf("解析文档 %q 失败: %w", slug, err)
	}
	source := body.Body
	if strings.TrimSpace(source) == "" {
		source = body.BodyDraft
	}
	markdown, boards, err := convertHTML(source)
	if err != nil {
		return "", 0, fmt.Errorf("转换文档 %q 失败: %w", slug, err)
	}
	return markdown, boards, nil
}

func boardMarkdown(title, sourceURL string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n\n", escapeMarkdown(title))
	if sourceURL != "" {
		fmt.Fprintf(&b, "[在语雀中打开画板](%s)\n\n", markdownDestination(sourceURL))
	}
	b.WriteString("> 此内容来自语雀画板，暂不支持交互编辑。\n")
	return b.String()
}

func documentID(doc metaDocument) string {
	if id := normalizeScalar(doc.ID); id != "" {
		return id
	}
	return normalizeScalar(doc.DocID)
}

func normalizeScalar(raw []byte) string {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return ""
	}
	if value[0] == '"' {
		var decoded string
		if json.Unmarshal(raw, &decoded) == nil {
			return strings.TrimSpace(decoded)
		}
	}
	if parsed, err := strconv.ParseFloat(value, 64); err == nil {
		return strconv.FormatFloat(parsed, 'f', -1, 64)
	}
	return strings.Trim(value, `"`)
}

func urlSlug(raw string) string {
	clean := strings.SplitN(raw, "#", 2)[0]
	clean = strings.SplitN(clean, "?", 2)[0]
	clean = strings.TrimRight(clean, "/")
	if clean == "" {
		return ""
	}
	return path.Base(clean)
}

func bookName(filename string) string {
	name := filepath.Base(filename)
	if strings.EqualFold(filepath.Ext(name), ".lakebook") {
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	name = strings.TrimSpace(name)
	if name == "" || name == "." {
		return "语雀知识库"
	}
	return name
}
