# fan-web 项目记忆（AGENTS.md）

本文件是 **fan-web** 仓库的开发规范。任何开发工作（含 AI 助手）开始前必须阅读本文件。详细背景知识参见 `docs/` 目录下的开发指南。

## 一、项目事实（必须知道）

- **fan-web** 是自托管个人看番网站：Go 后端 + Vue3 前端一体编译成**单文件无依赖可执行程序**；另有 Flutter Android App（`mobile/`）。
- 发布与自动更新都通过 **GitHub Releases**：服务器端与移动端**共用同一个版本号 tag**（如 `v1.2.0`），但各自只下载**自己对应的产物**。
- 线上仓库：`github.com/1781415302/fan-web`，发布者 owner 为 `1781415302`。

## 二、开发前必须遵循的规则（铁律）

### R1. `backend/web/dist/` 是构建产物，绝不手改、绝不提交
- `backend/web/dist/` 整个目录被 `.gitignore` 忽略；由 `build.sh` 从 `frontend/dist` 拷贝生成。
- 仓库中**只 track 了占位文件 `backend/web/dist/index.html`**。构建后它会显示 `modified/deleted`，这是正常现象，**不要提交该文件**、不要 `git add backend/web/dist`。
- 需要真正构建产物时用 `./build.sh`，不手动 copy。

### R2. 版本号与发布规范（发布前必读）
- 版本号统一 `vX.Y.Z`（UTF-8 无 b）。当前版本语义：
  - Go 后端：`build.sh` / 交叉编译时通过 `-ldflags "-X main.AppVersion=vX.Y.Z"` 注入，默认 `dev`。
  - 移动端：`mobile/pubspec.yaml` 的 `version` 与 `mobile/lib/widgets/user_sheet.dart` 的 `appVersion` 常量，**两者必须同步**且与要打的 tag 一致。
- **`mobile/pubspec.yaml` 的 build number（`+号后的整数`）= Android versionCode，每次发布必须递增**，否则老用户无法原地升级安装。
- 发布产物命名（对齐既有 Release）：
  - 服务器：`fan-web-server-{goos}-{goarch}`（Windows 加 `.exe`），平台为 linux-amd64 / linux-arm64 / darwin-arm64 / windows-amd64，共 4 个。
  - 移动端：`fan-web-app-vX.Y.Z.apk`。
  - 校验和：`SHA256SUMS.txt`（对齐 v1.1.1 格式，只含 4 个服务器二进制的 sha256）。
- 每个 Release 必须**两个端各自独立判断**是否提示更新：
  - 服务器端只在 Release 附带当前平台二进制时 `has_update=true`；
  - 移动端只在 Release 附带 APK 时认为有更新。
  - 因此：**只发服务器产物 → App 不弹更新；只发 APK → 服务器不弹更新**。发布时按实际改动决定附不带哪类产物。
  - 移动端必须用 release 签名：keystore `mobile/android/app/fan-web-release.jks` + 配置 `mobile/android/key.properties`（已 gitignore，绝不提交；丢失则无法再签名）。无 key.properties 时 `flutter build` 会回退 debug 签名，勿将 debug 签名包当正式版发布。

### R3. 提交前验证
- 后端改动：`cd backend && go build ./... && go test ./...`。
- 前端改动：`cd frontend && npm run build`（含 `vue-tsc -b` 类型检查）。
- 移动端改动：`cd mobile && flutter analyze lib/`（必要时 `flutter test`）。
- 发布流程执行 `./build.sh` 验证单文件可构建。

### R4. 不要误提交底下这些杂项文件
`.agents/`、`.commandcode/`、`fan-web.code-workspace`、`mobile/icon.png:Zone.Identifier` 是本地工具残留，不入库。

### R5. 阶段交接与长期文档必须同步
- `docs/` 可以创建阶段计划、执行交接和验收记录，用于明确范围、实现方案与完成状态。
- 阶段工作改变了当前系统行为、接口、配置、构建或发布契约时，完成前必须同步更新对应长期文档，不能只把新事实留在交接文档里。
- 阶段文档完成使命后可按实际需要保留或清理；长期有效的事实以 `docs/开发指南.md`、`docs/移动端开发指南.md` 和 `docs/更新与发布指南.md` 为准。

## WSL 工具链（开发环境事实）

- Go：`/home/bishe/go/bin/go`（go1.26.5 linux/amd64）
- Node/npm：nvm 管理，`/home/bishe/.nvm/versions/node/v26.7.0/bin/`（node v26.7.0）
- Flutter/Dart：`/home/bishe/flutter/bin/`（3.44.8 stable）
- 上述均在 PATH 中直接可用。`build.sh` / `dev.sh` 里 `/tmp/fan-web-node/bin`、`/tmp/fan-web-go/bin` 等旧路径**已失效**，改动时不要依赖它们；直接用系统 PATH 的 go/node/flutter。
- `go env GOPATH` 与 GOROOT 相同（均为 `/home/bishe/go`），`go build` 会打印一条 warning，属正常，不影响功能。
- `build.sh` 会在编译前自动 `npm run build` 并重新嵌入 `backend/web/dist`，用 WSL 原生工具链执行即可。

## 常用命令（速查）

```bash
# 前端（需 node 在 PATH；nvm 管理）
cd frontend && npm run build        # vue-tsc -b + vite build
cd frontend && npm run test:run     # Vitest（router/auth/theme 基线）
# 后端（需 go 在 PATH）
cd backend && go build ./...
cd backend && go test ./...
cd backend && go test -race ./...
# 移动端（需 Flutter/Dart 在 PATH）
cd mobile && flutter analyze
cd mobile && flutter test
cd mobile && flutter build apk --release
# 构建单文件服务器（产出 dist/fan-web-server）
./build.sh
# 本地开发运行（前后端）
./dev.sh
```

## 详阅参考
- `docs/README.md`（文档索引与维护约定）
- `docs/开发指南.md`（服务器端 + Web 前端）
- `docs/移动端开发指南.md`（Flutter App）
- `docs/更新与发布指南.md`（双端更新协议与 Release 规范）
- 发布完整流程：用 opencode 的发布 skill（`.opencode/skills/publish-release`）
