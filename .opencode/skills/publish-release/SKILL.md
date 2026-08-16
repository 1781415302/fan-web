---
name: publish-release
description: 发布 fan-web 新版本到 GitHub Releases。使用场景：用户说"发布/发版/打 tag/发布新版本/做 release/submit release/v1.x.y"，或需要跨平台编译服务器二进制、构建 APK、写 release notes 时。同时适用于只发服务器版或只发 App 版的单端发布。必须严格遵循 AGENTS.md 的 R2 发布规范。
---

# fan-web 发布 Release 工作流

本 skill 指导如何将 fan-web（Go 服务器 + Flutter App）发布一个版本到 GitHub Releases。两边**共用同一个 tag**，但产物各自独立，可只发布其中一边。稳定的资产与更新契约见 `docs/更新与发布指南.md`；本文只维护可执行的发布步骤。

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
- 两个文件都要改，否则 App 端更新检测会误报或无法升级。

### 移动端发布签名（keystore）铁律
移动端正式发布**必须用 release 签名**（从 v1.2.2 起）。签名链：
- **keystore**：`mobile/android/app/fan-web-release.jks`
- **配置**：`mobile/android/key.properties`（含 storePassword / keyPassword / keyAlias=fanweb / storeFile=fan-web-release.jks）
- 两者都被 `.gitignore` 忽略（`mobile/android/key.properties`、`*.keystore`、`*.jks`），**绝不提交**。
- 若 `key.properties` 或 keystore 丢失：**无法再用原签名**，换新 keystore 会导致所有用户需卸载重装。务必提示用户异地备份。
- 构建时分别校验：用 `aapt dump badging` 检查 `versionName` / `versionCode`，用 `apksigner verify --print-certs` 检查证书 DN 为既有 release 证书而非 `CN=Android Debug`。

## 三、验证改动

按 `AGENTS.md` R3 依次：
```bash
(cd frontend && npm run test:run && npm run build)
(cd backend && go build ./... && go test ./...)
(cd mobile && flutter analyze lib/ && flutter test)
```
服务器 Release 执行前端和后端验证；App Release 执行移动端验证；同时发布则全部执行。最后确认 `git diff --check` 通过。

## 四、提交并推送代码（如有代码改动）

```bash
git add <改动的文件>
git commit -m "<贴切的中文提交信息>"
git push origin main
```
> 注意：构建出来的 `backend/web/dist/` 是构建产物，**不要 add**（见 AGENTS.md R1）。

## 五、编译发布产物

先按发布范围选择步骤：App-only 只跳过 5.1、5.2，**不跳过 5.4**；Server-only 跳过 5.3；两端发布则全部执行。

用唯一临时目录承载本次产物，避免旧版本文件混入：

```bash
RELEASE_DIR="$(mktemp -d /tmp/fan-web-release-vX.Y.Z-XXXXXX)"
echo "$RELEASE_DIR"
```

记录输出的绝对路径。后续命令中的 `$RELEASE_DIR` 均指这个路径；若工具的每次命令使用独立 shell，直接把记录的绝对路径代入，不依赖环境变量跨命令保留。

### 5.1 构建前端并嵌入 Go（包含服务器端时）
```bash
(cd frontend && npm run build)
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
(
  cd backend
  VERSION=vX.Y.Z
  CGO_ENABLED=0 GOOS=linux  GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.AppVersion=$VERSION" -o "$RELEASE_DIR/fan-web-server-linux-amd64" .
  CGO_ENABLED=0 GOOS=linux  GOARCH=arm64 go build -trimpath -ldflags "-s -w -X main.AppVersion=$VERSION" -o "$RELEASE_DIR/fan-web-server-linux-arm64" .
  CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-s -w -X main.AppVersion=$VERSION" -o "$RELEASE_DIR/fan-web-server-darwin-arm64" .
  CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.AppVersion=$VERSION" -o "$RELEASE_DIR/fan-web-server-windows-amd64.exe" .
)
```
校验版本注入：`chmod +x "$RELEASE_DIR/fan-web-server-linux-amd64" && "$RELEASE_DIR/fan-web-server-linux-amd64" -version` 输出应为 `vX.Y.Z`。

