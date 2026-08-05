# 阶段五-FIX 交接文档：库自动扫描（文件名解析模式）

## 1. 阶段目标

实现"库自动扫描"功能：用户将视频文件放入 `anime/` 目录（可混放多部番剧），点击"库扫描"后系统自动从文件名解析番剧标题、调用 Bangumi API 匹配、自动创建番剧条目并关联集数。

**与阶段五（Watcher 方案）的区别**：
- 阶段五要求每部番剧建子目录，本方案不需要
- 阶段五是后台自动轮询，本方案是手动点击触发
- 阶段五有 pending 队列，本方案取第一个搜索结果自动匹配
- 两个方案可以共存，互不冲突

**本阶段完成后系统状态**：
- 用户下载番剧文件丢进 `anime/` 目录，多部番剧的文件可以混放
- 点击"库扫描"按钮，系统自动解析文件名、搜索 Bangumi、创建番剧条目并关联集数
- 已在库中的番剧自动追加新集数
- 已关联的文件不会重复导入
- 无法识别的文件在扫描报告中列出

**可选**：执行者可使用 `ui-ux-pro-max` skill 对前端页面进行 UI/UX 重设计。

---

## 2. 前置条件

阶段一至四已完成。关键现有代码：

| 能力 | 位置 | 复用方式 |
|------|------|---------|
| 集数文件名识别 | `services/scanner.go` 的 `extractEpisodeNumber` | 直接调用 |
| 视频扩展名集合 | `services/scanner.go` 的 `videoExts` | 直接引用 |
| Zone.Identifier 过滤 | `services/scanner.go` | 参照相同逻辑 |
| Bangumi 搜索 | `services/bangumi.go` 的 `Search(keyword)` | 直接调用，返回 `[]BangumiSearchItem` |
| Bangumi 详情 | `services/bangumi.go` 的 `GetSubject(id)` | 直接调用，返回 `*BangumiSubjectInfo` |
| 番剧创建 | `database/anime.go` 的 `CreateAnime` | 直接调用 |
| 番剧查询 | `database/anime.go` 的 `GetAnimeByID` | 直接调用 |
| 集数查询 | `database/anime.go` 的 `ListEpisodesByAnimeID` | 直接调用 |
| 集数替换 | `database/anime.go` 的 `ReplaceEpisodes` | 不要用（会删旧集数），用新增的 `CreateEpisode` |
| 路径安全校验 | `services/scanner.go` 的 `ValidateRelativeVideoPath` | 直接调用 |
| 统一响应 | `utils/response.go` 的 `Success`/`Error` | 直接调用 |
| 前端 API 封装 | `frontend/src/api/index.ts` 的 `api`/`unwrap`/`ApiError` | 直接引用 |

**测试文件**（`anime/` 目录下现有）：
```
[CheeseAni] Re：Zero kara Hajimeru Isekai Seikatsu 4th season Soshitsu-Hen [67][CR-WebRip HEVC AAC SRT][简繁内封].mkv
```

---

## 3. 后端详细设计

### 3.1 新增文件清单

| 文件 | 用途 |
|------|------|
| `services/parser.go` | 文件名解析器，从文件名提取搜索标题和集数 |
| `services/library.go` | 库扫描服务，协调文件遍历、Bangumi 搜索、自动创建 |
| `handlers/library.go` | 库扫描接口处理器 |
| `database/library.go` | 库扫描所需的数据查询函数 |

修改文件：
- `database/anime.go`（新增 `GetAnimeByBangumiID` 和 `CreateEpisode` 函数）
- `main.go`（注册新服务和路由）

### 3.2 文件名解析器 `services/parser.go`

#### 3.2.1 设计目标

从典型动画文件名中提取两部分信息：
- **搜索标题**：用于 Bangumi 搜索的关键词
- **集数编号**：已有的 `extractEpisodeNumber` 函数获取

#### 3.2.2 解析示例

| 输入文件名 | 提取标题 | 集数 |
|-----------|---------|------|
| `[CheeseAni] Re：Zero kara Hajimeru Isekai Seikatsu 4th season Soshitsu-Hen [67][CR-WebRip HEVC AAC SRT][简繁内封].mkv` | `Re：Zero kara Hajimeru Isekai Seikatsu 4th season Soshitsu-Hen` | 67 |
| `[ANi] Bocchi the Rock! - 01 [1080p].mkv` | `Bocchi the Rock!` | 1 |
| `Frieren - S01E05 [1080p].mkv` | `Frieren` | 5 |
| `[SubGroup] 葬送的芙莉莲 第3集 [1080p].mkv` | `葬送的芙莉莲` | 3 |

