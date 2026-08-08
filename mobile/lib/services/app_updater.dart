import 'dart:io';

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
  });

  final bool hasUpdate;
  final String currentVersion;
  final String latestVersion;
  final String releaseNotes;
  final String? downloadUrl;
  final int? downloadSize;
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
  final dio = Dio(BaseOptions(connectTimeout: const Duration(seconds: 10)));
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
  final hasUpdate = isNewerVersion(currentVersion, tagName);
  String? downloadUrl;
  int? downloadSize;
  if (hasUpdate) {
    final assets = (data['assets'] as List?) ?? [];
    for (final item in assets) {
      if (item is! Map) continue;
      final name = (item['name'] as String?) ?? '';
      if (name.startsWith('fan-web-app-') && name.endsWith('.apk')) {
        downloadUrl = item['browser_download_url'] as String?;
        final size = item['size'];
        if (size is int) downloadSize = size;
        if (size is num) downloadSize = size.toInt();
        break;
      }
    }
  }
  return UpdateCheckResult(
    hasUpdate: hasUpdate,
    currentVersion: currentVersion,
    latestVersion: tagName,
    releaseNotes: body,
    downloadUrl: downloadUrl,
    downloadSize: downloadSize,
  );
}

Future<String> downloadAndInstallApk(
  String url, {
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
  final result = await OpenFilex.open(filePath);
  if (result.type != ResultType.done) {
    throw Exception(result.message);
  }
  return filePath;
}