再校验嵌入的是真实前端（防止嵌入占位页事故）：
```bash
strings "$RELEASE_DIR/fan-web-server-linux-amd64" | grep -c "前端资源未构建"   # 必须为 0
strings "$RELEASE_DIR/fan-web-server-linux-amd64" | grep -c "index-"          # 应 > 0，确认含真实 assets
```

### 5.3 构建 APK（仅本次需出 App 时）
先确认 release 签名就绪：`mobile/android/key.properties` 存在（见第二节"移动端发布签名铁律"），否则 `flutter build` 会回退 debug 签名（证书 DN 为 `CN=Android Debug`），**不要**把 debug 签名 APK 当正式版发布。

```bash
(
  cd mobile
  flutter build apk --release
  cp build/app/outputs/flutter-apk/app-release.apk "$RELEASE_DIR/fan-web-app-vX.Y.Z.apk"
)
```
校验版本与签名：
```bash
AAPT=$(find /home/bishe/Android /usr -name "aapt" -type f 2>/dev/null | head -1)
SIGNER=$(find /home/bishe/Android -name "apksigner" -type f 2>/dev/null | head -1)
"$AAPT" dump badging "$RELEASE_DIR/fan-web-app-vX.Y.Z.apk" | grep '^package:'
"$SIGNER" verify --print-certs "$RELEASE_DIR/fan-web-app-vX.Y.Z.apk" | grep 'Signer #1 certificate DN'
```

### 5.4 生成校验和（按实际存在的产物；文本模式、两个空格，不用 `-b` / `*name`）
```bash
(cd "$RELEASE_DIR" && {
  shopt -s nullglob
  files=(fan-web-server* fan-web-app-*.apk)
  shopt -u nullglob
  [ ${#files[@]} -gt 0 ] || { echo "no assets to hash" >&2; exit 1; }
  sha256sum "${files[@]}" > SHA256SUMS.txt
})
```

## 六、打 tag 并发布

```bash
git tag vX.Y.Z
git push origin vX.Y.Z
```

把 release notes 写入 `$RELEASE_DIR/release-notes.md`（`## vX.Y.Z` + `### 新增`/`### 改进` + 下载表格），然后按本次实际产物上传。

**只发服务器端**（无 APK）：
```bash
gh release create vX.Y.Z --title "vX.Y.Z" --notes-file "$RELEASE_DIR/release-notes.md" \
  "$RELEASE_DIR"/fan-web-server-* "$RELEASE_DIR/SHA256SUMS.txt"
```
**只发移动端**（无服务器二进制）：
```bash
gh release create vX.Y.Z --title "vX.Y.Z" --notes-file "$RELEASE_DIR/release-notes.md" \
  "$RELEASE_DIR/fan-web-app-vX.Y.Z.apk" "$RELEASE_DIR/SHA256SUMS.txt"
```
**两端都发**：
```bash
gh release create vX.Y.Z --title "vX.Y.Z" --notes-file "$RELEASE_DIR/release-notes.md" \
  "$RELEASE_DIR"/fan-web-server-* "$RELEASE_DIR/fan-web-app-vX.Y.Z.apk" \
  "$RELEASE_DIR/SHA256SUMS.txt"
```

## 七、发布后检查

- `gh release view vX.Y.Z` 确认 tag、说明和本次应有的资产齐全，且没有混入其他版本文件。
- `curl -s https://api.github.com/repos/1781415302/fan-web/releases/latest` 的 `tag_name` 应为 `vX.Y.Z`。
- 只要本次生成了 `SHA256SUMS.txt`，执行 `cd "$RELEASE_DIR" && sha256sum -c SHA256SUMS.txt`。包含服务器端时，再抽查 Linux AMD64 的 `-version` 与真实前端嵌入状态。
- 包含 App 时，再次用 `aapt dump badging` 核对 `versionName` / `versionCode`，用 `apksigner verify --print-certs` 核对 release 签名。
- 按资产组合确认独立提示规则：只发服务器端时 App 不提示更新；只发 APK 时服务器端不提示更新。

## 附注
- 更新机制：服务器端只在 Release 有匹配当前平台的二进制时判定 `has_update`；移动端只在有 APK 时判定有更新。
- 不要 `git add backend/web/dist`、不要提交构建产物。