#### 3.2.3 解析算法

**关键**：必须先提取集数，再去除方括号。因为集数常出现在方括号内（如 `[67]`），如果先去除方括号会丢失集数信息。

步骤：
1. **提取集数**：调用已有的 `extractEpisodeNumber(filename)` 函数，得到集数编号
2. **去除文件扩展名**：用 `filepath.Ext` 获取扩展名，`strings.TrimSuffix` 去除
3. **去除所有 `[...]` 块**：用正则 `\[.*?\]` 将所有方括号内容替换为空格（包括字幕组名 `[CheeseAni]`、集数方括号 `[67]`、编码信息 `[CR-WebRip HEVC AAC SRT]`、语言标签 `[简繁内封]`）
4. **去除非方括号内的集数模式**：用 `scanner.go` 中 `epPatterns` 的正则将残留的集数文本替换为空格（如 `- 01`、`EP03`、`S01E05`、`第3集`）
5. **清理**：合并连续空格为单个空格，去除首尾空格和首尾的 `-` 和空格
6. 结果即为搜索标题

#### 3.2.4 导出接口

```
type ParsedFilename struct {
    Title      string
    EpisodeNum int
    FileName   string
}

func ParseFilename(filename string) ParsedFilename
```

### 3.3 库扫描数据层 `database/library.go`

#### 3.3.1 函数签名

```
// 检查某文件是否已被任意番剧关联
// fileName: 视频文件名（不含目录路径）
// dirPath: 该文件所在目录（相对 video root，根目录为空字符串）
func IsFileAssociated(fileName, dirPath string) (bool, error)
```

**SQL**：
```sql
SELECT COUNT(*) FROM episodes ep
JOIN animes a ON ep.anime_id = a.id
WHERE ep.file_path = ? AND a.file_path = ?
```
- 返回 count > 0

`database/anime.go` 追加两个函数：

```
// 按 Bangumi ID 查找已有番剧（不存在返回 nil, nil，不是错误）
func GetAnimeByBangumiID(bangumiID int) (*models.Anime, error)

// 插入单条集数（不删除已有集数）
func CreateEpisode(ep *models.Episode) error
```

**`GetAnimeByBangumiID`** SQL：
```sql
SELECT id, title, title_cn, bangumi_id, cover, summary, ep_count, file_path, created_at
FROM animes WHERE bangumi_id = ?
```
- 复用 `animeSelect` 常量和 `scanAnime` 函数
- `sql.ErrNoRows` 时返回 `nil, nil`

**`CreateEpisode`** SQL：
```sql
INSERT INTO episodes (anime_id, ep_number, title, file_path, duration) VALUES (?, ?, ?, ?, ?)
```
- 参数从 `models.Episode` 结构体取

### 3.4 库扫描服务 `services/library.go`

#### 3.4.1 结构体定义

```
type LibraryService struct {
    bangumi  *BangumiService
    rootPath string
}

type LibraryScanResult struct {
    TotalFiles   int
    Skipped      int
    NewAnimes    int
    NewEpisodes  int
    Unidentified []UnidentifiedFile
}

type UnidentifiedFile struct {
    FileName string `json:"file_name"`
    Reason   string `json:"reason"`
}
```

#### 3.4.2 构造函数

```
func NewLibraryService(bangumi *BangumiService, rootPath string) *LibraryService
```

#### 3.4.3 `Scan` 方法处理流程

```
func (s *LibraryService) Scan() (*LibraryScanResult, error)
```

**第 1 步：遍历目录**

