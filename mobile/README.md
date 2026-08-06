# fan-web Mobile

fan-web Android 客户端：配置服务器地址后登录，浏览番剧库、查看详情、播放 HEVC/mkv 视频（含内封字幕切换、手势控制、断点续播、进度上报）。登录凭证保存在设备本地，启动时自动校验并恢复会话。

## 功能概览

- **认证**：服务器地址 + 登录合一页，token 持久化，401 自动回登录页
- **番剧列表**：分页网格、封面异步加载（后端代理）、观看进度条、下拉刷新
- **番剧详情**：简介展开/收起、集数网格、每集已看/进行中/未看标记、继续播放
- **播放器**：media_kit（libmpv）硬解播放、内封字幕切换与字号调节、手势控制（快进快退/音量/亮度/双击暂停）、断点续播、进度节流上报
- **体验**：用户菜单（账号信息 + 版本号 + 登出）、统一加载/错误/空态、网络异常分类提示、深色主题对齐 Web 端设计系统

## 开发环境

所有命令在 WSL Ubuntu 内执行，并使用 WSL 侧 Flutter SDK：

```bash
export PATH="$HOME/Android/Sdk/platform-tools:$HOME/flutter/bin:$PATH"
export FLUTTER_STORAGE_BASE_URL="https://storage.flutter-io.cn"
cd /home/bishe/work/fan-web/mobile
flutter pub get
flutter analyze
NO_PROXY=127.0.0.1,localhost flutter test
```

后端需要监听手机可访问的地址，例如在仓库根目录启动后端：

```bash
cd /home/bishe/work/fan-web
./dev.sh backend
```

登录页填写后端主机地址，不要附加 `/api`。使用 HTTP 地址时，Android 已配置允许明文流量。

## 真机调试

手机开启无线调试（Android 11+），在 WSL 内连接：

```bash
adb pair <ip>:<pair_port>   # 输入配对码
adb connect <ip>:<connect_port>
flutter devices              # 确认设备已识别
flutter run                  # 热重载开发
```

## 构建与安装

### Debug APK

```bash
cd /home/bishe/work/fan-web/mobile
flutter build apk --debug
```

APK 位于 `build/app/outputs/flutter-apk/app-debug.apk`。

### Release APK

#### 1. 生成签名密钥（仅需一次）

```bash
keytool -genkey -v -keystore /home/bishe/fan-web-release.keystore \
  -alias fanweb -keyalg RSA -keysize 2048 -validity 10000
```

输入两次密码并记住。**keystore 文件务必备份，丢失后无法给旧版本发覆盖更新。**

#### 2. 创建签名配置文件

在 `mobile/android/` 下创建 `key.properties`（该文件已被 `.gitignore` 忽略，不会入库）：

```properties
storePassword=<你的密码>
keyPassword=<你的密码>
keyAlias=fanweb
storeFile=/home/bishe/fan-web-release.keystore
```

#### 3. 构建

```bash
flutter build apk --release
```

> 若 `key.properties` 不存在，构建会自动回退到 debug 签名，仍可用于测试安装。

#### 4. 安装

```bash
adb install -r build/app/outputs/flutter-apk/app-release.apk
```

> release 与 debug 签名不同，覆盖安装需先卸载旧版本：`adb uninstall com.fanweb.fan_web`
