# fan-web 看番网站

一个自托管的个人看番网站。将本地番剧目录自动扫描入库，提供番剧管理、视频在线播放、观看进度记录、用户认证等功能。前后端一体化编译，部署只需一个可执行文件。同时提供 Android 原生 App，支持手机看番。

## 项目初衷

fan-web 诞生于对现有自托管方案的不满。此前我使用 Alist 管理本地番剧，但它只提供文件浏览，既无法呈现美观的番剧信息页，也无法记录「哪一集看过、看到哪里」。也曾考虑 Jellyfin 这类媒体服务器，但其为整库媒体设计，需要严格按文件夹规范整理资源，功能虽全却相对臃肿，在低配置 VPS 上运行偏重。

因此我决定做一个**专门为看番场景设计**的轻量方案，核心差异化如下：

- **无需手动整理**：直接把视频文件丢进根目录即可。系统会遍历目录、自动识别番剧、集数与季号（兼容 `[01]`、`[01v2]`、`EP01`、`第1集`、`S1E1` 等常见命名），并按季匹配 Bangumi 信息入库，省去建库时的逐个分类整理。未识别目录会落库，可事后确认。
- **为番剧优化**：以「番剧 -> 剧集」为粒度组织内容，配合观看进度记录、继续看、Bangumi 番组信息，解决「我追到哪里了」这类核心需求。
- **极致轻量**：前后端一体化编译为单个静态链接的可执行文件，常驻内存约 15 MB，空闲 CPU 近乎为 0，低配 VPS 也能流畅运行。
- **零配置部署**：首次运行直接打开 WebUI 引导页，在线设置管理员账号与视频目录即可，无需编写配置文件或搭建 Web 服务。
- **多端覆盖**：Web 端 + Android App 双端进度互通，手机看番不再依赖浏览器。

## 功能特性

- **番剧管理**：浏览、添加、编辑、删除番剧；Web 可重绑 Bangumi 元数据；手机管理员可扫描、添加、删除、单番重扫（重绑与编辑表单只在 Web）
- **自动扫描**：全库扫描为后台作业（POST 启动 / GET 轮询），从视频根目录自动识别番剧、剧集与季号（支持 `[01]`、`[01v2]`、`EP01`、`第1集`、`S1E1` 等常见命名格式）；剧场版/无集号电影可按 ep1 入库，Search 主名对不上时用 infobox 别名重打分（阈值不变）；未识别结果持久保存
- **在线播放**：浏览器直接播放本地视频（基于 ArtPlayer），支持 HEVC/mkv
- **内封字幕**：Web 端通过纯 Go 解析 MKV 容器提取字幕轨道并转为 VTT，默认显示字幕且可切换；移动端通过 libmpv 原生支持内封字幕
- **观看进度**：自动记录每位用户的观看进度，观看状态不可逆（已看不会降级）；App 列表顶「继续看」（先进行中再第一集未看，全看完不出现），Web 首页仍展示最近入库
- **番组信息**：集成 Bangumi 搜索与番剧详情
- **Bangumi 进度同步**：用个人 Access Token（在 https://next.bgm.tv/demo/access-token 签发）只同步「看过」布尔，不同步秒数
- **用户系统**：JWT 认证、登录限流，管理员可管理用户
- **零配置初始化**：首次运行自动进入 WebUI 引导页，在线设置管理员与视频目录，自动生成配置
- **单文件部署**：前端资源嵌入后端二进制，无需 nginx
- **低资源占用**：常驻内存约 15 MB，空闲 CPU 近乎为 0，可在低配 VPS 运行
- **Android App**：Flutter 原生应用，支持 HEVC 硬解、手势控制、断点续播、离线进度保存、断网会话保留；v1.4 起管理员可在手机管库

## 技术栈

| 端 | 技术 |
| --- | --- |
| 后端 | Go + Gin + SQLite（modernc，纯 Go 无 CGO）+ JWT + bcrypt |
| 前端 | Vue 3 + Vite + TypeScript + Pinia + vue-router + ArtPlayer |
| 移动端 | Flutter + media_kit（libmpv）+ Riverpod + dio |

## 目录结构

