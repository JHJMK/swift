# 语雀 Lakebook 导入设计

## 摘要

为语燕增加语雀 Lakebook 导入。Lakebook 是 tar 归档，里面有知识库目录、文档 JSON 和 Lake HTML 正文。导入后生成一个普通语燕知识库，正文转换为 Markdown，远程图片下载到该知识库的本地 `.assets` 目录。语雀画板不支持交互编辑，导入时保留为包含标题、文字、预览图和原链接的静态文档。

知识库侧栏和工作台使用统一的导入菜单：

- 复制导入知识库
- 引用外部知识库
- 导入语雀 Lakebook

文件菜单保持现状，不增加 Lakebook 入口。

## 目标

- 导入 Lakebook 的目录、文档和顺序。
- 保留常见正文结构：标题、段落、列表、代码块、表格、链接、内联格式、分割线和图片。
- 将独立画板和文档内嵌画板降级为可阅读的静态内容，不静默跳过。
- 将远程图片下载到知识库本地附件目录。
- 单张图片下载失败时继续导入，并保留远程 URL。
- 在创建知识库前完成归档解析和格式校验，避免坏包留下半成品。
- 使用合成归档和本地 HTTP 测试服务完成自动化测试，不依赖真实语雀导出文件。

## 非目标

- 不导入 PDF。
- 不兼容语雀其他导出格式。
- 不支持 Lakebook 中的评论、历史版本、权限、协作状态等应用元数据。
- 不支持语雀画板的交互编辑，也不尝试转换画板绘图数据。
- 不提供导入进度条、暂停、重试或后台任务管理。
- 不修改 Vault 存储格式、附件 API 或 Markdown 渲染器。
- 不为导入失败的知识库增加事务回滚或自动删除机制。

## 逆向得到的归档结构

Lakebook 是普通未加密 POSIX tar：

```text
<book-id>/$meta.json
<book-id>/<doc-slug>.json
```

`$meta.json` 外层包含 `meta` 和 `meta_digest`，其中 `meta` 是再次编码的 JSON 字符串。`meta` 中的关键字段包括 `book`、`docs`、`config` 和 `version`。

`book.tocYml` 是 YAML 字符串，解析后是扁平节点数组。节点使用以下字段恢复树和顺序：

- `uuid`
- `parent_uuid`
- `prev_uuid`
- `child_uuid`
- `level`
- `title`
- `url`
- `visible`
- `type`

当前支持并理解的节点类型：

- `META`：跳过。
- `TITLE`：语燕分组。
- `DOC`：语燕文档。
- `BOARD`：语雀独立画板，降级为语燕文档。

画板类型必须由 TOC 的 `type` 明确标识，按大小写不敏感匹配 `BOARD`。其他类型不按名称猜测。

`meta.docs` 中的 `slug` 对应 tar 中的 `<doc-slug>.json`。文档外层包含 `doc` 和 `doc_digest`，正文位于 `doc.body`；当 `doc.body` 为空时使用 `doc.body_draft`，不使用 `doc.body_asl`。

树恢复算法：

1. 每个非 `META` 节点先按 `parent_uuid` 放入对应父节点的子集合；空 `parent_uuid` 是根节点。
2. 同级节点优先按 `prev_uuid` 和 `child_uuid` 恢复链路。
3. 链路缺失或不完整时，用 TOC 扁平数组中的原始顺序补齐尚未排序的节点。
4. 不存在的父节点、重复 UUID、父子循环和无法稳定排序的兄弟节点视为损坏归档。

## 用户交互

### 知识库侧栏加号

菜单内容：

```text
新建知识库
----------
导入
```

点击“导入”后关闭第一层菜单，并在同一锚点展开第二层：

```text
复制导入知识库
引用外部知识库
导入语雀 Lakebook
```

复制导入和引用外部知识库改用现有流程，不改变行为。

### 工作台导入卡

“导入知识库”卡片点击后直接显示与侧栏第二层相同的三个选项。副标题从“复制或引用文件夹”改为覆盖三个来源的简短说明。

### Lakebook 文件选择

