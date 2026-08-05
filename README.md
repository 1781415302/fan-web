# fan-web 看番网站

一个自托管的个人看番网站。将本地番剧目录自动扫描入库，提供番剧管理、视频在线播放、观看进度记录、用户认证等功能。前后端一体化编译，部署只需一个可执行文件。

## 功能特性

- **番剧管理**：浏览、添加、编辑、删除番剧，支持番剧库扫描
- **自动扫描**：从视频根目录自动识别番剧与剧集（支持 `[01]`、`EP01`、`第1集`、`S1E1` 等常见命名格式）
- **在线播放**：浏览器直接播放本地视频（基于 ArtPlayer）
- **观看进度**：自动记录每位用户的观看进度，续播提醒
- **番组信息**：集成 Bangumi 搜索与番剧详情
- **用户系统**：JWT 认证、登录限流，管理员可管理用户
- **单文件部署**：前端资源嵌入后端二进制，无需 nginx

## 技术栈

| 端 | 技术 |
| --- | --- |
| 后端 | Go + Gin + SQLite（modernc，纯 Go 无 CGO）+ JWT + bcrypt |
| 前端 | Vue 3 + Vite + TypeScript + Pinia + vue-router + ArtPlayer |

## 目录结构

```
.
├── backend/               # Go 后端
│   ├── config/            # 配置加载
│   ├── database/          # SQLite 数据访问
│   ├── handlers/          # HTTP 处理
│   ├── middleware/        # JWT / CORS / 限流
│   ├── models/            # 数据模型
│   ├── services/          # 业务逻辑（扫描、番组、库）
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
├── docs/                  # 需求与阶段设计文档
├── dev.sh                 # 开发运行脚本
└── build.sh               # 一键构建单文件可执行程序
```

## 快速开始（开发）

环境要求：Go 1.21+、Node.js 18+（含 npm）。

```bash
# 启动后端（默认监听 :8080）
./dev.sh backend

# 另开终端启动前端（默认 :5173，/api 代理到 8080）
./dev.sh frontend
```

浏览器访问 http://localhost:5173 即可，前端开发服务器会自动将 `/api` 请求代理到后端。

## 构建单个可执行文件

```bash
./build.sh
```

脚本依次执行：前端构建（`vue-tsc && vite build`）→ 拷贝到 `backend/web/dist` → 后端交叉编译（`CGO_ENABLED=0`），最终产物为 `dist/fan-web-server`，已包含全部前端资源。

> 构建会覆盖 `backend/web/dist/` 下由 git 跟踪的占位文件，`git status` 显示其 modified 属正常现象。

## 部署到 VPS

1. 在 VPS 上构建（推荐，避免本机环境差异），或上传本机构建的产物：

   ```bash
   ./build.sh
   ```

2. 将可执行文件与配置一起部署：

   ```bash
   mkdir -p /opt/fan-web && cd /opt/fan-web
   cp <产物路径>/fan-web-server .
   cp backend/config.yaml .
   # 修改 config.yaml 中的视频目录等配置
   ```

3. 运行：

   ```bash
   ./fan-web-server
   ```

访问 `http://<vps-ip>:8080`，使用管理员账号登录。

## 配置说明

配置文件 `backend/config.yaml`：

```yaml
server:
  port: 8080
  mode: debug          # 生产环境建议改为 release

database:
  path: ./data/fan-web.db

jwt:
  secret: "..."        # 生产环境务必修改为随机密钥
  expire: 168h

admin:
  username: admin
  password: admin123   # 首次启动创建管理员时生效，请及时修改

video:
  root_path: ../anime  # 番剧视频根目录
```

> **注意**：`admin` 账号仅在数据库中尚无管理员时创建（`database/db.go`）。已运行过的实例修改 `config.yaml` 不会更新已有账号密码，请通过 `backend/data/fan-web.db` 处理或直接调用管理接口。

## 主要 API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/health` | 健康检查 |
| POST | `/api/auth/login` | 登录（带限流） |
| GET | `/api/auth/me` | 当前用户信息 |
| GET/POST/PUT/DELETE | `/api/animes` `/api/animes/:id` | 番剧增删改查 |
| POST | `/api/animes/:id/scan` | 扫描番剧剧集 |
| GET | `/api/animes/:id/episodes` | 番剧剧集列表 |
| GET | `/api/episodes/:id/stream` | 视频流（无需认证） |
| GET/POST | `/api/progress/:episode_id` | 获取/上报观看进度 |
| POST | `/api/library/scan` | 扫描视频目录入库 |
| GET | `/api/bangumi/search` `/api/bangumi/subject/:id` | Bangumi 搜索/详情 |
| GET/POST/DELETE | `/api/admin/users` | 用户管理（仅管理员） |

## 开源协议

本项目目前未附带 LICENSE 文件，代码仅供学习参考。
