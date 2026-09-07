import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:path_provider/path_provider.dart';

import '../models/anime.dart';
import '../models/download.dart';
import '../services/episode_downloader.dart';

/// 下载根目录（未来可扩展为用户可选目录）。FutureProvider 包装 path_provider，
/// 测试用 overrideWith 指向临时目录——这是唯一的目录注入点。
final downloadsRootProvider = FutureProvider<String>((ref) async {
  return (await getApplicationDocumentsDirectory()).path;
});

/// app-scope（非 autoDispose）：页面关闭后下载继续。
final downloadProvider =
    NotifierProvider<DownloadNotifier, Map<DownloadKey, DownloadTaskState>>(
      DownloadNotifier.new,
    );

class DownloadNotifier extends Notifier<Map<DownloadKey, DownloadTaskState>> {
  late final EpisodeDownloader _downloader;
  bool _disposed = false;

  @override
  Map<DownloadKey, DownloadTaskState> build() {
    _downloader = EpisodeDownloader(ref, onStateChanged: _onEngineState);
    ref.onDispose(() {
      _disposed = true;
      _downloader.dispose();
    });
    // build 同步返回空 map，随后 fire-and-forget 恢复索引并合并 completed。
    // 冷启动到索引加载完成的极短窗口内，localFilePath 会瞬时 miss 回落
    // 网络流（离线表现为播放失败提示，重试即可）——已接受。
    Future.microtask(() async {
      try {
        await _downloader.restoreIndex();
      } catch (_) {
        // 索引恢复失败不阻塞启动，视为无已完成记录。
        return;
      }
      if (_disposed) return;
      final records = _downloader.completedRecords;
      if (records.isEmpty) return;
      final next = Map<DownloadKey, DownloadTaskState>.of(state);
      for (final record in records) {
        next[DownloadKey(record.serverUrl, record.episodeId)] =
            DownloadTaskState(
              status: DownloadStatus.completed,
              receivedBytes: record.fileSize,
              totalBytes: record.fileSize,
            );
      }
      _setState(next);
    });
    return const {};
  }

  Future<void> enqueue({
    required String serverUrl,
    required Episode episode,
    required String animeTitle,
  }) {
    if (serverUrl.isEmpty) {
      return Future.value();
    }
    final key = DownloadKey(serverUrl, episode.id);
    final existing = state[key];
    // 幂等：queued/downloading/completed 中 no-op；failed/idle 可重入。
    if (existing != null &&
        (existing.status == DownloadStatus.queued ||
            existing.status == DownloadStatus.downloading ||
            existing.status == DownloadStatus.completed)) {
      return Future.value();
    }
    _setState({
      ...state,
      key: const DownloadTaskState(status: DownloadStatus.queued),
    });
    return _downloader.enqueue(
      serverUrl: serverUrl,
      episode: episode,
      animeTitle: animeTitle,
    );
  }

  Future<void> cancel(String serverUrl, int episodeId) {
    return _downloader.cancel(serverUrl, episodeId);
  }

  Future<void> delete(String serverUrl, int episodeId) {
    return _downloader.delete(serverUrl, episodeId);
  }

  String? localFilePath(String serverUrl, int episodeId) {
    return _downloader.localFilePath(serverUrl, episodeId);
  }

  // 引擎经回调推状态更新；回调写 state 前必须判 disposed
  // （仿 player_provider 的 _setState 守卫）。
  void _onEngineState(DownloadKey key, DownloadTaskState next) {
    _setState({...state, key: next});
  }

  void _setState(Map<DownloadKey, DownloadTaskState> next) {
    if (!_disposed) {
      state = next;
    }
  }
}
