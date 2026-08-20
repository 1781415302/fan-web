import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/bangumi_me_api.dart';
import '../providers/auth_provider.dart';
import '../services/app_updater.dart';
import '../theme/app_theme.dart';
import '../utils/api_error.dart';
import 'update_dialog.dart';

/// App 版本号，与 pubspec.yaml 的 version 保持同步。
const appVersion = '1.4.0';

const _bangumiTokenSource = 'https://next.bgm.tv/demo/access-token';

/// 显示用户底部菜单：用户名、管理员徽章、服务器地址、版本号、Bangumi 令牌、退出登录。
Future<void> showUserSheet(BuildContext context, WidgetRef ref) async {
  final authState = ref.read(authProvider);
  final user = authState.user;

  await showModalBottomSheet<void>(
    context: context,
    showDragHandle: true,
    backgroundColor: AppTheme.muted,
    useSafeArea: true,
    isScrollControlled: true,
    builder: (sheetContext) {
      return _UserSheetBody(
        username: user?.username ?? '未知用户',
        isAdmin: user?.isAdmin == true,
        serverUrl: authState.serverUrl,
        parentContext: context,
        parentRef: ref,
        sheetContext: sheetContext,
      );
    },
  );
}

class _UserSheetBody extends ConsumerStatefulWidget {
  const _UserSheetBody({
    required this.username,
    required this.isAdmin,
    required this.serverUrl,
    required this.parentContext,
    required this.parentRef,
    required this.sheetContext,
  });

  final String username;
  final bool isAdmin;
  final String? serverUrl;
  final BuildContext parentContext;
  final WidgetRef parentRef;
  final BuildContext sheetContext;

  @override
  ConsumerState<_UserSheetBody> createState() => _UserSheetBodyState();
}

class _UserSheetBodyState extends ConsumerState<_UserSheetBody> {
  final _tokenController = TextEditingController();
  bool _linked = false;
  String _suffix = '';
  bool _loadingLink = true;
  bool _binding = false;
  bool _syncing = false;
  String? _error;
  String? _message;

  BangumiMeApi get _api => BangumiMeApi(ref.read(apiClientProvider));

  @override
  void initState() {
    super.initState();
    unawaited(_loadLink());
  }

  @override
  void dispose() {
    _tokenController.dispose();
    super.dispose();
  }

  Future<void> _loadLink() async {
    setState(() {
      _loadingLink = true;
      _error = null;
    });
    try {
      final link = await _api.getLink();
      if (!mounted) {
        return;
      }
      setState(() {
        _linked = link.linked;
        _suffix = link.suffix ?? '';
        _loadingLink = false;
      });
    } catch (error) {
      if (!mounted) {
        return;
      }
      setState(() {
        _loadingLink = false;
        _error = describeApiError(error);
      });
    }
  }

  Future<void> _bind() async {
    final token = _tokenController.text.trim();
    if (token.isEmpty) {
      setState(() => _error = '请输入 Access Token');
      return;
    }
    setState(() {
      _binding = true;
      _error = null;
      _message = null;
    });
    try {
      final link = await _api.putToken(token);
      _tokenController.clear();
      if (!mounted) {
        return;
      }
      setState(() {
        _linked = link.linked;
        _suffix = link.suffix ?? '';
        _binding = false;
        _message = link.suffix == null || link.suffix!.isEmpty
            ? '已绑定'
            : '已绑定 ···${link.suffix}';
      });
    } catch (error) {
      if (!mounted) {
        return;
      }
      setState(() {
        _binding = false;
        _error = describeApiError(error);
      });
    }
  }

  Future<void> _unbind() async {
    setState(() {
      _binding = true;
      _error = null;
      _message = null;
    });
    try {
      final link = await _api.deleteToken();
      _tokenController.clear();
      if (!mounted) {
        return;
      }
      setState(() {
        _linked = link.linked;
        _suffix = '';
        _binding = false;
        _message = '已解除绑定';
      });
    } catch (error) {
      if (!mounted) {
        return;
      }
      setState(() {
        _binding = false;
        _error = describeApiError(error);
      });
    }
  }