使用现有 `mygo.Dialog.Open`：

- 标题：`导入语雀 Lakebook`
- 按钮：`选择文件`
- 过滤器：`语雀 Lakebook`，扩展名 `lakebook`
- 单选

取消文件选择时不报错、不创建知识库。

## 架构

### `internal/lakebook`

新增独立包，只处理 Lakebook 的读取和转换，不依赖 App 或 Vault。

主要接口：

```go
func Parse(path string) (*Book, error)

type Book struct {
    Name       string
    Nodes      []*Node
    BoardCount int
}

type NodeKind int

const (
    NodeDoc NodeKind = iota
    NodeTitle
    NodeBoard
)

type Node struct {
    Kind     NodeKind
    Title    string
    Markdown string
    Children []*Node
}
```

`Parse` 负责：

1. 流式读取 tar。
2. 校验路径、条目类型、大小和数量。
3. 读取并解析 `$meta.json`、`tocYml` 和文档 JSON。
4. 恢复节点树和原有顺序。
5. 将 Lake HTML 转成 Markdown。
6. 将明确的画板 TOC 节点和正文画板卡片降级为 Markdown，并统计画板数量。
7. 返回完整的内存树，不写磁盘、不下载图片。

HTML 转换可以放在同包的独立文件中，但保持内部函数，不提前设计公共转换 API。

### App 导入器

新增 `internal/app/importlakebook.go`，职责：

1. 打开文件选择器。
2. 调用 `lakebook.Parse`。
3. 创建知识库。
4. 递归创建分组和文档。
5. 下载并导入图片。
6. 刷新文件树、打开新知识库并显示结果。

复用现有 Vault API：

- `CreateKnowledgeBase`
- `CreateFolder`
- `CreateDoc`
- `EnsureDocDir`
- `WriteDoc`
- `ImportAssetBytes`

不改 Vault 实现。

### 菜单复用

提取一个无状态的导入菜单内容函数，供侧栏和工作台共用。导入菜单三种来源的顺序、图标和文字只定义一次。

侧栏需要两级菜单状态。所有现有 dropdown 保持互斥，同一时间最多打开一个菜单。

## 数据映射

知识库名称取 Lakebook 文件名，去掉 `.lakebook` 扩展名。文件名无效或为空时使用 `语雀知识库`。Vault 负责清理非法字符和处理重名。

节点映射：

- `TITLE` 使用 `CreateFolder`。
- `DOC` 使用 `CreateDoc`。
- `BOARD` 使用 `CreateDoc`，正文由标题、可提取文字、预览图和原语雀链接组成。
- 有子节点的 `DOC` 使用 `EnsureDocDir`，子节点创建在文档同名目录中。
- 有子节点的 `BOARD` 同样使用 `EnsureDocDir`，保持原始目录位置。
- 根节点直接创建在知识库下。
- 空标题使用 `未命名`。
- 节点顺序遵循 Lakebook TOC 中由 `parent_uuid`、`prev_uuid` 和 `child_uuid` 表达的原始顺序。
- Vault 自动处理同父级重名；后续写入和子节点创建必须使用 Vault 返回的实际相对路径，不能自行拼接原始标题。

独立画板生成的文档采用以下结构：

1. 使用画板标题作为文档标题。
2. 有可提取文字时保留文字段落。
3. 有预览图时输出 Markdown 图片，并参与后续图片下载。
4. 有原链接时保留为可点击链接。
5. 始终加入简短说明：`此内容来自语雀画板，暂不支持交互编辑。`
6. 没有文字、预览图和链接时仍创建占位文档，不静默跳过。

TOC 是导入内容的树结构来源。`meta.docs` 只用于定位文档 JSON。没有出现在 TOC `DOC` 节点中的归档文档不导入。

语燕知识库下最多显示 4 层。Lakebook 树超过该限制时，整个导入在创建知识库前失败，并返回包含实际层数和限制的明确错误。

## Markdown 转换

Lake HTML 使用 `golang.org/x/net/html` 解析并遍历。

支持的块：

