---
name: publish-release
description: 发布 fan-web 新版本到 GitHub Releases。使用场景：用户说"发布/发版/打 tag/发布新版本/做 release/submit release/v1.x.y"，或需要跨平台编译服务器二进制、构建 APK、写 release notes 时。同时适用于只发服务器版或只发 App 版的单端发布。必须严格遵循 AGENTS.md 的 R2 发布规范。
---

# fan-web 发布 Release 工作流

本 skill 指导如何将 fan-web（Go 服务器 + Flutter App）发布一个版本到 GitHub Releases。两边**共用同一个 tag**，但产物各自独立，可只发布其中一边。

> 前置：本机需有 `go`、`node`/`npm`、`flutter`、`gh` 并已登录（`gh auth status`）。参照 `AGENTS.md`。

## 一、版本号决策

- 当前最新版本用 `gh release list` 查看（或 `git tag --sort=-creatordate | head`）。
- 递增规则：修复/小改进 → patch（`v1.1.1`→`v1.1.2`）；新增功能 → minor（`v1.1.1`→`v1.2.0`）。不与既有 tag 重复。
- **新版本 = 本次要打的 tag，例如 `v1.2.0`**。

## 二、代码改动的版本同步

先确认要发布的构思里涉及哪些端，分别处理：

### 服务器端（Go/前端）
- 服务器版本在**构建时**由 `-ldflags "-X main.AppVersion=vX.Y.Z"` 注入，源码无需改版本号。

### 移动端（Flutter）
- **`mobile/pubspec.yaml`**：`version: X.Y.Z+N`，其中：
  - `X.Y.Z` 与本次 tag 一致（如 `1.2.0`）；
  - **`N`（build number = Android versionCode）必须比上次大，至少 +1**，否则老用户无法原地升级。
- **`mobile/lib/widgets/user_sheet.dart`** 的 `const appVersion = 'X.Y.Z'` 必须与 `pubspec.yaml` 同步（App 内"检查更新"用它比对）。
- 两个文件都要改，否者 App 端更新检测会误报或无法升级。

### 移动端发布签名（keystore）铁律
移动端正式发布**必须用 release 签名**（从 v1.2.2 起）。签名链：
- **keystore**：`mobile/android/app/fan-web-release.jks`
- **配置**：`mobile/android/key.properties`（含 storePassword / keyPassword / keyAlias=fanweb / storeFile=fan-web-release.jks）
- 两者都被 `.gitignore` 忽略（`mobile/android/key.properties`、`*.keystore`、`*.jks`），**绝不提交**。
- 若 `key.properties` 或 keystore 丢失：**无法再用原签名**，换新 keystore 会导致所有用户需卸载重装。务必提示用户异地备份。
- 构建时校验：`aapt dump badging <apk> | grep package:` 的签名证书 DN 应为 `CN=fan-web, ...`（release），而非 `CN=Android Debug`。用 `apksigner verify --print-certs <apk>` 查看。

## 三、验证改动

按 `AGENTS.md` R3 依次：
```bash
cd frontend && npm run build            # 前端（vue-tsc + vite）
cd backend && go build ./... && go test ./...
cd mobile && flutter analyze lib/
```
改动两侧代码时全部跑；只改一边则跑对应命令。`git diff --check` 通过。

## 四、提交并推送代码（如有代码改动）

```bash
git add <改动的文件>
git commit -m "<贴切的中文提交信息>"
git push origin main
```
> 注意：构建出来的 `backend/web/dist/` 是构建产物，**不要 add**（见 AGENTS.md R1）。

## 五、编译发布产物

准备一个干净的产物目录（例如 `/tmp/rel-vX.Y.Z/`）：

```bash
mkdir -p /tmp/rel-vX.Y.Z
```

### 5.1 构建前端并嵌入 Go
```bash
cd frontend && npm run build
cd ..
rm -rf backend/web/dist && cp -r frontend/dist backend/web/dist
```

> ⚠️ **顺序铁律（曾致发布事故）**：`backend/web/dist/` 现在必须是**真实构建产物**。从这一刻起到 5.2 交叉编译全部结束之前，**一律不要执行 `git checkout -- backend/web/dist/index.html`、不要 `git add` 该文件、不要执行任何会恢复占位文件的命令**。占位文件只在发布结束后、清理工作区时才能恢复。
> 一旦在交叉编译前把 `index.html` 恢复为占位版，4 个平台二进制都会嵌入"前端资源未构建"的占位页，出现"更新后页面显示 前端资源未构建"。若发生此类事故：
> 1. 重新执行 `rm -rf backend/web/dist && cp -r frontend/dist backend/web/dist`；
> 2. **立即**重跑 5.2 交叉编译（趁 backend/web/dist 还是真实产物）；
> 3. `gh release delete-asset` 清理坏资产后重新 upload，并重算 SHA256SUMS。

