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
      final msg = e is DioException && e.type == DioExceptionType.cancel
          ? '已取消下载'
          : '下载失败: $e';
      setState(() {
        _downloading = false;
        _error = msg;
      });
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
        FilledButton(
          onPressed: _downloading ? null : _startDownload,
          child: Text(_downloading ? '下载中...' : '立即更新'),
        ),
      ],
    );
  }
}