- `p`：段落
- `h1` 到 `h6`：ATX 标题
- `ul`、`ol`、`li`：嵌套列表
- `pre > code[data-language]`：带语言标记的 fenced code block
- `hr.ne-hr`：分割线
- `img.ne-image`：Markdown 图片
- `table`、`thead`、`tbody`、`tr`、`th`、`td`：Markdown 表格
- `a.ne-link`：Markdown 链接
- 明确的画板卡片：Markdown 静态降级内容

支持的内联元素：

- `span.ne-text`
- `strong`
- `em`
- `code`
- `br`

文本中的 Markdown 控制字符需要按上下文转义。代码块内容不做 Markdown 转义，并根据原 `data-language` 输出语言标记。

未知 HTML 块不能静默丢失。转换器递归提取其中的文字和已知子节点，输出为普通段落或行内文本；无法识别的结构至少保留可读文本。

空段落和连续多余空行需要压缩，避免导入后文档出现大段空白。转换结果必须使用 LF 换行。

正文画板只做保守识别：元素具有 `ne-board` 或 `lake-board` 类，或带有明确取值为 `board` 的 `data-card-type`、`data-type`。识别到画板后：

- 从画板元素内提取标题和可见文字。
- 从嵌套图片或常见的 `preview`、`cover`、`thumbnail` 属性提取预览 URL。
- 从 `href` 或 `data-href` 提取原链接。
- 输出标题、文字、预览图、原链接和静态降级说明。
- 找不到任何内容时仍输出占位段落。

不根据普通链接文本、URL 片段或任意名称包含 “board” 来识别画板，避免把普通内容误判。

## 图片处理

Lakebook 不包含正文图片文件，图片以远程 HTTP/HTTPS URL 出现在 Lake HTML 中。

转换器在 Markdown 中保留远程图片 URL。App 创建文档后使用现有 `markdown.RewriteImageRefs` 重写图片：

1. 同一规范化 URL 在一次导入中只下载一次。
2. 下载成功后调用 `ImportAssetBytes(docRel, name, data)`。
3. 记录 Vault 返回的 `.assets` 相对路径。
4. 同一图片用于其他文档时，根据该文档位置换算相对路径，不重复保存。
5. 单张图片失败时 callback 返回未改写状态，Markdown 保留原远程 URL。

下载约束：

- 仅接受 HTTP 和 HTTPS。
- 最多跟随 5 次重定向。
- 单张图片请求超时 15 秒。
- 单张图片最多 20 MiB。
- 一次导入的图片总量最多 200 MiB。
- 仅接受 `2xx` 响应。
- `Content-Type` 必须为 `image/*`，或在没有 Content-Type 时 URL 路径带常见图片扩展名。

文件名优先取 URL 的基础文件名并清理查询参数；没有可用扩展名时根据 Content-Type 映射为 `.png`、`.jpg`、`.gif`、`.webp` 或 `.svg`。Vault 继续负责最终文件名的清理和时间戳前缀。

下载按唯一 URL 顺序执行。出现单张错误后继续下一张，不把 URL、正文或响应内容写入日志。

导入完成后通过现有 toast 显示：

```text
导入完成：N 篇文档，B 个画板仅保留静态预览，M 张图片，K 张未下载
```

没有画板时省略画板一段；没有失败图片时省略最后一段。画板可读取文字或预览时仍使用“仅保留静态预览”，因为交互绘图能力没有导入。

## 归档安全与错误处理

解析时只允许普通文件和目录条目。软链接、硬链接、设备文件和其他特殊条目直接拒绝。

路径必须满足：

- 不是绝对路径。
- 不包含 `..` 路径段。
- `path.Clean` 后仍位于归档根内。
- 不包含 NUL 或无法规范化的路径。

资源上限：

- 最多 2,000 个 tar 条目。
- 最多 2,000 篇 TOC 文档。
- 单个归档文件最多 32 MiB。
- 归档解开后的总字节数最多 256 MiB。
- 每个 tar 条目必须声明非负且可信的大小；读取时再次使用 `io.LimitReader`。

以下情况整次导入失败，且不创建知识库：