### 5.2 交叉编译 4 个服务器二进制
在 `backend/` 下分别执行（`VERSION=vX.Y.Z`）：

```bash
VERSION=vX.Y.Z
CGO_ENABLED=0 GOOS=linux  GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.AppVersion=$VERSION" -o /tmp/rel-vX.Y.Z/fan-web-server-linux-amd64 .
CGO_ENABLED=0 GOOS=linux  GOARCH=arm64 go build -trimpath -ldflags "-s -w -X main.AppVersion=$VERSION" -o /tmp/rel-vX.Y.Z/fan-web-server-linux-arm64 .
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-s -w -X main.AppVersion=$VERSION" -o /tmp/rel-vX.Y.Z/fan-web-server-darwin-arm64 .
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.AppVersion=$VERSION" -o /tmp/rel-vX.Y.Z/fan-web-server-windows-amd64.exe .
```
校验版本注入：`chmod +x /tmp/rel-vX.Y.Z/fan-web-server-linux-amd64 && /tmp/rel-vX.Y.Z/fan-web-server-linux-amd64 -version` 输出应为 `vX.Y.Z`。

再校验嵌入的是真实前端（防止嵌入占位页事故）：
```bash
strings /tmp/rel-vX.Y.Z/fan-web-server-linux-amd64 | grep -c "前端资源未构建"   # 必须为 0
strings /tmp/rel-vX.Y.Z/fan-web-server-linux-amd64 | grep -c "index-"          # 应 > 0，确认含真实 assets
```

### 5.3 构建 APK（仅本次需出 App 时）
先确认 release 签名就绪：`mobile/android/key.properties` 存在（见第二节"移动端发布签名铁律"），否则 `flutter build` 会回退 debug 签名（证书 DN 为 `CN=Android Debug`），**不要**把 debug 签名 APK 当正式版发布。

```bash
cd mobile && flutter build apk --release
cp build/app/outputs/flutter-apk/app-release.apk /tmp/rel-vX.Y.Z/fan-web-app-vX.Y.Z.apk
```
校验版本与签名：
```bash
AAPT=$(find /home/bishe/Android /usr -name "aapt" -type f 2>/dev/null | head -1)
SIGNER=$(find /home/bishe/Android -name "apksigner" -type f 2>/dev/null | head -1)
"$AAPT" dump badging <apk> | grep '^package:'                   # versionName / versionCode
"$SIGNER" verify --print-certs <apk> | grep 'Signer #1 certificate DN'   # 应为 CN=fan-web（release 签名）
```

### 5.4 生成校验和（只含 4 个服务器二进制，不含 APK，对齐 v1.1.1）
```bash
cd /tmp/rel-vX.Y.Z && sha256sum fan-web-server* > SHA256SUMS.txt
```

## 六、打 tag 并发布

```bash
git tag vX.Y.Z
git push origin vX.Y.Z
```

编写 release notes（`## vX.Y.Z` + `### 新增`/`### 改进` + 下载表格），然后按本次实际产物上传。

**只发服务器端**（无 APK）：
```bash
gh release create vX.Y.Z --title "vX.Y.Z" --notes-file notes.md \
  <4 个 server 二进制> SHA256SUMS.txt
```
**只发移动端**（无服务器二进制）：
```bash
gh release create vX.Y.Z --title "vX.Y.Z" --notes-file notes.md \
  fan-web-app-vX.Y.Z.apk
```
**两端都发**：上传上面全部 6 个文件。

## 七、发布后检查

- `gh release view vX.Y.Z` 确认产物齐全。
- `curl -s https://api.github.com/repos/1781415302/fan-web/releases/latest` 的 `tag_name` 应为 `vX.Y.Z`。
- **更新提示联动确认**（见 AGENTS.md R2）：本次只发了服务器端 → 移动端不会弹更新；只发 APK → 服务器端不会弹更新。

## 附注
- 更新机制：服务器端只在 Release 有匹配当前平台的二进制时判定 `has_update`；移动端只在有 APK 时判定有更新。
- 不要 `git add backend/web/dist`、不要提交构建产物。