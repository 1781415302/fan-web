import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers/auth_provider.dart';
import '../services/app_updater.dart';
import '../theme/app_theme.dart';
import 'update_dialog.dart';

/// App 版本号，与 pubspec.yaml 的 version 保持同步。
const appVersion = '1.2.6';

/// 显示用户底部菜单：用户名、管理员徽章、服务器地址、版本号、退出登录。
Future<void> showUserSheet(BuildContext context, WidgetRef ref) async {
  final authState = ref.read(authProvider);
  final user = authState.user;

  await showModalBottomSheet<void>(
    context: context,
    showDragHandle: true,
    backgroundColor: AppTheme.muted,
    useSafeArea: true,
    builder: (sheetContext) {
      return SafeArea(
        child: Padding(
          padding: const EdgeInsets.fromLTRB(20, 0, 20, 20),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Expanded(
                    child: Text(
                      user?.username ?? '未知用户',
                      style: const TextStyle(
                        color: AppTheme.foreground,
                        fontSize: 18,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                  if (user?.isAdmin == true)
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
              if (authState.serverUrl != null &&
                  authState.serverUrl!.isNotEmpty)
                Text(
                  authState.serverUrl!,
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
              SizedBox(
                width: double.infinity,
                child: OutlinedButton.icon(
                  onPressed: () async {
                    Navigator.of(sheetContext).pop();
                    await _checkUpdate(context);
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
                    Navigator.of(sheetContext).pop();
                    unawaited(_confirmLogout(context, ref));
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
    },
  );
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
