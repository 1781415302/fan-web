import 'package:dio/dio.dart';
import 'package:flutter/material.dart';

import '../services/app_updater.dart';

Future<void> showUpdateDialog(
  BuildContext context,
  UpdateCheckResult result,
) {
  return showDialog<void>(
    context: context,
    builder: (context) => _UpdateDialog(result: result),
  );
}

class _UpdateDialog extends StatefulWidget {
  const _UpdateDialog({required this.result});
  final UpdateCheckResult result;

  @override
  State<_UpdateDialog> createState() => _UpdateDialogState();
}

class _UpdateDialogState extends State<_UpdateDialog> {
  bool _downloading = false;
  int _received = 0;
  int _total = 0;
  String? _error;
  CancelToken? _cancelToken;
  // 下载成功但安装器启动失败时记录已下载 APK 路径，用于“直接打开安装包”。
  String? _apkPath;

  @override
  void dispose() {
    _cancelToken?.cancel();
    super.dispose();
  }

  Future<void> _startDownload() async {
    final url = widget.result.downloadUrl;
    if (url == null || url.isEmpty) {
      setState(() => _error = '未找到安装包下载地址');
      return;
    }
    setState(() {
      _downloading = true;
      _error = null;
      _received = 0;
      _total = widget.result.downloadSize ?? 0;
    });
    _cancelToken = CancelToken();
    try {
      await downloadAndInstallApk(
        url,
        cancelToken: _cancelToken,
        expectedSize: widget.result.downloadSize,
        sha256sumsUrl: widget.result.sha256sumsUrl,
        onProgress: (received, total) {
          if (!mounted) return;
          setState(() {
            _received = received;
            _total = total;
          });
        },
      );
      if (!mounted) return;
      Navigator.of(context).pop();
    } catch (e) {
      if (!mounted) return;
      if (e is ApkInstallException) {
        // APK 已完整下载，只是安装器启动失败：归因到安装环节，
        // 提示手动安装，并提供不再重新下载的直接打开路径。
        setState(() {
          _downloading = false;
          _apkPath = e.filePath;
          _error = '安装包已下载，请手动安装';
        });
        return;
      }
      final msg = e is DioException && e.type == DioExceptionType.cancel
          ? '已取消下载'
          : '下载失败: $e';
      setState(() {
        _downloading = false;
        _error = msg;
      });
    }
  }

  /// 直接打开已下载的 APK（不再重新下载），安装器启动失败后的重试路径。
  Future<void> _openApk() async {
    final path = _apkPath;
    if (path == null || path.isEmpty) return;
    try {
      await openDownloadedApk(path);
      if (!mounted) return;
      Navigator.of(context).pop();
    } catch (e) {
      if (!mounted) return;
      setState(() => _error = '打开安装包失败: $e');
    }
  }

  @override
  Widget build(BuildContext context) {
    final progress = _total > 0 ? _received / _total : 0.0;
    final percent = (_total > 0 ? progress * 100 : 0).toStringAsFixed(0);
    return AlertDialog(
      title: Text('发现新版本 ${widget.result.latestVersion}'),
      content: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('当前版本: ${widget.result.currentVersion}'),
            const SizedBox(height: 8),
            if (widget.result.releaseNotes.isNotEmpty) ...[
              const Text('更新内容:', style: TextStyle(fontWeight: FontWeight.w600)),
              const SizedBox(height: 4),
              Text(widget.result.releaseNotes),
              const SizedBox(height: 12),
            ],
            if (_downloading) ...[
              LinearProgressIndicator(value: _total > 0 ? progress : null),
              const SizedBox(height: 8),
              Text(_total > 0 ? '$percent% ($_received / $_total)' : '下载中...'),
            ],
            if (_error != null) ...[
              const SizedBox(height: 8),
              Text(_error!, style: const TextStyle(color: Colors.red)),
            ],
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: _downloading ? null : () => Navigator.of(context).pop(),
          child: const Text('稍后'),
        ),
        if (_apkPath != null)
          FilledButton(
            onPressed: _downloading ? null : _openApk,
            child: const Text('打开安装包'),
          )
        else
          FilledButton(
            onPressed: _downloading ? null : _startDownload,
            child: Text(_downloading ? '下载中...' : '立即更新'),
          ),
      ],
    );
  }
}