用 `filepath.WalkDir(s.rootPath, ...)` 递归遍历 `anime/` 目录。对每个文件：
1. 跳过目录本身和子目录（`entry.IsDir()` 时 continue，但 WalkDir 会自动递归进入子目录）
2. 跳过符号链接（`entry.Type() & os.ModeSymlink != 0`）
3. 跳过 `:Zone.Identifier` 文件（`strings.Contains(name, ":Zone.Identifier")`）
4. 检查扩展名是否在 `videoExts` 中（`videoExts` 在 `scanner.go` 中定义，同包可直接引用）
5. 计算相对目录：`relPath = filepath.Rel(s.rootPath, fullPath)`，`relDir = filepath.Dir(relPath)`，如果 `relDir == "."` 则设为空字符串 `""`
6. 记录文件信息：`fileName = entry.Name()`，`relDir`，`fullPath`

将所有视频文件信息存入一个切片 `allFiles []fileInfo`，其中 `fileInfo` 是内部结构体（不导出）：
```
type fileInfo struct {
    fileName string
    relDir   string  // 相对 rootPath 的目录，根目录为 ""
    fullPath string
}
```

`result.TotalFiles = len(allFiles)`

**第 2 步：过滤已关联文件**

对 `allFiles` 中的每个文件，调用 `database.IsFileAssociated(fileName, relDir)`：
- 如果已关联：`result.Skipped++`，从列表中移除
- 如果未关联：保留

过滤后得到 `newFiles []fileInfo`。

**第 3 步：解析文件名**

对 `newFiles` 中的每个文件，调用 `ParseFilename(fileName)`，得到 `(title, episodeNum)`。
- 如果 `title` 为空：加入 `Unidentified`（reason: "无法解析文件名"），跳过
- 如果 `episodeNum == 0`：加入 `Unidentified`（reason: "无法识别集数"），跳过
- 否则：保留，记录 `fileInfo` + `ParsedFilename`

**第 4 步：按标题分组**

将文件按 `title` 分组：`map[string][]fileInfo`。
同一标题的多个文件归为一组，属于同一部番剧。

> 这样同一番剧的 12 个文件只搜索一次 Bangumi。

**第 5 步：逐组搜索 Bangumi 并创建**

对每个标题组：

1. **搜索 Bangumi**：调用 `s.bangumi.Search(title)`
   - 如果出错：整组文件加入 `Unidentified`（reason: `"搜索失败: " + err.Error()`）
   - 如果结果为空（`len(results) == 0`）：整组文件加入 `Unidentified`（reason: "无搜索结果"）
   - 以上两种情况跳到下一个组

2. **取第一个结果**：`results[0]`（Bangumi 按相关性排序，第一个最匹配）

3. **获取详情**：调用 `s.bangumi.GetSubject(results[0].ID)` 获取完整元数据
   - 如果出错：用搜索结果中的信息创建（`BangumiSearchItem` 的 `EpsCount` 替代 `TotalEpisodes`，`Cover` 替代 `Cover`，`Summary` 为空）

4. **查找已有番剧**：调用 `database.GetAnimeByBangumiID(subject.ID)`
   - **如果已存在**（`existing != nil`）：
     - 检查 `existing.FilePath` 是否等于该组的 `relDir`（组内所有文件应在同一目录）
     - **如果相等**：将文件作为新集数添加到该番剧
     - **如果不等**：整组加入 `Unidentified`（reason: "番剧已存在但目录不同（已有: xxx，当前: yyy）"），跳过
   - **如果不存在**：
     - 创建新番剧：`database.CreateAnime(&models.Anime{...})`
     - `FilePath` = `relDir`（空字符串表示根目录）
     - `BangumiID` = `subject.ID`
     - `Title` = `subject.Name`
     - `TitleCn` = `subject.NameCn`
     - `Cover` = `subject.Cover`
     - `Summary` = `subject.Summary`
     - `EpCount` = `subject.TotalEpisodes`
     - `result.NewAnimes++`

5. **创建集数**：
   - 获取该番剧已有的集数列表：`database.ListEpisodesByAnimeID(animeID)`
   - 构建已有集数编号集合：`existingEps map[int]bool`
   - 对组内每个文件：
     - 检查 `episodeNum` 是否已在 `existingEps` 中
     - 如果已存在：跳过（不重复创建）
     - 如果不存在：调用 `database.CreateEpisode(&models.Episode{AnimeID: animeID, EpNumber: episodeNum, FilePath: fileName})`
     - `result.NewEpisodes++`

**第 6 步：返回结果**

返回 `LibraryScanResult`。

#### 3.4.4 错误处理策略

