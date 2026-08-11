import 'dart:async';
import 'dart:convert';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../api/progress_api.dart';
import '../providers/anime_provider.dart';
import '../providers/auth_provider.dart';

/// 一条待同步的进度记录。
class PendingProgress {
  const PendingProgress({
    required this.serverUrl,
    required this.userId,
    required this.episodeId,
    required this.position,
    required this.watched,
    required this.updatedAt,
  });

  final String serverUrl;
  final int userId;
  final int episodeId;
  final int position;
  final bool watched;
  final String updatedAt;

  Map<String, dynamic> toJson() => {
    'server_url': serverUrl,
    'user_id': userId,
    'episode_id': episodeId,
    'position': position,
    'watched': watched,
    'updated_at': updatedAt,
  };

  factory PendingProgress.fromJson(Map<String, dynamic> json) {
    return PendingProgress(
      serverUrl: json['server_url'] as String? ?? '',
      userId: (json['user_id'] as num?)?.toInt() ?? 0,
      episodeId: (json['episode_id'] as num?)?.toInt() ?? 0,
      position: (json['position'] as num?)?.toInt() ?? 0,
      watched: json['watched'] == true,
      updatedAt: json['updated_at'] as String? ?? '',
    );
  }

  /// 身份键：服务器地址 + 用户 ID + 集数 ID
  String get identityKey => '$serverUrl|$userId|$episodeId';
}

final progressOutboxProvider = Provider<ProgressOutbox>((ref) {
  return ProgressOutbox(ref);
});

/// 维护本地待同步进度记录，按服务器和用户隔离。
/// 同一服务器、同一用户、同一集只保留最新一条。
class ProgressOutbox {
  ProgressOutbox(this._ref);

  final Ref _ref;
  static const _storageKey = 'progress_outbox_v1';
  Future<void> _storageChain = Future<void>.value();
  Future<void> _syncChain = Future<void>.value();

  SharedPreferences get _prefs => _ref.read(sharedPreferencesProvider);
  ProgressApi get _progressApi => _ref.read(progressApiProvider);

  /// 写入一条待同步记录。如果同一身份键已存在，取 watched 逻辑或，
  /// position 取更新时间更新的那条。
  Future<void> save(PendingProgress record) => _enqueueStorage(() async {
    final all = _loadAll();
    final existing = all[record.identityKey];
    final latest =
        existing == null ||
            _isAtLeastAsNew(record.updatedAt, existing.updatedAt)
        ? record
        : existing;
    final merged = existing == null
        ? record
        : PendingProgress(
            serverUrl: latest.serverUrl,
            userId: latest.userId,
            episodeId: latest.episodeId,
            position: latest.position,
            watched: record.watched || existing.watched,
            updatedAt: latest.updatedAt,
          );
    all[record.identityKey] = merged;
    await _saveAll(all);
  });

  /// 删除指定记录（仅当内容完全匹配时）。
  Future<void> removeIfMatched(PendingProgress record) =>
      _enqueueStorage(() async {
        final all = _loadAll();
        final existing = all[record.identityKey];
        if (existing == null) return;
        if (existing.position == record.position &&
            existing.watched == record.watched &&
            existing.updatedAt == record.updatedAt) {
          all.remove(record.identityKey);
          await _saveAll(all);
        }
      });

  /// 获取指定用户和服务器的所有待同步记录。
  Future<List<PendingProgress>> getPending(String serverUrl, int userId) =>
      _enqueueStorage(() async {
        final all = _loadAll();
        return all.values
            .where((r) => r.serverUrl == serverUrl && r.userId == userId)
            .toList()
          ..sort((a, b) => a.updatedAt.compareTo(b.updatedAt));
      });

  /// 清除指定用户和服务器的所有待同步记录。
  Future<void> clearForUser(int userId, String? serverUrl) =>
      _enqueueStorage(() async {
        final all = _loadAll();
        all.removeWhere(
          (key, record) =>
              record.userId == userId &&
              (serverUrl == null || record.serverUrl == serverUrl),
        );
        await _saveAll(all);
      });

  /// 尝试同步所有待同步记录。
  Future<void> syncAll(String serverUrl, int userId, String token) =>
      _enqueueSync(() async {
        final pending = await getPending(serverUrl, userId);
        for (final record in pending) {
          try {
            await _progressApi.reportProgress(
              record.episodeId,
              record.position,
              record.watched,
            );
            await removeIfMatched(record);
          } catch (_) {
            // 单条记录发送失败保留并继续下一条，避免头阻塞导致
            // 其余待同步记录永远无法上报（如该集已被服务端删除）。
          }
        }
      });

  Future<T> _enqueueStorage<T>(Future<T> Function() operation) {
    final result = _storageChain.then((_) => operation());
    _storageChain = result.then<void>(
      (_) {},
      onError: (Object _, StackTrace _) {},
    );
    return result;
  }

  Future<T> _enqueueSync<T>(Future<T> Function() operation) {
    final result = _syncChain.then((_) => operation());
    _syncChain = result.then<void>(
      (_) {},
      onError: (Object _, StackTrace _) {},
    );
    return result;
  }

  bool _isAtLeastAsNew(String candidate, String current) {
    final candidateTime = DateTime.tryParse(candidate);
    final currentTime = DateTime.tryParse(current);
    if (candidateTime != null && currentTime != null) {
      return !candidateTime.isBefore(currentTime);
    }
    return candidate.compareTo(current) >= 0;
  }

  Map<String, PendingProgress> _loadAll() {
    try {
      final json = _prefs.getString(_storageKey);
      if (json == null || json.isEmpty) return {};
      final decoded = jsonDecode(json) as Map<String, dynamic>;
      return decoded.map((key, value) {
        final record = PendingProgress.fromJson(value as Map<String, dynamic>);
        return MapEntry(record.identityKey, record);
      });
    } catch (_) {
      return {};
    }
  }

  Future<void> _saveAll(Map<String, PendingProgress> all) async {
    try {
      final encoded = jsonEncode(
        all.map((key, value) => MapEntry(key, value.toJson())),
      );
      await _prefs.setString(_storageKey, encoded);
    } catch (_) {}
  }
}
