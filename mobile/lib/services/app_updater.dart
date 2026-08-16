import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:dio/dio.dart';
import 'package:open_filex/open_filex.dart';
import 'package:path_provider/path_provider.dart';

const _githubOwner = '1781415302';
const _githubRepo = 'fan-web';
const _githubApi =
    'https://api.github.com/repos/$_githubOwner/$_githubRepo/releases/latest';

class UpdateCheckResult {
  const UpdateCheckResult({
    required this.hasUpdate,
    required this.currentVersion,
    required this.latestVersion,
    required this.releaseNotes,
    this.downloadUrl,
    this.downloadSize,
    this.sha256sumsUrl,
  });

  final bool hasUpdate;
  final String currentVersion;
  final String latestVersion;
  final String releaseNotes;
  final String? downloadUrl;
  final int? downloadSize;
  final String? sha256sumsUrl;
}

bool isNewerVersion(String current, String latest) {
  List<int> normalize(String v) {
    v = v.trim();
    if (v.startsWith('v') || v.startsWith('V')) v = v.substring(1);
    if (v.isEmpty || v == 'dev') return [0, 0, 0];
    v = v.split('-').first.split('+').first;
    return v.split('.').map((p) {
      final n = int.tryParse(p.trim());
      return n ?? 0;
    }).toList();
  }

  final c = normalize(current);
  final l = normalize(latest);
  final maxLen = c.length > l.length ? c.length : l.length;
  for (var i = 0; i < maxLen; i++) {
    final cv = i < c.length ? c[i] : 0;
    final lv = i < l.length ? l[i] : 0;
    if (lv > cv) return true;
    if (lv < cv) return false;
  }
  return false;
}

Future<UpdateCheckResult> checkAppUpdate(String currentVersion) async {
  final dio = Dio(BaseOptions(
    connectTimeout: const Duration(seconds: 10),
    // 服务器（或中间网络）连接建立后不返回响应时避免请求无限挂起，
    // 与项目其他 HTTP 入口（ApiClient、checkHealth）的超时口径一致。
    receiveTimeout: const Duration(seconds: 10),
  ));
  final resp = await dio.get<dynamic>(
    _githubApi,
    options: Options(headers: {
      'Accept': 'application/vnd.github+json',
      'User-Agent': 'fan-web-app',
    }),
  );
  final data = resp.data as Map<String, dynamic>;
  final tagName = (data['tag_name'] as String?) ?? '';
  final body = (data['body'] as String?) ?? '';
  if (tagName.isEmpty) throw const FormatException('更新信息异常');
  final hasNewerTag = isNewerVersion(currentVersion, tagName);
  String? downloadUrl;
  int? downloadSize;
  String? sha256sumsUrl;
  if (hasNewerTag) {
    final assets = (data['assets'] as List?) ?? [];
    for (final item in assets) {
      if (item is! Map) continue;
      final name = (item['name'] as String?) ?? '';
      final lowerName = name.toLowerCase();
      // 查找发布附带的校验和文件（对齐服务器端 findSHA256Asset 的匹配口径）。
      if (sha256sumsUrl == null &&
          (lowerName == 'sha256sums.txt' ||
              lowerName == 'sha256sums' ||
              lowerName.startsWith('sha256'))) {
        sha256sumsUrl = item['browser_download_url'] as String?;
      }
      if (name.startsWith('fan-web-app-') && name.endsWith('.apk')) {
        downloadUrl = item['browser_download_url'] as String?;
        final size = item['size'];
        if (size is int) downloadSize = size;
        if (size is num) downloadSize = size.toInt();
      }
    }
  }
  // 服务器端与移动端共用同一个 Release 版本号。
  // 仅当本次发布附带 APK 时才视为移动端“有更新”，
  // 纯服务器端发布不会在 App 内弹出更新提示。
  final hasUpdate = hasNewerTag && downloadUrl != null;
  return UpdateCheckResult(
    hasUpdate: hasUpdate,
    currentVersion: currentVersion,
    latestVersion: tagName,
    releaseNotes: body,
    downloadUrl: downloadUrl,
    downloadSize: downloadSize,
    sha256sumsUrl: sha256sumsUrl,
  );
}

/// 下载成功但系统安装器启动失败时抛出。
/// 携带已下载 APK 的路径，UI 可用它提供“直接打开安装包、不再重新下载”的重试路径。
class ApkInstallException implements Exception {
  const ApkInstallException(this.message, this.filePath);

  final String message;
  final String filePath;