- 遍历目录失败：返回 error（整个扫描失败）
- 单个文件的数据库查询失败：记日志，跳过该文件
- Bangumi API 搜索失败：整组文件标记为 Unidentified，继续下一组
- 创建番剧/集数失败：记日志，该文件标记为 Unidentified
- **不中断整个扫描流程**：单个文件/组的错误不影响其他文件

### 3.5 库扫描处理器 `handlers/library.go`

```
type LibraryHandler struct {
    library *services.LibraryService
}

func NewLibraryHandler(library *services.LibraryService) *LibraryHandler
```

**接口**：`POST /api/library/scan`

**处理流程**：
1. 调用 `library.Scan()`
2. 如果出错：返回 `utils.Error(c, utils.CodeInternal, "库扫描失败: " + err.Error())`
3. 成功：返回 `utils.Success(c, result)`

**响应格式**：
```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "total_files": 10,
    "skipped": 2,
    "new_animes": 2,
    "new_episodes": 8,
    "unidentified": [
      { "file_name": "unknown.mkv", "reason": "无搜索结果" }
    ]
  }
}
```

### 3.6 路由注册 `main.go` 修改

新增初始化（在现有服务初始化之后）：
```go
libraryService := services.NewLibraryService(bangumiService, cfg.Video.RootPath)
libraryHandler := handlers.NewLibraryHandler(libraryService)
```

新增路由（注册在 `protected` 组下，所有登录用户可用）：
```go
protected.POST("/library/scan", libraryHandler.Scan)
```

---

## 4. 前端详细设计

### 4.1 新增文件清单

| 文件 | 操作 | 用途 |
|------|------|------|
| `types/library.ts` | 新增 | 库扫描结果类型 |
| `api/library.ts` | 新增 | 库扫描 API |
| `views/AnimeListView.vue` | 修改 | 添加"库扫描"按钮和结果展示 |

### 4.2 类型定义 `src/types/library.ts`

```typescript
export interface UnidentifiedFile {
  file_name: string
  reason: string
}

export interface LibraryScanResult {
  total_files: number
  skipped: number
  new_animes: number
  new_episodes: number
  unidentified: UnidentifiedFile[]
}
```

### 4.3 API 封装 `src/api/library.ts`

```typescript
import api, { type ApiResponse, unwrap } from './index'
import type { LibraryScanResult } from '../types/library'

export async function scanLibrary(): Promise<LibraryScanResult> {
  const response = await api.post<ApiResponse<LibraryScanResult>>('/library/scan')
  return unwrap(response)
}
```

### 4.4 番剧列表页修改 `src/views/AnimeListView.vue`

#### 4.4.1 新增"库扫描"按钮

在页头区域（`page-header` div 内），"添加番剧"按钮旁边新增"库扫描"按钮。两个按钮用 flex gap 间隔。

#### 4.4.2 新增状态变量

```typescript
const scanning = ref(false)
const scanResult = ref<LibraryScanResult | null>(null)
const scanError = ref('')
```

#### 4.4.3 扫描函数

```typescript
async function handleLibraryScan() {
  scanning.value = true
  scanError.value = ''
  scanResult.value = null
  try {
    const result = await scanLibrary()
    scanResult.value = result
    await load()  // 刷新番剧列表
  } catch (e: unknown) {
    scanError.value = e instanceof ApiError ? e.message : '库扫描失败'
  } finally {
    scanning.value = false
  }
}
```

#### 4.4.4 结果展示

在搜索框下方、番剧网格上方，当 `scanResult` 不为 null 时显示结果卡片：

**卡片内容**：
- 标题行："库扫描完成" + 右侧"关闭"按钮（点击后 `scanResult = null`）
- 统计行：
  - `扫描到 {total_files} 个视频文件`
  - `新增 {new_animes} 部番剧，{new_episodes} 集`
  - `跳过 {skipped} 个已关联文件`
  - `无法识别 {unidentified.length} 个`（如果 > 0，标红）
- 如果 `unidentified.length > 0`，展开列表显示每个文件名和原因

**样式**：复用现有 CSS 变量，卡片用 `var(--surface-color)` 背景 + `var(--border-color)` 边框 + 圆角。

#### 4.4.5 按钮状态

- 默认：显示"库扫描"
- 扫描中：禁用按钮，显示"扫描中..."
- `scanning` 期间两个按钮（"添加番剧"和"库扫描"）都禁用，避免冲突