```
.
├── backend/               # Go 后端
│   ├── config/            # 配置加载
│   ├── database/          # SQLite 数据访问
│   ├── handlers/          # HTTP 处理
│   ├── middleware/        # JWT / CORS / 限流
│   ├── models/            # 数据模型
│   ├── services/          # 业务逻辑（扫描、番组、库、字幕解析）
│   ├── utils/
│   ├── web/               # 嵌入的前端静态资源（go:embed）
│   ├── main.go
│   └── config.yaml        # 配置文件
├── frontend/              # Vue 3 前端
│   └── src/
│       ├── api/           # API 客户端
│       ├── components/
│       ├── router/
│       ├── stores/        # Pinia
│       ├── types/
│       └── views/         # 页面
├── mobile/                # Flutter Android App
│   ├── lib/
│   │   ├── api/           # API 客户端（dio）
│   │   ├── models/        # 数据模型
│   │   ├── providers/     # Riverpod 状态管理
│   │   ├── screens/       # 页面（登录/列表/添加/详情/播放器）
│   │   ├── services/      # 进度 outbox 等服务
│   │   ├── widgets/       # 可复用组件
│   │   └── utils/         # 工具函数
│   ├── test/              # 单元测试
│   └── icon.png           # 应用图标源文件
├── docs/                  # 需求与阶段设计文档
├── dev.sh                 # 开发运行脚本
└── build.sh               # 一键构建单文件可执行程序
```

## 快速开始（开发）

环境要求：Go 1.21+、Node.js 18+（含 npm）。移动端开发另需 Flutter SDK + Android SDK。

```bash
# 启动后端（默认监听 :8080）
./dev.sh backend

# 另开终端启动前端（默认 :5173，/api 代理到 8080）
./dev.sh frontend
```

浏览器访问 http://localhost:5173 即可，前端开发服务器会自动将 `/api` 请求代理到后端。

移动端开发见 [mobile/README.md](mobile/README.md)。

## 构建单个可执行文件

```bash
./build.sh
```

脚本依次执行：前端构建（`vue-tsc && vite build`）-> 拷贝到 `backend/web/dist` -> 后端交叉编译（`CGO_ENABLED=0`），最终产物为 `dist/fan-web-server`，已包含全部前端资源。

> 构建会覆盖 `backend/web/dist/` 下由 git 跟踪的占位文件，`git status` 显示其 modified 属正常现象。

### 构建 Android APK

```bash
cd mobile
flutter build apk --release
```

APK 位于 `mobile/build/app/outputs/flutter-apk/app-release.apk`。详见 [mobile/README.md](mobile/README.md)。

## 部署到 VPS

### 首次运行（零配置初始化）

可执行文件**无需自带配置文件**。**仅当目录中没有 `config.yaml` 且数据库也没有管理员**时，才以默认配置启动并进入初始化引导页。若 `config.yaml` 丢失但数据库已有管理员，进程会拒绝以未初始化状态启动，必须先恢复 `config.yaml`。

1. 上传并运行：

   ```bash
   mkdir -p /opt/fan-web && cd /opt/fan-web
   cp <产物路径>/fan-web-server .
   ./fan-web-server
   ```

2. 浏览器访问 `http://<vps-ip>:8080`，在引导页填写：管理员用户名/密码、**视频根目录（手动输入服务器绝对路径）**、可选端口。

3. 提交后自动生成 `config.yaml`、创建管理员并进入系统。后续可直接用管理员账号登录。

> 视频目录在浏览器中无法直接选择，需手动输入服务器上的路径，如 `/home/user/anime`。

> **首次部署不要预先放置 `config.yaml`**（包括不要从仓库复制 `backend/config.yaml`）：全新安装应在**没有** config.yaml 的情况下直接运行二进制并进入初始化页，由初始化流程生成 config.yaml。若预先放置了 config.yaml，实例会被标记为「已初始化」，而全新数据库还没有管理员，启动会被安全机制拦截（见下文「升级已有部署」）。复制 config.yaml 只适用于**升级**已有部署的场景。

### 端口说明

端口按以下优先级决定：

1. 命令行参数：`./fan-web-server -port 9090`
2. 配置文件 `config.yaml` 的 `server.port`
3. 默认 `8080`

