import 'package:flutter/material.dart';

import '../models/anime.dart';
import '../models/download.dart';
import '../theme/app_theme.dart';

enum EpisodeStatus { watched, inProgress, unwatched }

class EpisodeTile extends StatelessWidget {
  const EpisodeTile({
    required this.episode,
    required this.progress,
    required this.onTap,
    this.download,
    this.onDownloadTap,
    super.key,
  });

  final Episode episode;
  final EpisodeProgress? progress;
  final VoidCallback onTap;
  final DownloadTaskState? download;
  final VoidCallback? onDownloadTap;

  static EpisodeStatus statusOf(EpisodeProgress? progress) {
    if (progress == null) {
      return EpisodeStatus.unwatched;
    }
    if (progress.watched) {
      return EpisodeStatus.watched;
    }
    if (progress.position > 0) {
      return EpisodeStatus.inProgress;
    }
    return EpisodeStatus.unwatched;
  }

  @override
  Widget build(BuildContext context) {
    final status = statusOf(progress);
    final statusLabel = _statusLabel(status);
    final episodeLabel = '第${episode.epNumber}话';
    final detail = episode.title.trim();
    final semanticLabel = detail.isEmpty
        ? '$episodeLabel，$statusLabel'
        : '$episodeLabel，$detail，$statusLabel';

    final base = Semantics(
      button: true,
      label: semanticLabel,
      child: Material(
        color: AppTheme.muted,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(12),
          side: BorderSide(color: _statusColor(status)),
        ),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 9),
            child: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  episodeLabel,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                    color: AppTheme.foreground,
                    fontSize: 14,
                    fontWeight: FontWeight.w600,
                  ),
                ),
                const SizedBox(height: 6),
                Row(
                  children: [
                    Icon(
                      _statusIcon(status),
                      color: _statusColor(status),
                      size: 16,
                    ),
                    const SizedBox(width: 5),
                    Expanded(
                      child: Text(
                        statusLabel,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(
                          color: _statusColor(status),
                          fontSize: 12,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                    ),
                  ],
                ),
              ],
            ),
          ),
        ),
      ),
    );

    final task = download;
    if (task == null) {
      return base;
    }
    return Stack(
      children: [
        base,
        Positioned(
          top: 0,
          right: 0,
          child: Semantics(
            button: true,
            label: _downloadLabel(task.status),
            child: GestureDetector(
              behavior: HitTestBehavior.opaque,
              onTap: onDownloadTap,
              child: SizedBox(
                width: 48,
                height: 48,
                child: Center(
                  child: ExcludeSemantics(child: _downloadVisual(task)),
                ),
              ),
            ),
          ),
        ),
      ],
    );
  }

  String _downloadLabel(DownloadStatus status) {
    final ep = '第${episode.epNumber}话';
    return switch (status) {
      DownloadStatus.completed => '删除$ep下载',
      DownloadStatus.downloading || DownloadStatus.queued => '取消$ep下载',
      DownloadStatus.idle || DownloadStatus.failed => '下载$ep',
    };
  }

  Widget _downloadVisual(DownloadTaskState task) {
    return switch (task.status) {
      DownloadStatus.downloading => SizedBox(
        width: 24,
        height: 24,
        child: CircularProgressIndicator(
          strokeWidth: 2.5,
          value: task.totalBytes > 0
              ? (task.receivedBytes / task.totalBytes).clamp(0.0, 1.0)
              : null,
        ),
      ),
      DownloadStatus.completed => const Icon(
        Icons.download_done,
        size: 24,
        color: AppTheme.accent,
      ),
      DownloadStatus.failed => const Icon(
        Icons.error_outline,
        size: 24,
        color: AppTheme.destructive,
      ),
      DownloadStatus.queued => const Icon(Icons.schedule, size: 24),
      DownloadStatus.idle => const Icon(Icons.download, size: 24),
    };
  }

  static String _statusLabel(EpisodeStatus status) {
    return switch (status) {
      EpisodeStatus.watched => '已看',
      EpisodeStatus.inProgress => '进行中',
      EpisodeStatus.unwatched => '未看',
    };
  }

  static IconData _statusIcon(EpisodeStatus status) {
    return switch (status) {
      EpisodeStatus.watched => Icons.check_circle_outline,
      EpisodeStatus.inProgress => Icons.play_circle_outline,
      EpisodeStatus.unwatched => Icons.radio_button_unchecked,
    };
  }

  static Color _statusColor(EpisodeStatus status) {
    return switch (status) {
      EpisodeStatus.watched => AppTheme.accent,
      EpisodeStatus.inProgress => AppTheme.warning,
      EpisodeStatus.unwatched => AppTheme.foreground.withValues(alpha: 0.62),
    };
  }
}
