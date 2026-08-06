import 'package:flutter/material.dart';

import '../theme/app_theme.dart';

/// 统一的错误状态视图，带可选的重试按钮。
class ErrorStateView extends StatelessWidget {
  const ErrorStateView({
    required this.message,
    this.onRetry,
    this.icon = Icons.cloud_off_outlined,
    super.key,
  });

  final String message;
  final VoidCallback? onRetry;
  final IconData icon;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              icon,
              color: AppTheme.foreground.withValues(alpha: 0.65),
              size: 40,
            ),
            const SizedBox(height: 12),
            Text(
              message,
              textAlign: TextAlign.center,
              style: TextStyle(
                color: AppTheme.foreground.withValues(alpha: 0.78),
                height: 1.5,
              ),
            ),
            if (onRetry != null) ...[
              const SizedBox(height: 16),
              FilledButton.icon(
                onPressed: onRetry,
                icon: const Icon(Icons.refresh),
                label: const Text('重试'),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

/// 统一的空状态视图。
class EmptyStateView extends StatelessWidget {
  const EmptyStateView({
    required this.message,
    this.icon = Icons.inbox_outlined,
    super.key,
  });

  final String message;
  final IconData icon;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              icon,
              color: AppTheme.foreground.withValues(alpha: 0.65),
              size: 40,
            ),
            const SizedBox(height: 12),
            Text(
              message,
              textAlign: TextAlign.center,
              style: TextStyle(
                color: AppTheme.foreground.withValues(alpha: 0.78),
                height: 1.5,
              ),
            ),
          ],
        ),
      ),
    );
  }
}