默认/配置端口被占用时，会自动顺延尝试后续端口（最多 10 个），并在终端打印实际访问地址。回退绑定**不会**清理 `.old` / `.pre-migration.bak`，也不会把回退端口写回配置。`-port` 在监听前覆盖配置端口。

### 升级已有部署（已有配置）

以下流程**只适用于升级**一份已正常运行、且 `config.yaml` 与数据库管理员账号都已存在的部署：

```bash
mkdir -p /opt/fan-web && cd /opt/fan-web
cp <产物路径>/fan-web-server .        # 覆盖旧二进制
# 保留现有的 config.yaml 与 data/fan-web.db（不要用仓库里的 backend/config.yaml 覆盖）
./fan-web-server
```

仓库中的 `backend/config.yaml` 仅用于参考配置字段，**不要**把它复制进部署目录——一旦 config.yaml 存在，实例就被标记为「已初始化」（`Configured=true`），而全新部署的数据库还没有管理员，启动会被安全机制拦截（见下）。

### 启动行为区分

- **全新安装（目录中不存在 config.yaml，且数据库没有管理员）**：以默认配置启动，进入 WebUI 初始化页，提交后生成 config.yaml、创建管理员并进入系统。
- **config.yaml 缺失但数据库已有管理员**：**启动直接拒绝**，打印「配置文件缺失但数据库已有管理员，拒绝以未初始化状态启动，请恢复 config.yaml」。这不是首次运行。
- **config.yaml 存在且数据库中有管理员**：直接进入登录页，启动时仅做 JWT 密钥轮换、明文密码清理等安全迁移。
- **config.yaml 存在但数据库中没有管理员、且配置中也没有旧明文密码**：**启动直接拒绝**，打印类似「配置已标记为已初始化，但数据库中没有管理员（数据库文件 ...）：请人工检查数据库是否丢失或被误删，恢复数据后重启；不要重新执行 /api/setup 初始化，以免管理员账户被抢注」。这是**有意为之**：若静默降级为未初始化，任何能访问端口的人都可通过 `POST /api/setup` 抢注管理员。此时应检查数据库文件是否丢失/被误删并恢复数据后重启，**不要**重新执行初始化。

## 配置说明

配置文件 `backend/config.yaml`：

```yaml
server:
  port: 8080
  mode: debug          # 生产环境建议改为 release

database:
  path: ./data/fan-web.db

jwt:
  secret: ""           # 首次初始化自动生成并以 0600 权限写回
  expire: 168h

admin:
  username: admin

video:
  root_path: ../anime  # 番剧视频根目录
```

> **注意**：管理员密码不会写入 `config.yaml`，只以 bcrypt 哈希保存在数据库中。旧配置中的明文密码会在升级启动时迁移并从磁盘删除；使用仓库公开默认 JWT 密钥的旧实例会自动轮换密钥，因此需要重新登录一次。自定义 JWT 密钥不会被轮换。

## 主要 API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/health` | 健康检查 |
| GET | `/api/setup/status` | 查询是否已完成初始化 |
| POST | `/api/setup` | 首次初始化（创建管理员 + 生成配置） |
| POST | `/api/auth/login` | 登录（带限流） |
| GET | `/api/auth/me` | 当前用户信息 |
| GET | `/api/animes` `/api/animes/:id` | 登录用户读取番剧 |
| POST/PUT/DELETE | `/api/animes` `/api/animes/:id` | 管理员增删改番剧 |
| GET | `/api/animes/:id/cover` | 番剧封面代理（解决移动端无法直连 Bangumi CDN） |
| POST | `/api/animes/:id/scan` | 扫描番剧剧集（同步；有剧集才按目录清未识别） |
| POST | `/api/animes/:id/rebind` | 管理员重绑 Bangumi 元数据（仅 Web 提供表单） |
| GET | `/api/animes/:id/episodes` | 番剧剧集列表 |
| POST | `/api/episodes/:id/media-token` | 签发当前集的 12 小时媒体票据 |
| GET | `/api/episodes/:id/stream` | 视频流（媒体票据鉴权，支持 HTTP Range） |
| GET | `/api/episodes/:id/subtitles` | 媒体票据鉴权的字幕轨道列表 / VTT 内容 |
| GET | `/api/progress/continue` | 继续看（登录；App 列表顶使用） |
| GET/POST | `/api/progress/:episode_id` | 获取/上报观看进度 |
| POST | `/api/library/scan` | 管理员启动全库扫描作业（立即返回，禁止当同步 600s） |
| GET | `/api/library/scan` | 管理员轮询全库扫描作业 |
| GET | `/api/library/unidentified` | 管理员读取持久化未识别列表 |
| GET | `/api/library/dirs` | 管理员列出一层子目录（手机添加页点选） |
| GET | `/api/bangumi/search` `/api/bangumi/subject/:id` | 管理员使用的 Bangumi 搜索/详情 |
| GET/PUT/DELETE | `/api/me/bangumi` | 绑定/查询/解除个人 Bangumi PAT |
| POST | `/api/me/bangumi/sync` | 入站同步看过（客户端超时 120s） |
| GET/POST/DELETE | `/api/admin/users` | 用户管理（仅管理员） |