---

## 5. UI/UX 重设计指南（可选）

执行者可以使用 `ui-ux-pro-max` skill 对前端页面进行整体 UI/UX 重设计。

### 5.1 重设计范围

所有现有页面 + 新增的库扫描功能：
- 登录页 `LoginView.vue`
- 首页 `HomeView.vue`
- 番剧列表 `AnimeListView.vue`
- 添加番剧 `AnimeAddView.vue`
- 番剧详情 `AnimeDetailView.vue`
- 观看页 `WatchView.vue`
- 用户管理 `AdminUsersView.vue`
- 导航栏 `AppLayout.vue`

### 5.2 重设计约束

**必须保持**：
- 深色主题（CSS 变量在 `src/style.css` 的 `:root` 中）
- `<script setup lang="ts">` + Composition API 写法
- 现有的 API 调用逻辑和类型定义
- 响应式布局（桌面 + 移动端）
- 无障碍属性（`aria-*`、`role`、键盘导航）

**可以改变**：
- CSS 类名和样式
- 组件内部 DOM 结构
- 交互动画和过渡效果
- 字体大小和间距
- 图标使用（可引入轻量图标库如 `lucide-vue-next`）

**不要引入**：
- 重量级 UI 框架（Element Plus、Ant Design 等）
- Tailwind CSS
- 改变 Vue Router 路由结构或 Pinia store 接口

### 5.3 重设计优先级

1. **先完成库扫描功能**（后端 + 前端功能逻辑）
2. **再进行 UI 重设计**（在功能可用的基础上美化）
3. 重设计后确保 `npm run build` 和 `vue-tsc -b` 仍通过

---

## 6. 测试验证流程

### 6.1 准备

1. 启动后端和前端
2. 登录系统
3. 确认 `anime/` 目录下有测试文件（Re:Zero S4 第 67 集）
4. 清空库中所有番剧（如有），确保干净状态

### 6.2 首次库扫描

5. 进入番剧列表页
6. 点击"库扫描"按钮
7. 等待扫描完成（约 2-5 秒）
8. 确认扫描结果显示：
   - `total_files`: ≥ 1
   - `new_animes`: 1
   - `new_episodes`: 1
   - `unidentified`: 空
9. 确认番剧列表自动刷新，显示新创建的番剧（Re:Zero 相关条目）

### 6.3 验证番剧信息

10. 点击新创建的番剧，进入详情页
11. 确认封面、标题、简介等元数据来自 Bangumi
12. 确认集数列表显示"第 67 话"
13. 点击"播放"，确认视频可以正常播放

### 6.4 重复扫描（跳过已关联）

14. 回到番剧列表页
15. 再次点击"库扫描"
16. 确认扫描结果显示：
    - `skipped`: ≥ 1（已关联的文件被跳过）
    - `new_animes`: 0
    - `new_episodes`: 0

### 6.5 多番剧混放扫描

17. 在 `anime/` 目录中放入一个新的视频文件（不同番剧，如 Bocchi the Rock! 第 1 集）
18. 点击"库扫描"
19. 确认扫描结果：
    - `skipped`: 1（Re:Zero 文件被跳过）
    - `new_animes`: 1（新番剧）
    - `new_episodes`: 1
20. 确认番剧列表现在有 2 部番剧

### 6.6 同番剧多集扫描

21. 在 `anime/` 目录中放入同一番剧的另一个文件（如 Bocchi 第 2 集）
22. 点击"库扫描"
23. 确认扫描结果：
    - `new_animes`: 0（番剧已存在）
    - `new_episodes`: 1（追加新集数）
24. 进入该番剧详情页，确认集数列表有 2 集

---

## 7. 验收标准

### 后端

- [ ] `go build ./...` 无编译错误
- [ ] `go test ./...` 通过
- [ ] `go run main.go` 能正常启动
- [ ] `POST /api/library/scan` 返回扫描结果
- [ ] 已关联的文件被跳过（`skipped` 计数正确）
- [ ] 同一番剧的多个文件只搜索一次 Bangumi
- [ ] 已有番剧（同 bangumi_id）的新文件能追加集数
- [ ] 无法识别的文件在 `unidentified` 列表中返回
- [ ] 文件名解析能正确处理方括号格式
- [ ] 不影响现有的手动扫描功能