  Future<void> _sync() async {
    setState(() {
      _syncing = true;
      _error = null;
      _message = null;
    });
    try {
      final result = await _api.sync();
      if (!mounted) {
        return;
      }
      setState(() {
        _syncing = false;
        _message = '已同步 ${result.animes} 部，标记 ${result.episodesMarked} 集';
      });
    } catch (error) {
      if (!mounted) {
        return;
      }
      setState(() {
        _syncing = false;
        _error = describeApiError(error);
      });
      await _loadLink();
    }
  }

  @override
  Widget build(BuildContext context) {
    final bottomInset = MediaQuery.viewInsetsOf(context).bottom;
    return SafeArea(
      child: SingleChildScrollView(
        padding: EdgeInsets.fromLTRB(20, 0, 20, 20 + bottomInset),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    widget.username,
                    style: const TextStyle(
                      color: AppTheme.foreground,
                      fontSize: 18,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
                if (widget.isAdmin)
                  Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 8,
                      vertical: 2,
                    ),
                    decoration: BoxDecoration(
                      color: AppTheme.accent.withValues(alpha: 0.15),
                      borderRadius: BorderRadius.circular(4),
                      border: Border.all(
                        color: AppTheme.accent.withValues(alpha: 0.4),
                      ),
                    ),
                    child: const Text(
                      '管理员',
                      style: TextStyle(
                        color: AppTheme.accent,
                        fontSize: 12,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ),
              ],
            ),
            const SizedBox(height: 8),
            if (widget.serverUrl != null && widget.serverUrl!.isNotEmpty)
              Text(
                widget.serverUrl!,
                style: TextStyle(
                  color: AppTheme.foreground.withValues(alpha: 0.6),
                  fontSize: 13,
                ),
              ),
            const SizedBox(height: 12),
            Text(
              '版本号 $appVersion',
              style: TextStyle(
                color: AppTheme.foreground.withValues(alpha: 0.6),
                fontSize: 13,
              ),
            ),
            const SizedBox(height: 16),
            const Divider(),
            const SizedBox(height: 12),
            const Text(
              'Bangumi 令牌',
              style: TextStyle(
                color: AppTheme.foreground,
                fontSize: 15,
                fontWeight: FontWeight.w600,
              ),
            ),
            const SizedBox(height: 8),
            if (_loadingLink)
              const Padding(
                padding: EdgeInsets.symmetric(vertical: 8),
                child: LinearProgressIndicator(),
              )
            else if (_linked)
              Text(
                _suffix.isEmpty ? '已绑定' : '已绑定 ···$_suffix',
                style: const TextStyle(
                  color: AppTheme.foreground,
                  fontSize: 14,
                  fontWeight: FontWeight.w600,
                ),
              )
            else
              Text(
                '未绑定',
                style: TextStyle(
                  color: AppTheme.foreground.withValues(alpha: 0.7),
                  fontSize: 14,
                ),
              ),
            const SizedBox(height: 8),
            SelectableText(
              '令牌来源 $_bangumiTokenSource',
              style: TextStyle(
                color: AppTheme.foreground.withValues(alpha: 0.6),
                fontSize: 12,
              ),
            ),
            if (!_loadingLink && !_linked) ...[
              const SizedBox(height: 12),
              TextField(
                controller: _tokenController,
                obscureText: true,
                enableSuggestions: false,
                autocorrect: false,
                maxLength: 512,
                enabled: !_binding && !_syncing,
                decoration: const InputDecoration(
                  labelText: 'Access Token',
                  hintText: '粘贴令牌，绑定后只显示末 4 位',
                  counterText: '',
                ),
                onSubmitted: (_) => unawaited(_bind()),
              ),
              const SizedBox(height: 8),
              SizedBox(
                width: double.infinity,
                child: FilledButton(
                  onPressed: _binding || _syncing ? null : () => unawaited(_bind()),
                  child: Text(_binding ? '绑定中...' : '绑定'),
                ),
              ),
            ],
            if (!_loadingLink && _linked) ...[
              const SizedBox(height: 12),
              SizedBox(
                width: double.infinity,
                child: FilledButton(
                  onPressed: _binding || _syncing ? null : () => unawaited(_sync()),
                  child: Text(_syncing ? '同步中...' : '同步进度'),
                ),
              ),
              const SizedBox(height: 8),
              SizedBox(
                width: double.infinity,
                child: OutlinedButton(
                  onPressed: _binding || _syncing
                      ? null
                      : () => unawaited(_unbind()),
                  child: Text(_binding ? '处理中...' : '解除绑定'),
                ),
              ),
            ],
            if (_error != null) ...[
              const SizedBox(height: 8),
              Text(
                _error!,
                style: const TextStyle(color: AppTheme.destructive, fontSize: 13),
              ),
            ],
            if (_message != null) ...[
              const SizedBox(height: 8),
              Text(
                _message!,
                style: const TextStyle(color: AppTheme.accent, fontSize: 13),
              ),
            ],
            const SizedBox(height: 16),
            const Divider(),
            const SizedBox(height: 12),
            SizedBox(
              width: double.infinity,
              child: OutlinedButton.icon(
                onPressed: () async {
                  Navigator.of(widget.sheetContext).pop();
                  await _checkUpdate(widget.parentContext);
                },
                icon: const Icon(Icons.system_update_outlined),
                label: Text('检查更新（当前 $appVersion）'),
              ),
            ),
            const SizedBox(height: 8),
            SizedBox(
              width: double.infinity,
              child: OutlinedButton.icon(
                onPressed: () {
                  Navigator.of(widget.sheetContext).pop();
                  unawaited(_confirmLogout(widget.parentContext, widget.parentRef));
                },
                style: OutlinedButton.styleFrom(
                  foregroundColor: AppTheme.destructive,
                  side: BorderSide(
                    color: AppTheme.destructive.withValues(alpha: 0.4),
                  ),
                ),
                icon: const Icon(Icons.logout_outlined),
                label: const Text('退出登录'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

Future<void> _checkUpdate(BuildContext context) async {
  BuildContext? dialogContext;
  showDialog<void>(
    context: context,
    // 允许点击外部/按返回键关闭加载弹窗：网络异常卡死时用户仍有退出路径。
    // 弹窗关闭后 dialogContext 不再 mounted，后续 pop 会被下方的 mounted 判断跳过。
    barrierDismissible: true,
    builder: (c) {
      dialogContext = c;
      return const AlertDialog(content: LinearProgressIndicator());
    },
  );
  try {
    final result = await checkAppUpdate(appVersion);
    if (dialogContext != null && dialogContext!.mounted) {
      Navigator.of(dialogContext!).pop();
    }
    if (!context.mounted) return;
    if (result.hasUpdate) {
      await showUpdateDialog(context, result);
    } else {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('已是最新版本')),
      );
    }
  } catch (e) {
    if (dialogContext != null && dialogContext!.mounted) {
      Navigator.of(dialogContext!).pop();
    }
    if (!context.mounted) return;
    final msg = e is DioException ? '无法连接更新服务器' : '检查更新失败: $e';
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(msg)));
  }
}

Future<void> _confirmLogout(BuildContext context, WidgetRef ref) async {
  final confirmed = await showDialog<bool>(
    context: context,
    builder: (context) {
      return AlertDialog(
        title: const Text('退出登录'),
        content: const Text('退出后需要重新登录才能继续浏览番剧。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('退出'),
          ),
        ],
      );
    },
  );
  if (confirmed == true) {
    await ref.read(authProvider.notifier).logout();
  }
}