  @override
  String toString() => message;
}

Future<String> downloadAndInstallApk(
  String url, {
  int? expectedSize,
  String? sha256sumsUrl,
  void Function(int received, int total)? onProgress,
  CancelToken? cancelToken,
}) async {
  final dir = await getTemporaryDirectory();
  final filePath = '${dir.path}/fan-web-update.apk';
  final dio = Dio();
  await dio.download(
    url,
    filePath,
    options: Options(headers: {'User-Agent': 'fan-web-app'}),
    onReceiveProgress: onProgress,
    cancelToken: cancelToken,
    deleteOnError: true,
  );
  final file = File(filePath);
  if (!await file.exists()) throw const FileSystemException('下载文件不存在');
  // 交给系统安装器前先做完整性校验（GitHub 声明的 size + SHA256SUMS.txt），
  // 与服务器端自更新（backend/services/updater.go PerformUpdate）口径对齐。
  await _verifyDownloadIntegrity(file, url, expectedSize, sha256sumsUrl);
  final result = await OpenFilex.open(filePath);
  if (result.type != ResultType.done) {
    throw ApkInstallException(result.message, filePath);
  }
  return filePath;
}

/// 直接打开已下载的安装包（不重新下载），供安装器启动失败后重试。
Future<void> openDownloadedApk(String filePath) async {
  final file = File(filePath);
  if (!await file.exists()) {
    throw const FileSystemException('安装包文件不存在，请重新下载');
  }
  final result = await OpenFilex.open(filePath);
  if (result.type != ResultType.done) {
    throw ApkInstallException(result.message, filePath);
  }
}

/// 校验下载文件的完整性：
/// 1. 若 GitHub 声明了 size，比对文件大小，不符视为下载损坏；
/// 2. 若发布附带 SHA256SUMS.txt 且其中含本 APK 的条目，比对 sha256。
/// 校验失败时删除已下载文件并抛异常，由调用方提示重新下载。
Future<void> _verifyDownloadIntegrity(
  File file,
  String url,
  int? expectedSize,
  String? sha256sumsUrl,
) async {
  var failReason = '';
  if (expectedSize != null && expectedSize > 0) {
    final actualSize = await file.length();
    if (actualSize != expectedSize) {
      failReason =
          '文件大小不匹配（GitHub 声明 $expectedSize 字节，实际 $actualSize 字节）';
    }
  }
  if (failReason.isEmpty && sha256sumsUrl != null && sha256sumsUrl.isNotEmpty) {
    String? expectedHash;
    try {
      expectedHash = await _lookupApkSha256(sha256sumsUrl, url);
    } catch (_) {
      // 校验和文件获取失败时跳过哈希比对，size 校验仍生效。
    }
    if (expectedHash != null) {
      final digest = await sha256.bind(file.openRead()).first;
      if (digest.toString() != expectedHash.toLowerCase()) {
        failReason = 'SHA256 校验失败';
      }
    }
  }
  if (failReason.isNotEmpty) {
    try {
      await file.delete();
    } catch (_) {
      // 删除失败不掩盖校验失败本身。
    }
    throw Exception('安装包校验失败（$failReason），已删除，请重新下载');
  }
}

/// 从 SHA256SUMS.txt 中查找本 APK 资产对应的 sha256。
/// APK 行现为必填并参与哈希校验；SUMS 获取或解析失败时仍 fail-open
/// （跳过哈希比对，size 校验仍生效）。
Future<String?> _lookupApkSha256(String sha256sumsUrl, String downloadUrl) async {
  final dio = Dio(BaseOptions(
    connectTimeout: const Duration(seconds: 10),
    receiveTimeout: const Duration(seconds: 10),
  ));
  final resp = await dio.get<String>(
    sha256sumsUrl,
    options: Options(
      headers: {'User-Agent': 'fan-web-app'},
      // 校验和文件是纯文本，需按 plain 读取，避免 JSON 解析失败。
      responseType: ResponseType.plain,
    ),
  );
  final assetName = downloadUrl.split('/').last;
  final content = resp.data ?? '';
  if (assetName.isEmpty || content.isEmpty) return null;
  for (final line in content.split('\n')) {
    final trimmed = line.trim();
    if (trimmed.isEmpty) continue;
    final parts = trimmed.split(RegExp(r'\s+'));
    if (parts.length < 2) continue;
    final entryName = parts.sublist(1).join(' ');
    if (entryName == assetName || entryName.endsWith('/$assetName')) {
      return parts[0];
    }
  }
  return null;
}