### 前端

- [ ] `npm run build` 构建成功
- [ ] `npm run dev` 能正常启动
- [ ] 番剧列表页显示"库扫描"按钮
- [ ] 点击按钮后显示 loading 状态
- [ ] 扫描完成后显示结果摘要
- [ ] 有无法识别文件时显示列表
- [ ] 扫描后番剧列表自动刷新
- [ ] 控制台无报错
- [ ] 移动端布局正常

### UI 重设计（如执行）

- [ ] 所有页面保持深色主题
- [ ] 所有页面响应式正常
- [ ] 所有功能逻辑不变
- [ ] `npm run build` 和 `vue-tsc -b` 通过
- [ ] 控制台无报错

---

## 8. 注意事项

### 8.1 解析顺序

**必须先提取集数，再去除方括号**。集数常出现在方括号内（如 `[67]`），如果先去除方括号会丢失集数信息。正确顺序：
1. `extractEpisodeNumber(filename)` -> 得到集数
2. 去除所有 `[...]` -> 得到标题文本

### 8.2 Bangumi 匹配准确性

- 自动取搜索结果的第一个（最相关），不保证 100% 准确
- 如果匹配错误，用户可以删除番剧后重新扫描
- 文件名越规范（包含完整标题），匹配越准确
- 包含季数信息（如 "4th season"）有助于区分不同季

### 8.3 与手动扫描的关系

- **库扫描**（本阶段新增）：扫描整个 `anime/` 目录，逐文件识别，适合文件混放
- **手动扫描**（阶段三已有）：扫描某个番剧的 `file_path` 目录，适合每部番有独立子目录
- 两个功能独立，不互相干扰
- 对自动创建的番剧（`file_path` 为空）使用手动扫描会把根目录所有文件归入该番剧，应避免

### 8.4 已有番剧追加集数

- 如果 Bangumi 匹配到的番剧已在库中（同 `bangumi_id`），且文件在同一目录（`file_path` 相同），则追加集数
- 追加前检查集数编号是否已存在，避免重复
- **不要使用 `ReplaceEpisodes`**（会删除已有集数），用新增的 `CreateEpisode` 函数逐个插入

### 8.5 目录递归

- 使用 `filepath.WalkDir` 递归扫描，支持子目录
- 子目录中的文件：`anime.file_path` = 子目录名，`episode.file_path` = 文件名
- 根目录中的文件：`anime.file_path` = ""（空字符串），`episode.file_path` = 文件名
- 路径构造 `root / anime.file_path / episode.file_path` 在两种情况下都正确

### 8.6 性能

- Bangumi API 每次搜索约 0.5-2 秒
- 按标题分组后每组只搜一次，避免重复搜索
- 10 个不同番剧的文件约需 5-20 秒
- 前端显示 loading 状态，按钮在 scanning 期间禁用

### 8.7 不要做的事

- **不要**修改数据库表结构
- **不要**修改现有的手动扫描逻辑（`handlers/anime.go` 的 `Scan` 方法）
- **不要**修改现有的视频播放逻辑
- **不要**移动或重命名 `anime/` 目录中的文件
- **不要**在自动扫描中删除已有番剧或集数
- **不要**实现后台自动轮询（那是阶段五 Watcher 方案的职责）

---

## 9. 完整项目目录预览

```
fan-web/
├── backend/
│   ├── main.go                         # [修改] 注册 /api/library/scan 路由
│   ├── services/
│   │   ├── parser.go                   # [新增] 文件名解析器
│   │   └── library.go                  # [新增] 库扫描服务
│   ├── database/
│   │   ├── anime.go                    # [修改] 新增 GetAnimeByBangumiID、CreateEpisode
│   │   └── library.go                  # [新增] IsFileAssociated
│   └── handlers/
│       └── library.go                  # [新增] 库扫描处理器
├── frontend/
│   └── src/
│       ├── types/
│       │   └── library.ts              # [新增] 库扫描结果类型
│       ├── api/
│       │   └── library.ts              # [新增] 库扫描 API
│       └── views/
│           └── AnimeListView.vue       # [修改] 添加库扫描按钮和结果展示
└── ... (其他已有文件不变，除非 UI 重设计)
```