- 不是 tar 或归档已损坏。
- 缺少 `$meta.json`。
- 缺少或无法解析 `meta`、`tocYml`。
- TOC 引用不存在的文档。
- TOC 包含重复 UUID、孤立节点或父子循环。
- 文档 JSON 损坏或正文格式不支持。
- 未知的非画板 TOC 类型。
- 路径穿越、异常 tar 类型或超过归档限制。
- 知识库树超过 4 层。

错误面向用户使用中文并指出失败阶段。底层错误可以包装后返回，但不能包含文档正文、图片 URL 或凭据。

解析完成并校验层级后才调用 `CreateKnowledgeBase`。后续磁盘写入失败时返回错误并停止继续写入。由于 Vault 当前没有事务 API，这一版不删除已经写入的部分内容，也不声称导入成功。

## 依赖

- 使用现有 `golang.org/x/net/html`，将其从间接依赖调整为直接依赖。
- 新增 `gopkg.in/yaml.v3`，用于解析真实的 `tocYml`，不手写 YAML 子集解析器。

## 测试

所有测试在运行时生成合成 tar，不使用真实 Ucloud 导出文件。

### `internal/lakebook`

表驱动单元测试覆盖：

- `TITLE`、`DOC` 和嵌套文档的树及顺序。
- `META` 跳过。
- `body` 优先和 `body_draft` 回退。
- 标题、段落、粗体、斜体、行内代码、换行和链接。
- 无序列表、有序列表和嵌套列表。
- fenced code block 与语言标记。
- 表格、分割线和图片 URL。
- 独立画板节点、画板文字、预览图和原链接。
- 文档内嵌画板卡片、无预览画板卡片和画板占位内容。
- 未知 HTML 块保留可读文本。
- 损坏外层 JSON、损坏 `meta` 字符串、损坏文档 JSON。
- 缺失 meta、TOC、被引用文档和未知的非画板 TOC 类型。
- 重复 UUID、孤立节点和父子循环。
- 绝对路径、`..`、路径规范化穿越和异常 tar 条目。
- 条目数量、单文件大小和总大小超限。
- 超过 4 层。
- 同名节点。
- 空标题。

### `internal/app`

使用临时 Vault 和 `httptest.Server` 覆盖：

- 导入后形成正确的知识库树和节点顺序。
- 文档正文写入后保留核心 Markdown。
- 文档子节点位于同名目录。
- 图片下载后写入 `.assets`，正文引用指向本地路径。
- 同一 URL 只下载一次且只保存一份。
- 单张图片失败时文档仍导入，原 URL 保留，未下载数量正确。
- 独立画板和无预览画板都创建文档，画板数量进入完成提示。
- 文档内嵌画板保留标题、文字、预览和原链接，并计入画板数量。
- 非 2xx、超时、非图片 Content-Type、超过单图上限。
- 同名知识库由 Vault 自动去重。
- 解析失败时不创建知识库。
- 取消文件选择时不创建知识库。

### UI 验证

- 自动化检查侧栏和工作台都只显示一个“导入”入口。
- 启动应用并截图，人工检查侧栏两级菜单、工作台菜单、文件名选择器和 toast 的文字、间距、对齐及遮挡。
- 截图前不打开或展示真实 Ucloud 文档正文。

## 验收标准

- 知识库侧栏加号中的三个导入来源通过统一“导入”菜单进入。
- 工作台导入卡显示与侧栏相同的三个来源。
- 文件菜单不增加 Lakebook 入口。
- 合成的真实结构 Lakebook 能生成完整知识库目录，节点顺序和嵌套正确。
- 图片成功下载到当前知识库的 `.assets`，Markdown 使用可访问的本地相对路径。
- 单张图片失败不阻止其余内容导入。
- 独立画板和正文内嵌画板都生成静态可读内容，没有预览图时保留占位内容。
- 未知的非画板 TOC 类型整次失败；明确画板类型不失败。
- 损坏或恶意归档不会创建知识库。
- 超过 4 层会明确报错。
- `go test ./...` 通过。
- 应用实际启动并完成菜单截图检查。