## 下载

最新版本请前往 [Releases](https://github.com/1781415302/fan-web/releases) 页面下载。

| 文件 | 平台 | 说明 |
| --- | --- | --- |
| `fan-web-server-linux-amd64` | Linux x86_64 | 服务器部署 |
| `fan-web-server-linux-arm64` | Linux ARM64 | 树莓派/ARM 服务器 |
| `fan-web-server-darwin-arm64` | macOS Apple Silicon | Mac 本地运行 |
| `fan-web-server-windows-amd64.exe` | Windows x86_64 | Windows 本地运行 |
| `fan-web-app-*.apk` | Android | 移动端 App |
| `SHA256SUMS.txt` | - | 校验和 |

各平台二进制为静态无依赖单文件，内含完整前端资源，直接运行即可。**全新部署无需任何配置文件**；只有升级已有部署时才沿用现有 `config.yaml`（见上文「升级已有部署」）。

### 服务器端自动更新

- **支持平台**：Linux / macOS 上的 `fan-web-server` 可通过管理端的「检查更新」自动下载并替换自身二进制（校验 SHA256 后替换）。
- **Windows 不支持自动更新**：Windows 对正在运行的可执行文件加锁，无法改名替换，程序会直接提示「Windows 平台暂不支持自动更新，请到 GitHub Releases 手动下载...」。Windows 用户请到 [Releases](https://github.com/1781415302/fan-web/releases) 手动下载 `fan-web-server-windows-amd64.exe` 并替换可执行文件。
- 自动更新成功后，旧版本二进制保留为同目录下的 `<可执行文件>.old`；若本次升级有待执行的数据库迁移，还会额外保留一份迁移前的一致快照 `<数据库>.pre-migration.bak`。新版本**绑定到配置端口**后将 `.old` 晋升为 `.prev` 并删除 bak；端口回退或绑定失败时二者都保留。不是「任意成功 bind 就清理 `.old`」。

### 更新失败如何回滚

新版启动失败（数据库迁移失败、配置错误等）时，可手动回滚到旧版本：

1. 停止新版进程。
2. 若存在 `<数据库>.pre-migration.bak`，将其覆盖回 `<数据库>`，并删除 `<数据库>-wal`、`<数据库>-shm` 两个 SQLite WAL 伴随文件。
3. 用 `<可执行文件>.old` 替换当前可执行文件。
4. 启动旧版本。

该回滚方案覆盖两类失败场景：**迁移提交之前**（新版启动失败）与**迁移提交之后、端口绑定成功之前**（新版仍启动/绑定失败）。新版一旦成功绑定端口，`.old` 晋升为 `.prev`、bak 被删除；`.prev` 不是迁移后的受支持回滚路径。

### 残留 .old 与重试更新

- 若上一次更新留下 `<可执行文件>.old`（监听失败未晋升，或晋升未完成），回滚副本会残留在目录中。
- 下次触发更新时程序**不会覆盖已有 `.old`**，会返回「检测到更新残留备份 ...，请确认上一次更新已成功启动后手动删除再重试」。
- 处理方式：确认现场后**手动删除**残留的 `.old`，然后重新触发更新。成功绑定端口只会把 `.old` 晋升为 `.prev`，残留的 `.old` 仍需手动删除。

## 开源协议

本项目目前未附带 LICENSE 文件，代码仅供学习参考。
